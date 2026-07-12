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
// aad is the purpose-binding for the current token generation (see callbackaad.go):
// the acted-on message id for answer/seen/report, a constant for block/unblock,
// the whole id set for delete. It authenticates the id(s) embedded in the SAME
// callback_data so a tampered id fails Open — the S-0001 fix. Callers MUST pass
// the non-nil aad matching how the button was minted.
//
//   - Open(token, aad) matches a current, correctly-bound token.
//   - If that fails AND aad != nil, Open(token, nil) is a back-compat BRIDGE for
//     buttons delivered before mid-binding shipped (they were sealed with nil).
//     Those resolve UNBOUND, exactly as they did before — the frozen residual of
//     S-0001 for already-delivered buttons, which ages out as messages do. It is
//     safe: a current token is sealed with a non-nil aad, so it never opens under
//     nil; only genuinely-old tokens take the bridge.
//
// Trying the sealed cipher FIRST is safe: its 64-bit MAC makes a legacy token
// opening as a new one a ~2^-64 event, and legacy/garbage inputs return false so
// we fall through. (Ported/renamed from handle_cid_or_chid, which returned an
// encoded chevaletid; callers now get the uid straight, dropping a decode + DB
// lookup each.)
//
// ok=false with err==nil is the "link gone" END sentinel: a "link changed" reply
// was already sent and the caller must stop. A non-nil error routes to onError.
func (b *Bot) resolveTargetUID(ctx context.Context, msg *gotgbot.Message, token string, aad []byte) (uid string, ok bool, err error) {
	// (1) NEW sealed token, bound to this callback's id(s) via aad.
	if u, good := b.Tokens.Open(token, aad); good {
		return strconv.FormatInt(u, 10), true, nil
	}
	// (1b) BRIDGE: a pre-mid-binding token was sealed with nil aad. Only reachable
	// for genuinely-old buttons — a current token's non-nil aad never opens here.
	if aad != nil {
		if u, good := b.Tokens.Open(token, nil); good {
			return strconv.FormatInt(u, 10), true, nil
		}
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
