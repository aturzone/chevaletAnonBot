package bot

import "github.com/PaulSonOfLars/gotgbot/v2/ext"

// Sentinels mirroring decorators.handle_target_send's "del+0"/"del+e" return
// values, which flow up to delete_notify_on_END. del0 -> conversation state 0
// (keep composing); delE -> END.
const (
	delNone = ""
	del0    = "del+0"
	delE    = "del+e"
)

// classifyTargetSendErr ports the except blocks of decorators.handle_target_send.
// It is called only when a send to the target failed (err != nil). externalReply
// reflects whether the source message was a reply to a channel.
//
// Returns one of delNone/del0/delE. delNone with a non-nil error means the
// failure was not one Python handled — it is propagated (Python re-raised). A
// failure while sending the user-facing reply is likewise propagated, matching
// the Python except bodies (which did not guard their own replies).
func (b *Bot) classifyTargetSendErr(ctx *ext.Context, externalReply bool, err error) (string, error) {
	switch {
	case errBotNotMember(err):
		// "Forbidden: bot is not a member of the channel chat"
		if e := b.replyHTMLNoPreview(ctx, txtChannelNotAddedRetry); e != nil {
			return delNone, e
		}
		return del0, nil

	case errBotBlocked(err):
		// "Forbidden: bot was blocked by the user" — Python sent this WITHOUT
		// quoting the triggering message.
		if e := b.sendHTMLNoQuote(ctx, txtBotBlockedByContact); e != nil {
			return delNone, e
		}
		return del0, nil

	case errReplyNotFound(err):
		// "Message to be replied not found"
		if externalReply {
			if e := b.replyHTMLNoPreview(ctx, txtChannelNotAddedRetry); e != nil {
				return delNone, e
			}
			return del0, nil
		}
		if e := b.replyHTMLNoPreview(ctx, txtMaybeBlockedOrCleared); e != nil {
			return delNone, e
		}
		return delE, nil

	case errMessageIDInvalid(err):
		// "MESSAGE_ID_INVALID"
		return delE, nil

	default:
		// Unhandled (Python: raise) -> propagate to the central error hook.
		return delNone, err
	}
}
