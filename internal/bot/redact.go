package bot

import (
	"encoding/json"
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// redactUpdate renders an update for diagnostics WITHOUT the message bodies.
//
// Why this exists: onError dumped the whole update as JSON into both the process
// logs and ERROR_CHAT_ID. On a bot whose entire purpose is that nobody can tell who
// wrote what, that put users' message text — next to their uid and username — in
// front of everyone with access to the error channel. Found while reading logs
// during the 429 investigation, where one incident contained a long, deeply
// personal message.
//
// Diagnostics keep everything that identifies the SHAPE of the failure — update and
// message ids, chat types, which fields were present, the reply/forward structure —
// and message content is replaced by its length, which is usually the diagnostic
// value anyway (an over-long caption, an empty body).
//
// Fields are redacted by NAME, recursively, so a nested reply_to_message,
// external_reply or quote is covered without enumerating every container.
func redactUpdate(u *gotgbot.Update) string {
	raw, err := json.Marshal(u)
	if err != nil {
		return ""
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return ""
	}
	redactTree(tree)
	out, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return ""
	}
	return string(out)
}

// contentFields are the keys whose values are user-authored content.
var contentFields = map[string]bool{
	"text":             true,
	"caption":          true,
	"query":            true, // inline queries
	"data":             true, // callback_data can carry a sealed token; not content, but not needed either
	"file_name":        true, // user-chosen, and can identify a person
	"performer":        true,
	"title":            true,
	"first_name":       false, // kept: needed to tell accounts apart while debugging
	"entities":         true,  // offsets+types of the text we just removed
	"caption_entities": true,
}

func redactTree(node any) {
	switch v := node.(type) {
	case map[string]any:
		for key, val := range v {
			if redact, ok := contentFields[key]; ok && redact {
				v[key] = placeholder(val)
				continue
			}
			redactTree(val)
		}
	case []any:
		for _, item := range v {
			redactTree(item)
		}
	}
}

// placeholder keeps the useful signal (how much there was) and drops the content.
func placeholder(val any) string {
	switch s := val.(type) {
	case string:
		return fmt.Sprintf("<redacted %d chars>", len([]rune(s)))
	case []any:
		return fmt.Sprintf("<redacted %d items>", len(s))
	default:
		return "<redacted>"
	}
}
