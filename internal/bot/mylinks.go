package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"

	"github.com/aturzone/chevaletAnonBot/internal/config"
	"github.com/aturzone/chevaletAnonBot/internal/db"
	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// This file ports modules/my_links.py: the /my_links ConversationHandler with
// add/remove/rename of cids (links), enforcing the per-user cid limit and cid
// uniqueness.

// myLinksTemplate ports my_links_template: renders the user's links list (with a
// channel promo when they have more than two), either editing the callback
// message or replying to the command.
func (b *Bot) myLinksTemplate(tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	dbctx, cancel := b.bg()
	defer cancel()
	cids, err := b.DB.GetCIDs(dbctx, userid)
	if err != nil {
		return err
	}
	limit, err := b.DB.GetCIDLimit(dbctx, userid)
	if err != nil {
		return err
	}
	text := userLinksText(cids, limit, b.TG.User.Username)
	if len(cids) > 2 {
		text += txtMyLinksPromo
	}
	markup := ikb(mylinksDefaultMenu()...)
	if ctx.CallbackQuery != nil {
		_, _ = ctx.CallbackQuery.Answer(tg, nil)
		_, _, e := ctx.EffectiveMessage.EditText(tg, text, &gotgbot.EditMessageTextOpts{
			ParseMode:          "HTML",
			ReplyMarkup:        markup,
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
		})
		return e
	}
	_, e := ctx.EffectiveMessage.Reply(tg, text, &gotgbot.SendMessageOpts{
		ParseMode:          "HTML",
		ReplyMarkup:        markup,
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
	})
	return e
}

// myLinksCmd ports my_links_cmd (/my_links and the mylinks-menu callback).
func myLinksCmd(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	if err := b.myLinksTemplate(tg, ctx, userid); err != nil {
		return err
	}
	return handlers.EndConversation()
}

// addLink ports add_link_clbk.
func addLink(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	clbk := ctx.CallbackQuery
	if clbk == nil {
		return nil
	}
	_, _ = clbk.Answer(tg, nil)

	dbctx, cancel := b.bg()
	defer cancel()
	limit, err := b.DB.GetCIDLimit(dbctx, userid)
	if err != nil {
		return err
	}
	cids, err := b.DB.GetCIDs(dbctx, userid)
	if err != nil {
		return err
	}
	if len(cids) >= limit {
		if e := b.sendPlain(ctx, fmt.Sprintf(
			"به حد مجازت رسیدی. برای لینکهای بیشتر به ادمین پیام بده: @%s", b.Cfg.SellerAdmin)); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	if _, err := b.DB.AddCID(dbctx, userid, encoder.GenerateCID(10)); err != nil {
		return err
	}
	if err := b.myLinksTemplate(tg, ctx, userid); err != nil {
		return err
	}
	_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: txtAddedNewLink})
	return nil
}

