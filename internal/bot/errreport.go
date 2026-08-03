package bot

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
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

// errDeepLinkPrefix marks a /start payload as "open this error report". The
// tracking code is 8 alphanumeric chars (encoder.GenerateCID), so the payload is
// deep-link safe and far inside Telegram's 64-character limit.
const errDeepLinkPrefix = "ERR-"

// moreLinkButton is the button that goes on the report in ERROR_CHAT_ID.
//
// It is a LINK, not a callback: ERROR_CHAT_ID is a channel, and a callback button
// on a channel post never reaches the bot (see reportactions.go — the tap is not
// even sent by the client). This button therefore opened nothing for as long as it
// existed. The link opens the admin's private chat, where paging works.
func moreLinkButton(botUsername, code string, total int) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
		{
			Text: fmt.Sprintf("🔎 جزئیات بیشتر (%d صفحه)", total),
			Url:  "https://t.me/" + botUsername + "?start=" + errDeepLinkPrefix + code,
		},
	}}}
}

// moreButton builds the inline keyboard that reveals page idx of report code. Used
// for paging INSIDE the admin's private chat, where callbacks do work.
func moreButton(code string, idx, total int) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
		{
			Text:         fmt.Sprintf("🔎 جزئیات بیشتر (%d/%d)", idx+1, total),
			CallbackData: "errmore|" + code + "|" + strconv.Itoa(idx),
		},
	}}}
}

// handleErrReport serves /start ERR-<code>: it shows page 1 of an error report's
// detail in the admin's private chat, with a button for the next page.
func (b *Bot) handleErrReport(tg *gotgbot.Bot, ctx *ext.Context, userid, arg string) error {
	code := strings.TrimPrefix(arg, errDeepLinkPrefix)

	// The link sits in a channel, so treat it as public: a non-admin gets the
	// ordinary "didn't understand" reply rather than any hint that it means
	// something.
	if !b.isAdmin(userid) {
		slog.Warn("error-report link followed by a non-admin", "actor", userid)
		if e := b.otherMessagesTemplate(ctx); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	pages, ok := b.errReports.get(code)
	if !ok || len(pages) == 0 {
		// Expected after a restart or eviction: the store is in-memory by design and
		// the full detail is in the server logs.
		if e := b.replyText(ctx, "این گزارش دیگه در حافظه نیست (جزئیات کامل در لاگ‌های سرور هست)."); e != nil {
			return e
		}
		return handlers.EndConversation()
	}

	// Pages are stored ALREADY html-escaped — see errMore for why re-escaping here
	// would corrupt the output and can overflow Telegram's 4096-char limit.
	text := fmt.Sprintf("<b>جزئیات خطا</b> <code>%s</code> — صفحه 1/%d\n<pre>%s</pre>",
		code, len(pages), pages[0])
	opts := &gotgbot.SendMessageOpts{ParseMode: "HTML"}
	if len(pages) > 1 {
		opts.ReplyMarkup = moreButton(code, 1, len(pages))
	}
	if _, err := ctx.EffectiveMessage.Reply(tg, text, opts); err != nil {
		return err
	}
	return handlers.EndConversation()
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

	// Paging now happens in an admin's PRIVATE chat (the channel button became a
	// deep link), so accept either the configured error chat or a private chat with
	// an admin — and nothing else.
	if msg != nil {
		inErrChat := b.Cfg.ErrorChatID != "" && strconv.FormatInt(msg.Chat.Id, 10) == b.Cfg.ErrorChatID
		privateAdmin := msg.Chat.Type == "private" && b.isAdmin(strconv.FormatInt(clbk.From.Id, 10))
		if !inErrChat && !privateAdmin {
			slog.Warn("errmore refused", "chat", msg.Chat.Id, "type", msg.Chat.Type,
				"actor", clbk.From.Id)
			_, _ = clbk.Answer(tg, nil)
			return nil
		}
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

	// pages are stored ALREADY html-escaped (bot.go onError/onPanic escape before
	// chunking), and Telegram un-escapes <pre> content exactly once — so do NOT
	// escape again here, or the admin sees literal entity codes and the inflated
	// body can overflow Telegram's 4096 limit (the send then fails and the
	// dispatcher files a spurious second incident).
	text := fmt.Sprintf("<b>جزئیات خطا</b> <code>%s</code> — صفحه %d/%d\n<pre>%s</pre>",
		code, idx+1, len(pages), pages[idx])

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

	// Intentionally DO NOT strip the tapped button: keep "🔎 more details" on the
	// report so the error detail can always be re-opened from Telegram and is
	// never lost after a single tap. (The full detail is also in the logs and in
	// the bounded in-memory store.)
	return nil
}

// errChatID returns ERROR_CHAT_ID as int64 (0 when unset/invalid).
func (b *Bot) errChatID() int64 {
	id, _ := strconv.ParseInt(b.Cfg.ErrorChatID, 10, 64)
	return id
}
