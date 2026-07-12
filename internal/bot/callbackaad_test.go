package bot

import (
	"testing"

	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

const aadTestSecret = "test-bot-token-for-aad-binding"

// tryResolve mirrors resolveTargetUID's token path (callbackaad.go / cidchid.go):
// Open under the callback's aad, then a single Open(nil) back-compat BRIDGE for
// pre-binding tokens. It omits the legacy chevaletid/cid fallbacks, which are
// irrelevant to the aad-binding property under test.
func tryResolve(tc *encoder.TokenCipher, token string, aad []byte) (int64, bool) {
	if u, ok := tc.Open(token, aad); ok {
		return u, true
	}
	if aad != nil {
		if u, ok := tc.Open(token, nil); ok {
			return u, true
		}
	}
	return 0, false
}

// TestMidBindingRejectsTamperedID is the core S-0001 property: a token minted for
// a specific message id must not resolve once that id is edited in callback_data.
func TestMidBindingRejectsTamperedID(t *testing.T) {
	tc := encoder.NewTokenCipher(aadTestSecret)
	const uid int64 = 500123
	tok, _ := tc.Seal(uid, midAAD("100"))

	if u, ok := tryResolve(tc, tok, midAAD("100")); !ok || u != uid {
		t.Fatalf("legit resolve = (%d,%v); want (%d,true)", u, ok, uid)
	}
	// A tampered mid must open under NEITHER the mismatched aad NOR the nil bridge.
	if _, ok := tryResolve(tc, tok, midAAD("9999")); ok {
		t.Error("tampered mid resolved; S-0001 mid-binding is broken")
	}
}

// TestBlockTokenIsNotSkeletonKey is the Fable BLOCKER-2 regression. A live token
// sealed for a NON-mid verb (block) must not open under a mid aad or via the nil
// bridge — otherwise an attacker grabs the bare block token that ships in every
// message and replays it into seen/report against an arbitrary id.
func TestBlockTokenIsNotSkeletonKey(t *testing.T) {
	tc := encoder.NewTokenCipher(aadTestSecret)
	const uid int64 = 700456
	blockTok, _ := tc.Seal(uid, blockAAD())

	if _, ok := tryResolve(tc, blockTok, midAAD("42")); ok {
		t.Error("block token opened under a mid aad — it is a skeleton key (BLOCKER 2)")
	}
	if u, ok := tryResolve(tc, blockTok, blockAAD()); !ok || u != uid {
		t.Fatalf("block token self-resolve = (%d,%v); want (%d,true)", u, ok, uid)
	}
}

// TestNilBridgeOnlyHelpsOldTokens: a pre-binding nil-sealed token still resolves
// (frozen back-compat for already-delivered buttons), while a current non-nil
// token never opens via the bridge under a wrong id.
func TestNilBridgeOnlyHelpsOldTokens(t *testing.T) {
	tc := encoder.NewTokenCipher(aadTestSecret)
	const uid int64 = 900789
	oldTok, _ := tc.Seal(uid, nil)           // a button delivered before this fix
	newTok, _ := tc.Seal(uid, midAAD("100")) // a current button

	if u, ok := tryResolve(tc, oldTok, midAAD("123")); !ok || u != uid {
		t.Errorf("old nil token failed the bridge = (%d,%v); would break delivered buttons", u, ok)
	}
	if _, ok := tryResolve(tc, newTok, midAAD("999")); ok {
		t.Error("current token opened via the nil bridge under a wrong mid — bridge weakens new tokens")
	}
}

// TestDeleteAADSetIntegrity: deleteAAD is order/dup-invariant but set-sensitive,
// and a delete token is bound to its exact id set (dropping OR adding an id fails).
func TestDeleteAADSetIntegrity(t *testing.T) {
	a := deleteAAD([]string{"10", "20", "None"})
	b := deleteAAD([]string{"None", "20", "10", "20"}) // reordered + duplicate
	if string(a) != string(b) {
		t.Errorf("deleteAAD not order/dup-invariant:\n a=%q\n b=%q", a, b)
	}
	if string(deleteAAD([]string{"10", "20"})) == string(a) {
		t.Error("deleteAAD collided for two different id sets")
	}

	tc := encoder.NewTokenCipher(aadTestSecret)
	const uid int64 = 111
	tok, _ := tc.Seal(uid, deleteAAD([]string{"10", "20", "None"}))
	if _, ok := tryResolve(tc, tok, deleteAAD([]string{"10", "20", "None"})); !ok {
		t.Error("legit delete set failed to resolve")
	}
	if _, ok := tryResolve(tc, tok, deleteAAD([]string{"10", "20"})); ok {
		t.Error("delete token opened after an id was DROPPED from the set (S-0001)")
	}
	if _, ok := tryResolve(tc, tok, deleteAAD([]string{"10", "20", "None", "30"})); ok {
		t.Error("delete token opened after an id was ADDED to the set (S-0001)")
	}
}

// TestAADsNeverEmpty guards the invariant that no aad can collide with the nil
// bridge: an empty []byte HMACs identically to nil, which would reintroduce the
// skeleton-key bypass.
func TestAADsNeverEmpty(t *testing.T) {
	if len(midAAD("")) == 0 || len(blockAAD()) == 0 || len(deleteAAD(nil)) == 0 {
		t.Error("an aad is empty; it would collide with the nil back-compat bridge")
	}
}
