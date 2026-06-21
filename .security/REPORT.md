# Security Audit Report — ChevaletAnonBot (Go)

Target: C:\projects\chevalet-build | Date: 2026-06-21 | Mode: audit (first run)
Auditor: Security Auditor (read-only toward source; findings under .security/ only)

Pre-existing gates already passed (per task): Python-parity audit, go vet,
staticcheck, govulncheck. This audit targets logic/abuse/anonymity issues those
do not surface.

## Counts by severity
- Critical: 0
- High:     4  (S-0001, S-0002, S-0003, S-0004)
- Medium:   3  (S-0005, S-0006, S-0007)
- Low:      3  (S-0008, S-0009, S-0010)

## Findings by severity, then OWASP

### High
- A01 Broken Access Control
  - [S-0001] BOLA/IDOR: seen/report/delete callbacks act on an attacker-supplied
    message id with no ownership binding. internal/bot/callbacks.go:114
- A04 Insecure Design
  - [S-0002] chevaletid cipher is keyless obfuscation; tokens self-describe and
    decode offline (key=0 == plaintext). internal/encoder/encoder.go:76
  - [S-0004] No rate limiting anywhere: anonymous-spam amplifier, report-bombing,
    unbounded in-memory user map. internal/bot/prep.go:27
- A03 Injection
  - [S-0003] Stored HTML/entity injection via display name & tags rendered into
    other users' chats with ParseMode HTML. internal/bot/settings.go:213

### Medium
- A09 Logging/Monitoring (info exposure)
  - [S-0005] Handler errors echo raw Go error (%T|%v) to the chat (admin path).
    internal/bot/admin.go:45
- A02 Cryptographic Failures
  - [S-0006] cids/chevaletids/report+error ids use math/rand/v2, not crypto/rand.
    internal/encoder/encoder.go:22
- A10 SSRF
  - [S-0007] admin-settable AI_URL POSTed server-side with no allowlist.
    internal/bot/background.go:200

### Low
- A01 Broken Access Control
  - [S-0008] /start UNBLOCK-<uid> & /admin link reveal @username + tg:// link for a
    supplied uid. internal/bot/start.go:72
- A05 Misconfiguration
  - [S-0009] dynamic_settings.json (AI session id) & mass-msg uid list written 0644.
    internal/dynset/dynset.go:63
- A04 Insecure Design
  - [S-0010] GM-group AI queue unbounded; per-message admin-controlled outbound HTTP.
    internal/bot/aiqueue.go:20

## Exploitable vs theoretical (honest)
- Practically exploitable by a normal user TODAY:
  - S-0003 stored HTML injection (set name -> victim sees attacker link). Clear PoC.
  - S-0004 anonymous-spam amplifier + report-bombing (just script the public flow).
  - S-0001 to the extent of notification spoofing (seen) and copying an
    attacker-chosen message id out of a victim's chat into the report channel;
    arbitrary deletion is capped by Telegram's bot-delete rules, but the
    authorization is still made from forgeable input.
- Enabling weakness / real but lower direct impact:
  - S-0002 keyless cipher — does not alone reveal the uid (DB-gated), but enables
    cross-message correlation and is the forgery substrate behind S-0001; the
    product's stated anonymity guarantee (SECURITY.md) is weaker than implied.
  - S-0006 predictable ids — meaningful mainly combined with S-0004 (cid
    enumeration) and S-0001 (forgery).
- Conditional / requires a role:
  - S-0007 SSRF — needs admin (or admin compromise); high impact if admin auth weak.
  - S-0005 — admin chat only as written; flagged to stop the pattern spreading.
- Low / local-host or low-sensitivity:
  - S-0008 username disclosure; S-0009 file perms; S-0010 GM-group-scoped.

## Coverage ledger
Reviewed IN FULL (every line read):
  internal/encoder/encoder.go (+ test, golden.json sampled), internal/config/config.go,
  internal/db/* (db.go, users.go, blocks.go, cids.go, reports.go),
  internal/bot/{callbacks,cidchid,start,prep,sendmsg,markup,helpers,getuser,
  background,handlers,bot,mylinks,autoreply,aichat,settings,othermsgs,userdata,
  aiqueue,errreport,inituser,jobs,media,tgerr,targetsend,texts_inline}.go,
  internal/dynset/dynset.go, internal/texts/texts.go, internal/health/health.go,
  cmd/bot/main.go, Dockerfile, .github/workflows/ci.yml, .env.example, .gitignore,
  SECURITY.md.
Reviewed by TARGETED GREP (not line-by-line): all ParseMode/HTML/OriginalHTML sinks,
  all getUsername/GetName/hrefUser uses, math/rand vs crypto/rand usage.
SAMPLED only:
  internal/encoder/testdata/golden.json (first ~40 lines; enough to confirm key=0
  identity property), Texts/*.txt (static templates; trusted, not user-controlled),
  internal/bot/texts_settings.go (string constants; not read in full).
NOT REVIEWED / out of scope this pass:
  internal/db/db_test.go, deploy/* compose & docs, backup*.sh / restore.sh /
  setup-backup-cron.sh (shell ops scripts — recommend a follow-up pass for command
  injection / secret handling in backups), Makefile, docker-compose*.yml secret
  wiring (env-file usage assumed; not verified against the host).
NOT verified dynamically: no code executed, no live system tested (read-only audit).

## Recommended order of fixing
1. S-0001 (BOLA) and S-0003 (stored HTML injection) — directly user-exploitable.
2. S-0004 (rate limiting) — required before 10k-user launch; gates spam/abuse.
3. S-0002 + S-0006 together — move to keyed/authenticated tokens + crypto/rand.
4. S-0007 (SSRF allowlist), S-0005 (stop reflecting errors).
5. S-0008, S-0009, S-0010.

## Suggested skills per finding
- S-0001, S-0008 -> sec-authz-review (BOLA/IDOR)
- S-0003, S-0005 -> sec-injection
- S-0002, S-0006, S-0009 -> sec-secrets-crypto
- S-0007 -> sec-ssrf-traversal
- S-0004, S-0010 -> sec-attacker-review (abuse/DoS modeling)
