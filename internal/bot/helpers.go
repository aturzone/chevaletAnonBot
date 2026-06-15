package bot

import (
	"context"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// bg returns a background context bounded by dbOpTimeout, for a handler's
// database work. Handlers call it for short-lived query groups.
func (b *Bot) bg() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), dbOpTimeout)
}

// replyText replies to the triggering message in plain text, quoting it — the
// gotgbot Reply helper auto-fills ReplyParameters with the triggering message,
// matching the Python reply_text(..., reply_parameters=ReplyParameters(msg.id)).
func (b *Bot) replyText(ctx *ext.Context, text string) error {
	_, err := ctx.EffectiveMessage.Reply(b.TG, text, nil)
	return err
}

// replyHTMLNoPreview replies in HTML, quoting the message, with the link preview
// disabled (Python reply_html(..., disable_web_page_preview=True)).
func (b *Bot) replyHTMLNoPreview(ctx *ext.Context, text string) error {
	return b.replyHTML(ctx, text, true)
}

// sendHTMLNoQuote sends an HTML message to the user's chat WITHOUT quoting the
// triggering message — for the few Python paths that called reply_html with no
// reply_parameters (e.g. the "bot was blocked by the user" notice).
func (b *Bot) sendHTMLNoQuote(ctx *ext.Context, text string) error {
	_, err := b.TG.SendMessage(ctx.EffectiveChat.Id, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

// htmlOpts builds SendMessageOpts with HTML parse mode and an optional inline
// keyboard, quoting the triggering message.
func htmlOpts(markup *gotgbot.InlineKeyboardMarkup) *gotgbot.SendMessageOpts {
	o := &gotgbot.SendMessageOpts{ParseMode: "HTML"}
	if markup != nil {
		o.ReplyMarkup = *markup
	}
	return o
}

// sendPlain sends a plain-text message to the user's chat WITHOUT quoting the
// triggering message (Python message.reply_text(...) with no reply_parameters).
func (b *Bot) sendPlain(ctx *ext.Context, text string) error {
	_, err := b.TG.SendMessage(ctx.EffectiveChat.Id, text, nil)
	return err
}

// fmtText substitutes each "%s" in text with the next arg, in order, inserting
// the replacement literally. This mirrors Python's `text % (a, b, ...)` for the
// Texts/* templates (which only use %s) while being safe against stray '%' in
// the template or the substituted values.
func fmtText(text string, args ...string) string {
	for _, a := range args {
		text = strings.Replace(text, "%s", a, 1)
	}
	return text
}

// indexOf returns the position of v in s, or -1. Mirrors Python's list.index
// but without raising on a miss.
func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// equalStringSlices reports whether two string slices are element-wise equal.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
