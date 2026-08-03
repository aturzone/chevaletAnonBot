package bot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// fakeClock drives the limiter deterministically. Real time would make these tests
// slow and flaky, and would not let us assert the pacing precisely.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestLimiter wires a limiter to a fake clock, recording what it was asked to
// sleep for instead of actually sleeping.
func newTestLimiter() (*sendLimiter, *fakeClock, *[]time.Duration) {
	clk := &fakeClock{t: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)}
	var slept []time.Duration
	l := newSendLimiter()
	l.now = clk.now
	l.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		clk.advance(d) // sleeping really does pass time
		return nil
	}
	return l, clk, &slept
}

// TestGlobalRateIsPaced is the core guarantee: a burst past the global burst size is
// told to wait, which is what stops the bot exceeding ~30/s and earning a flood wait
// in the first place.
func TestGlobalRateIsPaced(t *testing.T) {
	l, clk, _ := newTestLimiter()

	// The burst is free — a quiet bot must not be slowed down.
	for i := 0; i < globalBurst; i++ {
		if w := l.reserve(0); w != 0 {
			t.Fatalf("call %d inside the burst waited %v; the burst must be free", i, w)
		}
	}
	w := l.reserve(0)
	if w <= 0 {
		t.Fatal("the call after the burst was not paced; the bot would exceed the global limit")
	}
	if w > time.Second {
		t.Errorf("wait one call past the burst = %v; suspiciously long", w)
	}

	// After enough idle time the budget comes back.
	clk.advance(2 * time.Second)
	if w := l.reserve(0); w != 0 {
		t.Errorf("wait after 2s idle = %v; the bucket did not refill", w)
	}
}

// TestPerChatLimitProtectsOneUser covers the limit that bites in practice: several
// messages to the SAME chat in a moment.
func TestPerChatLimitProtectsOneUser(t *testing.T) {
	l, _, _ := newTestLimiter()
	const victim = int64(1142340791) // positive == private chat

	for i := 0; i < privateBurst; i++ {
		if w := l.reserve(victim); w != 0 {
			t.Fatalf("call %d to one chat waited %v; the burst must be free", i, w)
		}
	}
	if w := l.reserve(victim); w <= 0 {
		t.Error("a 4th rapid message to one user was not paced")
	}

	// A DIFFERENT chat must not be punished for it, or one busy conversation would
	// throttle everybody.
	if w := l.reserve(5118145008); w != 0 {
		t.Errorf("an unrelated chat waited %v because another chat was busy", w)
	}
}

// TestChannelLimitIsMuchTighter: the report and error chats are channels, where
// Telegram allows ~20/minute rather than ~1/second.
func TestChannelLimitIsMuchTighter(t *testing.T) {
	l, _, _ := newTestLimiter()
	const channel = int64(-1002157529004) // negative == channel

	for i := 0; i < groupBurst; i++ {
		if w := l.reserve(channel); w != 0 {
			t.Fatalf("call %d to a channel waited %v inside the burst", i, w)
		}
	}
	if w := l.reserve(channel); w < 2*time.Second {
		t.Errorf("channel pacing after the burst = %v; want seconds, not milliseconds", w)
	}
}

// TestFloodWaitPausesEverything: once Telegram says "retry after N", every caller
// backs off. Ignoring that is what turned a 92-second wait into 607.
func TestFloodWaitPausesEverything(t *testing.T) {
	l, clk, _ := newTestLimiter()

	l.noteFloodWait(120)
	if got := l.floodWaitRemaining(); got < 119*time.Second {
		t.Fatalf("floodWaitRemaining = %v; want ~120s", got)
	}
	// Even a chat that has sent nothing is held back.
	if w := l.reserve(999); w < 119*time.Second {
		t.Errorf("a fresh chat got wait %v during a flood pause; want ~120s", w)
	}

	// A LONGER wait extends it; a shorter one must not shorten it.
	l.noteFloodWait(300)
	if got := l.floodWaitRemaining(); got < 299*time.Second {
		t.Errorf("a longer flood wait did not extend the pause: %v", got)
	}
	l.noteFloodWait(5)
	if got := l.floodWaitRemaining(); got < 299*time.Second {
		t.Errorf("a shorter flood wait shortened the pause to %v", got)
	}

	clk.advance(301 * time.Second)
	if got := l.floodWaitRemaining(); got != 0 {
		t.Errorf("the pause did not clear after expiry: %v", got)
	}
}

// TestReportThrottle stops the bot reporting one flood 321 times.
func TestReportThrottle(t *testing.T) {
	l, clk, _ := newTestLimiter()
	if !l.allowReport() {
		t.Fatal("the first report was suppressed")
	}
	for i := 0; i < 50; i++ {
		if l.allowReport() {
			t.Fatal("a second report slipped through inside the throttle window")
		}
	}
	clk.advance(3 * time.Minute)
	if !l.allowReport() {
		t.Error("reporting never recovered after the window")
	}
}

// --- the client wrapper -----------------------------------------------------

type fakeInner struct {
	calls   []string
	err     error
	errOnce bool
	served  int
}

func (f *fakeInner) RequestWithContext(_ context.Context, _, method string, _ map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	f.served++
	if f.err != nil && (!f.errOnce || f.served == 1) {
		return nil, f.err
	}
	return json.RawMessage(`{"ok":true}`), nil
}
func (f *fakeInner) GetAPIURL(*gotgbot.RequestOpts) string               { return "" }
func (f *fakeInner) FileURL(string, string, *gotgbot.RequestOpts) string { return "" }

