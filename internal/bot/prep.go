package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// Handler is a prepared handler: it receives the resolved userid in addition to
// the usual gotgbot arguments, mirroring the Python signature
// (update, context, message, userid, bot, dbh) — here `b` carries the db handle
// and config, so dbh/bot are reachable through it.
//
// The returned error is either a genuine error (routed to the central error
// hook) OR a conversation state change produced by handlers.NextConversationState
// / handlers.EndConversation. prep passes both kinds straight through; only the
// enclosing ConversationHandler interprets the latter (see registerHandlers).
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

		// Serialise this user's updates (mirrors PTB's effectively per-user
		// processing and keeps media-group state race-free). Released when the
		// handler returns.
		ud := b.users.get(ctx.EffectiveUser.Id)
		ud.mu.Lock()
		defer ud.mu.Unlock()

		dbctx, cancel := context.WithTimeout(context.Background(), dbOpTimeout)
		defer cancel()

		if err := b.initUser(dbctx, userid, ctx.EffectiveUser); err != nil {
			// init_user returning False (a unique cid couldn't be allocated):
			// tell the user, wipe their state, end the conversation —
			// decorators.py:69-74.
			if errors.Is(err, errInitNoCID) {
				ud.clear()
				if ctx.EffectiveMessage != nil {
					_, _ = ctx.EffectiveMessage.Reply(tg, txtInitNoCID, nil)
				}
				return handlers.EndConversation()
			}
			return b.handleErr(tg, ctx, err)
		}

		banned, err := b.DB.IsBanned(dbctx, userid)
		if err != nil {
			return b.handleErr(tg, ctx, err)
		}
		if banned {
			return nil // banned users are silently ignored, as in the original
		}

		return b.handleErr(tg, ctx, fn(b, tg, ctx, userid))
	}
}

// handleErr mirrors the except blocks of @prep_function, which wrapped every
// handler. In order, it:
//   - lets conversation state changes through untouched;
//   - swallows the benign Telegram errors PTB ignored (an old callback query, a
//     vanished reply target, or a Forbidden from a user who blocked the bot);
//   - on a database error, replies the "database problem" notice to the user and
//     reports it to ERROR_CHAT_ID (decorators.py `except psycopg2.Error`);
//   - on a Telegram transport/network error, replies the "internet problem"
//     notice (decorators.py `except NetworkError`);
//   - propagates everything else to the central error hook (onError), which —
//     like the global PTB error_handler — notifies ERROR_CHAT_ID and replies a
//     tracking code.
//
// The user-facing replies are best-effort (ignored on failure), exactly like the
// Python `try: reply except: pass`.
func (b *Bot) handleErr(tg *gotgbot.Bot, ctx *ext.Context, err error) error {
	if err == nil {
		return nil
	}
	var sc *handlers.ConversationStateChange
	if errors.As(err, &sc) {
		return err
	}
	switch {
	case errQueryTooOld(err):
		return nil
	case errReplyNotFound(err):
		return nil
	case errForbidden(err): // covers "bot was blocked by the user" / "not a member"
		return nil
	case isDBError(err):
		if ctx.EffectiveMessage != nil {
			_, _ = ctx.EffectiveMessage.Reply(tg, txtDBProblem, nil)
		}
		b.reportToErrorChat(tg, "PostgreSQL ERROR: "+err.Error())
		return nil
	case isNetworkError(err):
		if ctx.EffectiveMessage != nil {
			_, _ = ctx.EffectiveMessage.Reply(tg, txtNetworkProblem, nil)
		}
		return nil
	default:
		return err
	}
}

// command registers a private-chat command handler wrapped with prep. Used for
// the self-contained Phase 2 commands; conversation entry points use the
// builders below instead so their state changes are honoured.
func (b *Bot) command(name string, fn Handler) {
	b.Dispatcher.AddHandler(handlers.NewCommand(name, b.topLevel(fn)))
}

// topLevel adapts a Handler for registration OUTSIDE a ConversationHandler. Some
// handlers (e.g. delete_msg_clbk) are reused both inside the conversation —
// where their handlers.EndConversation()/NextConversationState return values
// matter — and standalone, where PTB simply ignored that return. A conversation
// state change leaking to the dispatcher would be mistaken for a real error, so
// we swallow it here, exactly reproducing PTB's "return value ignored outside a
// conversation" semantics.
func (b *Bot) topLevel(fn Handler) handlers.Response {
	inner := b.prep(fn)
	return func(tg *gotgbot.Bot, ctx *ext.Context) error {
		err := inner(tg, ctx)
		var sc *handlers.ConversationStateChange
		if errors.As(err, &sc) {
			return nil
		}
		return err
	}
}

// Conversation state names. The Python ConversationHandler used the integer
// state 0 for "currently composing a message to a target"; we keep that label.
const stateSending = "0"
