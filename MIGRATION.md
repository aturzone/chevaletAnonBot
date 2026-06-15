# ChevaletAnonBot — Python → Go migration

This branch (`go`) ports the production Telegram bot from Python
(`python-telegram-bot` + psycopg2) to Go (`gotgbot` + pgx), with the goal of a
**near-zero-downtime production cutover** that preserves every user, link,
setting and feature.

The original Python code is preserved unchanged on the **`python`** branch.
`main` is the production pointer (today: Python; after this port is verified:
Go).

---

## Why the cutover is cheap (two load-bearing facts)

1. **Same schema, shared database → no data migration.** The bot's entire state
   lives in PostgreSQL (`users`, `blocks`, `cids`, `reports`). The Go bot keeps
   the **identical schema** and connects to the **same database**. Existing rows
   just work — there is nothing to copy or transform.

2. **Long polling → a few seconds of downtime.** The bot talks to Telegram via
   `getUpdates` (long polling), and Telegram forbids two pollers on one token
   (HTTP 409). So cutover is: stop the Python bot (releases the poll lock) →
   start the Go bot (acquires it, same DB). Updates during the gap are buffered
   by Telegram for up to 24h, so nothing is lost. Rollback is the reverse and is
   instant.

---

## Hard compatibility contract: the chevaletid cipher

Inline-keyboard buttons on messages **already delivered to users** embed a
Python-encoded `chevaletid` in their `callback_data`
(`answer|<encoded>|<mid>`, `block|<encoded>`, …). After cutover the Go bot
receives those callbacks and must decode them **byte-for-byte** identically, or
every reply/seen/block/report button on historical messages breaks.

- Ported in [`internal/encoder`](internal/encoder/encoder.go).
- Locked by a golden-vector test: `testdata/gen_golden.py` is a standalone,
  dependency-free copy of the Python `encode/decode` that emits **6966 vectors**
  (an exhaustive sweep of every key in `[0,100]` × every alphabet character,
  plus randomized realistic samples). `encoder_test.go` asserts Go
  `DecodeChevaletID` reproduces every one. ✅ green.
- Gotcha handled: Python `%` is always non-negative; Go `%` is not. The decoder
  normalizes `((idx-key)%64 + 64) % 64`.

---

## Architecture mapping (Python → Go)

| Python | Go | Notes |
| --- | --- | --- |
| `config.py` (+ `dotenv`) | `internal/config` | same env keys & parsing; built-in `.env` loader |
| `myhelpers.py` cipher, `cid_gen.py` | `internal/encoder` | **done**, golden-tested |
| `modules/Global/database.py` (psycopg2 pool + `DBHandler`) | `internal/db` (pgx pool) | **identical** `CREATE TABLE IF NOT EXISTS` DDL; port each query |
| `@prep_function` decorator | `internal/bot` middleware | injects `userid` + per-update `DBHandler`, inits user, ban check |
| `ApplicationBuilder().updater.start_polling()` | `gotgbot` `ext.Updater` long-poll | single poller |
| `ConversationHandler` (settings, my_links, start) | per-user state machine keyed by uid | in-memory is fine (single instance) |
| `JobQueue.run_*` | goroutines + tickers / `robfig/cron` | GM/GN are Asia/Tehran daily |
| `modules/Global/ai_queue.py` (in-mem list) + `ai_responser` | channel/slice + ticker goroutine | POST to `AI_URL` `{sessionId, chatInput}` → `{output}` |
| `health_check_app` TCP socket | `internal/health` | **done** |
| `dynamic_settings.json` read/write | `internal/dynset` | same file, same keys (`ai_url`, `ai_session_id`) |
| `Texts/*.txt` via `fetch_text` | embed or read at startup (cache) | keep the `Texts/` dir |
| `error_handler.py` | central dispatcher error hook | notify `ERROR_CHAT_ID` |

Static assets reused verbatim: `Texts/`, `dynamic_settings.json`, `.env`,
`docker-compose.yml` (bot image swapped), the `backup*.sh`/`restore.sh` scripts.

---

## Phased plan & status

- [x] **0. Foundation** — repo/branch setup, `go.mod`, `internal/config`,
  `internal/encoder` (+ golden tests), `internal/health`, `cmd/bot` skeleton.
- [x] **1. Database** — `internal/db`: pgx pool, identical schema, every
  `DBHandler` method (users/blocks/cids/reports) + integration test
  (`db_test.go`, skips without `DB_HOST`). ⏳ runtime-verify against a Postgres
  (no Docker on this dev box yet) / a restored prod backup.
- [x] **2. Bot skeleton** — gotgbot dispatcher + long-polling updater, `prep`
  middleware (private-chat filter, `init_user`, ban check, userid injection),
  central error hook (notifies `ERROR_CHAT_ID`), `internal/texts` loader, health
  + graceful shutdown wired in `main`. Vertical slice: `/help`, `/privacy`,
  `/donate` work end-to-end. ⏳ runtime-verify needs a token + Postgres.
