package bot

import (
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/aturzone/chevaletAnonBot/internal/db"
)

// /admin_stats — the daily admin dashboard.
//
// What it reports, and what it deliberately cannot:
//
//   - new users: users.created_at, which is NULL for everyone who joined before
//     tracking started. Those are reported separately rather than folded in, so a
//     daily figure is never confused with the whole user base.
//   - active users: users.last_active_at, written at most once per user per day.
//   - messages: a per-day counter, incremented on delivery.
//
// Nothing here records who messaged whom — the counter has no sender, recipient or
// content, and last_active_at says only THAT someone used the bot. That is the
// anonymity contract, and stats are not worth weakening it.
//
// One honest limitation: last_active_at holds only the LATEST day a user was seen,
// so a past day's "active" means "users whose last activity was that day". Today's
// figure is exact. The reply says so rather than quietly implying otherwise.
func adminStatsCmd(b *Bot, _ *gotgbot.Bot, ctx *ext.Context, userid string) error {
	if !b.isAdmin(userid) {
		return b.otherMessagesTemplate(ctx)
	}

	dbctx, cancel := b.bg()
	defer cancel()

	s, err := b.DB.GetStats(dbctx, 7)
	if err != nil {
		return err
	}
	out := formatStats(s)
	// Queue health, appended here rather than in formatStats so that stays a pure
	// function of db.Stats. A depth that grows means deliveries are not draining.
	if q := b.outboxStatus(); q != "" {
		out += "\n\n" + q
	}
	return b.replyHTML(ctx, out, true)
}

// formatStats renders the dashboard. Kept separate from the command so the layout
// is testable without a database or Telegram.
func formatStats(s db.Stats) string {
	var sb strings.Builder

	sb.WriteString("📊 <b>آمار روزانه</b>\n\n")

	sb.WriteString("<b>امروز</b>\n")
	sb.WriteString(fmt.Sprintf("• کاربر جدید: <b>%d</b>\n", s.Today.NewUsers))
	sb.WriteString(fmt.Sprintf("• کاربر فعال: <b>%d</b>\n", s.Today.Active))
	sb.WriteString(fmt.Sprintf("• پیام ارسال شده: <b>%d</b>\n\n", s.Today.Messages))

	sb.WriteString("<b>کل</b>\n")
	sb.WriteString(fmt.Sprintf("• کاربران: <b>%d</b>\n", s.TotalUsers))
	sb.WriteString(fmt.Sprintf("• لینک‌ها: <b>%d</b>\n", s.TotalLinks))
	sb.WriteString(fmt.Sprintf("• ریپورت‌ها: <b>%d</b>\n", s.TotalReports))
	sb.WriteString(fmt.Sprintf("• بن شده: <b>%d</b>\n", s.TotalBanned))

	if len(s.Days) > 0 {
		sb.WriteString("\n<b>۷ روز گذشته</b>\n<pre>")
		sb.WriteString("روز          جدید  فعال   پیام\n")
		for _, d := range s.Days {
			sb.WriteString(fmt.Sprintf("%-12s %5d %5d %6d\n",
				d.Day.Format("2006-01-02"), d.NewUsers, d.Active, d.Messages))
		}
		sb.WriteString("</pre>")
	}

	// State the caveats instead of letting a number be read as more than it is.
	sb.WriteString("\n<blockquote>«فعال» برای روزهای گذشته یعنی کاربرانی که آخرین فعالیتشون اون روز بوده؛ عدد امروز دقیقه.")
	if s.UsersWithoutJoinDate > 0 {
		sb.WriteString(fmt.Sprintf("\nتاریخ عضویت %d کاربر ثبت نشده (قبل از فعال شدن آمار عضو شدن)، پس توی «کاربر جدید» شمرده نمیشن.",
			s.UsersWithoutJoinDate))
	}
	sb.WriteString("</blockquote>")

	return sb.String()
}
