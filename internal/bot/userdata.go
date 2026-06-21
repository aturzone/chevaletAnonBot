package bot

import (
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// userData is the Go equivalent of python-telegram-bot's per-user
// context.user_data dict. The Python conversation flow (start/answer/send,
// media groups) stashed all of its cross-update state there; we keep the same
// fields here, typed explicitly.
//
// Each userData carries its own mutex. prep locks it for the whole duration of
// an update's handler so that two updates from the SAME user never race on
// these fields (gotgbot dispatches updates concurrently, up to MaxRoutines).
// Different users still proceed in parallel. This is a little stricter than
// PTB (which did not lock user_data) but is required for memory safety in Go
// and, helpfully, serialises the rapid-fire messages of a media group.
type userData struct {
	mu sync.Mutex
	d  convData

	// sendTimes: unix-nano timestamps of this user's recent anonymous sends, for
	// the outbound rate limit. Deliberately NOT part of convData, so clear()/
	// /cancel can't reset it (a flooder mustn't dodge the limit by cancelling).
	// Guarded by mu (prep holds it for the whole handler).
	sendTimes []int64

	// lastAccess: when this entry was last fetched. The userStore sweep evicts
	// long-idle entries to bound memory under a large/hostile population.
	// Guarded by the userStore mutex (set in get, read in sweep).
	lastAccess time.Time
}

// sendRateMax / sendRateWindow bound one user's anonymous sends. Generous by
// design — a human composing distinct messages never approaches 40/min, but it
// caps an automated flood (the anonymous-harassment amplifier). The Python
// original had no limit.
const (
	sendRateMax    = 40
	sendRateWindow = time.Minute
)

// allowSend reports whether this user may send another anonymous message now
// (sliding window). The caller holds u.mu (prep does).
func (u *userData) allowSend() bool {
	now := time.Now().UnixNano()
	cutoff := now - int64(sendRateWindow)
	kept := u.sendTimes[:0]
	for _, t := range u.sendTimes {
		if t >= cutoff {
			kept = append(kept, t)
		}
	}
	u.sendTimes = kept
	if len(kept) >= sendRateMax {
		return false
	}
	u.sendTimes = append(u.sendTimes, now)
	return true
}

// convData holds exactly the keys the Python handlers stored in user_data.
// Empty string / nil / false / zero stand in for Python's missing-key / None.
type convData struct {
	// single-message send flow (set by start_cmd / answer, read by send_msg_template)
	targetChid   string // "target_chid": already-encoded chevaletid
	targetCid    string // "target_cid": cid, or "" when not a /start send
	replyTo      string // "reply_to": target message id, or "" when not an answer
	channelReply bool   // "channel_reply": True only for reply-to-channel sends

	// media-group send flow (spans several updates)
	mediaGroupID         string                        // "media_group_id"
	groupMsgs            []*gotgbot.Message            // "group_msgs"
	groupExpiration      float64                       // "group_expiration": unix seconds, 0 = unset
	groupTargetChid      string                        // "group_target_chid": encoded
	groupWasChannelReply bool                          // "group_was_channel_reply"
	groupNotifyMsg       *gotgbot.Message              // "group_notify_msg"
	groupReplyMarkup     *gotgbot.InlineKeyboardMarkup // "group_reply_markup"
	groupWarningMsgID    string                        // "group_warning_msg_id"

	// "sent_medias": message ids already copied to the target for this group.
	sentMedias []string

	// settings conversation (settings.py): the menu message id to delete after a
	// name/tag update.
	ogMID int64 // "og_mid"

	// my_links conversation (my_links.py): the cid being renamed and the links
	// message id to refresh.
	chosenCid string // "chosen_cid"
	linksMID  int64  // "links_mid"
}

// clear mirrors context.user_data.clear(). It resets every stashed value but
// deliberately leaves the mutex untouched (prep still holds it).
func (u *userData) clear() { u.d = convData{} }

// userStore maps each Telegram user id to their userData. The store mutex only
// guards get-or-create of the map; the per-user mutex inside userData guards
// the fields themselves.
type userStore struct {
	mu sync.Mutex
	m  map[int64]*userData
}

func newUserStore() *userStore { return &userStore{m: make(map[int64]*userData)} }

func (s *userStore) get(uid int64) *userData {
	s.mu.Lock()
	defer s.mu.Unlock()
	ud := s.m[uid]
	if ud == nil {
		ud = &userData{}
		s.m[uid] = ud
	}
	ud.lastAccess = time.Now()
	return ud
}

// sweep evicts entries idle longer than idle that aren't currently in use (a
// successful TryLock proves no handler holds them), bounding memory under a
// large or hostile user population. Returns how many were evicted.
func (s *userStore) sweep(idle time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-idle)
	n := 0
	for uid, ud := range s.m {
		if ud.lastAccess.After(cutoff) {
			continue
		}
		if ud.mu.TryLock() {
			delete(s.m, uid)
			ud.mu.Unlock()
			n++
		}
	}
	return n
}

// ud returns the locked-by-prep userData for the effective user of this update.
// Handlers call this to reach the cross-update state; prep has already taken
// the lock for the duration of the handler.
func (b *Bot) ud(ctx *ext.Context) *userData {
	return b.users.get(ctx.EffectiveUser.Id)
}

// nowSeconds reproduces Python's time.time() (wall-clock seconds as a float).
func nowSeconds() float64 { return float64(time.Now().UnixNano()) / 1e9 }
