package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/aturzone/chevaletAnonBot/internal/config"
)

// gotgbot has no JobQueue, so the Python job_queue.run_once(...) one-shot timers
// are reproduced with time.AfterFunc goroutines. They fire after the handler has
// returned (and released the per-user lock); failures are swallowed, matching
// the Python jobs' broad try/except.

// scheduleDeleteWarning mirrors jobs.delete_warning scheduled via
// job_queue.run_once(delete_warning, deletion_timeout, ...): after the timeout
// it strips the countdown notice (DELETION_TEXT) from the warning message,
// which — because edit_text is called without a reply_markup — also drops the
// "delete" buttons.
func (b *Bot) scheduleDeleteWarning(warning *gotgbot.Message, timeout int) {
	if warning == nil {
		return
	}
	time.AfterFunc(time.Duration(timeout)*time.Second, func() {
		// Mirror Python's text_html.removesuffix(DELETION_TEXT % T).removesuffix(
		// DELETION_TEXT % T_EXTENDED): the leading "\n" between sent_text and the
		// notice is deliberately left in place (Python removes only the notice).
		stripped := warning.OriginalHTML()
		stripped = strings.TrimSuffix(stripped, fmt.Sprintf(config.DeletionText, config.DeletionTimeout))
		stripped = strings.TrimSuffix(stripped, fmt.Sprintf(config.DeletionText, config.DeletionTimeoutExtended))
		_, _, _ = warning.EditText(b.TG, stripped, &gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	})
}

// scheduleDeleteMessage mirrors jobs.delete_message scheduled via
// job_queue.run_once(delete_message, 2, ...): after 2s it deletes the message
// and, if that message was a reply to a bare "/start <cid>" command, deletes
// the command too.
func (b *Bot) scheduleDeleteMessage(msg *gotgbot.Message) {
	if msg == nil {
		return
	}
	time.AfterFunc(2*time.Second, func() {
		deleteReply := false
		if r := msg.ReplyToMessage; r != nil {
			fields := strings.Split(r.Text, " ")
			if len(fields) == 2 && fields[0] == "/start" {
				deleteReply = true
			}
		}
		if _, err := msg.Delete(b.TG, nil); err != nil {
			return // Python's delete_message bails on the first failure
		}
		if deleteReply {
			_, _ = msg.ReplyToMessage.Delete(b.TG, nil)
		}
	})
}
