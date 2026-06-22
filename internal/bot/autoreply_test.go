package bot

import (
	"regexp"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// linkRe mirrors what Bot.channelLinkRe() produces for a bot named "testbot":
// the username is lower-cased and the caller searches it against a lower-cased
// haystack. Building it directly keeps these tests free of a *Bot.
func linkRe() *regexp.Regexp {
	return regexp.MustCompile(`t\.me/testbot\?start=([A-Za-z0-9_-]+)`)
}

func TestFindCID(t *testing.T) {
	re := linkRe()

	// The parity-critical property: the regex matches the LOWER-CASED haystack,
	// but the cid is sliced from the ORIGINAL so its case is preserved — exactly
	// the Python `match.span(1)` + `text[offset:end]` behaviour. A mixed-case bot
	// username in the link must still match (it is lower-cased before searching).
	cases := []struct {
		name, original, want string
		ok                   bool
	}{
		{"case preserved + mixed-case username", "reply: t.me/TestBot?start=AbC_12-3 now", "AbC_12-3", true},
		{"all lower", "x t.me/testbot?start=hello y", "hello", true},
		{"cid with hyphen and underscore", "t.me/testbot?start=a-b_c-D", "a-b_c-D", true},
		{"no link", "just some text", "", false},
		{"different bot", "t.me/otherbot?start=zzz", "", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		got, ok := findCID(re, c.original)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: findCID(%q) = (%q,%v); want (%q,%v)", c.name, c.original, got, ok, c.want, c.ok)
		}
	}
}

func TestReMatches(t *testing.T) {
	re := linkRe()
	// reMatches lower-cases the haystack, so an UPPER-CASE link still matches.
	if !reMatches(re, "BIO: T.ME/TESTBOT?START=XYZ") {
		t.Error("reMatches should match an upper-case link (haystack is lower-cased)")
	}
	if reMatches(re, "no anonymous link in this bio") {
		t.Error("reMatches should not match unrelated text")
	}
	if reMatches(re, "") {
		t.Error("reMatches should not match empty text")
	}
}

func TestFirstURLEntityCID(t *testing.T) {
	re := linkRe()

	// the first "url" entity whose text carries the link wins. Type is a promoted
	// field of the embedded MessageEntity, so it must be set explicitly.
	ent := func(typ, text string) gotgbot.ParsedMessageEntity {
		return gotgbot.ParsedMessageEntity{MessageEntity: gotgbot.MessageEntity{Type: typ}, Text: text}
	}
	entities := []gotgbot.ParsedMessageEntity{
		ent("bold", "ignored"),
		ent("url", "see t.me/testbot?start=FirstOne"),
		ent("url", "t.me/testbot?start=SecondOne"),
	}
	if got := firstURLEntityCID(re, entities); got != "FirstOne" {
		t.Errorf("firstURLEntityCID = %q; want FirstOne", got)
	}

	// a url entity that does not contain OUR link is skipped.
	none := []gotgbot.ParsedMessageEntity{
		ent("url", "t.me/otherbot?start=nope"),
		ent("text_link", "t.me/testbot?start=AlsoSkipped"), // wrong type
	}
	if got := firstURLEntityCID(re, none); got != "" {
		t.Errorf("firstURLEntityCID (no matching url entity) = %q; want empty", got)
	}

	if got := firstURLEntityCID(re, nil); got != "" {
		t.Errorf("firstURLEntityCID(nil) = %q; want empty", got)
	}
}

func TestFirstURLButtonCID(t *testing.T) {
	re := linkRe()

	keyboard := [][]gotgbot.InlineKeyboardButton{
		{{Text: "no url button", CallbackData: "x"}},
		{{Text: "link", Url: "https://t.me/testbot?start=BtnCid"}},
	}
	if got := firstURLButtonCID(re, keyboard); got != "BtnCid" {
		t.Errorf("firstURLButtonCID = %q; want BtnCid", got)
	}

	noURL := [][]gotgbot.InlineKeyboardButton{
		{{Text: "a", CallbackData: "data"}},
		{{Text: "b", Url: "https://t.me/otherbot?start=skip"}},
	}
	if got := firstURLButtonCID(re, noURL); got != "" {
		t.Errorf("firstURLButtonCID (no matching url) = %q; want empty", got)
	}

	if got := firstURLButtonCID(re, nil); got != "" {
		t.Errorf("firstURLButtonCID(nil) = %q; want empty", got)
	}
}

// TestAuthorSignature confirms the external_reply origin signature extraction:
// channels and anonymous-admin chats carry one; other origins do not.
func TestAuthorSignature(t *testing.T) {
	if got := authorSignature(gotgbot.MessageOriginChannel{AuthorSignature: "Editor"}); got != "Editor" {
		t.Errorf("channel signature = %q; want Editor", got)
	}
	if got := authorSignature(gotgbot.MessageOriginChat{AuthorSignature: "Admin"}); got != "Admin" {
		t.Errorf("chat signature = %q; want Admin", got)
	}
	if got := authorSignature(gotgbot.MessageOriginUser{}); got != "" {
		t.Errorf("user origin signature = %q; want empty", got)
	}
}
