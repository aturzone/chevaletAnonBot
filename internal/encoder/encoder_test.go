package encoder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type goldenVector struct {
	PT  string `json:"pt"`
	Enc string `json:"enc"`
	Key int    `json:"key"`
}

func loadGolden(t *testing.T) []goldenVector {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden.json"))
	if err != nil {
		t.Fatalf("read golden.json: %v", err)
	}
	var vs []goldenVector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatalf("unmarshal golden.json: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("golden.json is empty")
	}
	return vs
}

// TestDecodeGolden is the core compatibility guarantee: every Python-produced
// (plaintext, encoded) pair must decode back to the exact plaintext in Go.
func TestDecodeGolden(t *testing.T) {
	vs := loadGolden(t)
	for _, v := range vs {
		got, ok := DecodeChevaletID(v.Enc)
		if !ok {
			t.Fatalf("DecodeChevaletID(%q) returned ok=false; want %q (key=%d)", v.Enc, v.PT, v.Key)
		}
		if got != v.PT {
			t.Fatalf("DecodeChevaletID(%q) = %q; want %q (key=%d)", v.Enc, got, v.PT, v.Key)
		}
	}
	t.Logf("verified %d Python golden vectors", len(vs))
}

// TestEncodeDecodeRoundTrip exercises the Go encoder (random key + patch letter)
// against the Go decoder across many inputs, including every alphabet character.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []string{
		allowedCIDChars, // every character once
		"a", "-", "_", "0", "9", "Z", "z", "A",
		"abcXYZ0189gkMNpq_q",
		GenerateChevaletID(),
		strings.Repeat("a", 64),
		strings.Repeat("-", 30),
	}
	// add a batch of generated chevaletids (the real shape encoded in production)
	for i := 0; i < 500; i++ {
		cases = append(cases, GenerateChevaletID())
	}

	for _, pt := range cases {
		// run several times since the key/patch are random each call
		for r := 0; r < 8; r++ {
			enc := EncodeChevaletID(pt)
			if strings.Contains(enc, "|") {
				t.Fatalf("encoded value contains '|' (callback_data delimiter): %q", enc)
			}
			got, ok := DecodeChevaletID(enc)
			if !ok || got != pt {
				t.Fatalf("round-trip failed: pt=%q enc=%q got=%q ok=%v", pt, enc, got, ok)
			}
		}
	}
}

// TestDecodeInvalid makes sure malformed inputs are rejected (ok=false) and
// never panic — mirroring the Python decode returning False.
func TestDecodeInvalid(t *testing.T) {
	invalid := []string{
		"",            // empty
		"abc",         // no key-patch digits
		"123",         // all digits, no patch letter
		"a",           // single letter, no digits
		"abc-",        // trailing non-digit, empty key-patch
		"ab1",         // key = 1 - ord('b') < 0
		"zzz0",        // key = 0 - ord('z') < 0
		"aaaa2147483648900", // overflow-ish digit run -> reject
	}
	for _, in := range invalid {
		if got, ok := DecodeChevaletID(in); ok {
			t.Fatalf("DecodeChevaletID(%q) = (%q, true); want ok=false", in, got)
		}
	}
}

// TestGenerateCID checks the shape and alphabet of generated cids.
func TestGenerateCID(t *testing.T) {
	for _, n := range []int{10, 8, 1, 0 /* -> default 10 */} {
		cid := GenerateCID(n)
		wantLen := n + 2
		if n <= 0 {
			wantLen = 12
		}
		if len(cid) != wantLen {
			t.Fatalf("GenerateCID(%d) length = %d; want %d (%q)", n, len(cid), wantLen, cid)
		}
		// every character must be inside the encoder alphabet so the value is
		// safe to later pass through EncodeChevaletID.
		for i := 0; i < len(cid); i++ {
			if indexOfAllowed(cid[i]) < 0 {
				t.Fatalf("GenerateCID(%d) produced out-of-alphabet byte %q in %q", n, cid[i], cid)
			}
		}
		// last two characters are lowercase letters
		tail := cid[len(cid)-2:]
		if !strings.ContainsRune(lowercaseLetters, rune(tail[0])) ||
			!strings.ContainsRune(lowercaseLetters, rune(tail[1])) {
			t.Fatalf("GenerateCID(%d) tail %q is not two lowercase letters", n, tail)
		}
	}
}
