// Package bot wires the Telegram side of ChevaletAnonBot on top of gotgbot:
// the Bot client, the long-polling Updater, the Dispatcher, the central error
// handler, and the prep middleware that mirrors the Python @prep_function.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/aturzone/chevaletAnonBot/internal/config"
	"github.com/aturzone/chevaletAnonBot/internal/db"
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

	users  *userStore
	admins map[string]bool
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
		TG:     tg,
		DB:     database,
		Cfg:    cfg,
		Texts:  txt,
		users:  newUserStore(),
		admins: make(map[string]bool, len(cfg.Admins)),
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
	<-ctx.Done()
	slog.Info("stopping updater")
	return b.Updater.Stop()
}

// isAdmin reports whether a uid is in the configured ADMINS list.
func (b *Bot) isAdmin(uid string) bool { return b.admins[uid] }

// onError is the central dispatcher error handler, mirroring
// modules/Global/error_handler.py: it logs, notifies ERROR_CHAT_ID, and replies
// to the user with a tracking code.
func (b *Bot) onError(tg *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
	code := encoder.GenerateCID(8)
	slog.Error("handler error", "code", code, "err", err)

	if b.Cfg.ErrorChatID != "" {
		if chatID, perr := strconv.ParseInt(b.Cfg.ErrorChatID, 10, 64); perr == nil {
			_, _ = tg.SendMessage(chatID,
				fmt.Sprintf("⚠️ error <code>%s</code>\n%v", code, err),
				&gotgbot.SendMessageOpts{ParseMode: "HTML"},
			)
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
