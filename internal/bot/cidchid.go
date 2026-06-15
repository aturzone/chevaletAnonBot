package bot

import (
	"context"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// handleCIDOrChID mirrors myhelpers.handle_cid_or_chid. The token comes from a
// button's callback_data; on OLD messages (pre-chevaletid) it may be a plain
// cid rather than an encoded chevaletid. The function returns the ENCODED
// target chevaletid.
//
// ok=false is the Go equivalent of the Python END sentinel: the link is gone
// (a "link changed" reply has already been sent), and the caller must stop.
// A non-nil error is a genuine failure routed to the central error hook.
func (b *Bot) handleCIDOrChID(ctx context.Context, msg *gotgbot.Message, token string) (encChid string, ok bool, err error) {
	// It may be old and be a chevaletid: decodes cleanly AND resolves to a user.
	if plain, decoded := encoder.DecodeChevaletID(token); decoded {
		uid, derr := b.DB.GetUIDByChevaletID(ctx, plain)
		if derr != nil {
			return "", false, derr
		}
		if uid != "" {
			return token, true, nil // it's a chevaletid; token is already encoded
		}
	}

	// Otherwise treat it as a cid.
	targetUID, err := b.DB.GetUIDByCID(ctx, token)
	if err != nil {
		return "", false, err
	}
	// No owner -> the link was deleted or changed.
	if targetUID == "" {
		if _, e := msg.Reply(b.TG, txtLinkDeletedOrChanged, nil); e != nil {
			return "", false, e
		}
		return "", false, nil
	}

	// Ensure the target has a chevaletid (mint one if missing).
	targetChid, err := b.DB.GetChevaletIDByUID(ctx, targetUID)
	if err != nil {
		return "", false, err
	}
	if targetChid == "" {
		targetChid = encoder.GenerateChevaletID()
		// Python's set_chevaletid always returned True, so its "failure" branch
		// (an extra reply + ERROR_CHAT notice) was dead code; a real DB failure
		// instead surfaced through psycopg2 to the central error handler. We do
		// the same here: a SetChevaletID error propagates to onError.
		if err := b.DB.SetChevaletID(ctx, targetUID, targetChid); err != nil {
			return "", false, err
		}
	}
	return encoder.EncodeChevaletID(targetChid), true, nil
}
