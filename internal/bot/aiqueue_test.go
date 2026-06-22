package bot

import (
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// TestAIQueueFIFOAndBound covers the AI work queue's FIFO order and the
// aiQueueMax capacity drop (oldest retained, newest dropped).
func TestAIQueueFIFOAndBound(t *testing.T) {
	q := newAIQueue()

	// empty pop
	if _, ok := q.popFront(); ok {
		t.Fatal("popFront on empty queue should return ok=false")
	}

	// FIFO order
	m1 := &gotgbot.Message{MessageId: 1}
	m2 := &gotgbot.Message{MessageId: 2}
	m3 := &gotgbot.Message{MessageId: 3}
	for _, m := range []*gotgbot.Message{m1, m2, m3} {
		if !q.add(m) {
			t.Fatalf("add(%d) should succeed", m.MessageId)
		}
	}
	for _, want := range []int64{1, 2, 3} {
		got, ok := q.popFront()
		if !ok || got.MessageId != want {
			t.Fatalf("popFront = (%v,%v); want id %d", got, ok, want)
		}
	}

	// capacity bound: fill to max, next add is dropped; head stays the oldest.
	q2 := newAIQueue()
	for i := 0; i < aiQueueMax; i++ {
		if !q2.add(&gotgbot.Message{MessageId: int64(i)}) {
			t.Fatalf("add %d within capacity should succeed", i)
		}
	}
	if q2.add(&gotgbot.Message{MessageId: 9999}) {
		t.Fatal("add beyond aiQueueMax should be dropped (return false)")
	}
	if len(q2.items) != aiQueueMax {
		t.Fatalf("queue len = %d; want %d", len(q2.items), aiQueueMax)
	}
	if head, _ := q2.popFront(); head.MessageId != 0 {
		t.Fatalf("oldest item should be retained at head; got id %d", head.MessageId)
	}
}
