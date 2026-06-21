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

// chunkString splits s into pieces of at most size runes each, preserving order.
// It splits on rune boundaries (not bytes) so a multi-byte UTF-8 character is
// never cut in half — important when the chunks are sent as separate Telegram
// messages. Mirrors the character-based slicing of error_handler.py's chunker.
func chunkString(s string, size int) []string {
	if size <= 0 {
		return []string{s}
	}
	runes := []rune(s)
	out := make([]string, 0, (len(runes)+size-1)/size)
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// truncate shortens s to at most max runes, appending an ellipsis when cut.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
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
