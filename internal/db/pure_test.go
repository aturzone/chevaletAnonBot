package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestTruncateRunes covers the rune-based name truncation (Python str(name)[:MAX]).
// It is a pure function but the only coverage today is inside the DB-gated
// integration tests, so it is skipped on machines without Postgres.
func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 10, "hello"}, // shorter than max -> unchanged
		{"hello", 5, "hello"},  // exactly max -> unchanged
		{"hello", 3, "hel"},    // longer -> first max runes
		{"", 5, ""},
		{"abc", 0, "abc"},  // max<=0 -> unchanged
		{"abc", -1, "abc"}, // negative -> unchanged
	}
	for _, c := range cases {
		if got := truncateRunes(c.s, c.max); got != c.want {
			t.Errorf("truncateRunes(%q,%d) = %q; want %q", c.s, c.max, got, c.want)
		}
	}

	// truncates by RUNE, not byte: a 4-rune Persian word cut to 2 keeps 2 runes
	// (4 bytes), never a broken multi-byte sequence.
	got := truncateRunes("سلام", 2)
	if n := len([]rune(got)); n != 2 {
		t.Errorf("truncateRunes(persian,2) kept %d runes; want 2 (got %q)", n, got)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	uv := &pgconn.PgError{Code: "23505"}
	if !IsUniqueViolation(uv) {
		t.Error("IsUniqueViolation(23505) should be true")
	}
	if !IsUniqueViolation(fmt.Errorf("insert: %w", uv)) {
		t.Error("IsUniqueViolation should see through a wrap")
	}
	if IsUniqueViolation(&pgconn.PgError{Code: "23503"}) { // foreign_key_violation
		t.Error("IsUniqueViolation(23503) should be false")
	}
	if IsUniqueViolation(errors.New("x")) || IsUniqueViolation(nil) {
		t.Error("IsUniqueViolation should be false for plain/nil errors")
	}
}

func TestIsNoRows(t *testing.T) {
	if !IsNoRows(pgx.ErrNoRows) {
		t.Error("IsNoRows(pgx.ErrNoRows) should be true")
	}
	if !IsNoRows(fmt.Errorf("query: %w", pgx.ErrNoRows)) {
		t.Error("IsNoRows should see through a wrap")
	}
	if IsNoRows(errors.New("x")) || IsNoRows(nil) {
		t.Error("IsNoRows should be false for plain/nil errors")
	}
}
