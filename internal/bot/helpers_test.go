package bot

import "testing"

func TestSanitizeUserHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		// plain names are untouched
		{"Ali", "Ali"},
		{"", ""},
		// safe formatting (from entities) is preserved — users keep their style
		{"<b>Ali</b>", "<b>Ali</b>"},
		{"<i>x</i> <b>y</b>", "<i>x</i> <b>y</b>"},
		// the phishing vector: anchor tags are stripped, the visible text stays
		{`<a href="https://evil.example">click</a>`, "click"},
		{`<a href="tg://user?id=5">x</a> <i>hi</i>`, "x <i>hi</i>"},
		{`pre <a href="x">L</a> post`, "pre L post"},
		// uppercase / attribute variations
		{`<A HREF="x">L</A>`, "L"},
		// a LITERAL "<a>" typed by the user is &lt;-escaped by OriginalHTML, so it
		// is NOT a real tag and must be left intact
		{"&lt;a&gt;", "&lt;a&gt;"},
		// a lone closing tag must also be stripped (no fast-path bypass)
		{"x </a> y", "x  y"},
	}
	for _, c := range cases {
		if got := sanitizeUserHTML(c.in); got != c.want {
			t.Errorf("sanitizeUserHTML(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestChunkString(t *testing.T) {
	// empty stays a single empty chunk
	if got := chunkString("", 5); len(got) != 1 || got[0] != "" {
		t.Fatalf("chunkString(\"\",5) = %v; want [\"\"]", got)
	}
	// exact + remainder
	got := chunkString("abcdefg", 3)
	want := []string{"abc", "def", "g"}
	if len(got) != len(want) {
		t.Fatalf("chunkString len = %d; want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk[%d] = %q; want %q", i, got[i], want[i])
		}
	}
	// splits on RUNE boundaries (Persian/multi-byte must never be cut mid-rune)
	for _, ch := range chunkString("سلامدنیا", 3) {
		for _, r := range ch {
			_ = r // iterating decodes; an invalid split would yield U+FFFD
		}
		if !utf8Valid(ch) {
			t.Fatalf("chunk %q is not valid UTF-8 (rune split)", ch)
		}
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

func TestFmtText(t *testing.T) {
	if got := fmtText("a %s b %s", "1", "2"); got != "a 1 b 2" {
		t.Errorf("fmtText = %q", got)
	}
	// a stray '%' in the template or args must be inert (no Printf verbs)
	if got := fmtText("100%% sure: %s", "x%y"); got != "100%% sure: x%y" {
		t.Errorf("fmtText %% handling = %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("hello", 3); got != "hel…" {
		t.Errorf("truncate = %q; want hel…", got)
	}
	// truncates by rune, not byte (no broken multi-byte chars)
	if got := truncate("سلام", 2); got != "سل…" {
		t.Errorf("truncate persian = %q; want سل…", got)
	}
}
