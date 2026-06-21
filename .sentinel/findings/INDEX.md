# Findings index — ChevaletAnonBot

Generated from finding files at SHA eb19359 (branch go-public). 14 open, 0 resolved.

## By severity

### High (3)
- [F-0001](F-0001.md) concurrency — Background timers/goroutines not awaited on shutdown; mass-msg can run against a closed DB pool — `internal/bot/jobs.go:247`
- [F-0006](F-0006.md) error-handling — Panics bypass onError (no user CID, no ERROR_CHAT report); Panic handler unset — `internal/bot/bot.go:85`
- [F-0009](F-0009.md) missing-test — Deletion-button callback-data packing (warningHandle/deleteMsgClbk) untested — `internal/bot/sendmsg.go:388`

### Medium (6)
- [F-0002](F-0002.md) concurrency — Conversation FSM state read/written outside the per-user lock (TOCTOU for one user's two near-simultaneous updates) — `internal/bot/handlers.go:194`
- [F-0003](F-0003.md) concurrency — scheduleMassMsg captures *ext.Context and uses it 7s later in a goroutine — `internal/bot/background.go:245`
- [F-0007](F-0007.md) error-handling — Brief DB outage floods ERROR_CHAT_ID (one report per update, no throttle) — `internal/bot/prep.go:115`
- [F-0010](F-0010.md) missing-test — Channel-reply regex extraction + rune-boundary/format helpers untested — `internal/bot/autoreply.go:86`
- [F-0011](F-0011.md) input-validation — report flow hard-fails (files nothing) when REPORT_CHAT_ID unset — `internal/bot/callbacks.go:362`
- [F-0012](F-0012.md) bug — "/admin report get all" never resets its chunk buffer → duplicated, growing sends — `internal/bot/admin.go:268`

### Low (5)
- [F-0004](F-0004.md) concurrency — userStore map grows unboundedly (no eviction) — `internal/bot/userdata.go:73`
- [F-0005](F-0005.md) resource-leak — mass-msg failure file removed even when the send fails — `internal/bot/background.go:281`
- [F-0008](F-0008.md) error-handling — DB unavailable at startup is fatal; relies on restart policy (present) — `cmd/bot/main.go:40`
- [F-0013](F-0013.md) edge — Media-group window ~0.6s; later album items dropped under load — `internal/bot/media.go:43`
- [F-0014](F-0014.md) perf — cidTaken full-column scans on each rename (scales with users) — `internal/bot/mylinks.go:336`

## Clusters (themes)

- **concurrency**: F-0001, F-0002, F-0003, F-0004
- **error-handling**: F-0006, F-0007, F-0008
- **test-coverage**: F-0009, F-0010
- **resource-leak**: F-0005
- **input-validation**: F-0011
- **edge**: F-0012, F-0013
- **perf**: F-0014

## Strong-edge adjacency (file / symbol / module / invariant)

- F-0001 — F-0003 (same scheduleMassMsg symbol + shutdown ordering)
- F-0001 — F-0002 (background/concurrency model), F-0002 — F-0004 (per-user state model)
- F-0006 — F-0007 — F-0008 (error-handling module)
- F-0006 — F-0011 (both: user sees a tracking code for a non-bug condition)
- F-0009 — F-0010 (internal/bot test gap; INV-4)
