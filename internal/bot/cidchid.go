package bot

import (
	"context"
	"strconv"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// resolveTargetUID resolves a button's callback-data token to the target user's
// uid, accepting three token generations (decode-both migration):
//
//  1. a NEW sealed token — b.Tokens.Open -> uid directly (current, unlinkable);
//  2. an OLD encoded chevaletid — DecodeChevaletID -> chevaletid -> uid;
//  3. a plain cid on very old (pre-chevaletid) buttons -> the link owner's uid.
//
// Trying the sealed cipher FIRST is safe: its 64-bit MAC makes a legacy token
// opening as a new one a ~2^-64 event, and legacy/garbage inputs return false so
// we fall through. (Ported/renamed from handle_cid_or_chid, which returned an
// encoded chevaletid; callers now get the uid straight, dropping a decode + DB
// lookup each.)
//
// ok=false with err==nil is the "link gone" END sentinel: a "link changed" reply
// was already sent and the caller must stop. A non-nil error routes to onError.
func (b *Bot) resolveTargetUID(ctx context.Context, msg *gotgbot.Message, token string) (uid string, ok bool, err error) {
	// (1) NEW sealed token.
	if u, good := b.Tokens.Open(token, nil); good {
		return strconv.FormatInt(u, 10), true, nil
	}

	// (2) OLD encoded chevaletid: decodes cleanly AND resolves to a user.
	if plain, decoded := encoder.DecodeChevaletID(token); decoded {
		u, derr := b.DB.GetUIDByChevaletID(ctx, plain)
		if derr != nil {
			return "", false, derr
		}
		if u != "" {
			return u, true, nil
		}
	}

	// (3) Otherwise treat it as a cid (pre-chevaletid buttons carried the cid).
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

	// Ensure the target still has a chevaletid (mint if missing) — preserves the
	// original side effect for any downstream that resolves by chevaletid.
	targetChid, err := b.DB.GetChevaletIDByUID(ctx, targetUID)
	if err != nil {
		return "", false, err
	}
	if targetChid == "" {
		if err := b.DB.SetChevaletID(ctx, targetUID, encoder.GenerateChevaletID()); err != nil {
			return "", false, err
		}
	}
	return targetUID, true, nil
}
