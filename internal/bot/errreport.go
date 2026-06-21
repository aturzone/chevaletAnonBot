package bot

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"sync"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// errReportStore keeps recent error reports' detail pages in memory so the
// "more" button on a report sent to ERROR_CHAT_ID can reveal them one page at a
// time, on demand, instead of flooding the channel with the whole dump at once.
// It is bounded (oldest evicted) and lost on restart — the full detail is also
// written to the process logs, so nothing is truly lost.
type errReportStore struct {
	mu    sync.Mutex
	pages map[string][]string // tracking code -> detail pages
	order []string            // codes oldest-first, for eviction
	max   int
}

func newErrReportStore(max int) *errReportStore {
	if max < 1 {
		max = 1
	}
	return &errReportStore{pages: make(map[string][]string), max: max}
}

func (s *errReportStore) put(code string, pages []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pages[code]; !ok {
		s.order = append(s.order, code)
	}
	s.pages[code] = pages
	for len(s.order) > s.max {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.pages, oldest)
	}
}

func (s *errReportStore) get(code string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pages[code]
	return p, ok
}

// moreButton builds the inline keyboard that reveals page idx of report code.
func moreButton(code string, idx, total int) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
		{
			Text:         fmt.Sprintf("🔎 جزئیات بیشتر (%d/%d)", idx+1, total),
			CallbackData: "errmore|" + code + "|" + strconv.Itoa(idx),
		},
	}}}
}

// errMore handles the "more" button on an error report in ERROR_CHAT_ID. It is
// registered OUTSIDE prep (the report lives in a group, not a private chat).
// callback_data: errmore|<code>|<page-index>. Each tap posts the requested page
// as a reply (with a button for the next page) and strips the button from the
// message just tapped, so the channel reveals detail progressively rather than
// all at once.
func (b *Bot) errMore(tg *gotgbot.Bot, ctx *ext.Context) error {
	clbk := ctx.CallbackQuery
	if clbk == nil {
		return nil
	}
	msg := ctx.EffectiveMessage

	// Defence in depth: only act on the configured error chat.
	if msg != nil && b.Cfg.ErrorChatID != "" && strconv.FormatInt(msg.Chat.Id, 10) != b.Cfg.ErrorChatID {
		_, _ = clbk.Answer(tg, nil)
		return nil
	}

	fields := strings.Split(clbk.Data, "|")
	if len(fields) < 3 {
		_, _ = clbk.Answer(tg, nil)
		return nil
	}
	code := fields[1]
	idx, err := strconv.Atoi(fields[2])
	if err != nil {
		_, _ = clbk.Answer(tg, nil)
		return nil
	}

	pages, ok := b.errReports.get(code)
	if !ok {
		_, _ = clbk.Answer(tg, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "این گزارش دیگه در حافظه نیست (جزئیات کامل در لاگ‌های سرور هست).",
			ShowAlert: true,
		})
		return nil
	}
	if idx < 0 || idx >= len(pages) {
		_, _ = clbk.Answer(tg, nil)
		return nil
	}
	_, _ = clbk.Answer(tg, nil)

	text := fmt.Sprintf("<b>جزئیات خطا</b> <code>%s</code> — صفحه %d/%d\n<pre>%s</pre>",
		code, idx+1, len(pages), html.EscapeString(pages[idx]))

	opts := &gotgbot.SendMessageOpts{ParseMode: "HTML"}
	if msg != nil {
		opts.ReplyParameters = &gotgbot.ReplyParameters{
			MessageId:                msg.MessageId,
			ChatId:                   msg.Chat.Id,
			AllowSendingWithoutReply: true,
		}
	}
	if idx+1 < len(pages) {
		opts.ReplyMarkup = moreButton(code, idx+1, len(pages))
	}

	chatID := b.errChatID()
	if msg != nil {
		chatID = msg.Chat.Id
	}
	if _, err := tg.SendMessage(chatID, text, opts); err != nil {
		return err
	}

	// Strip the button from the message just tapped so the same page can't be
	// revealed twice (best-effort; ignore if the message is too old to edit).
	if msg != nil {
		_, _, _ = msg.EditReplyMarkup(tg, &gotgbot.EditMessageReplyMarkupOpts{})
	}
	return nil
}

// errChatID returns ERROR_CHAT_ID as int64 (0 when unset/invalid).
func (b *Bot) errChatID() int64 {
	id, _ := strconv.ParseInt(b.Cfg.ErrorChatID, 10, 64)
	return id
}
