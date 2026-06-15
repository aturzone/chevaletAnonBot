package bot

import (
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// otherMessages ports other_msgs.other_messages: the catch-all for messages that
// aren't part of a conversation. It first tries the auto-reply paths (reply to a
// delivered message / a channel), then falls back to a "didn't understand" hint
// (or a "nothing to cancel" note for a stray /cancel).
func otherMessages(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, userid string) error {
	handled, err := b.checkIfAutoreply(ctx, userid)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	msg := ctx.EffectiveMessage
	if msg.Text != "" && containsField(msg.Text, "/cancel") {
		return b.replyText(ctx, txtNothingToCancel)
	}
	return b.otherMessagesTemplate(ctx)
}

// containsField reports whether v is one of the whitespace-separated fields of s
// (Python's `v in s.split()`).
func containsField(s, v string) bool {
	for _, f := range strings.Fields(s) {
		if f == v {
			return true
		}
	}
	return false
}
