// Package db ports modules/Global/database.py to Go using pgx.
//
// It keeps the EXACT same schema as the Python original (the bot will share the
// live production database), so MakeTables uses `CREATE TABLE IF NOT EXISTS`
// with identical column definitions — a no-op against the existing prod tables,
// and a faithful bootstrap for a fresh dev/test database.
//
// The Python DBHandler ran on a psycopg2 connection with autocommit=True, i.e.
// every statement committed immediately and there were no multi-statement
// transactions. pgxpool gives the same semantics: each method below acquires a
// pooled connection, runs one statement (implicit commit), and releases it.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aturzone/chevaletAnonBot/internal/config"
)

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505). It lets handlers reproduce the Python
// `except IntegrityError` paths (e.g. a cid rename racing another user).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// DB owns the connection pool and the per-install defaults that the Python
// schema baked into its DDL / inserts.
type DB struct {
	pool            *pgxpool.Pool
	defaultCIDLimit int
	defaultAudioTag string
	maxNameLength   int
}

// Connect opens the pool (mirroring psycopg2's SimpleConnectionPool minconn=1,
// maxconn=30) and verifies connectivity.
func Connect(ctx context.Context, cfg *config.Config) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}
	// Set connection fields explicitly so passwords with special characters
	// never need DSN escaping.
	port := cfg.DBPort
	if port == 0 {
		port = 5432
	}
	poolCfg.ConnConfig.Host = cfg.DBHost
	poolCfg.ConnConfig.Port = uint16(port)
	poolCfg.ConnConfig.User = cfg.DBUser
	poolCfg.ConnConfig.Password = cfg.DBPass
	poolCfg.ConnConfig.Database = cfg.DBName
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolCfg.ConnConfig.RuntimeParams["client_encoding"] = "UTF8"
	poolCfg.MaxConns = 30
	poolCfg.MinConns = 1

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return &DB{
		pool:            pool,
		defaultCIDLimit: cfg.DefaultCIDLimit,
		defaultAudioTag: config.DefaultAudioTag,
		maxNameLength:   cfg.MaxNameLength,
	}, nil
}

// Close releases the pool.
func (db *DB) Close() { db.pool.Close() }

// Pool exposes the underlying pool for advanced callers (jobs/health).
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// MakeTables creates the four tables if they do not exist. The DDL is identical
// to DBHandler.make_tables, including the cid_limit and audio_tag defaults.
func (db *DB) MakeTables(ctx context.Context) error {
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			uid VARCHAR(255) NOT NULL UNIQUE,
			name VARCHAR(255) NOT NULL,
			is_banned BOOLEAN NOT NULL DEFAULT FALSE,
			warning BOOLEAN NOT NULL DEFAULT TRUE,
			seen_option BOOLEAN NOT NULL DEFAULT FALSE,
			wpp BOOLEAN NOT NULL DEFAULT TRUE,
			cid_limit INTEGER NOT NULL DEFAULT %d,
			custom_tag VARCHAR(255),
			audio_tag VARCHAR(255) DEFAULT '%s',
			chevaletid VARCHAR(255))`, db.defaultCIDLimit, db.defaultAudioTag),
		`CREATE TABLE IF NOT EXISTS blocks (
			id SERIAL PRIMARY KEY,
			blocker_uid VARCHAR(255) NOT NULL,
			blocked_uid VARCHAR(255) NOT NULL,
			CONSTRAINT unique_pair UNIQUE (blocker_uid, blocked_uid))`,
		`CREATE TABLE IF NOT EXISTS cids (
			id SERIAL PRIMARY KEY,
			uid VARCHAR(255) NOT NULL,
			cid VARCHAR(255) NOT NULL UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS reports (
			id SERIAL PRIMARY KEY,
			reported_id VARCHAR(255) NOT NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("db: make tables: %w", err)
		}
	}
	return nil
}

// --- shared scan helpers ------------------------------------------------------

// queryOptString returns "" when the row is missing OR the column is NULL,
// matching the Python getters that returned None in both cases.
func (db *DB) queryOptString(ctx context.Context, sql string, args ...any) (string, error) {
	var s *string
	err := db.pool.QueryRow(ctx, sql, args...).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if s == nil {
		return "", nil
	}
	return *s, nil
}

// queryBool scans a single boolean. ErrNoRows propagates as an error because the
// callers (settings/getters) only run for users known to exist (prep ensures
// the row via init_user), matching the Python code that would raise on a missing
// row.
func (db *DB) queryBool(ctx context.Context, sql string, args ...any) (bool, error) {
	var b bool
	err := db.pool.QueryRow(ctx, sql, args...).Scan(&b)
	return b, err
}

// truncateRunes mirrors Python's `str(name)[:MAX_NAME_LENGTH]` (slicing by code
// point, not byte).
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
