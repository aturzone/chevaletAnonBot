package bot

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// Outbound rate limiting for EVERY Telegram API call.
//
// WHY HERE: gotgbot funnels every method through BotClient.RequestWithContext, so
// wrapping that covers copyMessage, sendMessage, editMessageText, sendMediaGroup
// and everything else at once. Handling it per call site would mean finding all of
// them and getting each one right forever.
//
// WHAT IT FIXES: the bot was exceeding Telegram's limits and being told to back off
// for up to 607 seconds. 321 such errors were logged in nine hours. Telegram's
// documented ceilings are roughly 30 messages/second overall, about one per second
// to a single user, and ~20 per minute to a group or channel. Nothing paced sends
// against them, so bursts went straight into a flood wait.
//
// THE CONSTRAINT THAT SHAPES THE DESIGN: the dispatcher runs with MaxRoutines=1
// (deliberately — the conversation and media-group state machines depend on strict
// ordering), so a blocking send stalls ALL update processing. Waiting a full second
// on a per-chat limit would throttle the whole bot to one update per second.
//
// So pacing is a bounded DELAY, never a rejection: a call waits at most
// maxInlineWait — which smooths bursts, the common case — and then sends anyway.
// Dropping somebody's message to protect a rate limit is worse than occasionally
// taking a 429 that is already handled.
//
// The only outright refusal is while Telegram itself has us paused, since the call
// would be rejected regardless. That is errFloodPaused, NOT a 429 — keep the two
// distinct, or handleErr records a pause because we are already paused and the bot
// mutes itself.
const (
	// Global ceiling, kept under Telegram's ~30/s with room for the API calls that
	// are not sends (answerCallbackQuery, edits).
	globalRatePerSec = 22
	globalBurst      = 22

	// A single private chat: Telegram tolerates about 1/s, with short bursts.
	privateRatePerSec = 1.0
	privateBurst      = 3

	// A group or channel: ~20/minute. This is what the report and error channels
	// live under, and what a flood there costs.
	groupRatePerMin = 20.0
	groupBurst      = 4

	// The longest a single call will wait for a token — deliberately SHORT.
	//
	// This is the number that decides whether one spammer can freeze the bot. The
	// dispatcher is serial (MaxRoutines=1), so every millisecond spent waiting here
	// is a millisecond nobody else's update is being processed. At 3s, a user
	// pushing their 40-per-minute allowance could park the pipeline for a minute at
	// a time and the bot looked dead to everyone else — which is exactly what was
	// reported.
	//
	// 300ms still smooths the micro-bursts that cause most 429s (a handful of sends
	// landing in the same instant) while keeping the worst case per update small
	// enough that a flood cannot starve other users. Beyond it the call sends
	// anyway and an occasional real 429 is handled, which is the better trade.
	maxInlineWait = 300 * time.Millisecond

	// A 429 shorter than this is slept through and retried once, since a two-second
	// pause is cheaper than failing the user's message. Longer waits are reported
	// upward instead of holding the pipeline.
	maxInlineRetry = 2 * time.Second

	// Per-chat buckets are pruned past this many entries, so 17k users cannot grow
	// the map without bound.
	maxTrackedChats = 4000
	chatBucketIdle  = 10 * time.Minute
)

// tokenBucket is a leaky bucket that permits debt: a caller that cannot be served
// now still takes its turn, and the wait it is told about accounts for everyone
// already queued. That makes the queue fair instead of letting a burst starve
// whoever arrived first.
type tokenBucket struct {
	capacity     float64
	refillPerSec float64
	tokens       float64
	last         time.Time
}

func newTokenBucket(capacity, refillPerSec float64) *tokenBucket {
	return &tokenBucket{capacity: capacity, refillPerSec: refillPerSec, tokens: capacity}
}

