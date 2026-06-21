# Attack Surface — ChevaletAnonBot (Go)

Scope: C:\projects\chevalet-build. Anonymous-message Telegram bot, gotgbot +
pgx/PostgreSQL. Audited 2026-06-21 (mode: audit / first run).

## Trust boundaries
1. Telegram user -> bot, via:
   - private-chat messages & commands (entry: `prep` in internal/bot/prep.go),
   - inline-button callbacks (callback_data is USER-CONTROLLED on receipt; echoed
     verbatim by Telegram),
   - deep links `/start <param>`.
2. GM-group members -> bot (AI input handler, NOT behind `prep`).
3. Bot -> PostgreSQL (pgx).
4. Bot -> Telegram API (outbound, optional proxy).
5. Bot -> AI endpoint (outbound HTTP, admin-settable URL, NO proxy).
6. Host filesystem: .env, dynamic_settings.json, backups/, Texts/, mass-msg log.

## Entry points / handlers (internal/bot/handlers.go registers all)
- Conversations (per-user+chat, in-memory state):
  - start: /start, answer|, seen|, alread-seen, report|, report_yes|, report_no|,
    block|, unblock| ; state-0 send_msg ; fallbacks (delete|, cancel, /cancel).
  - my_links: ch-link, add-link, rm-link, /my_links, mylinks-menu, what-is-cid ;
    state-0 updateCid.
  - settings: reply-quote|, media-settings|, change-name|, custom-tag|, audio-tag|,
    wpp|, warning|, easier-answer|, channel-signature|, seen-settings|,
    unblock-all|, unblock-me|, /settings ; states 0/1/2 (name/custom tag/audio tag).
- Standalone: errmore| (ERROR_CHAT only), no-callback, delete|, /help, more-links,
  /privacy, /donate, /admin, /myuid, /bug.
- GM group: AI input (reply-to-bot text) -> aiQueue.
- Catch-all: otherMessages -> auto-reply (reply to delivered msg / channel reply).

## Untrusted inputs -> sinks (source -> sink, defense)
- callback_data `token`/`targetMid` -> DecodeChevaletID + DB lookup + Telegram
  send/copy/delete into TARGET chat. Defense: IsBlocked only; NO ownership binding
  of mid. [S-0001]
- chevaletid cipher token -> keyless Caesar decode (key carried in token). [S-0002]
- user display name / custom tag / audio tag (OriginalHTML) -> stored -> rendered
  into other users' chats with ParseMode HTML, UNescaped. [S-0003]
- /start <cid> deep link -> startConnect -> deliver anonymous message to target.
  No rate limit -> spam/enumeration. [S-0004][S-0006]
- /start UNBLOCK-<uid> -> getUsername(GetChat) + tg://user link for supplied uid.
  [S-0008]
- report confirm -> copy reported message to REPORT_CHAT_ID; no cap. [S-0004]
- admin send-mass-msg -> copy to ALL uids, no pacing; failure list to disk 0644.
  [S-0004][S-0009]
- admin ai-url set <url> -> persisted -> POSTed server-side, no allowlist. [S-0007]
- GM-group reply -> unbounded aiQueue -> per-item outbound POST. [S-0010]
- handler error -> admin chat reflects raw `%T | %v`. [S-0005]
- All id generation -> math/rand/v2 (cids, chevaletids, report/error codes). [S-0006]

## SQL surface (internal/db) — REVIEWED, parameterized
Every query uses pgx parameter placeholders ($1,$2,...). No string-built SQL.
DDL in MakeTables uses fmt only for trusted config ints / the constant audio tag
(internal/db/db.go:95-119), not user input. No SQL injection found.

## Crypto / secrets surface
- chevaletid cipher: keyless (see S-0002).
- RNG: math/rand/v2 everywhere (S-0006).
- .env: git-ignored (.gitignore:6-7); .env.example placeholders only (no committed
  secret). Runtime files dynamic_settings.json / mass-msg log written 0644 (S-0009).
- Health endpoint: localhost-only TCP accept/close, no data (internal/health) — OK.
