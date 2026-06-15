package bot

import (
	"errors"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
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
