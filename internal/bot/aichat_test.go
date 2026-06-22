package bot

import (
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// TestAIInputFilter locks the GM-group AI-input predicate, including the Go
// null-guards (ReplyToMessage / .From) that Python's FilterReplyToBot lacked.
// (The AI feature is disabled by default, but the filter logic must stay correct
// for when an operator enables it.)
func TestAIInputFilter(t *testing.T) {
	const G int64 = -100123
	const B int64 = 777
	filt := (&Bot{}).aiInputFilter(G, B)

	replyToBot := func(text string, chatID, fromID int64) *gotgbot.Message {
		return &gotgbot.Message{
			Text:           text,
			Chat:           gotgbot.Chat{Id: chatID},
			ReplyToMessage: &gotgbot.Message{From: &gotgbot.User{Id: fromID}},
		}
	}

	// positive: text, in the GM group, replying to the bot.
	if !filt(replyToBot("hi", G, B)) {
		t.Error("should match a text reply-to-bot in the GM group")
	}
	// wrong chat
	if filt(replyToBot("hi", -999, B)) {
		t.Error("should not match the wrong chat")
	}
	// non-text
	if filt(&gotgbot.Message{Chat: gotgbot.Chat{Id: G}, ReplyToMessage: &gotgbot.Message{From: &gotgbot.User{Id: B}}}) {
		t.Error("should not match a non-text message")
	}
	// no reply
	if filt(&gotgbot.Message{Text: "hi", Chat: gotgbot.Chat{Id: G}}) {
		t.Error("should not match a message with no reply")
	}
	// reply.From == nil (would panic in Python; Go guards it)
	if filt(&gotgbot.Message{Text: "hi", Chat: gotgbot.Chat{Id: G}, ReplyToMessage: &gotgbot.Message{}}) {
		t.Error("should not match (and must not panic) when reply.From is nil")
	}
	// reply to a human, not the bot
	if filt(replyToBot("hi", G, 555)) {
		t.Error("should not match a reply to a non-bot user")
	}
}
