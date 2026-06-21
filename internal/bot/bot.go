// Package bot wires the Telegram side of ChevaletAnonBot on top of gotgbot:
// the Bot client, the long-polling Updater, the Dispatcher, the central error
// handler, and the prep middleware that mirrors the Python @prep_function.
package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/aturzone/chevaletAnonBot/internal/config"
	"github.com/aturzone/chevaletAnonBot/internal/db"
	"github.com/aturzone/chevaletAnonBot/internal/dynset"
	"github.com/aturzone/chevaletAnonBot/internal/encoder"
	"github.com/aturzone/chevaletAnonBot/internal/texts"
)

// dbOpTimeout bounds a single update's database work.
const dbOpTimeout = 30 * time.Second

// Bot bundles everything a handler needs.
type Bot struct {
	TG         *gotgbot.Bot
	Dispatcher *ext.Dispatcher
	Updater    *ext.Updater

	DB    *db.DB
	Cfg   *config.Config
	Texts *texts.Loader
	Dyn   *dynset.Settings

	users      *userStore
	aiQueue    *aiQueue
	admins     map[string]bool
	errReports *errReportStore
}

// New builds the Telegram bot, dispatcher and updater, and registers handlers.
func New(cfg *config.Config, database *db.DB, txt *texts.Loader) (*Bot, error) {
	botOpts := &gotgbot.BotOpts{
		RequestOpts: &gotgbot.RequestOpts{Timeout: 10 * time.Second},
	}
	if cfg.Proxy != "" {
		u, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("bot: invalid PROXY %q: %w", cfg.Proxy, err)
		}
		botOpts.BotClient = &gotgbot.BaseBotClient{
			Client: http.Client{
				Transport: &http.Transport{Proxy: http.ProxyURL(u)},
			},
			DefaultRequestOpts: &gotgbot.RequestOpts{Timeout: 30 * time.Second},
		}
	}

	tg, err := gotgbot.NewBot(cfg.BotToken, botOpts)
	if err != nil {
		return nil, fmt.Errorf("bot: new bot: %w", err)
	}

	b := &Bot{
		TG:         tg,
		DB:         database,
		Cfg:        cfg,
		Texts:      txt,
		Dyn:        dynset.New("dynamic_settings.json", cfg.AIURL, cfg.AISessionID),
		users:      newUserStore(),
		aiQueue:    newAIQueue(),
		admins:     make(map[string]bool, len(cfg.Admins)),
		errReports: newErrReportStore(50),
	}
	for _, a := range cfg.Admins {
		b.admins[a] = true
	}

	b.Dispatcher = ext.NewDispatcher(&ext.DispatcherOpts{
		Error: b.onError,
		Panic: b.onPanic,
	})
	b.Updater = ext.NewUpdater(b.Dispatcher, &ext.UpdaterOpts{})

	b.registerHandlers()
	return b, nil
}

// Run starts long polling and blocks until ctx is cancelled, then stops cleanly.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.Updater.StartPolling(b.TG, &ext.PollingOpts{
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			// Set AllowedUpdates EXPLICITLY. Telegram remembers the last
			// allowed_updates passed to getUpdates and reuses it whenever the
			// parameter is omitted; if anything ever called getUpdates with a list
			// that excluded callback_query, polling without this would silently stop
			// delivering button presses. Listing exactly what the bot handles makes
			// it deterministic (and immune to that gotcha).
			AllowedUpdates: []string{"message", "edited_message", "callback_query"},
			Timeout:        9, // long-poll seconds
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: 11 * time.Second, // slightly above the long-poll timeout
			},
		},
	}); err != nil {
		return fmt.Errorf("bot: start polling: %w", err)
	}

	slog.Info("bot polling", "username", b.TG.User.Username)

	// Start the background jobs (set_commands, AI responder, GM/GN greetings,
	// hourly DB check); they stop when ctx is cancelled.
	b.startBackground(ctx)

	<-ctx.Done()
	slog.Info("stopping updater")
	return b.Updater.Stop()
}

// isAdmin reports whether a uid is in the configured ADMINS list.
func (b *Bot) isAdmin(uid string) bool { return b.admins[uid] }

