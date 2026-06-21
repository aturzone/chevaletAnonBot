# MASTER CLAUDE — team roster

Project: ChevaletAnonBot (Go port) — anonymous-message Telegram bot
(gotgbot + pgx/Postgres). Pre-production review before a shared-DB cutover
replacing a live Python bot with 10,000+ users.

Mode: **God Mode + Essential** — continuous pre-cutover review while the human
testers haven't started.

| Member | Role | Why on this team |
|---|---|---|
| **Sentinel** (agent) | Project cartographer | Live map + finds gaps, bugs, missing tests, risky code, dead code — the runtime/logic/concurrency issues that go vet/staticcheck/govulncheck and the Python-parity audit don't catch. Writes `.sentinel/`. |
| **Security Auditor** (agent) | Front→back security audit | This bot's whole value is anonymity; audits deanonymization (chevaletid cipher), BOLA/IDOR via callback_data & deep-links, admin authz, injection, abuse/DoS, SSRF. Writes `.security/`. |

Already done before this team: deep Python→Go parity audit (6 agents),
`go vet` / `gofmt` / `staticcheck` / `govulncheck` — all clean.

Standing by to add: testmedic (flaky tests), debtradar (hotspots), guardian
(verification guardrails) if the work shifts.
