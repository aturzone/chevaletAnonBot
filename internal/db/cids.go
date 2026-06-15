package db

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/aturzone/chevaletAnonBot/internal/config"
	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// queryStrings collects a single-column string result set.
func (db *DB) queryStrings(ctx context.Context, sql string, args ...any) ([]string, error) {
	rows, err := db.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// GetCIDs returns a user's cids ordered by insertion (id ASC). Mirrors
// DBHandler.get_cids.
func (db *DB) GetCIDs(ctx context.Context, uid string) ([]string, error) {
	return db.queryStrings(ctx, `SELECT cid FROM cids WHERE uid=$1 ORDER BY id ASC`, uid)
}

// GetAllCIDs returns every cid. Mirrors DBHandler.get_all_cids.
func (db *DB) GetAllCIDs(ctx context.Context) ([]string, error) {
	return db.queryStrings(ctx, `SELECT cid FROM cids`)
}

// GetUIDByCID returns the uid that owns a cid, or "" when the cid is unknown.
// Mirrors DBHandler.get_uid_by_cid (which returned None for a missing cid).
func (db *DB) GetUIDByCID(ctx context.Context, cid string) (string, error) {
	return db.queryOptString(ctx, `SELECT uid FROM cids WHERE cid=$1`, cid)
}

// AddCID inserts a cid for a user, regenerating on the (astronomically rare)
// unique collision up to config.MaxTryAddCID times. Returns false if every
// attempt collided.
//
// Mirrors DBHandler.add_cid, fixing the original's retry path: it called
// int(generate_cid()), which would have raised ValueError on the non-numeric
// cid string — meaning a real collision crashed instead of retrying.
func (db *DB) AddCID(ctx context.Context, uid, cid string) (bool, error) {
	for try := 0; try < config.MaxTryAddCID; try++ {
		tag, err := db.pool.Exec(ctx,
			`INSERT INTO cids (uid, cid) VALUES ($1, $2) ON CONFLICT (cid) DO NOTHING`,
			uid, cid,
		)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() > 0 {
			return true, nil
		}
		cid = encoder.GenerateCID(10) // collision -> new cid and retry
	}
	return false, nil
}

// RemoveCID mirrors DBHandler.rm_cid.
func (db *DB) RemoveCID(ctx context.Context, uid, cid string) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM cids WHERE cid=$1 AND uid=$2`, cid, uid)
	return err
}

// SetCID renames a cid. Mirrors DBHandler.set_cid.
func (db *DB) SetCID(ctx context.Context, newCID, oldCID string) error {
	_, err := db.pool.Exec(ctx, `UPDATE cids SET cid=$1 WHERE cid=$2`, newCID, oldCID)
	return err
}
