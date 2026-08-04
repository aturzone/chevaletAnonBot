package bot

import (
	"context"
	"testing"
	"time"
)

// TestOneSpammerCannotStallTheBot is the regression test for the freeze.
//
// The dispatcher is serial (MaxRoutines=1), so any wait inside a send is time no
// other user's update is being processed. With a 3-second cap, one user pushing
// their per-minute allowance parked the pipeline for tens of seconds and the bot
// looked dead to everybody else.
//
// This asserts the budget directly: a realistic flood from ONE chat must not cost
// more than a small, bounded amount of total dispatcher time.
func TestOneSpammerCannotStallTheBot(t *testing.T) {
	l, _, slept := newTestLimiter()
	c := &limitedClient{inner: &fakeInner{}, limiter: l}

	// 40 sends is the per-user allowance (sendRateMax) — the most one user can push
	// through in a minute before allowSend starts refusing them.
	const floodSize = 40
	params := map[string]any{"chat_id": "1988454449"} // one private chat
	for i := 0; i < floodSize; i++ {
		if _, err := c.RequestWithContext(context.Background(), "t", "copyMessage", params, nil); err != nil {
			t.Fatalf("send %d in a flood was refused (%v); pacing must never drop a message", i, err)
		}
	}

	var total time.Duration
	for _, d := range *slept {
		if d > maxInlineWait {
			t.Errorf("a single send slept %v, past the %v cap", d, maxInlineWait)
		}
		total += d
	}

	// The whole flood must not monopolise the dispatcher. 40 sends × 300ms is the
	// hard ceiling; anything near the old 3s cap (which reached ~2 minutes) means a
	// spammer can starve other users again.
	if max := floodSize * maxInlineWait; total > max {
		t.Errorf("a %d-message flood cost %v of dispatcher time; ceiling is %v", floodSize, total, max)
	}
	if total > 15*time.Second {
		t.Errorf("a single user's flood blocked the pipeline for %v; other users would see a dead bot", total)
	}
}

// TestFloodFromManyUsersStaysResponsive: the realistic shape is many different
// chats at once, where the per-chat limits do not apply and only the global one
// does. That path must stay cheap.
func TestFloodFromManyUsersStaysResponsive(t *testing.T) {
	l, _, slept := newTestLimiter()
	c := &limitedClient{inner: &fakeInner{}, limiter: l}

	for i := 0; i < 200; i++ {
		params := map[string]any{"chat_id": itoa(1000000 + i)} // 200 distinct users
		if _, err := c.RequestWithContext(context.Background(), "t", "copyMessage", params, nil); err != nil {
			t.Fatalf("send %d across many chats was refused: %v", i, err)
		}
	}

	var total time.Duration
	for _, d := range *slept {
		total += d
	}
	// 200 messages spread across users is ~9s of real time at 22/s, and the limiter
	// must not add more waiting than that to the dispatcher.
	if total > 12*time.Second {
		t.Errorf("200 sends across 200 chats cost %v of dispatcher time; too much", total)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