- [x] **3. Core messaging** — *foundations:* `markup.go` (reply_markups +
  message keyboard builder), `getuser.go`, `tgerr.go`. *Core (done):*
  `sendmsg.go` (`send_msg_template`: copy-to-target, wpp-preview removal,
  multi-link button, notify message, custom/audio/reply tags, warning +
  `time.AfterFunc` auto-delete), `autoreply.go` (`is_answer`,
  `is_reply_to_channel` incl. author-signature links + external_reply/quotes,
  `check_if_autoreply`), `start.go` (`/start`, `/start <cid>`,
  `/start UNBLOCK-<uid>`), `callbacks.go` (answer/seen/already-seen/block/
  unblock/report/report_confirm/delete/cancel/no-callback), `media.go`
  (`handle_media` media groups), `othermsgs.go`, `targetsend.go`
  (`handle_target_send` del+0/del+e sentinels), `jobs.go`, `cidchid.go`
  (`handle_cid_or_chid`), `userdata.go` (per-user `context.user_data` store,
  locked per-update in `prep`). The gotgbot `ConversationHandler` is wired in
  `handlers.go` (per-user `KeyStrategySender`, entry points + state `0` +
  fallbacks) in the same group-0 order as the Python `main.py`. `prep` now also
  mirrors `@prep_function`'s error filtering (swallow query-too-old /
  reply-not-found / Forbidden). ⏳ runtime-verify needs a token + Postgres.
- [x] **4. Settings & links** — `settings.go` (settings conversation: states
  name/custom-tag/audio-tag + wpp/seen/warning toggles + reply-quote/media/
  easier-answer/channel-signature explainers + unblock-me/unblock-all +
  what-is-formatting popup) and `mylinks.go` (`my_links` conversation:
  add/remove/rename cid with the per-user limit, charset + uniqueness checks,
  IntegrityError race handling via `db.IsUniqueViolation`) + the more-links
  callback. privacy/help/donate/myuid/bug already done in Phase 2. ⏳
  runtime-verify needs a token + Postgres.
- [x] **5. Admin & jobs** — `admin.go` (the /admin panel: help, send-mass-msg,
  send-msg, user-count, stats, ban/unban, cid get/set, link, report
  add/del/get, ai-url, ai-session, backup, with the Python wrong-syntax /
  wrong-value error mapping), `internal/dynset` (dynamic_settings.json
  ai_url/ai_session_id get/set/reset), `aiqueue.go` + `aichat.go` (GM-group
  reply-to-bot → queue + 🤔 reaction) + `background.go` (set_commands, hourly DB
  check, GM/GN daily Asia/Tehran via embedded tzdata, the AI responder loop
  POSTing to AI_URL and replying in the GM group, and the admin mass-msg job).
  Conversations re-keyed to KeyStrategySenderAndChat (= PTB per_user+per_chat)
  so GM-group messages reach the AI handler instead of a stale private state.
  ⏳ runtime-verify needs a token + Postgres + the AI endpoint.
- [~] **6. Parity & cutover** — *parity self-review done* (the whole Python
  surface is ported; see the parity notes below). *Pending:* the user's
  server-side live test with a staging token against a copy of prod DB, then the
  production switch (runbook below).

### Parity notes (Python quirks reproduced / bugs fixed)

Reproduced verbatim (harmless quirks): `@None` username for a user without one;
`delete_notify_on_END`'s notify-deletion is dead code (the `user_data.clear()`
wipes `wrapper_list` before the append), so the "جواب جدید:" notify is never
deleted; the deletion-warning leaves a trailing newline after stripping the
countdown; the literal `"None"` mid in a warning's delete callback_data when
there is no notify message; `/help` (or any message) while composing triggers
the "cancelled" fallback.

Fixed (with code comments): `AddCID` collision retry; `RemoveBlock` real
affected-row count (so unblock's "wasn't blocked" popup can fire); the pre-send
notify crash (`notify_msg.message_id` on a rejected send); `ai_responser`
`return` on empty output that permanently killed the worker; the
seen/block/unblock keyboard swap silently failing because the rebuilt donation
URL button was invalid (now preserved verbatim); block/unblock prefix-only
replace (vs Python's `replace`-all that could corrupt the chevaletid); warning
text formats only `DELETION_TEXT` so a `%` in a display name can't break it.

Behavioural matches worth noting: handler precedence follows `main.py`'s group-0
order (one handler per update); conversations use `KeyStrategySenderAndChat`
(= PTB `per_user`+`per_chat`) so GM-group messages reach the AI handler; `prep`
swallows the same benign errors `@prep_function` did (query-too-old,
reply-not-found, Forbidden).

---

## Cutover runbook (production)

1. `make backup` — safety snapshot (`pg_dump | gzip`).
2. Build & ship the Go bot image; point it at the **same** Postgres,
   `dynamic_settings.json`, and `Texts/`.
3. `docker compose stop bot` (Python) — releases the Telegram poll lock.
4. Start the Go bot. It runs `CREATE TABLE IF NOT EXISTS` (no-op on prod) and
   begins polling.
5. Smoke test: send a fresh message; click **reply/seen/block** on an *old*
   pre-cutover message (proves cipher compatibility); check `/settings`,
   `/my_links`, `/admin`.
6. Watch the health endpoint + logs.

**Rollback (instant):** stop the Go bot, `docker compose start bot` (Python) —
same database, no data loss.

Accepted, negligible losses at the switch: in-flight conversation state (someone
mid-compose) and the in-memory AI queue. Both are seconds-scale and self-heal.

---

## Build & test

```sh
go build ./...
go test ./...                     # encoder golden vectors must stay green
go run ./cmd/bot                  # needs a populated .env (see .env.example)
```

Regenerate the cipher golden vectors (only if the Python original ever changes):

```sh
python internal/encoder/testdata/gen_golden.py
```
