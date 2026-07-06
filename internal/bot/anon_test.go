package bot

import "testing"

// TestAnonSignature covers the sender-nickname line builder: the enabled/empty
// gating, the emoji-prefix layout, and the anchor sanitisation that stops a
// text_link nickname from rendering as a real link in the recipient's message.
func TestAnonSignature(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		nick    string
		emoji   string
		want    string
	}{
		{"disabled hides everything", false, "روباه آبی", "🦊", ""},
		{"enabled but no name", true, "", "🦊", ""},
		{"name only", true, "روباه آبی", "", "روباه آبی"},
		{"name and emoji", true, "روباه آبی", "🦊", "🦊 روباه آبی"},
		{"disabled with name only", false, "روباه آبی", "", ""},
		{"anchor stripped from name", true, `<a href="http://evil">x</a>روباه`, "🦊", "🦊 xروباه"},
		{"anchor stripped from emoji", true, "روباه", `<a href="http://evil">🦊</a>`, "🦊 روباه"},
		{"safe formatting kept", true, "<b>روباه</b>", "🦊", "🦊 <b>روباه</b>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := anonSignature(c.enabled, c.nick, c.emoji); got != c.want {
				t.Errorf("anonSignature(%v,%q,%q) = %q; want %q", c.enabled, c.nick, c.emoji, got, c.want)
			}
		})
	}
}

// TestWithSig locks the footer composition. The critical property is the empty-sig
// path: it must return base byte-for-byte, so a user who never enabled a nickname
// gets exactly the pre-feature message (the whole feature's no-op guarantee).
func TestWithSig(t *testing.T) {
	cases := []struct {
		name string
		base string
		sig  string
		want string
	}{
		{"no sig keeps base verbatim", "tag\nlink", "", "tag\nlink"},
		{"no sig keeps trailing newline", "tag\n", "", "tag\n"},
		{"no sig keeps empty base", "", "", ""},
		{"sig onto empty base", "", "🦊 روباه", "🦊 روباه"},
		{"sig appended as last line", "custom tag", "🦊 روباه", "custom tag\n🦊 روباه"},
		{"trailing newline trimmed before sig", "tag\n", "🦊 روباه", "tag\n🦊 روباه"},
		{"multiple trailing newlines collapse", "tag\n\n", "🦊 روباه", "tag\n🦊 روباه"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := withSig(c.base, c.sig); got != c.want {
				t.Errorf("withSig(%q,%q) = %q; want %q", c.base, c.sig, got, c.want)
			}
		})
	}
}
