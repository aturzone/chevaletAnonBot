package bot

import (
	"sync"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// aiQueue ports modules/Global/ai_queue.py (AIQueueManager): an in-memory FIFO
// of GM-group messages awaiting an AI reply. Safe for concurrent use (the GM
// chat handler appends; the ai_responser worker pops).
type aiQueue struct {
	mu    sync.Mutex
	items []*gotgbot.Message
}

// aiQueueMax bounds the in-memory queue so a slow/down AI endpoint (or a flood
// of GM-group replies-to-bot) can't grow it without limit. Excess is dropped —
// the AI reply is best-effort.
const aiQueueMax = 500

func newAIQueue() *aiQueue { return &aiQueue{} }

// add appends a message (add_to_queue); it drops the message (returns false)
// when the queue is at capacity.
func (q *aiQueue) add(m *gotgbot.Message) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) >= aiQueueMax {
		return false
	}
	q.items = append(q.items, m)
	return true
}

// popFront removes and returns the first queued message (queue[0] +
// delete_item(0)); ok is false when the queue is empty.
func (q *aiQueue) popFront() (*gotgbot.Message, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil, false
	}
	m := q.items[0]
	q.items = q.items[1:]
	return m, true
}