// onError is the central dispatcher error handler, mirroring
// modules/Global/error_handler.py: it logs, sends a diagnostic report to
// ERROR_CHAT_ID, and replies to the user with a tracking code.
//
// To keep the error channel readable, the report is sent as a compact SUMMARY
// (the tracking code + the error) with a "🔎 more" button; the full detail — the
// triggering update dumped as pretty-printed JSON, paged into <pre> blocks under
// Telegram's length limit — is revealed one page at a time on demand by errMore,
// not dumped all at once. The complete detail is ALSO written to the process
// logs, so it survives even after the in-memory pages are evicted. (A Go error
// carries no stack trace here — unlike a Python exception's __traceback__ — so
// the error string stands in for the traceback and the update dump is the
// primary diagnostic.)
func (b *Bot) onError(tg *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
	code := encoder.GenerateCID(8)

	// Build the update dump once; it goes both to the logs and to the paged report.
	var updateJSON string
	if ctx != nil && ctx.Update != nil {
		if raw, jerr := json.MarshalIndent(ctx.Update, "", "  "); jerr == nil {
			updateJSON = string(raw)
		}
	}
	slog.Error("handler error", "code", code, "err", err, "update", updateJSON)

	var pages []string
	if updateJSON != "" {
		// Page the update dump (escaped) so each page fits in one message under
		// the <pre></pre> wrapper; split on rune boundaries so a multi-byte
		// character is never cut in half.
		pages = chunkString(html.EscapeString(updateJSON), 3500)
	}
	summary := fmt.Sprintf("⚠️ <b>خطایی هنگام پردازش یک آپدیت رخ داد</b>\nکد پیگیری: <code>%s</code>\n\n<pre>%s</pre>",
		code, html.EscapeString(truncate(fmt.Sprintf("%v", err), 600)))
	b.reportIncident(tg, ctx, code, summary, pages)
	return ext.DispatcherActionNoop
}

// onPanic is the dispatcher panic handler. gotgbot recovers handler panics to
// keep the single bot process alive, but WITHOUT this hook a recovered panic is
// only returned to the updater and logged — it never reaches ERROR_CHAT_ID and
// the user gets no tracking code (worse than the Python error_handler, which
// reported exceptions). We route it through the same incident report as onError,
// paging the STACK TRACE (which a plain error doesn't carry).
func (b *Bot) onPanic(tg *gotgbot.Bot, ctx *ext.Context, r any) {
	code := encoder.GenerateCID(8)
	stack := string(debug.Stack())
	slog.Error("handler panic", "code", code, "panic", fmt.Sprintf("%v", r), "stack", stack)

	pages := chunkString(html.EscapeString(stack), 3500)
	summary := fmt.Sprintf("🛑 <b>panic هنگام پردازش یک آپدیت</b>\nکد پیگیری: <code>%s</code>\n\n<pre>%s</pre>",
		code, html.EscapeString(truncate(fmt.Sprintf("%v", r), 600)))
	b.reportIncident(tg, ctx, code, summary, pages)
}

// reportIncident files a tracked incident: it stores the detail pages under the
// tracking code, sends a compact summary to ERROR_CHAT_ID with a "🔎 more" button
// that pages the detail on demand, and replies to the triggering user with the
// code. Shared by onError (the update dump) and onPanic (the stack trace).
func (b *Bot) reportIncident(tg *gotgbot.Bot, ctx *ext.Context, code, summary string, detailPages []string) {
	if b.Cfg.ErrorChatID != "" {
		if chatID, perr := strconv.ParseInt(b.Cfg.ErrorChatID, 10, 64); perr == nil {
			b.errReports.put(code, detailPages)
			opts := &gotgbot.SendMessageOpts{ParseMode: "HTML"}
			if len(detailPages) > 0 {
				opts.ReplyMarkup = moreButton(code, 0, len(detailPages))
			}
			_, _ = tg.SendMessage(chatID, summary, opts)
		}
	}
	if ctx != nil && ctx.EffectiveMessage != nil {
		_, _ = ctx.EffectiveMessage.Reply(tg,
			fmt.Sprintf("خطایی رخ داد. کد پیگیری: <code>%s</code>", code),
			&gotgbot.SendMessageOpts{ParseMode: "HTML"},
		)
	}
}

// reportToErrorChat sends a plain diagnostic line to ERROR_CHAT_ID, best-effort
// (used by handleErr's database-error branch, mirroring decorators.py's
// "PostgreSQL ERROR: ..." notice). A no-op when ERROR_CHAT_ID is unset/invalid.
func (b *Bot) reportToErrorChat(tg *gotgbot.Bot, text string) {
	if b.Cfg.ErrorChatID == "" {
		return
	}
	chatID, err := strconv.ParseInt(b.Cfg.ErrorChatID, 10, 64)
	if err != nil {
		return
	}
	_, _ = tg.SendMessage(chatID, text, nil)
}
