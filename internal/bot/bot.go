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

	users   *userStore
	aiQueue *aiQueue
	admins  map[string]bool
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
		TG:      tg,
		DB:      database,
		Cfg:     cfg,
		Texts:   txt,
		Dyn:     dynset.New("dynamic_settings.json", cfg.AIURL, cfg.AISessionID),
		users:   newUserStore(),
		aiQueue: newAIQueue(),
		admins:  make(map[string]bool, len(cfg.Admins)),
	}
	for _, a := range cfg.Admins {
		b.admins[a] = true
	}

	b.Dispatcher = ext.NewDispatcher(&ext.DispatcherOpts{
		Error: b.onError,
	})
	b.Updater = ext.NewUpdater(b.Dispatcher, &ext.UpdaterOpts{})

	b.registerHandlers()
	return b, nil
}

// Run starts long polling and blocks until ctx is cancelled, then stops cleanly.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.Updater.StartPolling(b.TG, &ext.PollingOpts{
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9, // long-poll seconds
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
// The report reproduces the Python error_handler's shape as closely as Go
// allows: a header with the tracking code and the error, followed by the
// triggering update dumped as pretty-printed JSON and split into <pre> chunks
// under Telegram's message-length limit. (A Go error carries no stack trace at
// this point — unlike a Python exception's __traceback__ — so the error string
// stands in for the traceback and the update dump is the primary diagnostic.)
func (b *Bot) onError(tg *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
	code := encoder.GenerateCID(8)
	slog.Error("handler error", "code", code, "err", err)

	if b.Cfg.ErrorChatID != "" {
		if chatID, perr := strconv.ParseInt(b.Cfg.ErrorChatID, 10, 64); perr == nil {
			header := fmt.Sprintf(
				"An exception was raised while handling an update\n"+
					"Error code: <code>%s</code>\n\n<pre>%s</pre>",
				code, html.EscapeString(fmt.Sprintf("%v", err)),
			)
			hdr, _ := tg.SendMessage(chatID, header, &gotgbot.SendMessageOpts{ParseMode: "HTML"})

			if ctx != nil && ctx.Update != nil {
				if raw, jerr := json.MarshalIndent(ctx.Update, "", "  "); jerr == nil {
					var replyTo *gotgbot.ReplyParameters
					if hdr != nil {
						replyTo = &gotgbot.ReplyParameters{MessageId: hdr.MessageId, AllowSendingWithoutReply: true}
					}
					// 4050 chars, less the surrounding <pre></pre>, matching the
					// Python chunker; chunkString splits on rune boundaries so a
					// multi-byte character is never cut in half.
					const maxChunk = 4050 - len("<pre></pre>")
					for _, chunk := range chunkString(html.EscapeString(string(raw)), maxChunk) {
						_, _ = tg.SendMessage(chatID, "<pre>"+chunk+"</pre>",
							&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyParameters: replyTo})
					}
				}
			}
		}
	}
	if ctx.EffectiveMessage != nil {
		_, _ = ctx.EffectiveMessage.Reply(tg,
			fmt.Sprintf("خطایی رخ داد. کد پیگیری: <code>%s</code>", code),
			&gotgbot.SendMessageOpts{ParseMode: "HTML"},
		)
	}
	return ext.DispatcherActionNoop
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
