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

func newAIQueue() *aiQueue { return &aiQueue{} }

// add appends a message (add_to_queue).
func (q *aiQueue) add(m *gotgbot.Message) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, m)
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
