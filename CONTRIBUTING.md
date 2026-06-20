# Contributing to ChevaletAnonBot

Thanks for your interest in improving ChevaletAnonBot! This document covers how
to build, test, and submit changes.

## Development setup

You need [Go](https://go.dev/dl/) 1.25+ (see `go.mod` for the exact version).

```sh
git clone https://github.com/aturzone/chevaletAnonBot.git
cd chevaletAnonBot
go build ./...
go test ./...
```

To run the bot locally you need a populated `.env` (copy `.env.example`) and a
reachable PostgreSQL. Telegram is usually unreachable without a `PROXY`.

```sh
go run ./cmd/bot
```

For an isolated, throwaway end-to-end run against a fresh Postgres, use the
staging stack in [`deploy/go/`](deploy/go/README.md).

## Before you open a pull request

CI runs the following on every push — please run them locally first:

```sh
go build ./...
go vet ./...
gofmt -l .        # must print nothing; run `gofmt -w .` to fix
go test -race ./...
```

The `Makefile` has shortcuts: `make go-build`, `make go-test`, `make go-vet`,
`make go-fmt`.

## ⚠️ Do NOT change the chevaletid cipher

`internal/encoder` implements the encoding embedded in the inline-keyboard
`callback_data` of **every message already delivered to a user**. Changing it
would break the reply / seen / block / report buttons on all historical
messages. It is a frozen compatibility contract, locked by the golden-vector
test in `internal/encoder/encoder_test.go`. Don't touch it unless you truly know
what you're doing — and never in a way that changes its output.

## Commit messages

This project uses short conventional-style prefixes:

- `feat` — a new feature of value
- `fix` — a bug fix
- `maint` — maintenance: refactors, dependency bumps, chores
- `docs` — documentation only
- `ci` — CI / build configuration

Keep commits focused and the subject line in the imperative mood.

## Code style

- Standard `gofmt` formatting (enforced by CI).
- Keep new code consistent with the surrounding package — match its naming,
  comment density, and idioms.
- Behavior is intentionally faithful to the original bot; if you change
  user-visible behavior, call it out explicitly in the PR description.

## Reporting bugs & requesting features

Use the GitHub issue templates. For anything security- or privacy-sensitive,
follow [SECURITY.md](SECURITY.md) instead of opening a public issue.
