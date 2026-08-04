package db

import (
	"context"
	"time"
)

// The durable send outbox.
//
// Sends stay SYNCHRONOUS: a handler still gets the new message id back, so the
// reply/seen/block buttons and the "sent" confirmation work exactly as before.
// This is the safety net underneath that — when a send fails for a reason that
// might succeed later (a Telegram rate limit, a network blip, the process being
// restarted mid-send), the call is written here and retried until it lands.
//
// Before this, such a send was simply lost: the user was told to try again, or in
// the restart case told nothing at all.
//
// Deliberately NOT a log of what was sent. A row exists only while a delivery is
// outstanding and is deleted the moment it succeeds, so this does not become a
// record of who messaged whom.

// OutboxItem is one pending delivery.
type OutboxItem struct {
	ID       int64
	ChatID   int64
	Method   string
	Params   []byte // the raw JSON params of the original API call
	Attempts int
}

// EnqueueSend records a failed send for retry.
func (db *DB) EnqueueSend(ctx context.Context, chatID int64, method string, params []byte) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO outbox (chat_id, method, params) VALUES ($1,$2,$3)`,
		chatID, method, params)
	return err
}

// ClaimDueSends takes up to limit deliveries that are due, and schedules their next
// attempt in the same statement.
//
// Claim and reschedule are atomic on purpose: if the process dies mid-delivery the
// row is already pushed into the future, so it is retried later rather than being
// hammered in a tight loop on restart. FOR UPDATE SKIP LOCKED keeps two workers (or
// two bot instances during a deploy overlap) from picking up the same row.
//
// Backoff is exponential on the attempt count and capped, so a chat that keeps
// failing is retried on a widening interval instead of consuming the send budget.
func (db *DB) ClaimDueSends(ctx context.Context, limit int) ([]OutboxItem, error) {
	rows, err := db.pool.Query(ctx,
		`UPDATE outbox
		    SET attempts = attempts + 1,
		        next_try = now() + (interval '1 second' *
		                            least(900, power(4, least(attempts + 1, 6))))
		  WHERE id IN (
		        SELECT id FROM outbox
		         WHERE next_try <= now()
		         ORDER BY id
		         LIMIT $1
		         FOR UPDATE SKIP LOCKED)
		RETURNING id, chat_id, method, params, attempts`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		if err := rows.Scan(&it.ID, &it.ChatID, &it.Method, &it.Params, &it.Attempts); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DeleteSend removes a delivered (or permanently undeliverable) item.
func (db *DB) DeleteSend(ctx context.Context, id int64) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM outbox WHERE id=$1`, id)
	return err
}

// DropExhaustedSends removes items that have used up their attempts, returning how
// many went. Giving up is deliberate: a message nobody can receive — the account is
// gone, the bot is blocked — must not be retried forever.
func (db *DB) DropExhaustedSends(ctx context.Context, maxAttempts int) (int64, error) {
	tag, err := db.pool.Exec(ctx, `DELETE FROM outbox WHERE attempts >= $1`, maxAttempts)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// OutboxDepth is how many deliveries are outstanding, for /admin_stats.
func (db *DB) OutboxDepth(ctx context.Context) (int, error) {
	var n int
	err := db.pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&n)
	return n, err
}

// OldestOutboxAge is how long the oldest outstanding delivery has been waiting; 0
// when the queue is empty. A number that grows is the signal that deliveries are
// not draining.
func (db *DB) OldestOutboxAge(ctx context.Context) (time.Duration, error) {
	var secs *float64
	err := db.pool.QueryRow(ctx,
		`SELECT EXTRACT(EPOCH FROM (now() - min(created_at))) FROM outbox`).Scan(&secs)
	if err != nil || secs == nil {
		return 0, err
	}
	return time.Duration(*secs) * time.Second, nil
}
