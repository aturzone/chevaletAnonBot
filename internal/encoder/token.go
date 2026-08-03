package encoder

// token.go replaces the KEYLESS chevaletid obfuscation (EncodeChevaletID) for
// NEW callback_data with a server-SECRET-keyed, per-emission-randomized token.
//
// WHY: EncodeChevaletID is reversible by anyone (it carries its own key), so once
// the source is public a recipient can read a message's inline-button
// callback_data via an MTProto userbot, decode the sender's STABLE chevaletid,
// and link every message from that sender without consent (S-0002). The Telegram
// uid never leaves the DB, but linkability alone breaks the "can't tell one
// person from many" guarantee.
//
// FIX (Fable "Option C", uid-sealed): the token seals the user id (7 bytes, not
// the long chevaletid string, so it fits Telegram's 64-byte callback_data limit)
// under a key derived from a server secret, with a fresh random nonce each time:
//
//	token = base64url( nonce(8) || ct(7 = uid XOR keystream) || tag(8 = HMAC) )
//	keystream = HMAC(key, "enc" || nonce)[:7]      (one-time pad for the uid)
//	tag       = HMAC(key, "mac" || nonce || ct || aad)[:8]   (encrypt-then-MAC)
//
// Only a holder of the key can Open it; two tokens for the same uid are
// computationally unlinkable (fresh nonce => semantic security). Truncated-HMAC
// authentication (64-bit) makes forgery require ~2^64 ONLINE callbacks. aad binds
// extra framing (e.g. a message id) with zero token bytes when a caller wants it;
// v1 passes nil.
//
// The legacy EncodeChevaletID/DecodeChevaletID stay UNTOUCHED (golden-locked) as
// the decode-both fallback so buttons on already-delivered messages keep working.
import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
)

const (
	tokenNonceLen = 8 // 64-bit random nonce; birthday collision ~2^32 emissions
	tokenUIDLen   = 7 // 56-bit, covers Telegram's documented <2^52 id range
	tokenTagLen   = 8 // 64-bit truncated HMAC; online forgery ~2^64
	tokenRawLen   = tokenNonceLen + tokenUIDLen + tokenTagLen
)

// tokenKeyLabel domain-separates the callback-token key derivation from any other
// use of the same server secret. "v1" allows a future scheme bump.
const tokenKeyLabel = "chevalet:callback-token:v1"

// tokenEnc is base64url WITHOUT padding, in Strict mode.
//
// Strict matters: tokenRawLen (23) bytes encode to 31 base64 chars, which hold
// 186 bits for 184 bits of payload — so the final character carries 2 bits that
// are not part of the message. The non-strict decoder silently ignores them, which
// made every token have FOUR distinct string encodings that all opened to the same
// uid (…Ku8/…Ku9/…Ku-/…Ku_). Nothing here keys off the token string, so that was
// malleability rather than an exploitable hole, but non-canonical ciphertext is
// worth refusing on principle — and it made TestTokenTamperRejected fail ~6% of
// runs (whenever flipping the last char changed only padding bits), which is a
// tamper test that silently stops testing tampering.
//
// Encoding is unaffected (Go always writes those bits as zero), so every token
// ever issued still opens — only malleated variants are now rejected.
var tokenEnc = base64.RawURLEncoding.Strict()

// errNoRand is returned by Seal if the OS CSPRNG fails — the caller must fail the
// send rather than emit a token with a predictable nonce (which would silently
// destroy unlinkability).
var errNoRand = errors.New("encoder: crypto/rand unavailable")

// TokenCipher seals/opens callback tokens under a keyring. keys[0] is the current
// key (used to Seal); every key is tried on Open so a key can be rotated by
// PREPENDING a new one while keeping the old — old buttons keep resolving. Keys
// are never removed in normal operation (32 bytes each), so nothing breaks.
type TokenCipher struct {
	keys [][]byte
}