// reserve consumes one token and returns how long the caller must wait before it is
// actually available. Zero means "go now".
func (tb *tokenBucket) reserve(now time.Time) time.Duration {
	if tb.last.IsZero() {
		tb.last = now
	}
	if elapsed := now.Sub(tb.last).Seconds(); elapsed > 0 {
		tb.tokens = math.Min(tb.capacity, tb.tokens+elapsed*tb.refillPerSec)
		tb.last = now
	}

	tb.tokens--
	if tb.tokens >= 0 {
		return 0
	}
	// Debt is capped so a pathological burst cannot promise a wait of minutes.
	if tb.tokens < -tb.capacity {
		tb.tokens = -tb.capacity
	}
	return time.Duration((-tb.tokens / tb.refillPerSec) * float64(time.Second))
}

// chatBucket pairs a bucket with when it was last used, for pruning.
type chatBucket struct {
	bucket *tokenBucket
	seen   time.Time
}

// sendLimiter paces outbound calls and owns the Telegram-imposed flood pause.
type sendLimiter struct {
	mu         sync.Mutex
	global     *tokenBucket
	perChat    map[int64]*chatBucket
	floodUntil time.Time
	lastReport time.Time

	// Injectable for tests: real time makes rate-limit tests slow and flaky.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

func newSendLimiter() *sendLimiter {
	return &sendLimiter{
		global:  newTokenBucket(globalBurst, globalRatePerSec),
		perChat: make(map[int64]*chatBucket),
		now:     time.Now,
		sleep:   sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// noteFloodWait records a Telegram-imposed pause, keeping the furthest deadline.
func (l *sendLimiter) noteFloodWait(seconds int64) {
	if seconds <= 0 {
		seconds = 5
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if until := l.now().Add(time.Duration(seconds) * time.Second); until.After(l.floodUntil) {
		l.floodUntil = until
	}
}

// floodWaitRemaining reports how long Telegram still wants us to wait.
func (l *sendLimiter) floodWaitRemaining() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if d := l.floodUntil.Sub(l.now()); d > 0 {
		return d
	}
	return 0
}

// allowReport throttles the admin notice to one per two minutes, so a flood does
// not produce a flood of reports about the flood.
func (l *sendLimiter) allowReport() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !l.lastReport.IsZero() && now.Sub(l.lastReport) < 2*time.Minute {
		return false
	}
	l.lastReport = now
	return true
}

// reserve returns the wait this call must observe: the larger of the global and
// per-chat reservations, plus any outstanding flood pause.
func (l *sendLimiter) reserve(chatID int64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	wait := l.global.reserve(now)

	if chatID != 0 {
		cb, ok := l.perChat[chatID]
		if !ok {
			cb = &chatBucket{bucket: newChatBucket(chatID)}
			l.perChat[chatID] = cb
			l.pruneLocked(now)
		}
		cb.seen = now
		if w := cb.bucket.reserve(now); w > wait {
			wait = w
		}
	}

	// The flood pause is deliberately NOT folded in here; the client checks it
	// separately. Mixing it into the pacing wait is what let one pause become a
	// self-sustaining lockup.
	return wait
}

// newChatBucket picks the limit by chat kind. Telegram ids are negative for
// groups, supergroups and channels and positive for users, which is the only
// signal available at this layer — and it is a stable one.
func newChatBucket(chatID int64) *tokenBucket {
	if chatID < 0 {
		return newTokenBucket(groupBurst, groupRatePerMin/60.0)
	}
	return newTokenBucket(privateBurst, privateRatePerSec)
}

// pruneLocked drops idle per-chat buckets. Caller holds the lock.
func (l *sendLimiter) pruneLocked(now time.Time) {
	if len(l.perChat) <= maxTrackedChats {
		return
	}
	for id, cb := range l.perChat {
		if now.Sub(cb.seen) > chatBucketIdle {
			delete(l.perChat, id)
		}
	}
}

// unlimitedMethods are calls that must not be delayed.
//
// getUpdates is the polling loop — throttling it would stall the bot rather than
// protect it. answerCallbackQuery has a few seconds before Telegram calls the query
// too old, and it produces no message, so delaying it only makes buttons feel
// broken. The rest are metadata reads.
var unlimitedMethods = map[string]bool{
	"getUpdates":          true,
	"getMe":               true,
	"getFile":             true,
	"answerCallbackQuery": true,
	"getChat":             true,
	"getChatMember":       true,
	"setMyCommands":       true,
	"getMyCommands":       true,
	"logOut":              true,
	"close":               true,
}

// errFloodPaused means Telegram has told us to wait and we are honouring it. It is
// deliberately NOT a TelegramError with code 429: handleErr must be able to tell our
// own back-pressure apart from Telegram's, or recording a pause in response to our
// own pause becomes a loop that mutes the bot.
var errFloodPaused = errors.New("bot: sending paused by a Telegram flood wait")

// limitedClient wraps a gotgbot BotClient with the limiter.
type limitedClient struct {
	inner   gotgbot.BotClient
	limiter *sendLimiter
	// onFlood is called when Telegram returns a 429, so the bot can report it.
	onFlood func(retryAfter int64)
	// onRetryable is called when a send fails for a reason that may succeed later,
	// so the bot can queue it durably instead of losing it. See outbox.go.
	onRetryable func(method string, params map[string]any)
}

var _ gotgbot.BotClient = (*limitedClient)(nil)

func (c *limitedClient) GetAPIURL(opts *gotgbot.RequestOpts) string { return c.inner.GetAPIURL(opts) }

func (c *limitedClient) FileURL(token, path string, opts *gotgbot.RequestOpts) string {
	return c.inner.FileURL(token, path, opts)
}

func (c *limitedClient) RequestWithContext(
	ctx context.Context, token, method string, params map[string]any, opts *gotgbot.RequestOpts,
) (json.RawMessage, error) {
	if unlimitedMethods[method] {
		return c.inner.RequestWithContext(ctx, token, method, params, opts)
	}

	// A real, Telegram-imposed pause is the only reason to refuse outright: the call
	// WILL be rejected, so sending is pointless. Reported as its own sentinel, never
	// as a 429 — see errFloodPaused.
	if fw := c.limiter.floodWaitRemaining(); fw > 0 {
		if fw > maxInlineWait {
			// Queue it rather than lose it: the pause may outlast this update.
			if c.onRetryable != nil {
				c.onRetryable(method, params)
			}
			return nil, errFloodPaused
		}
		if err := c.limiter.sleep(ctx, fw); err != nil {
			return nil, err
		}
	}

	// Pacing is a bounded DELAY, never a rejection. Waiting the cap and then sending
	// anyway is the important choice: dropping somebody's message to protect a rate
	// limit is worse than occasionally taking a 429 that is already handled.
	//
	// An earlier version returned a synthetic 429 here instead. handleErr could not
	// tell it from Telegram's own, so back-pressure recorded a flood pause, the pause
	// pushed every later call past the cap, and each of those extended it again — a
	// loop that silently muted the bot, answers included.
	if wait := c.limiter.reserve(chatIDFromParams(params)); wait > 0 {
		if wait > maxInlineWait {
			wait = maxInlineWait
		}
		if err := c.limiter.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}

	raw, err := c.inner.RequestWithContext(ctx, token, method, params, opts)
	if err == nil {
		return raw, nil
	}

	// Telegram still said no: record the pause so every other caller backs off too,
	// which is what stops a short wait escalating into a long ban.
	if secs, ok := errTooManyRequests(err); ok {
		c.limiter.noteFloodWait(secs)
		if c.onFlood != nil {
			c.onFlood(secs)
		}
		// One inline retry for a short wait: cheaper than failing the message.
		if d := time.Duration(secs) * time.Second; d > 0 && d <= maxInlineRetry {
			if serr := c.limiter.sleep(ctx, d); serr == nil {
				return c.inner.RequestWithContext(ctx, token, method, params, opts)
			}
		}
	}
	if c.onRetryable != nil && retryableSendFailure(err) {
		c.onRetryable(method, params)
	}
	return raw, err
}

// chatIDFromParams pulls chat_id out of a request. gotgbot passes params as
// strings, but the other numeric shapes are handled so a future change cannot
// silently disable per-chat limiting.
func chatIDFromParams(params map[string]any) int64 {
	v, ok := params["chat_id"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case string:
		id, _ := strconv.ParseInt(n, 10, 64)
		return id
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
