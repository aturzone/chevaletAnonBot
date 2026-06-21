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