// TestPollingIsNeverThrottled: getUpdates is the polling loop. Delaying it stalls
// the bot rather than protecting it.
func TestPollingIsNeverThrottled(t *testing.T) {
	l, _, slept := newTestLimiter()
	c := &limitedClient{inner: &fakeInner{}, limiter: l}

	for i := 0; i < 200; i++ { // far more than any burst allows
		if _, err := c.RequestWithContext(context.Background(), "t", "getUpdates", nil, nil); err != nil {
			t.Fatalf("getUpdates %d failed: %v", i, err)
		}
	}
	if len(*slept) != 0 {
		t.Errorf("polling was delayed %d times; it must never be throttled", len(*slept))
	}
}

// TestSendsArePacedAtTheChannelRate: a serial caller is smoothed to roughly the
// channel limit, and no single wait exceeds the cap that keeps the pipeline moving.
func TestSendsArePacedAtTheChannelRate(t *testing.T) {
	l, _, slept := newTestLimiter()
	c := &limitedClient{inner: &fakeInner{}, limiter: l}
	params := map[string]any{"chat_id": "-1002157529004"} // a channel: tight limit

	for i := 0; i < 12; i++ {
		if _, err := c.RequestWithContext(context.Background(), "t", "sendMessage", params, nil); err != nil {
			t.Fatalf("serial send %d failed: %v", i, err)
		}
	}
	if len(*slept) == 0 {
		t.Fatal("no send was ever paced; the channel limit is not being applied")
	}
	for _, d := range *slept {
		if d > maxInlineWait {
			t.Errorf("a call slept %v, past the %v cap that keeps the pipeline moving", d, maxInlineWait)
		}
	}
}

// TestFailsFastWhenDebtStacks: with callers already queued — the real shape under
// load, since background jobs send alongside the dispatcher — the wait grows past
// the cap, and the call must fail fast rather than park the pipeline.
func TestFailsFastWhenDebtStacks(t *testing.T) {
	l, _, _ := newTestLimiter()
	c := &limitedClient{inner: &fakeInner{}, limiter: l}
	const channel = int64(-1002157529004)

	// Stack debt without letting any time pass, as concurrent callers would.
	for i := 0; i < 12; i++ {
		l.reserve(channel)
	}

	_, err := c.RequestWithContext(context.Background(), "t", "sendMessage",
		map[string]any{"chat_id": "-1002157529004"}, nil)
	if err == nil {
		t.Fatal("a deeply queued send was accepted; it would have parked the dispatcher")
	}
	var te *gotgbot.TelegramError
	if !errors.As(err, &te) || te.Code != 429 {
		t.Errorf("fail-fast error = %v; want a 429 so handleErr tells the user to retry", err)
	}
}

// TestTelegram429IsRecordedAndRetried: a real 429 must pause everyone AND be retried
// once when the wait is short, rather than losing the user's message.
func TestTelegram429IsRecordedAndRetried(t *testing.T) {
	l, _, _ := newTestLimiter()
	inner := &fakeInner{
		err:     &gotgbot.TelegramError{Code: 429, Description: "Too Many Requests: retry after 1"},
		errOnce: true,
	}
	var reported int64
	c := &limitedClient{inner: inner, limiter: l, onFlood: func(s int64) { reported = s }}

	raw, err := c.RequestWithContext(context.Background(), "t", "copyMessage",
		map[string]any{"chat_id": "12345"}, nil)
	if err != nil {
		t.Fatalf("a 1-second flood wait was not retried: %v", err)
	}
	if string(raw) != `{"ok":true}` {
		t.Errorf("retry returned %s", raw)
	}
	if reported != 1 {
		t.Errorf("the flood was reported as %d; want 1", reported)
	}
	if len(inner.calls) != 2 {
		t.Errorf("inner was called %d times; want 2 (original + one retry)", len(inner.calls))
	}
	// The pause itself is asserted by TestFloodWaitPausesEverything. Not re-checked
	// here: the retry slept through the whole 1s, so by now it has legitimately
	// expired — which is the correct behaviour, not a missing pause.
}

// TestLongFloodIsNotRetriedInline: a 607-second wait must not be slept through, or
// the bot freezes for ten minutes.
func TestLongFloodIsNotRetriedInline(t *testing.T) {
	l, _, slept := newTestLimiter()
	inner := &fakeInner{err: &gotgbot.TelegramError{Code: 429, Description: "Too Many Requests: retry after 607"}}
	c := &limitedClient{inner: inner, limiter: l}

	if _, err := c.RequestWithContext(context.Background(), "t", "copyMessage",
		map[string]any{"chat_id": "12345"}, nil); err == nil {
		t.Fatal("a 607-second flood wait was swallowed instead of reported")
	}
	for _, d := range *slept {
		if d > maxInlineRetry {
			t.Errorf("slept %v inline on a long flood wait; the bot would freeze", d)
		}
	}
	if l.floodWaitRemaining() < 600*time.Second {
		t.Errorf("the long wait was not recorded: %v", l.floodWaitRemaining())
	}
}

// TestChatIDParsing guards per-chat limiting against a params shape change silently
// disabling it — every send would then share only the global bucket.
func TestChatIDParsing(t *testing.T) {
	for _, tc := range []struct {
		val  any
		want int64
	}{
		{"12345", 12345},
		{"-1002157529004", -1002157529004},
		{int64(7), 7},
		{int(8), 8},
		{float64(9), 9},
		{"not-a-number", 0},
		{nil, 0},
	} {
		if got := chatIDFromParams(map[string]any{"chat_id": tc.val}); got != tc.want {
			t.Errorf("chatIDFromParams(%v) = %d; want %d", tc.val, got, tc.want)
		}
	}
	if got := chatIDFromParams(map[string]any{}); got != 0 {
		t.Errorf("missing chat_id gave %d; want 0", got)
	}
}
