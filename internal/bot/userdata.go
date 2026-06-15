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
	return ud
}

// ud returns the locked-by-prep userData for the effective user of this update.
// Handlers call this to reach the cross-update state; prep has already taken
// the lock for the duration of the handler.
func (b *Bot) ud(ctx *ext.Context) *userData {
	return b.users.get(ctx.EffectiveUser.Id)
}

// nowSeconds reproduces Python's time.time() (wall-clock seconds as a float).
func nowSeconds() float64 { return float64(time.Now().UnixNano()) / 1e9 }