// NewTokenCipher derives the current key from primary (the bot's BOT_TOKEN, so
// there is no separate key to manage or lose — it lives exactly as long as the
// bot) and prepends any non-empty extra secrets as older keyring entries for
// rotation. A nil/empty primary yields a cipher whose Seal fails closed.
func NewTokenCipher(primary string, extra ...string) *TokenCipher {
	tc := &TokenCipher{}
	if primary != "" {
		tc.keys = append(tc.keys, deriveTokenKey(primary))
	}
	for _, s := range extra {
		if s != "" {
			tc.keys = append(tc.keys, deriveTokenKey(s))
		}
	}
	return tc
}

// Ready reports whether the cipher has at least one key (so callers can fail
// closed at startup rather than emit unprotected tokens).
func (tc *TokenCipher) Ready() bool { return tc != nil && len(tc.keys) > 0 }

// deriveTokenKey turns a high-entropy secret into a 32-byte key via one HMAC
// (a single-block HKDF-Expand); sound because the secret is already high-entropy.
func deriveTokenKey(secret string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(tokenKeyLabel))
	return m.Sum(nil)
}

// prf = HMAC-SHA256(key, domain || data), domain-separating the keystream ("enc")
// from the tag ("mac").
func prf(key []byte, domain string, parts ...[]byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(domain))
	for _, p := range parts {
		m.Write(p)
	}
	return m.Sum(nil)
}

// Seal encrypts uid into an opaque, unlinkable token. aad (may be nil) is
// authenticated but not transmitted.
func (tc *TokenCipher) Seal(uid int64, aad []byte) (string, error) {
	if !tc.Ready() {
		return "", errors.New("encoder: token cipher has no key")
	}
	key := tc.keys[0]

	var nonce [tokenNonceLen]byte
	if _, err := crand.Read(nonce[:]); err != nil {
		return "", errNoRand
	}

	var uidb [tokenUIDLen]byte
	putUID(uidb[:], uid)

	ks := prf(key, "enc", nonce[:])[:tokenUIDLen]
	ct := make([]byte, tokenUIDLen)
	for i := range ct {
		ct[i] = uidb[i] ^ ks[i]
	}
	tag := prf(key, "mac", nonce[:], ct, aad)[:tokenTagLen]

	raw := make([]byte, 0, tokenRawLen)
	raw = append(raw, nonce[:]...)
	raw = append(raw, ct...)
	raw = append(raw, tag...)
	return tokenEnc.EncodeToString(raw), nil
}

// Open reverses Seal: returns (uid, true) iff token is a well-formed, key-valid,
// aad-matching token under some keyring key; (0, false) otherwise (including any
// legacy/garbage input — the caller then tries the legacy decoder).
func (tc *TokenCipher) Open(token string, aad []byte) (int64, bool) {
	if !tc.Ready() {
		return 0, false
	}
	raw, err := tokenEnc.DecodeString(token)
	if err != nil || len(raw) != tokenRawLen {
		return 0, false
	}
	nonce := raw[:tokenNonceLen]
	ct := raw[tokenNonceLen : tokenNonceLen+tokenUIDLen]
	tag := raw[tokenNonceLen+tokenUIDLen:]

	for _, key := range tc.keys {
		want := prf(key, "mac", nonce, ct, aad)[:tokenTagLen]
		if subtle.ConstantTimeCompare(tag, want) == 1 {
			ks := prf(key, "enc", nonce)[:tokenUIDLen]
			var uidb [tokenUIDLen]byte
			for i := range uidb {
				uidb[i] = ct[i] ^ ks[i]
			}
			return getUID(uidb[:]), true
		}
	}
	return 0, false
}

// putUID writes uid as tokenUIDLen big-endian bytes (uid is a positive Telegram
// id < 2^52, so the top byte of an int64 is never significant here).
func putUID(b []byte, uid int64) {
	u := uint64(uid)
	for i := tokenUIDLen - 1; i >= 0; i-- {
		b[i] = byte(u)
		u >>= 8
	}
}

// getUID reverses putUID.
func getUID(b []byte) int64 {
	var u uint64
	for _, c := range b {
		u = u<<8 | uint64(c)
	}
	return int64(u)
}
