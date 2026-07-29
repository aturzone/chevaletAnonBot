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
	"path/filepath"
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

// startBackground launches the background goroutines as TRACKED work (so Run can
// drain them on shutdown); they stop when ctx is done.
func (b *Bot) startBackground(ctx context.Context) {
	b.setCommands()
	if b.Cfg.AIEnabled {
		b.goBG(func() { b.aiResponderLoop(ctx) })
	}
	b.goBG(func() { b.checkConnectionLoop(ctx) })
	b.goBG(func() { b.userStoreSweepLoop(ctx) })
	if b.Cfg.SendGMGN {
		b.goBG(func() { b.gmgnLoop(ctx, b.Cfg.GMTime, true) })  // morning
		b.goBG(func() { b.gmgnLoop(ctx, b.Cfg.GNTime, false) }) // night
	}
	// Nightly DB backup to one admin's DM — off unless BACKUP_ADMIN_ID is set.
	if b.Cfg.BackupAdminID != 0 {
		b.goBG(func() { b.backupLoop(ctx, b.Cfg.BackupTime, b.Cfg.BackupAdminID) })
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

// userStoreSweepLoop periodically evicts long-idle per-user state so the
// in-memory userStore (and the rate-limit timestamps it holds) can't grow
// without bound under a large or hostile user population.
func (b *Bot) userStoreSweepLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := b.users.sweep(2 * time.Hour); n > 0 {
				slog.Debug("evicted idle user states", "count", n)
			}
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

// latestBackupFile returns the name of the most-recently-MODIFIED file in dir
// (newest by mtime — robust to mixed file-name prefixes, unlike a plain name
// sort), or "" when dir holds no eligible file. Shared by /admin backup and the
// nightly delivery so both always pick the truly newest dump.
//
// minAge skips any file younger than it: the hourly backup.sh gzips straight
// into the final filename (no temp+rename), so a file written seconds ago may be
// a dump still in progress — sending it would ship a truncated, un-gunzippable
// archive that Telegram accepts and we'd log as "sent". The scheduled path passes
// a few minutes (an older hourly dump always exists); the manual /admin backup
// passes 0 to keep its exact "newest file, right now" semantics.
func latestBackupFile(dir string, minAge time.Duration) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var newest string
	var newestT time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime()
		if minAge > 0 && time.Since(mt) < minAge {
			continue // too fresh — may be a dump still being written
		}
		// Newest by mtime; on an mtime tie, the lexicographically larger name
		// wins (matches the old name-sort's "last wins", stable after a restore
		// that flattens mtimes).
		if newest == "" || mt.After(newestT) || (mt.Equal(newestT) && e.Name() > newest) {
			newest, newestT = e.Name(), mt
		}
	}
	return newest, nil
}

// backupLoop delivers the newest DB backup to the configured admin's DM once a
// day at hm (Asia/Tehran), mirroring gmgnLoop's daily-scheduler shape. It is
// launched only when a valid BackupAdminID is configured (see startBackground),
// so it can never run — or reach any chat other than that one id — by default.
func (b *Bot) backupLoop(ctx context.Context, hm [2]int, adminID int64) {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		slog.Error("backup: cannot load Asia/Tehran", "err", err)
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
			b.sendLatestBackupTo(adminID)
		}
	}
}

// backupMinAge is how long a backup file must have existed before the nightly
// job will send it, so it can never grab an hourly dump mid-write (see
// latestBackupFile). An older hourly backup always exists, so this never starves.
const backupMinAge = 3 * time.Minute

// nightlySentMarker is (re)written to the backups dir after a successful
// nightly send. deploy/go/nightly-backup-backstop.sh — a host-side cron that
// re-sends the backup if this job silently failed — checks this file's mtime
// instead of grepping `docker logs`: `docker logs --since <duration>` proved
// unreliable in production (it returned zero lines regardless of the window,
// even moments after this job logged successfully), which made the backstop
// fire every night and double-send. A plain mtime check has no such failure
// mode and needs no log driver cooperation.
const nightlySentMarker = ".nightly-backup-sent"

// touchNightlySentMarker (re)creates the marker file in dir, refreshing its
// mtime to now. Best-effort: a write failure is the caller's to log, not fatal
// (the Telegram send already succeeded either way).
func touchNightlySentMarker(dir string) error {
	return os.WriteFile(filepath.Join(dir, nightlySentMarker), nil, 0o600)
}

