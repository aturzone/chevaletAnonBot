package bot

import (
	"strconv"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters"
	msgfilters "github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
)

// aiInput ports modules/ai_chat.py ai_input_message: a GM-group text that
// replies to one of the bot's messages is queued for the AI worker and marked
// with a 🤔 reaction. This handler is intentionally NOT wrapped with prep (prep
// only serves private chats); it runs directly for the GM group.
func (b *Bot) aiInput(tg *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg == nil {
		return nil
	}
	b.aiQueue.add(msg)
	_, _ = msg.SetReaction(tg, &gotgbot.SetMessageReactionOpts{
		Reaction: []gotgbot.ReactionType{gotgbot.ReactionTypeEmoji{Emoji: "🤔"}}, // ReactionEmoji.THINKING_FACE
	})
	return nil
}

// aiInputFilter mirrors `filters.TEXT & filters.Chat(GM_GROUP_ID) &
// FilterReplyToBot`: a text message in the GM group that replies to the bot.
func (b *Bot) aiInputFilter(gmGroupID, botID int64) filters.Message {
	return func(m *gotgbot.Message) bool {
		return msgfilters.Text(m) &&
			m.Chat.Id == gmGroupID &&
			m.ReplyToMessage != nil &&
			m.ReplyToMessage.From != nil &&
			m.ReplyToMessage.From.Id == botID
	}
}

// gmGroupID parses GM_GROUP_ID; 0 (and a logged-skip) if unset/invalid.
func (b *Bot) gmGroupID() (int64, bool) {
	id, err := strconv.ParseInt(b.Cfg.GMGroupID, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
