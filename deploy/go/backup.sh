#!/bin/sh
# Hourly production DB backup for the Go bot.
#
# Dumps telegram-bot-db using the postgres container's OWN credentials (so it is
# correct regardless of the app .env) into the host dir that is bind-mounted to
# the bot's /app/backups — so the `/admin backup` command can send the newest
# file. Keeps the newest 100 backups.
#
# NOTE: no `docker exec -t` here — cron has no TTY and -t both fails and corrupts
# the piped binary (that was the old backup-tiered.sh bug).
#
# Cron (hourly):
#   0 * * * * /opt/chevalet-go-staging/deploy/go/backup.sh >> /opt/chevalet-go-staging/logs/backup.log 2>&1
set -e

BACKUP_DIR=/opt/chevalet-go-staging/backups
DB_CONTAINER=telegram-bot-db
KEEP=100

mkdir -p "$BACKUP_DIR"
TS=$(date +%Y%m%d_%H%M%S)
OUT="$BACKUP_DIR/backup_mydatabase_${TS}.sql.gz"

docker exec "$DB_CONTAINER" sh -c \
  'PGPASSWORD=$POSTGRES_PASSWORD pg_dump -U $POSTGRES_USER -d $POSTGRES_DB --clean --if-exists --create --encoding=UTF8' \
  | gzip > "$OUT"

# Guard against a truncated/empty dump being kept as a "backup".
SIZE=$(stat -c%s "$OUT" 2>/dev/null || echo 0)
if [ "$SIZE" -lt 1000 ]; then
  echo "$(date -u +%FT%TZ) ERROR: backup too small ($SIZE bytes), removing $OUT"
  rm -f "$OUT"
  exit 1
fi

# Retention: keep the newest $KEEP backups.
ls -1t "$BACKUP_DIR"/backup_*.sql.gz 2>/dev/null | tail -n +$((KEEP + 1)) | xargs -r rm -f

echo "$(date -u +%FT%TZ) backup ok: $OUT ($(du -h "$OUT" | cut -f1))"
