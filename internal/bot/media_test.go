package bot

import (
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

func TestDedupeSortMsgs(t *testing.T) {
	mk := func(ids ...int64) []*gotgbot.Message {
		out := make([]*gotgbot.Message, 0, len(ids))
		for _, id := range ids {
			out = append(out, &gotgbot.Message{MessageId: id})
		}
		return out
	}

	// out-of-order arrival (concurrent dispatch) + a redelivered duplicate
	got := dedupeSortMsgs(mk(101, 99, 100, 99))
	want := []int64{99, 100, 101}
	if len(got) != len(want) {
		t.Fatalf("len = %d; want %d (%v)", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].MessageId != id {
			t.Fatalf("got[%d].MessageId = %d; want %d", i, got[i].MessageId, id)
		}
	}
	// must be STRICTLY increasing — the tg.CopyMessages requirement that the
	// album bug violated.
	for i := 1; i < len(got); i++ {
		if got[i].MessageId <= got[i-1].MessageId {
			t.Fatalf("ids not strictly increasing at %d: %v", i, got)
		}
	}

	// empty input is safe
	if out := dedupeSortMsgs(nil); len(out) != 0 {
		t.Fatalf("dedupeSortMsgs(nil) = %v; want empty", out)
	}
}

func TestKindOf(t *testing.T) {
	cases := []struct {
		name string
		msg  *gotgbot.Message
		want string
	}{
		{"audio", &gotgbot.Message{Audio: &gotgbot.Audio{}}, "audio"},
		{"voice", &gotgbot.Message{Voice: &gotgbot.Voice{}}, "voice"},
		{"photo", &gotgbot.Message{Photo: []gotgbot.PhotoSize{{}}}, "photo"},
		{"video", &gotgbot.Message{Video: &gotgbot.Video{}}, "video"},
		{"document", &gotgbot.Message{Document: &gotgbot.Document{}}, "document"},
		{"animation", &gotgbot.Message{Animation: &gotgbot.Animation{}}, "animation"},
		{"video_note", &gotgbot.Message{VideoNote: &gotgbot.VideoNote{}}, "video_note"},
		{"sticker", &gotgbot.Message{Sticker: &gotgbot.Sticker{}}, "sticker"},
		{"text", &gotgbot.Message{Text: "hi"}, "text"},
		{"other", &gotgbot.Message{}, "other"},
	}
	for _, c := range cases {
		if got := kindOf(c.msg); got != c.want {
			t.Errorf("kindOf(%s) = %q; want %q", c.name, got, c.want)
		}
	}
	// an animation also sets Document on real updates; kindOf must report the more
	// specific "audio" ordering is preserved — audio outranks a co-set document.
	if got := kindOf(&gotgbot.Message{Audio: &gotgbot.Audio{}, Document: &gotgbot.Document{}}); got != "audio" {
		t.Errorf("kindOf(audio+document) = %q; want audio", got)
	}
}

func TestMediaType(t *testing.T) {
	// mediaType only distinguishes the three branches handle_media tags on; every
	// other kind (document, voice, ...) collapses to "other".
	cases := []struct {
		msg  *gotgbot.Message
		want string
	}{
		{&gotgbot.Message{Audio: &gotgbot.Audio{}}, "audio"},
		{&gotgbot.Message{Photo: []gotgbot.PhotoSize{{}}}, "photo"},
		{&gotgbot.Message{Video: &gotgbot.Video{}}, "video"},
		{&gotgbot.Message{Document: &gotgbot.Document{}}, "other"},
		{&gotgbot.Message{Voice: &gotgbot.Voice{}}, "other"},
		{&gotgbot.Message{}, "other"},
	}
	for _, c := range cases {
		if got := mediaType(c.msg); got != c.want {
			t.Errorf("mediaType(%q) = %q; want %q", kindOf(c.msg), got, c.want)
		}
	}
}

func TestParseIDs(t *testing.T) {
	got, ok := parseIDs([]string{"10", "20", "30"})
	if !ok || len(got) != 3 || got[0] != 10 || got[1] != 20 || got[2] != 30 {
		t.Fatalf("parseIDs ok=%v got=%v; want [10 20 30]", ok, got)
	}
	// any non-numeric id fails the whole parse (ok=false), so the caller skips the
	// stale-delete rather than deleting the wrong ids.
	if _, ok := parseIDs([]string{"10", "notanumber"}); ok {
		t.Error("parseIDs should fail on a non-numeric id")
	}
	if out, ok := parseIDs(nil); !ok || len(out) != 0 {
		t.Errorf("parseIDs(nil) = (%v,%v); want ([],true)", out, ok)
	}
}

func TestSelectTagTargets(t *testing.T) {
	cap := &gotgbot.Message{Caption: "c"} // OriginalCaptionHTML() != ""
	plain := &gotgbot.Message{}           // OriginalCaptionHTML() == ""

	eq := func(a, b []int) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	cases := []struct {
		name string
		mt   string
		msgs []*gotgbot.Message
		want []int
	}{
		// photo/video: exactly one captioned -> tag THAT item
		{"photo one captioned", "photo", []*gotgbot.Message{plain, cap, plain}, []int{1}},
		// photo/video: two-or-more captioned -> tag ALL
		{"photo two captioned", "photo", []*gotgbot.Message{cap, plain, cap}, []int{0, 1, 2}},
		// photo/video: none captioned -> tag the first
		{"photo none captioned", "photo", []*gotgbot.Message{plain, plain}, []int{0}},
		{"video one captioned", "video", []*gotgbot.Message{cap, plain}, []int{0}},
		// audio -> all, regardless of captions
		{"audio all", "audio", []*gotgbot.Message{cap, plain}, []int{0, 1}},
		// other (document/voice/…) -> all
		{"other all", "other", []*gotgbot.Message{plain, plain, plain}, []int{0, 1, 2}},
		// empty -> nil
		{"empty", "photo", nil, nil},
	}
	for _, c := range cases {
		if got := selectTagTargets(c.mt, c.msgs); !eq(got, c.want) {
			t.Errorf("%s: selectTagTargets(%q) = %v; want %v", c.name, c.mt, got, c.want)
		}
	}
}

func TestDedupeOrdered(t *testing.T) {
	got := dedupeOrdered([]string{"a", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("dedupeOrdered len = %d (%v); want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dedupeOrdered[%d] = %q; want %q (first-seen order)", i, got[i], want[i])
		}
	}
	if out := dedupeOrdered(nil); len(out) != 0 {
		t.Errorf("dedupeOrdered(nil) = %v; want empty", out)
	}
}
