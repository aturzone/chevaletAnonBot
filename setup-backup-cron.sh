#!/bin/bash
# Setup automated hourly backups using cron

set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CRON_CMD="cd $PROJECT_DIR && bash $PROJECT_DIR/backup-tiered.sh >> $PROJECT_DIR/logs/backup.log 2>&1"

echo "Setting up hourly backup cron job..."
echo "Project directory: $PROJECT_DIR"
echo ""

# Check if cron is available
if ! command -v crontab &> /dev/null; then
    echo "ERROR: crontab command not found."
    echo "Please install cron or use Windows Task Scheduler (see setup-backup-windows.txt)"
    exit 1
fi

# Create logs directory if it doesn't exist
mkdir -p "$PROJECT_DIR/logs"

# Check if cron job already exists
if crontab -l 2>/dev/null | grep -q "backup-tiered.sh"; then
    echo "Backup cron job already exists:"
    crontab -l | grep "backup-tiered.sh"
    echo ""
    read -p "Do you want to replace it? (yes/no): " replace
    if [ "$replace" != "yes" ]; then
        echo "Aborted."
        exit 0
    fi
    # Remove existing backup cron job
    crontab -l 2>/dev/null | grep -v "backup-tiered.sh" | crontab -
fi

# Add new cron job (runs every hour at minute 0)
(crontab -l 2>/dev/null; echo "0 * * * * $CRON_CMD") | crontab -

echo "✓ Cron job added successfully!"
echo ""
echo "Cron schedule: Every hour (at minute 0)"
echo "Command: $CRON_CMD"
echo ""
echo "Current crontab:"
crontab -l
echo ""
echo "Logs will be written to: $PROJECT_DIR/logs/backup.log"
echo ""
echo "To remove this cron job later, run:"
echo "  crontab -e"
echo "  # then delete the line containing 'backup-tiered.sh'"
