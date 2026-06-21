---
generated_by: sentinel
schema: 1
last_sha: eb19359
generated_at: 2026-06-21T13:34:20Z
coverage: { files: 40, full: 40, sampled: 0, skipped: 0 }
---

# Project Map — ChevaletAnonBot (Go)

## 1. Overview
ChevaletAnonBot is an anonymous-message Telegram bot written in Go (module
`github.com/aturzone/chevaletAnonBot`, go 1.25), a faithful port of a Python (python-telegram-bot)
bot that it is about to REPLACE on a shared PostgreSQL database (cutover, not migration). Stack:
gotgbot v2.0.0-rc.35 (long-polling Updater + Dispatcher + ConversationHandler) and pgx/pgxpool
v5.10 against the existing prod schema. ~6k LOC: `cmd/bot` (entrypoint) + `internal/` (bot, db,
config, dynset, encoder, texts, health). Users get anonymous "links" (cids); messages are relayed
between users without revealing identities; recipients can reply/seen/block/report/delete. Persian
UI strings are reproduced byte-for-byte. The encoder's `DecodeChevaletID` is a locked
byte-compatibility contract (golden-tested) so inline buttons on already-delivered Python-era
messages keep working after cutover.

## 2. Modules
| Module | Path | Responsibility | Key types / entry fns | Coverage |
|---|---|---|---|---|
| entrypoint | cmd/bot/main.go | load config, connect DB, start health + bot, signal-driven shutdown | `main`, `newLogger` | full |
| bot core | internal/bot/bot.go | build TG client/dispatcher/updater, central `onError`, run loop | `Bot`, `New`, `Run`, `onError` | full |
| prep middleware | internal/bot/prep.go | per-update filtering, init user, ban check, per-user lock, error mapping | `prep`, `handleErr`, `Handler` | full |
| per-user state | internal/bot/userdata.go | `context.user_data` equivalent + per-user mutex + user store | `userData`, `convData`, `userStore` | full |
| handler wiring | internal/bot/handlers.go | register all handlers + 3 ConversationHandlers in Python order | `registerHandlers`, conversations | full |
| start/connect | internal/bot/start.go | /start, /start <cid>, /start UNBLOCK-<uid> | `startCmd`, `startConnect`, `startUnblock` | full |
| send core | internal/bot/sendmsg.go | the relay: copy to target, tags, reply/quote, deletion buttons, warning | `sendMsgCore`, `warningHandle`, `addTag` | full |
| callbacks | internal/bot/callbacks.go | answer/seen/block/unblock/report/delete inline buttons | `answer`,`seen`,`block`,`report`,`deleteMsgClbk` | full |
| media groups | internal/bot/media.go | album relay across multiple updates | `handleMedia`, `mediaType` | full |
| auto-reply | internal/bot/autoreply.go | reply-to-delivered + reply-to-channel link extraction | `isAnswer`, `isReplyToChannel`, `findCID` | full |
| settings | internal/bot/settings.go | /settings conversation: name/tags/toggles/unblock | `settingsCmd`, `settingsToggle`, `updateName` | full |
| my_links | internal/bot/mylinks.go | /my_links conversation: add/remove/rename cids | `myLinksCmd`, `updateCid`, `cidTaken` | full |
| admin | internal/bot/admin.go | /admin sub-commands (ban, stats, cid, report, ai-url, backup, mass-msg) | `adminCmd`, `adminDispatch`, `adminReport` | full |
| background jobs | internal/bot/jobs.go, background.go | set_commands, AI responder loop, GM/GN greetings, hourly DB probe, mass-msg, auto-delete timers | `startBackground`, `aiResponderLoop`, `scheduleMassMsg` | full |
| AI queue | internal/bot/aiqueue.go, aichat.go | FIFO of GM-group msgs awaiting AI reply (mutex) | `aiQueue`, `aiInput` | full |
| error reports | internal/bot/errreport.go | bounded in-memory paged error detail for ERROR_CHAT_ID | `errReportStore`, `errMore` | full |
| tg error class. | internal/bot/tgerr.go | classify Telegram/DB errors by description/type | `isDBError`,`isNetworkError`,`errForbidden` | full |
| markup/texts | internal/bot/markup.go, texts_inline.go, texts_settings.go, cidchid.go, getuser.go, helpers.go, inituser.go, othermsgs.go, targetsend.go | keyboards, constant strings, small helpers | `messageKeyboard`, `handleCIDOrChID`, `chunkString` | full |
| db | internal/db/*.go | pgxpool, schema bootstrap, all queries (users/cids/blocks/reports) | `DB`, `Connect`, `MakeTables`, `AddCID` | full (test: live-PG, skipped w/o DB_HOST) |
| config | internal/config/config.go | env + .env loading, typed Config, parity parsing | `Config`, `Load` | full |
| dynset | internal/dynset/dynset.go | runtime AI url/session, persisted JSON, RWMutex | `Settings`, `AIURL`, `SetAIURL` | full |
| encoder | internal/encoder/encoder.go | reversible chevaletid cipher + cid/chevaletid gen | `EncodeChevaletID`, `DecodeChevaletID` | full (golden-tested) |
| texts | internal/texts/texts.go | cached Texts/*.txt loader (RWMutex) | `Loader`, `Get` | full |
| health | internal/health/health.go | TCP liveness socket for Docker healthcheck | `Listener`, `Listen` | full |

## 3. Entry points
- cmd/bot/main.go `main` — process start: config → DB connect+MakeTables → texts → bot.New → health.Listen → bot.Run; SIGINT/SIGTERM via signal.NotifyContext.
- internal/bot/bot.go `Run` — StartPolling (AllowedUpdates explicit) + startBackground; blocks on ctx.Done then Updater.Stop.
- Telegram updates → Dispatcher (MaxRoutines=50, concurrent) → ConversationHandlers / command / callback / message handlers, almost all wrapped by `prep`.
- GM-group AI input handler (aichat.go) — registered WITHOUT prep (group chat, not private).
- Background goroutines (background.go) — AI responder, GM/GN, hourly DB probe; time.AfterFunc one-shots for auto-delete + mass-msg.

## 4. Data flow
User DM → long-poll Updater → Dispatcher goroutine → matched handler's `prep` wrapper: ignore
edited/non-private → `users.get(uid).mu.Lock()` (held for whole handler) → `initUser` (upsert user,
ensure a cid + chevaletid) → ban check → real handler. A relay send (`sendMsgCore`) decodes the
stashed target chevaletid → resolves target uid via DB → block/ban checks → `Copy`s the message to
the target with an inline keyboard (answer/seen/report/block + donation) whose callback_data embeds
the ENCODED chevaletid + message ids → optionally tags it (custom/audio tag) → sends the sender a
"sent" + countdown-delete warning, scheduling a time.AfterFunc to strip the notice. Recipient button
presses come back as callbacks carrying that encoded chevaletid, decoded by `handleCIDOrChID` (with a
legacy plain-cid fallback) and routed to answer/seen/block/report/delete. Media albums: first item
stashes group state + returns END; later items hit `handleMedia` which re-copies the whole album.
AI: GM-group replies-to-bot are queued (`aiQueue`) and drained by `aiResponderLoop`, which POSTs to a
configurable AI endpoint and replies in the group.

## 5. Invariants
- INV-1 Single poller per bot token (Telegram 409 otherwise). Cutover = stop Python → start Go. (CUTOVER.md; bot.go:95)
- INV-2 `prep` holds the per-user `ud.mu` for the entire handler, so two updates from the SAME user never race on `convData`; different users run in parallel (up to 50). (userdata.go:16-21, prep.go:48-50) — NOTE: does NOT cover the gotgbot conversation FSM transition; see F-0002.
- INV-3 `DecodeChevaletID` is byte-for-byte compatible with the Python encoder (historical buttons). Locked by encoder golden test. (encoder.go:5-19)
- INV-4 callback_data per button ≤ 64 bytes; deletion buttons greedily packed to honor it. (sendmsg.go:393) — UNTESTED; see F-0009.
- INV-5 DB is autocommit, one statement per pooled conn; pgx rows always closed (CollectRows or defer+Err). (db.go:8-12) — holds across the db package.
- INV-6 Schema is identical to Python prod; MakeTables is CREATE IF NOT EXISTS (no-op on prod). (db.go:2-6, 91-126)
- INV-7 AllowedUpdates is set explicitly so polling never silently stops delivering callback_query. (bot.go:96-104)

## 6. Smells / open questions
- Background time.AfterFunc work and goroutines are not awaited at shutdown; mass-msg can run against a closed pool → F-0001/F-0003.
- Panics are recovered (process is safe) but bypass onError → no user CID, no ERROR_CHAT report → F-0006.
- DB-outage error reports are per-update with no throttle → flood at 10k users → F-0007.
- internal/bot has no tests at all; the highest-risk pure logic (deletion-button packing, channel-link regex) is unverified → F-0009/F-0010.
- `/admin report get all` chunk buffer never resets → duplicated sends → F-0012.
- report flow hard-fails when REPORT_CHAT_ID unset → F-0011.
- userStore map never evicts → unbounded growth → F-0004.
- cidTaken does full-column scans on each rename → F-0014.
- Media-group window is ~0.6s wall-clock; may truncate albums under load → F-0013.
- Clean categories (verified): pgx rows/resource handling in internal/db; dynset/aiqueue/errreport/texts mutex usage; AllowedUpdates handling; the documented fixes vs Python (AddCID retry, transformKeyboard, %-format safety) are sound.

## 7. Coverage ledger
40 Go source/test files read in FULL (top to bottom). 0 sampled, 0 skipped. Non-Go assets
(Texts/*.txt, deploy/*, Dockerfile, shell scripts, CI) were not exhaustively read; CUTOVER.md and
docker-compose.prod.yml were read to validate cutover/shutdown findings. gotgbot ext (dispatcher.go,
updater.go, conversation handler + in-memory storage) was read from the module cache to verify the
concurrency/shutdown/panic model underpinning F-0001/F-0002/F-0006.
Tests present: internal/encoder (golden + round-trip, always runs), internal/db (integration,
skipped unless DB_HOST set). internal/bot: NONE.
