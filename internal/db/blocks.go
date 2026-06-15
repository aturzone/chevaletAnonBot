package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// AddBlock mirrors DBHandler.add_block: true if newly created, false if it
// already existed.
func (db *DB) AddBlock(ctx context.Context, blockerUID, blockedUID string) (bool, error) {
	tag, err := db.pool.Exec(ctx,
		`INSERT INTO blocks (blocker_uid, blocked_uid) VALUES ($1, $2)
		 ON CONFLICT (blocker_uid, blocked_uid) DO NOTHING`,
		blockerUID, blockedUID,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RemoveBlock deletes a block and reports whether a row was actually removed.
//
// The Python remove_block always returned True (so its "wasn't blocked anyway"
// branch was dead code). Exposing the real affected-row count lets the ported
// handler render an accurate message; whether to use it is a behavior decision
// deferred to handler-porting time.
func (db *DB) RemoveBlock(ctx context.Context, blockerUID, blockedUID string) (bool, error) {
	tag, err := db.pool.Exec(ctx,
		`DELETE FROM blocks WHERE blocker_uid=$1 AND blocked_uid=$2`, blockerUID, blockedUID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// UnblockAll mirrors DBHandler.unblock_all.
func (db *DB) UnblockAll(ctx context.Context, uid string) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM blocks WHERE blocker_uid=$1`, uid)
	return err
}

// IsBlocked mirrors DBHandler.is_blocked.
func (db *DB) IsBlocked(ctx context.Context, blockerUID, blockedUID string) (bool, error) {
	var one int
	err := db.pool.QueryRow(ctx,
		`SELECT 1 FROM blocks WHERE blocker_uid=$1 AND blocked_uid=$2 LIMIT 1`,
		blockerUID, blockedUID,
	).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
