#!/bin/bash
# Backup PostgreSQL database with tiered retention
# Keeps: hourly (24h), daily (7d), weekly (30d)

# Load environment variables from .env
if [ -f .env ]; then
    set -a
    source <(grep -v '^#' .env | grep -v '^$' | sed 's/#.*//')
    set +a
fi

set -e

BACKUP_DIR="./backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
CONTAINER="telegram-bot-db"
DB_NAME="${DB_NAME:-mydatabase}"
DB_USER="${DB_USER:-botuser}"

mkdir -p "$BACKUP_DIR"

BACKUP_FILE="$BACKUP_DIR/backup_${DB_NAME}_${TIMESTAMP}.sql.gz"

echo "Starting backup..."
docker exec -t "$CONTAINER" pg_dump -U "$DB_USER" -d "$DB_NAME" \
    --clean --if-exists --create --encoding=UTF8 | gzip > "$BACKUP_FILE"

SIZE=$(du -h "$BACKUP_FILE" | cut -f1)
echo "Backup completed: $BACKUP_FILE (Size: $SIZE)"

# Tiered retention cleanup
echo "Applying tiered retention policy..."

NOW=$(date +%s)
HOUR_AGO=$((NOW - 3600))
DAY_AGO=$((NOW - 86400))
WEEK_AGO=$((NOW - 604800))
MONTH_AGO=$((NOW - 2592000))

# Arrays to track which backups to keep
declare -A keep_backups
declare -A daily_kept
declare -A weekly_kept

# Process all backup files
for file in "$BACKUP_DIR"/backup_*.sql.gz; do
    [ -e "$file" ] || continue

    # Extract timestamp from filename: backup_dbname_YYYYMMDD_HHMMSS.sql.gz
    filename=$(basename "$file")
    timestamp=$(echo "$filename" | sed -n 's/.*_\([0-9]\{8\}_[0-9]\{6\}\)\.sql\.gz/\1/p')

    if [ -z "$timestamp" ]; then
        continue
    fi

    # Convert timestamp to epoch (format: YYYYMMDD_HHMMSS -> YYYY-MM-DD HH:MM:SS)
    year=${timestamp:0:4}
    month=${timestamp:4:2}
    day=${timestamp:6:2}
    hour=${timestamp:9:2}
    minute=${timestamp:11:2}
    second=${timestamp:13:2}
    backup_date="$year-$month-$day $hour:$minute:$second"
    backup_epoch=$(date -d "$backup_date" +%s 2>/dev/null || echo "0")

    if [ "$backup_epoch" -eq 0 ]; then
        continue
    fi

    age=$((NOW - backup_epoch))
    day_key=$(date -d "@$backup_epoch" +%Y%m%d)
    week_key=$(date -d "@$backup_epoch" +%Y%U)

    # Tier 1: Keep all backups from last 24 hours
    if [ $backup_epoch -gt $DAY_AGO ]; then
        keep_backups["$file"]=1
        echo "  [HOURLY] Keeping: $filename ($(($age / 3600))h old)"
        continue
    fi

    # Tier 2: Keep one backup per day for last 7 days
    if [ $backup_epoch -gt $WEEK_AGO ] && [ $backup_epoch -le $DAY_AGO ]; then
        if [ -z "${daily_kept[$day_key]}" ]; then
            keep_backups["$file"]=1
            daily_kept[$day_key]=1
            echo "  [DAILY] Keeping: $filename (day $day_key)"
            continue
        fi
    fi

    # Tier 3: Keep one backup per week for last 30 days
    if [ $backup_epoch -gt $MONTH_AGO ] && [ $backup_epoch -le $WEEK_AGO ]; then
        if [ -z "${weekly_kept[$week_key]}" ]; then
            keep_backups["$file"]=1
            weekly_kept[$week_key]=1
            echo "  [WEEKLY] Keeping: $filename (week $week_key)"
            continue
        fi
    fi

    # Delete backups not in any tier
    echo "  [DELETE] Removing: $filename ($(($age / 86400))d old)"
    rm -f "$file"
done

echo "Retention policy applied!"
echo "Total backups retained: ${#keep_backups[@]}"
echo "Done!"
