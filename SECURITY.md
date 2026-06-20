# Security Policy

ChevaletAnonBot is built around **user anonymity**, so security and privacy bugs
are taken seriously — especially anything that could deanonymize a user or link
a `chevaletid` back to a real Telegram account.

## Reporting a vulnerability

**Please do not open a public issue for security or privacy vulnerabilities.**

Instead, report privately via one of:

- GitHub's [private vulnerability reporting](https://github.com/aturzone/chevaletAnonBot/security/advisories/new)
  (Security → Report a vulnerability), or
- a direct message to the project's support admin (the `SUPPORT_ADMIN` configured
  for the running bot).

Please include:

- a description of the issue and its impact (e.g. potential deanonymization,
  data exposure, privilege escalation),
- steps to reproduce or a proof of concept,
- the affected version / commit if known.

We will acknowledge your report, investigate, and coordinate a fix and
disclosure timeline with you. Thank you for helping keep users safe.

## Scope highlights

- The `chevaletid` cipher (`internal/encoder`) — any way to recover a Telegram
  user id from a `callback_data` token without the database.
- The database layer (`internal/db`) — injection or data-exposure issues.
- The admin surface (`/admin`) — authorization bypass.
- Any path that leaks one user's identity to another.
