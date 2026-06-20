package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// This file ports the background jobs from modules/Global/jobs.py: set_commands,
// the GM/GN daily greetings, the AI responder loop, and the hourly DB check.
// gotgbot has no JobQueue, so each is a goroutine driven by the run context.

// aiHTTP is the client used to reach the AI endpoint (NOT via the Telegram
// proxy — Python used requests directly).
var aiHTTP = &http.Client{Timeout: 60 * time.Second}

// startBackground launches the background goroutines; they stop when ctx is done.
func (b *Bot) startBackground(ctx context.Context) {
	b.setCommands()
	go b.aiResponderLoop(ctx)
	go b.checkConnectionLoop(ctx)
	if b.Cfg.SendGMGN {
		go b.gmgnLoop(ctx, b.Cfg.GMTime, true)  // morning
		go b.gmgnLoop(ctx, b.Cfg.GNTime, false) // night
	}
}

// setCommands ports jobs.set_commands: registers the bot's command menu. (The
// list matches the job's, which includes /donate — it superseded the shorter
// list main.py set at startup.)
func (b *Bot) setCommands() {
	_, err := b.TG.SetMyCommands([]gotgbot.BotCommand{
		{Command: "donate", Description: "کمک مالی 🙏"},
		{Command: "help", Description: "🆘 کمک!"},
		{Command: "my_links", Description: "🔗 لینک های من"},
		{Command: "settings", Description: "⚙️ تنظیمات و قابلیت ها"},
		{Command: "cancel", Description: "❌ کنسل کردن هرکاری که داری انجام میدی"},
	}, nil)
	if err != nil {
		slog.Warn("set_commands failed", "err", err)
		return
	}
	slog.Info("successfully set the commands")
}

// checkConnectionLoop ports jobs.check_connection. Python registered it with
// run_repeating(check_connection, interval=3600, first=5): an initial probe ~5s
// after startup (early detection of a bad pool) and hourly thereafter. pgxpool
// reconnects on its own, so this is just a probe + log, replacing Python's
// manual reconnect dance.
func (b *Bot) checkConnectionLoop(ctx context.Context) {
	check := func() {
		c, cancel := context.WithTimeout(ctx, dbOpTimeout)
		defer cancel()
		if _, err := b.DB.UserCount(c); err != nil {
			slog.Error("error while checking connection", "err", err)
		}
	}

	// initial probe ~5s after startup (Python's `first=5`).
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
		check()
	}

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// gmgnLoop ports jobs.send_gm_gn scheduled via run_daily at GM_TIME / GN_TIME in
// Asia/Tehran: it sends the greeting to the GM group topic each day at hm.
func (b *Bot) gmgnLoop(ctx context.Context, hm [2]int, isMorning bool) {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		slog.Error("gmgn: cannot load Asia/Tehran", "err", err)
		loc = time.UTC
	}
	for {
		now := time.Now().In(loc)
		next := time.Date(now.Year(), now.Month(), now.Day(), hm[0], hm[1], 0, 0, loc)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			b.sendGMGN(isMorning)
		}
	}
}

func (b *Bot) sendGMGN(isMorning bool) {
	gid, ok := b.gmGroupID()
	if !ok {
		return
	}
	text := "شب بخیر"
	if isMorning {
		text = "صبح بخیر"
	}
	opts := &gotgbot.SendMessageOpts{ParseMode: "HTML"}
	if tid, err := strconv.ParseInt(b.Cfg.GMGroupTopic, 10, 64); err == nil {
		opts.MessageThreadId = tid
	}
	if _, err := b.TG.SendMessage(gid, text, opts); err != nil {
		slog.Warn("send_gm_gn failed", "err", err)
	}
}

// aiResponderLoop ports jobs.ai_responser: it drains the GM-group AI queue one
// message at a time, asking the configured AI endpoint and replying in the GM
// group. First run after 3s; then AI_INTERVAL between answers, 5s when idle.
func (b *Bot) aiResponderLoop(ctx context.Context) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(b.aiResponderOnce(ctx))
	}
}

