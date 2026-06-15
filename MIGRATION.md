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
- [ ] **1. Database** — `internal/db`: pgx pool, identical schema, every
  `DBHandler` method (users/blocks/cids/reports). Verify against a restored prod
  backup.
- [ ] **2. Bot skeleton** — gotgbot dispatcher, `prep` middleware (userid + dbh
  injection), `init_user`, error hook, wire health + graceful shutdown.
- [ ] **3. Core messaging** — `/start [cid|UNBLOCK-uid]`, send/copy, the
  reply/seen/block/report/delete callbacks, media groups, reply-to-channel,
  tags (custom/audio), warning + auto-delete.
- [ ] **4. Settings & links** — settings conversation (name, tags, wpp, seen,
  warning), `my_links` (add/remove/rename cid with limits), privacy/help/donate/
  myuid/bug.
- [ ] **5. Admin & jobs** — admin panel (ban, stats, report, mass-msg, backup,
  ai-url/ai-session dynamic settings), AI chat queue, GM/GN, set_commands, db
  check.
- [ ] **6. Parity & cutover** — run Go against a copy of prod DB with a staging
  token, diff behavior vs Python, then the production switch (runbook below).

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
