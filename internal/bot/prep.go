package bot

import (
	"context"
	"strconv"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// Handler is a prepared handler: it receives the resolved userid in addition to
// the usual gotgbot arguments, mirroring the Python signature
// (update, context, message, userid, bot, dbh) — here `b` carries the db handle
// and config, so dbh/bot are reachable through it.
type Handler func(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error

// prep wraps a Handler with the middleware from modules/Global/decorators.py
// (@prep_function): it filters out non-private and edited updates, initialises
// the user (row, a cid, a chevaletid), rejects banned users, then dispatches.
func (b *Bot) prep(fn Handler) handlers.Response {
	return func(tg *gotgbot.Bot, ctx *ext.Context) error {
		// Ignore edited messages (Python bailed on update.edited_message).
		if ctx.Update.EditedMessage != nil {
			return nil
		}
		// Only operate in private chats — the Python prep rejected group/channel
		// updates (the GM group's AI flow is handled by a separate, un-prepped
		// handler).
		if ctx.EffectiveChat != nil && ctx.EffectiveChat.Type != "private" {
			return nil
		}
		if ctx.EffectiveUser == nil {
			return nil
		}

		userid := strconv.FormatInt(ctx.EffectiveUser.Id, 10)

		dbctx, cancel := context.WithTimeout(context.Background(), dbOpTimeout)
		defer cancel()

		if err := b.initUser(dbctx, userid, ctx.EffectiveUser); err != nil {
			return err
		}

		banned, err := b.DB.IsBanned(dbctx, userid)
		if err != nil {
			return err
		}
		if banned {
			return nil // banned users are silently ignored, as in the original
		}

		return fn(b, tg, ctx, userid)
	}
}

// command registers a private-chat command handler wrapped with prep.
func (b *Bot) command(name string, fn Handler) {
	b.Dispatcher.AddHandler(handlers.NewCommand(name, b.prep(fn)))
}
