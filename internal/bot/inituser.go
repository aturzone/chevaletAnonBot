package bot

import (
	"context"
	"errors"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

// errInitNoCID signals that initUser could not allocate a unique cid for the
// user after config.MaxTryAddCID attempts — the Go equivalent of the Python
// init_user returning False. prep turns it into the user-facing "couldn't make
// an anonymous link" reply (decorators.py:69-74).
var errInitNoCID = errors.New("init: could not allocate a unique cid")

// initUser mirrors modules/Global/user_init.py plus the chevaletid bootstrap
// from @prep_function: it upserts the user, ensures they have at least one cid,
// and ensures they have a chevaletid.
//
// Unlike the Python original (which called bot.get_chat(uid) on every update to
// fetch full_name) this uses the triggering user's name straight from the
// update — the same person, no extra API round-trip. The name is only persisted
// on first insert (add_user is an upsert that does nothing on conflict), so
// later renames via /settings are preserved.
func (b *Bot) initUser(ctx context.Context, userid string, u *gotgbot.User) error {
	name := u.FirstName
	if u.LastName != "" {
		name += " " + u.LastName
	}

	if _, err := b.DB.AddUser(ctx, userid, name); err != nil {
		return err
	}

	cids, err := b.DB.GetCIDs(ctx, userid)
	if err != nil {
		return err
	}
	if len(cids) == 0 {
		ok, err := b.DB.AddCID(ctx, userid, encoder.GenerateCID(10))
		if err != nil {
			return err
		}
		if !ok {
			// every cid attempt collided (astronomically unlikely) — mirror Python
			// init_user returning False so prep can tell the user to retry.
			return errInitNoCID
		}
	}

	chev, err := b.DB.GetChevaletIDByUID(ctx, userid)
	if err != nil {
		return err
	}
	if chev == "" {
		if err := b.DB.SetChevaletID(ctx, userid, encoder.GenerateChevaletID()); err != nil {
			return err
		}
	}
	return nil
}
