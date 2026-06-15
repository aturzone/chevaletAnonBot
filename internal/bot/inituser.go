package bot

import (
	"context"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/aturzone/chevaletAnonBot/internal/encoder"
)

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
		if _, err := b.DB.AddCID(ctx, userid, encoder.GenerateCID(10)); err != nil {
			return err
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
