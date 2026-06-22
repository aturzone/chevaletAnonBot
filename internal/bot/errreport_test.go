package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/aturzone/chevaletAnonBot/internal/config"
)

func TestErrReportStoreEviction(t *testing.T) {
	s := newErrReportStore(2)
	s.put("a", []string{"pa"})
	s.put("b", []string{"pb"})
	s.put("c", []string{"pc"}) // evicts the oldest ("a")

	if _, ok := s.get("a"); ok {
		t.Error("oldest entry 'a' should have been evicted")
	}
	if p, ok := s.get("b"); !ok || len(p) != 1 || p[0] != "pb" {
		t.Errorf("get(b) = (%v,%v); want ([pb],true)", p, ok)
	}
	if _, ok := s.get("c"); !ok {
		t.Error("get(c) should be present")
	}

	// re-putting an existing code updates pages WITHOUT consuming an eviction slot.
	s.put("b", []string{"pb2"})
	s.put("b", []string{"pb3"})
	if len(s.order) != 2 {
		t.Errorf("order len = %d; want 2 (re-put must not grow it)", len(s.order))
	}
	if _, ok := s.get("c"); !ok {
		t.Error("re-put of existing 'b' must not evict 'c'")
	}
	if p, _ := s.get("b"); p[0] != "pb3" {
		t.Errorf("get(b) pages = %v; want [pb3]", p)
	}

	// max < 1 clamps to 1.
	s1 := newErrReportStore(0)
	s1.put("x", []string{"px"})
	s1.put("y", []string{"py"})
	if _, ok := s1.get("x"); ok {
		t.Error("newErrReportStore(0) should clamp max to 1 (x evicted by y)")
	}
}

func TestMoreButton(t *testing.T) {
	mk := moreButton("abc123defg", 0, 3)
	if len(mk.InlineKeyboard) != 1 || len(mk.InlineKeyboard[0]) != 1 {
		t.Fatalf("moreButton shape = %v; want one 1-button row", mk.InlineKeyboard)
	}
	btn := mk.InlineKeyboard[0][0]
	if btn.CallbackData != "errmore|abc123defg|0" {
		t.Errorf("callback_data = %q; want errmore|abc123defg|0", btn.CallbackData)
	}
	if !strings.Contains(btn.Text, "(1/3)") {
		t.Errorf("button text %q should contain the 1-based counter (1/3)", btn.Text)
	}
	// round-trips with errMore's parser, and stays within Telegram's 64-byte limit.
	for _, idx := range []int{0, 9, 99, 999} {
		d := moreButton("abc123defg", idx, 1000).InlineKeyboard[0][0].CallbackData
		if len(d) > 64 {
			t.Errorf("callback_data %q is %d bytes (>64)", d, len(d))
		}
		fields := strings.Split(d, "|")
		if len(fields) != 3 || fields[0] != "errmore" {
			t.Errorf("callback_data %q does not parse to errmore|code|idx", d)
		}
	}
}

func TestErrChatID(t *testing.T) {
	b := &Bot{Cfg: &config.Config{ErrorChatID: "-1001234567890"}}
	if got := b.errChatID(); got != -1001234567890 {
		t.Errorf("errChatID = %d; want -1001234567890", got)
	}
	b.Cfg.ErrorChatID = ""
	if got := b.errChatID(); got != 0 {
		t.Errorf("errChatID(empty) = %d; want 0", got)
	}
	b.Cfg.ErrorChatID = "notanumber"
	if got := b.errChatID(); got != 0 {
		t.Errorf("errChatID(invalid) = %d; want 0", got)
	}
}

func TestAllowDBErrReport(t *testing.T) {
	b := &Bot{}
	if !b.allowDBErrReport() {
		t.Fatal("first report should be allowed")
	}
	if b.allowDBErrReport() {
		t.Fatal("a second report within the 30s window should be throttled")
	}
	// simulate the window elapsing (set the field directly; no sleeping).
	b.lastDBErr = time.Now().Add(-31 * time.Second)
	if !b.allowDBErrReport() {
		t.Fatal("a report after the window should be allowed again")
	}
}
