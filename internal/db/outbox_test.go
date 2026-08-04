package db

import (
	"context"
	"testing"
)

// TestOutbox exercises the durable send queue against a real PostgreSQL. This is
// the safety net that stops a failed send being lost, so the claim/backoff/delete
// cycle has to be right — a bug here either loses messages or delivers them twice.
func TestOutbox(t *testing.T) {
	ctx := context.Background()
	d, err := Connect(ctx, testConfig(t))
	noErr(t, err, "Connect")
	defer d.Close()
	noErr(t, d.MakeTables(ctx), "MakeTables")
	_, _ = d.pool.Exec(ctx, `DELETE FROM outbox`)

	// Empty to begin with, and the status helpers must cope with that.
	depth, err := d.OutboxDepth(ctx)
	noErr(t, err, "OutboxDepth (empty)")
	if depth != 0 {
		t.Fatalf("depth = %d on an empty queue", depth)
	}
	if age, err := d.OldestOutboxAge(ctx); err != nil || age != 0 {
		t.Errorf("OldestOutboxAge on empty = (%v,%v); want (0,nil)", age, err)
	}

	// Enqueue two deliveries.
	noErr(t, d.EnqueueSend(ctx, 111, "copyMessage", []byte(`{"chat_id":"111","from_chat_id":"222"}`)), "EnqueueSend 1")
	noErr(t, d.EnqueueSend(ctx, 333, "sendMessage", []byte(`{"chat_id":"333","text":"x"}`)), "EnqueueSend 2")

	depth, err = d.OutboxDepth(ctx)
	noErr(t, err, "OutboxDepth")
	if depth != 2 {
		t.Fatalf("depth = %d; want 2", depth)
	}

	// Claiming returns them and, crucially, pushes their next attempt into the
	// FUTURE in the same statement — so a crash mid-delivery cannot cause a tight
	// retry loop on restart.
	items, err := d.ClaimDueSends(ctx, 10)
	noErr(t, err, "ClaimDueSends")
	if len(items) != 2 {
		t.Fatalf("claimed %d items; want 2", len(items))
	}
	for _, it := range items {
		if it.Attempts != 1 {
			t.Errorf("claimed item has attempts=%d; want 1 (claim must count the try)", it.Attempts)
		}
		if len(it.Params) == 0 || it.Method == "" || it.ChatID == 0 {
			t.Errorf("claimed item is missing data: %+v", it)
		}
	}

	// A second claim right away must return NOTHING: the backoff is in effect, so
	// two ticks in a row cannot double-send the same message.
	again, err := d.ClaimDueSends(ctx, 10)
	noErr(t, err, "ClaimDueSends (immediately again)")
	if len(again) != 0 {
		t.Errorf("claimed %d items again immediately; backoff is not being applied — this would double-send", len(again))
	}

	// Delivering one removes it.
	noErr(t, d.DeleteSend(ctx, items[0].ID), "DeleteSend")
	depth, err = d.OutboxDepth(ctx)
	noErr(t, err, "OutboxDepth after delete")
	if depth != 1 {
		t.Fatalf("depth = %d after delivering one; want 1", depth)
	}

	// The oldest-age helper reports something sane once a row is waiting.
	if _, err := d.OldestOutboxAge(ctx); err != nil {
		t.Errorf("OldestOutboxAge: %v", err)
	}

	// Giving up: an item that used its attempts is dropped, so an undeliverable
	// message is not retried forever.
	_, err = d.pool.Exec(ctx, `UPDATE outbox SET attempts = 8`)
	noErr(t, err, "exhaust attempts")
	n, err := d.DropExhaustedSends(ctx, 8)
	noErr(t, err, "DropExhaustedSends")
	if n != 1 {
		t.Errorf("dropped %d exhausted items; want 1", n)
	}
	depth, err = d.OutboxDepth(ctx)
	noErr(t, err, "OutboxDepth final")
	if depth != 0 {
		t.Errorf("depth = %d after dropping the exhausted item; want 0", depth)
	}

	// An item still under its attempt budget must NOT be dropped.
	noErr(t, d.EnqueueSend(ctx, 444, "sendMessage", []byte(`{"chat_id":"444"}`)), "EnqueueSend 3")
	n, err = d.DropExhaustedSends(ctx, 8)
	noErr(t, err, "DropExhaustedSends (fresh item)")
	if n != 0 {
		t.Errorf("dropped %d fresh items; a deliverable message was thrown away", n)
	}

	_, _ = d.pool.Exec(ctx, `DELETE FROM outbox`)
}