// rmLinkButtons builds the "حذف لینک N" chooser keyboard + back-to-menu.
func rmLinkButtons(cids []string) gotgbot.InlineKeyboardMarkup {
	rows := make([][]gotgbot.InlineKeyboardButton, 0, len(cids)+1)
	for idx, cid := range cids {
		rows = append(rows, row(cb(fmt.Sprintf("حذف لینک %d", idx+1), "rm-link|"+cid)))
	}
	rows = append(rows, row(mylinksButtons["back-to-menu"]))
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// chLinkButtons builds the "شخصی سازی لینک N" chooser keyboard (without the
// trailing rows, which differ between the choose and refresh cases).
func chLinkButtons(cids []string) [][]gotgbot.InlineKeyboardButton {
	rows := make([][]gotgbot.InlineKeyboardButton, 0, len(cids))
	for idx, cid := range cids {
		rows = append(rows, row(cb(fmt.Sprintf("شخصی سازی لینک %d", idx+1), "ch-link|"+cid)))
	}
	return rows
}

// removeLink ports remove_link_clbk: choose -> confirm -> apply.
func removeLink(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	clbk := ctx.CallbackQuery
	if clbk == nil || clbk.Data == "" {
		return nil
	}
	_, _ = clbk.Answer(tg, nil)
	parts := strings.Split(clbk.Data, "|")

	dbctx, cancel := b.bg()
	defer cancel()
	cids, err := b.DB.GetCIDs(dbctx, userid)
	if err != nil {
		return err
	}

	switch len(parts) {
	case 1:
		// ask which link to remove
		_, _, e := ctx.EffectiveMessage.EditText(tg, getUserLinks(cids, b.TG.User.Username, ""),
			&gotgbot.EditMessageTextOpts{
				ParseMode:          "HTML",
				ReplyMarkup:        rmLinkButtons(cids),
				LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
			})
		return e

	case 2:
		// ask for confirmation
		chosenCid := parts[1]
		text := getUserLinks(cids, b.TG.User.Username, chosenCid) + txtRmConfirmTail
		markup := ikb(row(
			cb(btnRmSure, "rm-link|"+chosenCid+"|yes"),
			cb(btnRmNo, "rm-link|"+chosenCid+"|no"),
		))
		_, _, e := ctx.EffectiveMessage.EditText(tg, text, &gotgbot.EditMessageTextOpts{
			ParseMode:          "HTML",
			ReplyMarkup:        markup,
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
		})
		return e

	case 3:
		chosenCid, sure := parts[1], parts[2]
		if sure == "yes" {
			if err := b.DB.RemoveCID(dbctx, userid, chosenCid); err != nil {
				return err
			}
			remaining, err := b.DB.GetCIDs(dbctx, userid)
			if err != nil {
				return err
			}
			if len(remaining) < 1 {
				// the user had only one link -> mint a replacement.
				if _, err := b.DB.AddCID(dbctx, userid, encoder.GenerateCID(10)); err != nil {
					return err
				}
				_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: txtRmDeletedRegen, ShowAlert: true})
			} else {
				_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: txtRmDeleted})
			}
			cids, err = b.DB.GetCIDs(dbctx, userid)
			if err != nil {
				return err
			}
		} else {
			_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{Text: txtRmCancelled})
		}
		_, _, e := ctx.EffectiveMessage.EditText(tg, getUserLinks(cids, b.TG.User.Username, ""),
			&gotgbot.EditMessageTextOpts{
				ParseMode:          "HTML",
				ReplyMarkup:        rmLinkButtons(cids),
				LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
			})
		return e
	}
	return nil
}

// changeLink ports change_link_clbk: choose a link (END) then prompt for the new
// id (state 0).
func changeLink(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	clbk := ctx.CallbackQuery
	if clbk == nil || clbk.Data == "" {
		return nil
	}
	_, _ = clbk.Answer(tg, nil)
	parts := strings.Split(clbk.Data, "|")

	dbctx, cancel := b.bg()
	defer cancel()
	cids, err := b.DB.GetCIDs(dbctx, userid)
	if err != nil {
		return err
	}

	switch len(parts) {
	case 1:
		rows := append(chLinkButtons(cids),
			row(mylinksButtons["what-is-customize"]),
			row(mylinksButtons["back-to-menu"]),
		)
		_, _, e := ctx.EffectiveMessage.EditText(tg, getUserLinks(cids, b.TG.User.Username, "")+txtChChooseTail,
			&gotgbot.EditMessageTextOpts{
				ParseMode:          "HTML",
				ReplyMarkup:        gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows},
				LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
			})
		if e != nil {
			return e
		}
		return handlers.EndConversation()

	case 2:
		chosenCid := parts[1]
		tmpl, err := b.Texts.Get("mylinks")
		if err != nil {
			return err
		}
		text := getUserLinks(cids, b.TG.User.Username, chosenCid) + "\n\n\n" +
			fmtText(tmpl, strconv.Itoa(b.Cfg.MinCIDLength), strconv.Itoa(b.Cfg.MaxCIDLength))
		msg, _, e := ctx.EffectiveMessage.EditText(tg, text, &gotgbot.EditMessageTextOpts{
			ParseMode:          "HTML",
			ReplyMarkup:        ikb(row(mylinksButtons["what-is-cid"]), row(mylinksButtons["back-to-menu"])),
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
		})
		if e != nil {
			return e
		}
		ud := b.ud(ctx)
		ud.d.chosenCid = chosenCid
		if msg != nil {
			ud.d.linksMID = msg.MessageId
		} else {
			ud.d.linksMID = ctx.EffectiveMessage.MessageId
		}
		return handlers.NextConversationState("0")
	}
	return nil
}

