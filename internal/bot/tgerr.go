package bot

import (
	"errors"
	"net/url"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5/pgconn"
)

// These predicates classify gotgbot API errors the way modules/Global/
// decorators.py classified PTB exceptions via str(e). They match the raw
// Telegram error descriptions case-insensitively (substring), which is
// equivalent to — and a little more robust than — the original exact-string
// checks.

func asTelegramError(err error) *gotgbot.TelegramError {
	var te *gotgbot.TelegramError
	if errors.As(err, &te) {
		return te
	}
	return nil
}

func descContains(err error, sub string) bool {
	te := asTelegramError(err)
	if te == nil {
		return false
	}
	return strings.Contains(strings.ToLower(te.Description), strings.ToLower(sub))
}

// errBotBlocked: "Forbidden: bot was blocked by the user".
func errBotBlocked(err error) bool { return descContains(err, "bot was blocked by the user") }

// errBotNotMember: "Forbidden: bot is not a member of the channel chat".
func errBotNotMember(err error) bool {
	return descContains(err, "bot is not a member of the channel chat")
}

// errReplyNotFound: "Bad Request: message to be replied not found".
func errReplyNotFound(err error) bool { return descContains(err, "message to be replied not found") }

// errMessageIDInvalid: "Bad Request: MESSAGE_ID_INVALID".
func errMessageIDInvalid(err error) bool { return descContains(err, "MESSAGE_ID_INVALID") }

// errQueryTooOld: "Bad Request: query is too old and response timeout expired …".
func errQueryTooOld(err error) bool { return descContains(err, "query is too old") }

// errForbidden matches any HTTP 403 "Forbidden: …" Telegram error, mirroring the
// PTB `except Forbidden` in is_reply_to_channel (e.g. a private channel the bot
// was never added to).
func errForbidden(err error) bool {
	te := asTelegramError(err)
	return te != nil && te.Code == 403
}

// isDBError reports whether err is a PostgreSQL/database failure, mirroring the
// Python prep's `except (psycopg2.Error, psycopg2.DatabaseError)`: a server-side
// SQL error (PgError) or a failure to establish a connection (ConnectError).
// Both warrant the user-facing "database problem" reply and an ERROR_CHAT_ID
// report. (A clean separation from isNetworkError: DB failures are pgconn types,
// never a *url.Error.)
func isDBError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return true
	}
	var connErr *pgconn.ConnectError
	return errors.As(err, &connErr)
}

// isNetworkError reports whether err is a transport-level failure talking to
// Telegram — not a Telegram API error (those carry a TelegramError) — mirroring
// the Python prep's `except NetworkError`. gotgbot speaks HTTP via net/http, so
// such failures surface as a *url.Error.
func isNetworkError(err error) bool {
	if asTelegramError(err) != nil {
		return false
	}
	var uerr *url.Error
	return errors.As(err, &uerr)
}
