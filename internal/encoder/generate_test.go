package encoder

import (
	"strings"
	"testing"
)

// TestRandN covers the randomness primitive behind key/patch/cid generation: the
// n<=1 guard and the [0,n) range (with both endpoints observed).
func TestRandN(t *testing.T) {
	if randN(0) != 0 || randN(1) != 0 {
		t.Fatal("randN(0) and randN(1) must return 0")
	}
	for _, n := range []int{2, 52, 57, 101} {
		var sawMin, sawMax bool
		for i := 0; i < 20000; i++ {
			v := randN(n)
			if v < 0 || v >= n {
				t.Fatalf("randN(%d) = %d; out of [0,%d)", n, v, n)
			}
			if v == 0 {
				sawMin = true
			}
			if v == n-1 {
				sawMax = true
			}
		}
		if !sawMin || !sawMax {
			t.Errorf("randN(%d): sawMin=%v sawMax=%v; expected both endpoints over 20000 draws", n, sawMin, sawMax)
		}
	}
}

// TestGenerateChevaletID pins the generated-id shape: a GenerateCID(8) prefix
// (10 chars: 8 base57 + 2 lowercase) followed by an all-ASCII-digit time tail,
// every byte within the cipher alphabet (so it is encode-safe), and a clean
// Encode/Decode round-trip. Also a light uniqueness/entropy sanity check.
func TestGenerateChevaletID(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := GenerateChevaletID()
		if len(id) < 11 {
			t.Fatalf("GenerateChevaletID() = %q; want len >= 11", id)
		}
		// every byte must be in the cipher alphabet (else EncodeChevaletID is unsafe).
		for j := 0; j < len(id); j++ {
			if !strings.ContainsRune(allowedCIDChars, rune(id[j])) {
				t.Fatalf("GenerateChevaletID() = %q has out-of-alphabet byte %q", id, id[j])
			}
		}
		// the tail after the 10-char cid prefix is the time fraction: ASCII digits.
		for _, r := range id[10:] {
			if r < '0' || r > '9' {
				t.Fatalf("GenerateChevaletID() tail %q is not all digits", id[10:])
			}
		}
		// round-trips through the cipher.
		if dec, ok := DecodeChevaletID(EncodeChevaletID(id)); !ok || dec != id {
			t.Fatalf("round-trip failed for %q: dec=%q ok=%v", id, dec, ok)
		}
		if seen[id] {
			t.Fatalf("duplicate id generated: %q", id)
		}
		seen[id] = true
	}
}