// updateCid ports update_cid (state 0): validates length/charset/uniqueness then
// renames the cid.
func updateCid(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	msg := ctx.EffectiveMessage
	newCid := msg.Text
	ud := b.ud(ctx)
	chosenCid := ud.d.chosenCid
	linksMID := ud.d.linksMID

	// chosenCid/linksMID live in the per-user userData, which is idle-evicted after
	// 2h — but the conversation state lives in a SEPARATE store with no eviction. A
	// >2h pause mid-rename therefore leaves a fresh, empty userData here. With an
	// empty chosenCid, SetCID below would run `UPDATE cids SET cid=$1 WHERE cid=''`,
	// match zero rows, raise no error, and still report success — telling the user a
	// rename worked when nothing changed. Detect the desync and ask them to retry.
	if chosenCid == "" {
		if e := b.replyText(ctx, txtChangingCidCancelled); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	// length
	if n := runeLen(newCid); n < b.Cfg.MinCIDLength || n > b.Cfg.MaxCIDLength {
		if e := b.sendPlain(ctx, fmt.Sprintf("خطا: تعداد حروف مجاز %d تا %d حرفه",
			b.Cfg.MinCIDLength, b.Cfg.MaxCIDLength)); e != nil {
			return e
		}
		return handlers.NextConversationState("0")
	}

	// charset
	for _, r := range newCid {
		if !strings.ContainsRune(config.AllowedCIDChars, r) {
			if e := b.replyText(ctx, txtCidCharErr); e != nil {
				return e
			}
			return handlers.NextConversationState("0")
		}
	}

	dbctx, cancel := b.bg()
	defer cancel()

	// uniqueness across all cids + chevaletids
	taken, err := b.cidTaken(dbctx, newCid)
	if err != nil {
		return err
	}
	if taken {
		if e := b.replyText(ctx, txtCidTaken); e != nil {
			return e
		}
		return handlers.NextConversationState("0")
	}

	if err := b.DB.SetCID(dbctx, newCid, chosenCid); err != nil {
		if db.IsUniqueViolation(err) {
			// Python's `except IntegrityError`: someone grabbed it first.
			if e := b.replyText(ctx, txtCidIntegrity); e != nil {
				return e
			}
			return handlers.NextConversationState("0")
		}
		return err
	}

	cids, err := b.DB.GetCIDs(dbctx, userid)
	if err != nil {
		return err
	}
	rows := append(chLinkButtons(cids), row(mylinksButtons["back-to-menu"]))
	_, _, _ = b.TG.EditMessageText(getUserLinks(cids, b.TG.User.Username, ""), &gotgbot.EditMessageTextOpts{
		ChatId:             ctx.EffectiveChat.Id,
		MessageId:          linksMID,
		ParseMode:          "HTML",
		ReplyMarkup:        gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows},
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{IsDisabled: true},
	}) // best effort, like the Python try/except pass
	if e := b.sendPlain(ctx, txtCidChanged); e != nil {
		return e
	}
	return handlers.EndConversation()
}

// cidTaken reports whether cid already exists as any cid or chevaletid.
func (b *Bot) cidTaken(ctx context.Context, cid string) (bool, error) {
	allCids, err := b.DB.GetAllCIDs(ctx)
	if err != nil {
		return false, err
	}
	if indexOf(allCids, cid) >= 0 {
		return true, nil
	}
	allChev, err := b.DB.GetAllChevaletIDs(ctx)
	if err != nil {
		return false, err
	}
	return indexOf(allChev, cid) >= 0, nil
}

// whatIsCid ports what_is_cid.
func whatIsCid(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, _ string) error {
	if ctx.CallbackQuery == nil {
		return nil
	}
	_, _ = ctx.CallbackQuery.Answer(tg, nil)
	txt, err := b.Texts.Get("cid_explanation")
	if err != nil {
		return err
	}
	return b.replyHTML(ctx, txt, false)
}

// othersWhileSending ports others_while_sending (the my_links fallback).
func othersWhileSending(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, _ string) error {
	b.ud(ctx).clear()
	if e := b.replyText(ctx, txtChangingCidCancelled); e != nil {
		return e
	}
	return handlers.EndConversation()
}

// moreLinks ports help.more_links_clbk: the "why multiple links" explanation.
func moreLinks(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, _ string) error {
	clbk := ctx.CallbackQuery
	if clbk == nil {
		return nil
	}
	_, _ = clbk.Answer(tg, nil)
	if clbk.Data == "" {
		return nil
	}
	txt, err := b.Texts.Get("more_links")
	if err != nil {
		return err
	}
	return b.replyHTML(ctx, txt, false)
}
