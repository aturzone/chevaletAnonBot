package bot

import (
	"strconv"
	"testing"
	"time"
)

// TestGlobalSendLimit covers the total-output cap: a normal person never reaches it,
// an automated flood does.
func TestGlobalSendLimit(t *testing.T) {
	u := &userData{}

	// Spread across many targets so only the GLOBAL limit can bite.
	for i := 0; i < sendRateMax; i++ {
		allowed, _ := u.allowSendTo("target" + strconv.Itoa(i))
		if !allowed {
			t.Fatalf("send %d was blocked below the global limit of %d", i, sendRateMax)
		}
	}
	allowed, warn := u.allowSendTo("targetN")
	if allowed {
		t.Errorf("send %d was allowed past the global limit", sendRateMax+1)
	}
	if !warn {
		t.Error("the first block did not warn the user; they would see nothing happen")
	}
}

// TestPerTargetLimitProtectsOneVictim is the important one. The global limit alone
// permits 40 messages a minute all aimed at the SAME person, which is the
// harassment anonymity makes possible. This bounds what one inbox can be made to
// receive.
func TestPerTargetLimitProtectsOneVictim(t *testing.T) {
	u := &userData{}
	const victim = "1988454449"

	for i := 0; i < perTargetMax; i++ {
		if allowed, _ := u.allowSendTo(victim); !allowed {
			t.Fatalf("message %d to one person was blocked below the per-target limit of %d", i, perTargetMax)
		}
	}
	if allowed, _ := u.allowSendTo(victim); allowed {
		t.Errorf("message %d to the same person was allowed; a victim can be flooded", perTargetMax+1)
	}

	// Crucially, a DIFFERENT recipient is still reachable: the sender is not banned
	// outright, only stopped from piling on one person.
	if allowed, _ := u.allowSendTo("5118145008"); !allowed {
		t.Error("a different recipient was blocked; the per-target limit must not act as a global ban")
	}
}

// TestWarnIsRateLimited: a flooder tripping the limit hundreds of times must not
// receive hundreds of replies. Each reply is an API call, so warning every time
// would make the anti-spam measure a flood of its own — and give an automated
// flooder a steady stream of responses.
func TestWarnIsRateLimited(t *testing.T) {
	u := &userData{}
	const victim = "1988454449"

	for i := 0; i < perTargetMax; i++ {
		u.allowSendTo(victim)
	}

	warned := 0
	for i := 0; i < 200; i++ {
		allowed, warn := u.allowSendTo(victim)
		if allowed {
			t.Fatalf("attempt %d slipped past the limit", i)
		}
		if warn {
			warned++
		}
	}
	if warned != 1 {
		t.Errorf("a 200-attempt flood produced %d warnings; want exactly 1 inside the cooldown", warned)
	}
}

// TestLimitsRecoverAfterTheWindow: the limits are a speed bump, not a ban. A user
// who waits must be able to talk again, or a burst would silence them for good.
func TestLimitsRecoverAfterTheWindow(t *testing.T) {
	u := &userData{}
	const victim = "1988454449"

	for i := 0; i < perTargetMax; i++ {
		u.allowSendTo(victim)
	}
	if allowed, _ := u.allowSendTo(victim); allowed {
		t.Fatal("the per-target limit did not engage")
	}

	// Age every recorded send past the window, as real time would.
	shift := int64(sendRateWindow) + int64(time.Second)
	for i := range u.sendTimes {
		u.sendTimes[i] -= shift
	}
	for tgt, times := range u.perTarget {
		for i := range times {
			times[i] -= shift
		}
		u.perTarget[tgt] = times
	}

	if allowed, _ := u.allowSendTo(victim); !allowed {
		t.Error("the user was still blocked after the window elapsed; the limit is acting as a ban")
	}
}

// TestCancelCannotResetTheLimit: clear() wipes the conversation, and a flooder must
// not be able to dodge the limit by cancelling between messages.
func TestCancelCannotResetTheLimit(t *testing.T) {
	u := &userData{}
	const victim = "1988454449"

	for i := 0; i < perTargetMax; i++ {
		u.allowSendTo(victim)
	}
	u.clear()

	if allowed, _ := u.allowSendTo(victim); allowed {
		t.Error("clearing the conversation reset the rate limit; /cancel would defeat the anti-spam")
	}
}

// TestStaleTargetsAreForgotten: perTarget must not grow for the life of the process.
// A user who messages many people over hours would otherwise accumulate an entry per
// recipient forever.
func TestStaleTargetsAreForgotten(t *testing.T) {
	u := &userData{}
	for i := 0; i < 50; i++ {
		u.allowSendTo("t" + strconv.Itoa(i))
	}
	if len(u.perTarget) == 0 {
		t.Fatal("no targets were recorded at all")
	}

	// Age EVERYTHING past the window — the global list as well as the per-target
	// ones. Ageing only the per-target entries left 50 fresh global timestamps, over
	// the 40 cap, so the next call was blocked and recorded nothing.
	shift := int64(sendRateWindow) + int64(time.Second)
	for i := range u.sendTimes {
		u.sendTimes[i] -= shift
	}
	for tgt, times := range u.perTarget {
		for i := range times {
			times[i] -= shift
		}
		u.perTarget[tgt] = times
	}

	// One more call triggers the sweep and records itself.
	if allowed, _ := u.allowSendTo("fresh"); !allowed {
		t.Fatal("a send after the window elapsed was blocked")
	}

	if len(u.perTarget) != 1 {
		t.Errorf("perTarget holds %d entries after they all expired; want just the fresh one", len(u.perTarget))
	}
}

// TestEmptyTargetStillCountsGlobally guards the path where the target is unknown
// (a malformed or cancelled flow): it must still consume global budget, or that
// becomes a way to send without limit.
func TestEmptyTargetStillCountsGlobally(t *testing.T) {
	u := &userData{}
	for i := 0; i < sendRateMax; i++ {
		if allowed, _ := u.allowSendTo(""); !allowed {
			t.Fatalf("send %d with no target was blocked early", i)
		}
	}
	if allowed, _ := u.allowSendTo(""); allowed {
		t.Error("sends with no target ignore the global limit; that is an unlimited path")
	}
}
