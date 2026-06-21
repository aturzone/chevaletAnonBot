// Package encoder ports the reversible chevaletid cipher and the cid/chevaletid
// generators from the Python original (modules/Global/myhelpers.py and
// modules/Global/cid_gen.py).
//
// CRITICAL COMPATIBILITY CONTRACT:
//
// DecodeChevaletID must be byte-for-byte compatible with the Python
// decode_chevaletid. Inline-keyboard buttons on messages that were already
// delivered to users embed Python-encoded chevaletids inside their
// callback_data (e.g. "answer|<encoded_chid>|<mid>"). After the production
// cutover the Go bot will receive those callbacks and MUST decode them
// verbatim, or every "reply / seen / block / report" button on historical
// messages breaks. This contract is locked by the golden-vector test in
// encoder_test.go, whose vectors are produced by the standalone Python copy in
// testdata/gen_golden.py.
//
// EncodeChevaletID is randomized (like the original); only its output's
// decodability is contractual, so it is covered by a round-trip test.
package encoder

import (
	crand "crypto/rand"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// allowedCIDChars mirrors Python's `string.ascii_letters + string.digits + "_-"`.
// Exactly 64 characters. It must NEVER contain '|' (the callback_data delimiter).
//
// Index layout (used by the cipher's modular shift):
//
//	a..z = 0..25, A..Z = 26..51, 0..9 = 52..61, '_' = 62, '-' = 63
const allowedCIDChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"

const allowedLen = len(allowedCIDChars)

// keyMaxInt mirrors config.KEY_MAX_INT (the inclusive upper bound of the cipher key).
const keyMaxInt = 100

// shortUUIDAlphabet is the default base57 alphabet of the Python `shortuuid`
// package (used by generate_cid). It excludes the visually ambiguous
// characters 0, O, 1, I, l.
const shortUUIDAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

const lowercaseLetters = "abcdefghijklmnopqrstuvwxyz"

// patchLetters mirrors `string.ascii_lowercase + string.ascii_uppercase`, the
// pool the original draws the key-patch letter from.
const patchLetters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// indexOfAllowed returns the position of b within allowedCIDChars, or -1.
// allowedCIDChars is pure ASCII, so byte position == character position.
func indexOfAllowed(b byte) int {
	return strings.IndexByte(allowedCIDChars, b)
}

// randN returns a uniform random int in [0, n) from a cryptographically secure
// source. ids generated here (cids = anonymous links, chevaletids, report and
// error tracking codes) must not be predictable/enumerable, so we use
// crypto/rand instead of math/rand. crypto/rand never fails on a running OS; if
// it ever did we degrade to a time-based value rather than crash the send path.
func randN(n int) int {
	if n <= 1 {
		return 0
	}
	if v, err := crand.Int(crand.Reader, big.NewInt(int64(n))); err == nil {
		return int(v.Int64())
	}
	f := time.Now().UnixNano() % int64(n)
	if f < 0 {
		f += int64(n)
	}
	return int(f)
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

func allASCIIDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isASCIIDigit(s[i]) {
			return false
		}
	}
	return true
}

// EncodeChevaletID applies the reversible Caesar-style shift with a random key
// in [0, keyMaxInt] and appends the key-patch (a random letter followed by
// ord(letter)+key) so DecodeChevaletID can recover the key. Mirrors the Python
// encode_chevaletid.
func EncodeChevaletID(chevaletid string) string {
	key := randN(keyMaxInt + 1) // inclusive [0, 100], matching random.randint

	var sb strings.Builder
	sb.Grow(len(chevaletid) + 4)
	for i := 0; i < len(chevaletid); i++ {
		idx := indexOfAllowed(chevaletid[i])
		if idx < 0 {
			// The Python original raises ValueError here; real chevaletids only
			// contain alphabet characters, so this branch is defensive and keeps
			// the byte unchanged rather than panicking.
			sb.WriteByte(chevaletid[i])
			continue
		}
		sb.WriteByte(allowedCIDChars[(idx+key)%allowedLen])
	}

	patch := patchLetters[randN(len(patchLetters))]
	sb.WriteByte(patch)
	sb.WriteString(strconv.Itoa(int(patch) + key))
	return sb.String()
}

// DecodeChevaletID reverses EncodeChevaletID. It returns (plaintext, true) on
// success or ("", false) when the input is not a validly-encoded chevaletid —
// mirroring the Python decode_chevaletid, which returns False in those cases.
//
// Note: Telegram only ever echoes callback_data that the bot itself produced,
// so inputs are always our own ASCII encodings; the broader Unicode semantics
// of Python's str.isnumeric() are intentionally narrowed to ASCII digits here.
func DecodeChevaletID(encoded string) (string, bool) {
	if encoded == "" {
		return "", false
	}

	// The key-patch letter is the last non-digit byte (scanning from the end);
	// everything after it is the decimal key-patch number.
	i := -1
	for j := len(encoded) - 1; j >= 0; j-- {
		if !isASCIIDigit(encoded[j]) {
			i = j
			break
		}
	}
	if i < 0 {
		return "", false
	}

	keyPatchLetter := encoded[i]
	keyPatch := encoded[i+1:]
	chevaletid := encoded[:i]

	if !allASCIIDigits(keyPatch) {
		return "", false
	}
	n, err := strconv.Atoi(keyPatch)
	if err != nil {
		// Overflow on a pathologically long digit run -> undecodable. Python
		// would compute a huge int, yielding key > keyMaxInt and the same reject.
		return "", false
	}

	key := n - int(keyPatchLetter)
	if key > keyMaxInt || key < 0 {
		return "", false
	}

	var sb strings.Builder
	sb.Grow(len(chevaletid))
	for k := 0; k < len(chevaletid); k++ {
		idx := indexOfAllowed(chevaletid[k])
		if idx < 0 {
			// Python's `.index` would raise; treat as undecodable.
			return "", false
		}
		// Python's `%` is always non-negative for a positive modulus; Go's `%`
		// can be negative, so normalize before indexing.
		pos := ((idx-key)%allowedLen + allowedLen) % allowedLen
		sb.WriteByte(allowedCIDChars[pos])
	}
	return sb.String(), true
}

// GenerateCID mirrors cid_gen.generate_cid: `suidLength` random characters from
// the shortuuid base57 alphabet, followed by two random lowercase letters.
// A non-positive suidLength falls back to the Python default of 10.
func GenerateCID(suidLength int) string {
	if suidLength <= 0 {
		suidLength = 10
	}
	var sb strings.Builder
	sb.Grow(suidLength + 2)
	for i := 0; i < suidLength; i++ {
		sb.WriteByte(shortUUIDAlphabet[randN(len(shortUUIDAlphabet))])
	}
	sb.WriteByte(lowercaseLetters[randN(len(lowercaseLetters))])
	sb.WriteByte(lowercaseLetters[randN(len(lowercaseLetters))])
	return sb.String()
}

// GenerateChevaletID mirrors myhelpers.generate_chevaletid: an 8-length cid
// followed by the sub-second fractional digits of the current time. The result
// is opaque and stored as-is; every character is within allowedCIDChars so the
// value is safe to pass through EncodeChevaletID.
func GenerateChevaletID() string {
	frac := time.Now().Nanosecond() // 0..999_999_999 -> ASCII digits only
	return GenerateCID(8) + strconv.Itoa(frac)
}
