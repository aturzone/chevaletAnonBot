package encoder

import (
	"strconv"
	"strings"
	"testing"
)

const testBotToken = "1234567890:AA-fake-token-for-tests-only_xyz"

func TestTokenRoundTrip(t *testing.T) {
	tc := NewTokenCipher(testBotToken)
	for _, uid := range []int64{1, 1000000001, 1000000002, 999999999999, 1<<52 - 1, 0} {
		tok, err := tc.Seal(uid, nil)
		if err != nil {
			t.Fatalf("Seal(%d): %v", uid, err)
		}
		got, ok := tc.Open(tok, nil)
		if !ok || got != uid {
			t.Errorf("Open(Seal(%d)) = (%d,%v); want (%d,true)", uid, got, ok, uid)
		}
	}
}

func TestTokenUnlinkable(t *testing.T) {
	tc := NewTokenCipher(testBotToken)
	uid := int64(1000000002)
	t1, _ := tc.Seal(uid, nil)
	t2, _ := tc.Seal(uid, nil)
	// Same sender, two messages -> the tokens in the buttons MUST differ (this is
	// the whole point: a recipient can't link them without the key).
	if t1 == t2 {
		t.Fatal("two Seals of the same uid produced identical tokens (linkable!)")
	}
	// ...yet both open to the same uid for the bot.
	if u1, _ := tc.Open(t1, nil); u1 != uid {
		t.Errorf("t1 opened to %d; want %d", u1, uid)
	}
	// The plaintext uid must not appear verbatim in the token.
	if strings.Contains(t1, strconv.FormatInt(uid, 10)) {
		t.Errorf("token %q leaks the decimal uid", t1)
	}
}

func TestTokenTamperRejected(t *testing.T) {
	tc := NewTokenCipher(testBotToken)
	tok, _ := tc.Seal(1000000002, nil)
	b := []byte(tok)
	// Every position, not a sample of three: the flip is cheap and a gap here is
	// a gap in the tamper guarantee. This used to test only 0, len/2 and len-1,
	// and the len-1 case was the one that misbehaved (see below).
	for i := range b {
		orig := b[i]
		b[i] = flipChar(orig)
		if _, ok := tc.Open(string(b), nil); ok {
			t.Errorf("tampered token (byte %d of %d) opened; want reject", i, len(b))
		}
		b[i] = orig
	}
}

// TestTokenCanonical pins down that a token has exactly ONE valid string form.
//
// The last base64 char carries 2 bits that are not payload (23 bytes = 184 bits
// into 31 chars = 186 bits). While Open used the non-strict decoder those bits
// were ignored, so all 4 values of that char opened to the same uid, and
// TestTokenTamperRejected only noticed when the flip happened to cross that
// boundary — a ~6%-of-runs failure that read as flakiness rather than as the real
// finding. Asserting canonicality directly makes a regression here deterministic.
func TestTokenCanonical(t *testing.T) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	tc := NewTokenCipher(testBotToken)
	uid := int64(1000000002)

	// Seal many tokens so every possible value of the final char gets covered,
	// rather than depending on which one this run's random nonce happened to yield.
	for n := 0; n < 200; n++ {
		tok, err := tc.Seal(uid, nil)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		opened := 0
		for i := 0; i < len(alphabet); i++ {
			variant := tok[:len(tok)-1] + string(alphabet[i])
			if _, ok := tc.Open(variant, nil); ok {
				opened++
				if variant != tok {
					t.Errorf("non-canonical token opened: %q (canonical %q)", variant, tok)
				}
			}
		}
		if opened != 1 {
			t.Fatalf("%d final-char variants of %q opened; want exactly 1", opened, tok)
		}
	}
}

func TestTokenAAD(t *testing.T) {
	tc := NewTokenCipher(testBotToken)
	uid := int64(1000000001)
	tok, _ := tc.Seal(uid, []byte("624600")) // e.g. bound to a message id
	if got, ok := tc.Open(tok, []byte("624600")); !ok || got != uid {
		t.Errorf("Open with matching aad = (%d,%v); want (%d,true)", got, ok, uid)
	}
	if _, ok := tc.Open(tok, []byte("624601")); ok {
		t.Error("Open with a DIFFERENT aad succeeded; aad binding is broken")
	}
	if _, ok := tc.Open(tok, nil); ok {
		t.Error("Open with nil aad succeeded for an aad-sealed token")
	}
}

func TestTokenWrongKey(t *testing.T) {
	a := NewTokenCipher(testBotToken)
	b := NewTokenCipher("a-completely-different-secret")
	tok, _ := a.Seal(1000000002, nil)
	if _, ok := b.Open(tok, nil); ok {
		t.Error("a token sealed under key A opened under key B")
	}
}

func TestTokenKeyring(t *testing.T) {
	old := NewTokenCipher("old-secret")
	tokOld, _ := old.Seal(1000000002, nil)

	// After rotation the current key is "new-secret" but "old-secret" stays in the
	// ring, so buttons minted before rotation still resolve.
	rotated := NewTokenCipher("new-secret", "old-secret")
	if got, ok := rotated.Open(tokOld, nil); !ok || got != 1000000002 {
		t.Errorf("rotated ring failed to open an old-key token: (%d,%v)", got, ok)
	}
	// New seals use the current (first) key, which the OLD cipher can't open.
	tokNew, _ := rotated.Seal(1000000002, nil)
	if _, ok := old.Open(tokNew, nil); ok {
		t.Error("old cipher opened a token sealed with the new current key")
	}
}

func TestTokenBudget(t *testing.T) {
	tc := NewTokenCipher(testBotToken)
	tok, _ := tc.Seal(1<<52-1, nil) // max-length uid
	if len(tok) != 31 {
		t.Errorf("token length = %d; expected 31 chars", len(tok))
	}
	// Worst-case framing: the longest verb + a 10-digit message id must fit 64.
	worst := "report_yes|" + tok + "|" + "9999999999"
	if len(worst) > 64 {
		t.Errorf("worst-case callback_data %q is %d bytes (>64)", worst, len(worst))
	}
}

func TestTokenRejectsLegacyAndGarbage(t *testing.T) {
	tc := NewTokenCipher(testBotToken)
	// A legacy EncodeChevaletID output must NOT open as a new token (so the caller
	// falls through to the legacy decoder) — and must never panic.
	legacy := EncodeChevaletID(GenerateChevaletID())
	if _, ok := tc.Open(legacy, nil); ok {
		t.Error("a legacy token opened as a new token (decode-both ambiguity)")
	}
	for _, junk := range []string{"", "!!!not base64!!!", "short", strings.Repeat("A", 200)} {
		if _, ok := tc.Open(junk, nil); ok {
			t.Errorf("garbage %q opened as a token", junk)
		}
	}
}

func TestTokenNotReady(t *testing.T) {
	tc := NewTokenCipher("") // no key
	if tc.Ready() {
		t.Fatal("empty-secret cipher reports Ready")
	}
	if _, err := tc.Seal(1, nil); err == nil {
		t.Error("Seal on a keyless cipher must fail closed")
	}
	if _, ok := tc.Open("anything", nil); ok {
		t.Error("Open on a keyless cipher must return false")
	}
}

// flipChar returns a different base64url char than c.
func flipChar(c byte) byte {
	if c == 'A' {
		return 'B'
	}
	return 'A'
}
