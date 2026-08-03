package bot

import (
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// The "سایر" toggle on a delivered anonymous message.
//
// A delivered message used to carry four rows — answer, report/block, "sent with
// link N", donation. Donation and the blank spacer are gone, and report/block now
// live behind this toggle, so the message shows two rows: the actions used
// constantly, and which link it arrived through.
//
// HOW THIS STAYS STATELESS. callback_data is capped at 64 bytes and each sealed
// token is 31, so a button cannot carry both tokens plus a message id. Instead:
//
//   - the ANSWER button is never hidden, so tokenMid and the message id are always
//     readable straight off the keyboard;
//   - the toggle itself carries tokenBlock;
//   - whether the target is blocked is read from the database at expand time, not
//     remembered in the data — otherwise collapsing and reopening could offer
//     "block" to someone who has already blocked.
//
// Expanding therefore only swaps the toggle button and inserts one row, leaving
// every other row (the "sent with link N" row, and the seen button's used/unused
// state) exactly as it was.
const (
	othVerbExpand   = "oth|"
	othVerbCollapse = "othx|"
)

// otherActions handles both the expand and the collapse tap.
func otherActions(b *Bot, tg *gotgbot.Bot, ctx *ext.Context, userid string) error {
	clbk := ctx.CallbackQuery
	if clbk == nil || clbk.Data == "" {
		return nil
	}
	msg := ctx.EffectiveMessage
	if msg == nil || msg.ReplyMarkup == nil {
		_, _ = clbk.Answer(tg, nil)
		return nil
	}
	rows := msg.ReplyMarkup.InlineKeyboard

	expanding := strings.HasPrefix(clbk.Data, othVerbExpand)
	tokenBlock := strings.TrimPrefix(strings.TrimPrefix(clbk.Data, othVerbCollapse), othVerbExpand)

	var newRows [][]gotgbot.InlineKeyboardButton

	if expanding {
		tokenMid, midStr, ok := answerTokenFromKeyboard(rows)
		if !ok {
			// No answer button means this is not a keyboard we built (or it is far
			// older than this scheme); say so instead of rendering half a menu.
			_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "این پیام قدیمیه، دکمه‌هاش کار نمیکنه.",
				ShowAlert: true,
			})
			return nil
		}

		blocked := false
		dbctx, cancel := b.bg()
		defer cancel()
		if targetUID, ok, err := b.resolveTargetUID(dbctx, msg, tokenBlock, blockAAD()); err == nil && ok {
			if bl, berr := b.DB.IsBlocked(dbctx, userid, targetUID); berr == nil {
				blocked = bl
			}
		}

		for i, r := range rows {
			newRows = append(newRows, swapToggle(r, tokenBlock, true))
			if i == 0 {
				newRows = append(newRows, otherActionsRow(tokenMid, tokenBlock, midStr, blocked))
			}
		}
	} else {
		for _, r := range rows {
			// Drop the revealed row rather than tracking its index: identifying it by
			// what it contains survives any other row being added later.
			if isOtherActionsRow(r) {
				continue
			}
			newRows = append(newRows, swapToggle(r, tokenBlock, false))
		}
	}

	_, _ = clbk.Answer(tg, nil)
	if _, _, err := msg.EditReplyMarkup(tg, &gotgbot.EditMessageReplyMarkupOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: newRows},
	}); err != nil && !errMessageNotModified(err) {
		return err
	}
	return nil
}

// swapToggle rewrites the سایر/بستن button in a row, leaving everything else
// (including a used "seen" button) untouched.
func swapToggle(r []gotgbot.InlineKeyboardButton, tokenBlock string, expanded bool) []gotgbot.InlineKeyboardButton {
	out := make([]gotgbot.InlineKeyboardButton, len(r))
	copy(out, r)
	for i, btn := range out {
		switch {
		case strings.HasPrefix(btn.CallbackData, othVerbExpand) && expanded:
			out[i] = cb(msgBtnOtherBack, othVerbCollapse+tokenBlock)
		case strings.HasPrefix(btn.CallbackData, othVerbCollapse) && !expanded:
			out[i] = cb(msgBtnOther, othVerbExpand+tokenBlock)
		}
	}
	return out
}

// isOtherActionsRow reports whether a row is the revealed report/block row.
func isOtherActionsRow(r []gotgbot.InlineKeyboardButton) bool {
	for _, btn := range r {
		if strings.HasPrefix(btn.CallbackData, "report|") ||
			strings.HasPrefix(btn.CallbackData, "block|") ||
			strings.HasPrefix(btn.CallbackData, "unblock|") {
			return true
		}
	}
	return false
}

// answerTokenFromKeyboard reads tokenMid and the message id back off the answer
// button, which is why that button is never hidden.
func answerTokenFromKeyboard(rows [][]gotgbot.InlineKeyboardButton) (tokenMid, midStr string, ok bool) {
	for _, r := range rows {
		for _, btn := range r {
			if !strings.HasPrefix(btn.CallbackData, "answer|") {
				continue
			}
			parts := strings.SplitN(btn.CallbackData, "|", 3)
			if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
				continue
			}
			if _, err := strconv.ParseInt(parts[2], 10, 64); err != nil {
				continue
			}
			return parts[1], parts[2], true
		}
	}
	return "", "", false
}
