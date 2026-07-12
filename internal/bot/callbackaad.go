package bot

import (
	"sort"
	"strings"
)

// callbackaad.go builds the Additional-Authenticated-Data that binds each inline
// button's callback token to the thing it is allowed to act on, closing S-0001
// (BOLA: a callback trusted an attacker-editable message id from callback_data).
//
// The token cipher authenticates aad but does NOT transmit it, so binding costs
// zero callback_data bytes. The bot re-derives the SAME aad when it opens the
// token; if the attacker edits the embedded message id(s) the derived aad no
// longer matches and Open fails, so the callback is rejected.
//
// CRITICAL invariant (see the S-0001 review): every LIVE token the bot mints must
// use a NON-NIL aad. resolveTargetUID keeps an Open(token,nil) bridge so buttons
// delivered before this change still resolve; a single nil-sealed live token
// would be a mid-agnostic skeleton key that the bridge would open regardless of
// the acted-on id, defeating the whole fix. Hence blockAAD is a constant (not
// nil) even though block/unblock act on no message id. All aads are non-empty, so
// none can collide with the nil bridge (an empty aad HMACs identically to nil).

// midAAD binds a single acted-on message id (answer / seen / report / report_yes,
// and the isAnswer reply path). The verb is intentionally NOT bound: one sender
// token is shared across answer/seen/report, so binding a verb would break that
// sharing — and the id alone is what S-0001 is about.
func midAAD(mid string) []byte { return []byte("m:" + mid) }

// blockAAD is the constant binding for block/unblock tokens. block has no message
// id (and block<->unblock reuse one token across the verb swap), so there is
// nothing per-message to bind — but it must still be non-nil (see the invariant
// above). A constant is enough: block/unblock act on the resolved uid only.
func blockAAD() []byte { return []byte("b") }

// deleteAAD binds the WHOLE set of deletable message ids to a delete token.
// packDeleteButtons may split the ids across several buttons that all share the
// one token, and deleteMsgClbk harvests them back from EVERY button in
// unspecified order — so the set is deduped and lexically sorted here to give the
// seal side and the harvest side a byte-identical aad regardless of packing or
// iteration order. The literal "None" notify slot is kept verbatim (it is never
// ParseInt'd), so both sides agree on it too. Lexical (not numeric) sort is
// deliberate: it keeps "None" orderable alongside the numeric ids.
func deleteAAD(mids []string) []byte {
	seen := make(map[string]struct{}, len(mids))
	uniq := make([]string, 0, len(mids))
	for _, m := range mids {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		uniq = append(uniq, m)
	}
	sort.Strings(uniq)
	return []byte("d:" + strings.Join(uniq, "|"))
}