// sendLatestBackupTo sends the newest settled backups/ file to a SINGLE chat id
// (the nightly admin DM). Best-effort — every failure is logged, never fatal —
// and it targets exactly one chat: it never iterates users / never broadcasts.
// On any failure it also pings that same chat with a short text so a silent
// stall (e.g. the dump one day exceeds Telegram's 50MB bot limit) is noticed.
func (b *Bot) sendLatestBackupTo(chatID int64) {
	latest, err := latestBackupFile("backups", backupMinAge)
	if err != nil || latest == "" {
		slog.Warn("nightly backup: no file to send", "err", err)
		b.notifyBackupFail(chatID, "فایلِ بکاپی پیدا نشد")
		return
	}
	f, err := os.Open(filepath.Join("backups", latest))
	if err != nil {
		slog.Warn("nightly backup: open failed", "file", latest, "err", err)
		b.notifyBackupFail(chatID, "بازکردنِ فایلِ بکاپ نشد")
		return
	}
	defer f.Close()
	// A DB dump can outgrow the 10s default request timeout as the data grows;
	// give the upload its own generous budget so it scales with the file.
	if _, err := b.TG.SendDocument(chatID, gotgbot.InputFileByReader(latest, f),
		&gotgbot.SendDocumentOpts{
			Caption:     "🌙 بکاپِ خودکارِ شبانه‌ی دیتابیس — " + latest,
			RequestOpts: &gotgbot.RequestOpts{Timeout: 120 * time.Second},
		}); err != nil {
		slog.Warn("nightly backup: send failed", "to", chatID, "err", err)
		b.notifyBackupFail(chatID, "ارسالِ فایل ناموفق بود (شاید حجمش از حدِ تلگرام بیشتر شده)")
		return
	}
	slog.Info("nightly backup sent", "to", chatID, "file", latest)
	if err := touchNightlySentMarker("backups"); err != nil {
		slog.Warn("nightly backup: marker write failed", "err", err)
	}
}

// notifyBackupFail best-effort tells the admin chat the nightly backup didn't go
// through, so absence-of-file is never mistaken for "nothing to back up".
func (b *Bot) notifyBackupFail(chatID int64, reason string) {
	_, _ = b.TG.SendMessage(chatID, "⚠️ بکاپِ خودکارِ شبانه ارسال نشد: "+reason, nil)
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

// scheduleMassMsg ports the admin send-mass-msg trigger (run_once 7s later). The
// wait is a TRACKED, ctx-aware background goroutine (replaces a bare
// time.AfterFunc) so a shutdown during the 7s window cancels it cleanly.
func (b *Bot) scheduleMassMsg(ctx *ext.Context) {
	msg := ctx.EffectiveMessage
	b.goBG(func() {
		select {
		case <-time.After(7 * time.Second):
			b.sendMassMsg(msg)
		case <-b.runCtx.Done():
		}
	})
}

// sendMassMsg ports jobs.send_mass_msg: copy the replied message to every user,
// logging failures to a file that is then sent back to the admin. It derives its
// context from the run context and stops cleanly mid-broadcast on shutdown (so it
// never touches a closed DB pool); a truncated broadcast is logged.
func (b *Bot) sendMassMsg(msg *gotgbot.Message) {
	if msg.ReplyToMessage == nil {
		return
	}
	rctx := b.runCtx
	_, _ = msg.Reply(b.TG, "starting to send mass msg...", nil)

	dbctx, cancel := context.WithTimeout(rctx, dbOpTimeout)
	uids, err := b.DB.GetAllUIDs(dbctx)
	cancel()
	if err != nil {
		slog.Warn("send_mass_msg failed", "err", err)
		return
	}

	var failures []string
	for i, uid := range uids {
		select {
		case <-rctx.Done():
			slog.Warn("mass msg interrupted by shutdown", "sent", i, "total", len(uids))
			return
		default:
		}
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
		if werr := os.WriteFile(logfile, []byte(strings.Join(failures, "\n")), 0o600); werr == nil {
			if f, oerr := os.Open(logfile); oerr == nil {
				_, _ = msg.ReplyDocument(b.TG, gotgbot.InputFileByReader(logfile, f), nil)
				f.Close()
			}
			_ = os.Remove(logfile)
		}
	}

	_, _ = msg.Reply(b.TG, "sent the message to everyone.", nil)
}