// aiResponderOnce processes a single queued message and returns the delay until
// the next run.
func (b *Bot) aiResponderOnce(ctx context.Context) time.Duration {
	msg, ok := b.aiQueue.popFront()
	if !ok {
		return 5 * time.Second
	}
	interval := time.Duration(b.Cfg.AIInterval) * time.Second

	gid, gok := b.gmGroupID()
	resultText, genErr := b.aiGenerate(ctx, msg.Text)
	if genErr != nil {
		slog.Error("failed to generate ai response", "err", genErr)
		if b.Cfg.ErrorChatID != "" {
			if eid, e := strconv.ParseInt(b.Cfg.ErrorChatID, 10, 64); e == nil {
				_, _ = b.TG.SendMessage(eid, fmt.Sprintf(
					"bot failed to generate response due to: %v\ninput message: %s", genErr, msg.Text), nil)
			}
		}
		return interval
	}

	if resultText != "" && gok {
		opts := &gotgbot.SendMessageOpts{
			ParseMode:       "Markdown",
			ReplyParameters: &gotgbot.ReplyParameters{MessageId: msg.MessageId, ChatId: gid},
		}
		if _, err := b.TG.SendMessage(gid, resultText, opts); err != nil {
			// Retry without Markdown when the entities can't be parsed.
			if descContains(err, "can't parse entities") {
				_, _ = b.TG.SendMessage(gid, resultText, &gotgbot.SendMessageOpts{
					ReplyParameters: &gotgbot.ReplyParameters{MessageId: msg.MessageId, ChatId: gid},
				})
			} else {
				slog.Error("failed to send ai response", "err", err)
			}
		}
	}
	return interval
}

// aiGenerate POSTs {sessionId, chatInput} to the configured AI URL and returns
// the cleaned output (formatting/Cf characters stripped, like the Python
// unicodedata.category == "Cf" filter).
func (b *Bot) aiGenerate(ctx context.Context, input string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"sessionId": b.Dyn.AISessionID(),
		"chatInput": input,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.Dyn.AIURL(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := aiHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		// FIX: the Python original `return`ed here, which (being a run_once job)
		// permanently stopped the AI worker on a single empty response. We log and
		// carry on instead, so one blank reply can't kill the loop.
		slog.Warn("ai: no output")
		return "", nil
	}
	var parsed struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	return stripFormatChars(parsed.Output), nil
}

// stripFormatChars removes Unicode "Cf" (format) characters such as ZWNJ,
// mirroring the Python `unicodedata.category(char) == "Cf"` filter.
func stripFormatChars(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// scheduleMassMsg ports the admin send-mass-msg trigger (run_once 7s later).
func (b *Bot) scheduleMassMsg(ctx *ext.Context) {
	msg := ctx.EffectiveMessage
	time.AfterFunc(7*time.Second, func() { b.sendMassMsg(msg) })
}

// sendMassMsg ports jobs.send_mass_msg: copy the replied message to every user,
// logging failures to a file that is then sent back to the admin. No rate
// limiting, matching the Python original.
func (b *Bot) sendMassMsg(msg *gotgbot.Message) {
	if msg.ReplyToMessage == nil {
		return
	}
	_, _ = msg.Reply(b.TG, "starting to send mass msg...", nil)

	dbctx, cancel := context.WithTimeout(context.Background(), dbOpTimeout)
	uids, err := b.DB.GetAllUIDs(dbctx)
	cancel()
	if err != nil {
		slog.Warn("send_mass_msg failed", "err", err)
		return
	}

	var failures []string
	for _, uid := range uids {
		tid, perr := strconv.ParseInt(uid, 10, 64)
		if perr != nil {
			failures = append(failures, fmt.Sprintf("%s | %v", uid, perr))
			continue
		}
		if _, e := msg.ReplyToMessage.Copy(b.TG, tid, nil); e != nil {
			failures = append(failures, fmt.Sprintf("%s | %v", uid, e))
		}
	}

	if len(failures) > 0 {
		const logfile = "mass-msg-failurs.txt" // (sic) matches the Python filename
		if werr := os.WriteFile(logfile, []byte(strings.Join(failures, "\n")), 0o644); werr == nil {
			if f, oerr := os.Open(logfile); oerr == nil {
				_, _ = msg.ReplyDocument(b.TG, gotgbot.InputFileByReader(logfile, f), nil)
				f.Close()
			}
			_ = os.Remove(logfile)
		}
	}

	_, _ = msg.Reply(b.TG, "sent the message to everyone.", nil)
}
