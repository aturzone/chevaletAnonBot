package bot

import (
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"

	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// startCmd ports start.start_cmd: /start (help text), /start <cid> (connect to a
// target), and /start UNBLOCK-<uid> (unblock a user). It is the conversation's
// primary entry point; the connect path enters the sending state (Python 0).
func startCmd(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	msg := ctx.EffectiveMessage
	splitText := strings.Fields(msg.Text)

	if len(splitText) == 1 {
		// /start with no argument -> the start/help text.
		txt, err := b.Texts.Get("start_help")
		if err != nil {
			return err
		}
		txt = strings.ReplaceAll(txt, "%s", b.Cfg.DonationLink)
		if e := b.replyHTML(ctx, txt, true); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	arg := splitText[1]
	if strings.HasPrefix(arg, "UNBLOCK-") {
		return b.startUnblock(tg, ctx, userid, arg)
	}
	return b.startConnect(tg, ctx, userid, arg)
}

// startUnblock ports the /start UNBLOCK-<uid> branch.
func (b *Bot) startUnblock(tg *gotgbot.Bot, ctx *ext.Context, userid, arg string) error {
	msg := ctx.EffectiveMessage

	// Python: target_uid = arg.split("-", 1)[-1] -> everything after the first '-'.
	targetUID := ""
	if i := strings.IndexByte(arg, '-'); i >= 0 {
		targetUID = arg[i+1:]
	}
	if _, err := strconv.ParseInt(targetUID, 10, 64); err != nil || targetUID == "" {
		if e := b.replyText(ctx, txtWrongLink); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	dbctx, cancel := b.bg()
	defer cancel()

	blocked, err := b.DB.IsBlocked(dbctx, userid, targetUID)
	if err != nil {
		return err
	}
	if blocked {
		if _, err := b.DB.RemoveBlock(dbctx, userid, targetUID); err != nil {
			return err
		}
		chid, err := b.DB.GetChevaletIDByUID(dbctx, targetUID)
		if err != nil {
			return err
		}
		text := "این یوزر برات آنبلاک شد:\n" + b.getUsername(targetUID) + " | " + hrefUser(targetUID, "")
		markup := gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			cb(btnBlockAgain, "block|"+encoder.EncodeChevaletID(chid)),
		}}}
		if _, err := msg.Reply(tg, text, &gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: markup}); err != nil {
			return err
		}
	} else {
		if e := b.replyText(ctx, txtNotBlockedAnyway); e != nil {
			return e
		}
	}
	return handlers.EndConversation()
}

// startConnect ports the /start <cid> branch.
func (b *Bot) startConnect(tg *gotgbot.Bot, ctx *ext.Context, userid, targetCid string) error {
	msg := ctx.EffectiveMessage

	dbctx, cancel := b.bg()
	defer cancel()

	targetUID, err := b.DB.GetUIDByCID(dbctx, targetCid)
	if err != nil {
		return err
	}
	if targetUID == "" {
		if e := b.replyText(ctx, txtLinkDeletedOrChanged); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	targetChid, err := b.DB.GetChevaletIDByUID(dbctx, targetUID)
	if err != nil {
		return err
	}
	if targetChid == "" {
		targetChid = encoder.GenerateChevaletID()
		// set_chevaletid always succeeded in Python (its failure branch was dead
		// code); a real DB failure propagates to the central error handler.
		if err := b.DB.SetChevaletID(dbctx, targetUID, targetChid); err != nil {
			return err
		}
	}
	encChid := encoder.EncodeChevaletID(targetChid)

	blocked, err := b.DB.IsBlocked(dbctx, targetUID, userid)
	if err != nil {
		return err
	}
	if blocked {
		if e := b.replyText(ctx, txtBlockedYou); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	banned, err := b.DB.IsBanned(dbctx, targetUID)
	if err != nil {
		return err
	}
	if banned {
		if e := b.replyText(ctx, txtUserBanned); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	name, err := b.DB.GetName(dbctx, targetUID)
	if err != nil {
		return err
	}

	ud := b.ud(ctx)
	ud.d.targetCid = targetCid
	ud.d.targetChid = encChid // already encoded
	ud.d.replyTo = ""

	selfPrefix := ""
	if targetUID == userid {
		selfPrefix = txtConnectSelf
	}
	text := selfPrefix + "\nبه " + sanitizeUserHTML(name) + " وصل شدی. پیامتو بفرست\n\n" + txtConnectBody
	if _, err := msg.Reply(tg, text, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: *cancelMarkup(),
	}); err != nil {
		return err
	}
	return handlers.NextConversationState(stateSending)
}
