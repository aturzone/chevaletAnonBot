package bot

import (
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// TestRedactUpdateRemovesMessageBodies is the privacy guard. The raw dump used to
// put users' message text — beside their uid and username — into the error channel
// and the logs, on a bot whose whole purpose is that nobody can tell who wrote
// what. A regression here leaks real people's private messages.
func TestRedactUpdateRemovesMessageBodies(t *testing.T) {
	const secret = "a deeply personal message nobody else should read"
	const secretCaption = "and a caption that is just as private"

	u := &gotgbot.Update{
		UpdateId: 574679277,
		Message: &gotgbot.Message{
			MessageId: 1060505,
			Text:      secret,
			Caption:   secretCaption,
			Chat:      gotgbot.Chat{Id: 1142340791, Type: "private"},
			From:      &gotgbot.User{Id: 1142340791, Username: "someone"},
			ReplyToMessage: &gotgbot.Message{
				MessageId: 1060557,
				Text:      "a nested message body, also private",
			},
		},
	}

	out := redactUpdate(u)
	if out == "" {
		t.Fatal("redactUpdate produced nothing")
	}

	// The content must be gone, at every nesting level.
	for _, leak := range []string{secret, secretCaption, "a nested message body, also private"} {
		if strings.Contains(out, leak) {
			t.Errorf("redacted dump still contains user content: %q", leak)
		}
	}

	// …and the diagnostics must survive, or the dump is useless.
	for _, keep := range []string{"574679277", "1060505", "private", "1060557"} {
		if !strings.Contains(out, keep) {
			t.Errorf("redacted dump lost the diagnostic field %q", keep)
		}
	}

	// The length is kept, because "how long was it" is usually the actual clue.
	if !strings.Contains(out, "redacted") {
		t.Error("no redaction placeholder in the output")
	}
}

// TestRedactHandlesNilAndEmpty guards the error path: the dump builder runs while
// something is already going wrong, so it must not panic or hide the incident.
func TestRedactHandlesNilAndEmpty(t *testing.T) {
	if got := redactUpdate(&gotgbot.Update{}); got == "" {
		t.Error("an empty update produced no dump at all")
	}
	// A message with no text must still render, not crash on the missing field.
	u := &gotgbot.Update{Message: &gotgbot.Message{MessageId: 1}}
	if got := redactUpdate(u); !strings.Contains(got, "\"message_id\": 1") {
		t.Errorf("dump lost message_id: %s", got)
	}
}
