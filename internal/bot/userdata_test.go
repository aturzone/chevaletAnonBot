package bot

import (
	"testing"
	"time"
)

func TestAllowSendCap(t *testing.T) {
	u := &userData{}
	for i := 0; i < sendRateMax; i++ {
		if !u.allowSend() {
			t.Fatalf("send %d/%d should be allowed", i+1, sendRateMax)
		}
	}
	if u.allowSend() {
		t.Fatal("send over the cap should be blocked")
	}
}

func TestAllowSendWindowAgesOut(t *testing.T) {
	u := &userData{}
	old := time.Now().Add(-2 * sendRateWindow).UnixNano()
	for i := 0; i < sendRateMax; i++ {
		u.sendTimes = append(u.sendTimes, old)
	}
	if !u.allowSend() {
		t.Fatal("timestamps outside the window should age out; send should be allowed")
	}
	if len(u.sendTimes) != 1 {
		t.Fatalf("aged-out timestamps not pruned: have %d", len(u.sendTimes))
	}
}

func TestUserStoreSweep(t *testing.T) {
	s := newUserStore()
	u1 := s.get(1) // recently accessed
	u2 := s.get(2)
	u2.lastAccess = time.Now().Add(-3 * time.Hour) // idle past the cutoff
	_ = u1

	if n := s.sweep(2 * time.Hour); n != 1 {
		t.Fatalf("sweep evicted %d; want 1 (only the idle entry)", n)
	}
	if _, ok := s.m[1]; !ok {
		t.Fatal("recently-accessed entry was wrongly evicted")
	}
	if _, ok := s.m[2]; ok {
		t.Fatal("idle entry was not evicted")
	}

	// An idle entry that is currently in use (locked) must NOT be evicted.
	u3 := s.get(3)
	u3.lastAccess = time.Now().Add(-3 * time.Hour)
	u3.mu.Lock()
	if n := s.sweep(2 * time.Hour); n != 0 {
		t.Fatalf("sweep evicted a locked (in-use) entry: %d", n)
	}
	u3.mu.Unlock()
}
