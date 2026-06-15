package db

import "context"

// AddUser upserts a user with the Python defaults (not banned, warning on, no
// seen, wpp on, default cid_limit, no custom_tag, default audio_tag, no
// chevaletid). Returns true if a new row was inserted, false if uid existed.
// Mirrors DBHandler.add_user.
func (db *DB) AddUser(ctx context.Context, uid, name string) (bool, error) {
	name = truncateRunes(name, db.maxNameLength)
	tag, err := db.pool.Exec(ctx,
		`INSERT INTO users
			(uid, name, is_banned, warning, seen_option, wpp, cid_limit, custom_tag, audio_tag, chevaletid)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (uid) DO NOTHING`,
		uid, name, false, true, false, true, db.defaultCIDLimit, nil, db.defaultAudioTag, nil,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// IsBanned mirrors DBHandler.is_banned.
func (db *DB) IsBanned(ctx context.Context, uid string) (bool, error) {
	return db.queryBool(ctx, `SELECT is_banned FROM users WHERE uid=$1`, uid)
}

// BanAction sets/clears a user's ban flag, first ensuring the row exists (so an
// admin can ban a uid that never started the bot). Mirrors DBHandler.ban_action.
func (db *DB) BanAction(ctx context.Context, uid string, ban bool) error {
	_, _ = db.AddUser(ctx, uid, "-") // ignore error, matching the Python try/except
	_, err := db.pool.Exec(ctx, `UPDATE users SET is_banned=$1 WHERE uid=$2`, ban, uid)
	return err
}

// GetAllUIDs returns every user's uid. Mirrors DBHandler.get_all_uids.
func (db *DB) GetAllUIDs(ctx context.Context) ([]string, error) {
	return db.queryStrings(ctx, `SELECT uid FROM users`)
}

// GetName mirrors DBHandler.get_name ("" when missing).
func (db *DB) GetName(ctx context.Context, uid string) (string, error) {
	return db.queryOptString(ctx, `SELECT name FROM users WHERE uid=$1`, uid)
}

// GetUIDByChevaletID mirrors DBHandler.get_uid_by_chevaletid.
func (db *DB) GetUIDByChevaletID(ctx context.Context, chevaletid string) (string, error) {
	return db.queryOptString(ctx, `SELECT uid FROM users WHERE chevaletid=$1`, chevaletid)
}

// GetChevaletIDByUID mirrors DBHandler.get_chevaletid_by_uid ("" when missing or NULL).
func (db *DB) GetChevaletIDByUID(ctx context.Context, uid string) (string, error) {
	return db.queryOptString(ctx, `SELECT chevaletid FROM users WHERE uid=$1`, uid)
}

// GetAllChevaletIDs returns every non-NULL chevaletid. Mirrors
// DBHandler.get_all_chevaletids (used for cid uniqueness checks).
func (db *DB) GetAllChevaletIDs(ctx context.Context) ([]string, error) {
	return db.queryStrings(ctx, `SELECT chevaletid FROM users WHERE chevaletid IS NOT NULL`)
}

// GetCIDLimit mirrors DBHandler.get_cid_limit.
func (db *DB) GetCIDLimit(ctx context.Context, uid string) (int, error) {
	var n int
	err := db.pool.QueryRow(ctx, `SELECT cid_limit FROM users WHERE uid=$1`, uid).Scan(&n)
	return n, err
}

// GetWarning mirrors DBHandler.get_warning.
func (db *DB) GetWarning(ctx context.Context, uid string) (bool, error) {
	return db.queryBool(ctx, `SELECT warning FROM users WHERE uid=$1`, uid)
}

// GetSeenStatus mirrors DBHandler.get_seen_status.
func (db *DB) GetSeenStatus(ctx context.Context, uid string) (bool, error) {
	return db.queryBool(ctx, `SELECT seen_option FROM users WHERE uid=$1`, uid)
}

// GetWPP mirrors DBHandler.get_wpp.
func (db *DB) GetWPP(ctx context.Context, uid string) (bool, error) {
	return db.queryBool(ctx, `SELECT wpp FROM users WHERE uid=$1`, uid)
}

// GetCustomTag mirrors DBHandler.get_custom_tag ("" when NULL/missing).
func (db *DB) GetCustomTag(ctx context.Context, uid string) (string, error) {
	return db.queryOptString(ctx, `SELECT custom_tag FROM users WHERE uid=$1`, uid)
}

// GetAudioTag mirrors DBHandler.get_audio_tag ("" when NULL/missing).
func (db *DB) GetAudioTag(ctx context.Context, uid string) (string, error) {
	return db.queryOptString(ctx, `SELECT audio_tag FROM users WHERE uid=$1`, uid)
}

// SetName mirrors DBHandler.set_name.
func (db *DB) SetName(ctx context.Context, uid, name string) error {
	_, err := db.pool.Exec(ctx, `UPDATE users SET name=$1 WHERE uid=$2`, name, uid)
	return err
}

// SetCIDLimit mirrors DBHandler.set_cid_limit.
func (db *DB) SetCIDLimit(ctx context.Context, uid string, cidLimit int) error {
	_, err := db.pool.Exec(ctx, `UPDATE users SET cid_limit=$1 WHERE uid=$2`, cidLimit, uid)
	return err
}

// SetWarning mirrors DBHandler.set_warning.
func (db *DB) SetWarning(ctx context.Context, uid string, warning bool) error {
	_, err := db.pool.Exec(ctx, `UPDATE users SET warning=$1 WHERE uid=$2`, warning, uid)
	return err
}

// SetSeenOption mirrors DBHandler.set_seen_option.
func (db *DB) SetSeenOption(ctx context.Context, uid string, seen bool) error {
	_, err := db.pool.Exec(ctx, `UPDATE users SET seen_option=$1 WHERE uid=$2`, seen, uid)
	return err
}

// SetWPP mirrors DBHandler.set_wpp.
func (db *DB) SetWPP(ctx context.Context, uid string, wpp bool) error {
	_, err := db.pool.Exec(ctx, `UPDATE users SET wpp=$1 WHERE uid=$2`, wpp, uid)
	return err
}

// SetCustomTag mirrors DBHandler.set_custom_tag. A nil tag clears the column
// (stores NULL), matching set_custom_tag(uid, None).
func (db *DB) SetCustomTag(ctx context.Context, uid string, tag *string) error {
	_, err := db.pool.Exec(ctx, `UPDATE users SET custom_tag=$1 WHERE uid=$2`, tag, uid)
	return err
}

// SetAudioTag mirrors DBHandler.set_audio_tag. A nil tag stores NULL.
func (db *DB) SetAudioTag(ctx context.Context, uid string, tag *string) error {
	_, err := db.pool.Exec(ctx, `UPDATE users SET audio_tag=$1 WHERE uid=$2`, tag, uid)
	return err
}

// SetChevaletID mirrors DBHandler.set_chevaletid (which always returned True;
// here a real failure surfaces as a non-nil error instead).
func (db *DB) SetChevaletID(ctx context.Context, uid, chevaletid string) error {
	_, err := db.pool.Exec(ctx, `UPDATE users SET chevaletid=$1 WHERE uid=$2`, chevaletid, uid)
	return err
}

// UserStatus returns (is_banned, cid_limit). Mirrors DBHandler.user_status.
func (db *DB) UserStatus(ctx context.Context, uid string) (isBanned bool, cidLimit int, err error) {
	err = db.pool.QueryRow(ctx,
		`SELECT is_banned, cid_limit FROM users WHERE uid=$1`, uid,
	).Scan(&isBanned, &cidLimit)
	return
}

// UserCount mirrors DBHandler.user_count (also used by the hourly DB health check).
func (db *DB) UserCount(ctx context.Context) (int, error) {
	var n int
	err := db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}
