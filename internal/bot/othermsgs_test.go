package bot

import "testing"

// TestContainsField pins the /cancel detection that drives the catch-all's
// "nothing to cancel" branch: whole-token match over whitespace-separated fields
// (Go strings.Fields, ~ Python `v in s.split()`).
func TestContainsField(t *testing.T) {
	cases := []struct {
		s, v string
		want bool
	}{
		{"/cancel", "/cancel", true},
		{"a /cancel b", "/cancel", true},
		{"a\t/cancel\nb", "/cancel", true}, // Fields splits on any whitespace run
		{"/cancelfoo", "/cancel", false},   // not a whole token
		{"/canceled", "/cancel", false},
		{"cancel", "/cancel", false},
		{"/cancel@BotName", "/cancel", false},
		{"", "/cancel", false},
		{"x y z", "", false},
	}
	for _, c := range cases {
		if got := containsField(c.s, c.v); got != c.want {
			t.Errorf("containsField(%q,%q) = %v; want %v", c.s, c.v, got, c.want)
		}
	}
}
