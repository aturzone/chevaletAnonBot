package bot

import "testing"

// TestStripFormatChars locks the Unicode Cf (format-char) removal that mirrors
// Python's unicodedata.category(c)=="Cf" filter: ZWNJ/ZWJ/BOM are dropped while
// ordinary letters (incl. Persian), digits, spaces and emoji are preserved.
// The format chars are built from code points (a literal U+FEFF mid-source is an
// "illegal byte order mark", and the others are invisible).
func TestStripFormatChars(t *testing.T) {
	zwnj := string(rune(0x200C)) // ZERO WIDTH NON-JOINER
	zwj := string(rune(0x200D))  // ZERO WIDTH JOINER
	bom := string(rune(0xFEFF))  // BOM / ZERO WIDTH NO-BREAK SPACE

	persianMi := string([]rune{0x0645, 0x06CC})          // "می"
	persianRud := string([]rune{0x0631, 0x0648, 0x062F}) // "رود"
	cases := []struct{ in, want string }{
		{"a" + zwnj + "b" + zwj + "c" + bom, "abc"},
		{"سلام", "سلام"},                                        // no format chars -> unchanged
		{persianMi + zwnj + persianRud, persianMi + persianRud}, // ZWNJ removed, letters kept
		{"plain ascii 123", "plain ascii 123"},
		{"emoji \U0001F600 ok", "emoji \U0001F600 ok"},
		{"", ""},
	}
	for _, c := range cases {
		if got := stripFormatChars(c.in); got != c.want {
			t.Errorf("stripFormatChars(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
