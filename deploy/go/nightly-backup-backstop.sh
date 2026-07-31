#!/usr/bin/env bash
# Nightly DB-backup backstop for the Go bot.
#
# The bot itself sends the newest backup to BACKUP_CHAT_ID (a DM or a private
# channel) once a night at BACKUP_TIME (see internal/bot/background.go:
# backupLoop/sendLatestBackupTo). This script is a safety net that runs a few
# minutes later and re-sends the same file ONLY if that in-process job didn't
# already succeed tonight — otherwise the destination gets the backup twice.
#
# Dedup: sendLatestBackupTo (re)touches backups/.nightly-backup-sent on every
# successful send. This script skips if that marker is fresh (< 6h old).
#
# A prior version of this script deduped by grepping `docker logs --since`,
# which turned out to return zero lines regardless of the requested window in
# this environment — the dedup silently never matched, so the backstop fired
# (and double-sent) every single night. See internal/bot/background.go's
# nightlySentMarker doc comment for the full story. Do not revert to that
# approach without first confirming `docker logs --since` is reliable here.
#
# Cron (a few minutes after BACKUP_TIME in the .env.prod timezone, Asia/Tehran):
#   40 20 * * * /opt/chevalet-go-staging/deploy/go/nightly-backup-backstop.sh >> /opt/chevalet-go-staging/logs/backup-backstop.log 2>&1
#
# Usage: nightly-backup-backstop.sh [force]
#   force - skip the dedup check and send unconditionally (manual testing).
set -uo pipefail

ENVP=/opt/chevalet-go-staging/deploy/go/.env.prod
BDIR=/opt/chevalet-go-staging/backups
LOG=/opt/chevalet-go-staging/logs/backup-backstop.log
MARKER="$BDIR/.nightly-backup-sent"
DEDUP_WINDOW_SECONDS=$((6 * 3600))

mkdir -p "$(dirname "$LOG")"
ts(){ date -u +%FT%TZ; }

BOT_TOKEN=$(grep -E '^BOT_TOKEN=' "$ENVP" | cut -d= -f2- | tr -d '\r')
CHAT_ID=$(grep -E '^BACKUP_CHAT_ID=' "$ENVP" | cut -d= -f2- | tr -d '\r')
FORCE="${1:-}"

if [ "$FORCE" != "force" ] && [ -f "$MARKER" ]; then
  MARKER_MTIME=$(stat -c %Y "$MARKER" 2>/dev/null || stat -f %m "$MARKER" 2>/dev/null || echo 0)
  AGE=$(( $(date +%s) - MARKER_MTIME ))
  if [ "$AGE" -lt "$DEDUP_WINDOW_SECONDS" ]; then
    echo "$(ts) skip: bot already sent tonight (marker ${AGE}s old)" >> "$LOG"
    exit 0
  fi
fi

NEWEST=$(ls -1t "$BDIR"/backup_*.sql.gz 2>/dev/null | head -1)
[ -z "$NEWEST" ] && { echo "$(ts) ERROR no backup file" >> "$LOG"; echo NO_FILE; exit 1; }

RESP=$(curl -sS --max-time 150 -F chat_id="$CHAT_ID" -F document=@"$NEWEST" -F caption="DB backup (backstop): $(basename "$NEWEST")" "https://api.telegram.org/bot${BOT_TOKEN}/sendDocument")
if echo "$RESP" | grep -q '"ok":true'; then
  echo "$(ts) sent $(basename "$NEWEST")" >> "$LOG"; echo SENT_OK
else
  echo "$(ts) FAILED: $(echo "$RESP" | tr -d '\n' | head -c 180)" >> "$LOG"; echo SENT_FAIL
fi
