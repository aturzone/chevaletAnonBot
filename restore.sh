#!/bin/bash
# Restore PostgreSQL database

# Load environment variables from .env
if [ -f .env ]; then
    set -a
    source <(grep -v '^#' .env | grep -v '^$' | sed 's/#.*//')
    set +a
fi

set -e

if [ -z "$1" ]; then
    echo "Usage: ./restore.sh <backup_file.sql.gz>"
    echo ""
    echo "Available backups:"
    ls -lh ./backups/
    exit 1
fi

BACKUP_FILE="$1"
CONTAINER="telegram-bot-db"
DB_NAME="${DB_NAME:-mydatabase}"
DB_USER="${DB_USER:-botuser}"

if [ ! -f "$BACKUP_FILE" ]; then
    echo "Error: File not found: $BACKUP_FILE"
    exit 1
fi

echo "WARNING: This will replace the current database!"
read -p "Continue? (yes/no): " CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo "Cancelled."
    exit 0
fi

echo "Stopping bot..."
docker compose stop bot

echo "Restoring..."
if [[ "$BACKUP_FILE" == *.gz ]]; then
    gunzip -c "$BACKUP_FILE" | docker exec -i "$CONTAINER" psql -U "$DB_USER" -d postgres
else
    docker exec -i "$CONTAINER" psql -U "$DB_USER" -d postgres < "$BACKUP_FILE"
fi

echo "Restored successfully!"

echo "Starting bot..."
docker compose start bot

echo "Done!"
