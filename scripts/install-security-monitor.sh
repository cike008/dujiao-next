#!/usr/bin/env bash

set -euo pipefail

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OPS_DIR="${OPS_DIR:-/home/deploy/ops/dujiao-security}"
CRON_BEGIN="# BEGIN DUJIAO SECURITY MONITOR"
CRON_END="# END DUJIAO SECURITY MONITOR"
MONITOR_CRON_ENTRY="17 */6 * * * $OPS_DIR/monitor-security-hardening.sh >/dev/null 2>&1"
BACKUP_CRON_ENTRY="43 19 * * * $OPS_DIR/backup-production-database.sh >>$OPS_DIR/database-backup.log 2>&1"

mkdir -p "$OPS_DIR"
install -m 0755 "$SOURCE_DIR/verify-security-hardening-v1.4.8.sh" "$OPS_DIR/verify-security-hardening-v1.4.8.sh"
install -m 0755 "$SOURCE_DIR/monitor-security-hardening.sh" "$OPS_DIR/monitor-security-hardening.sh"
install -m 0755 "$SOURCE_DIR/backup-production-database.sh" "$OPS_DIR/backup-production-database.sh"

existing_crontab="$(crontab -l 2>/dev/null || true)"
filtered_crontab="$(printf '%s\n' "$existing_crontab" | awk \
  -v begin="$CRON_BEGIN" -v end="$CRON_END" '
    $0 == begin { skipping = 1; next }
    $0 == end { skipping = 0; next }
    !skipping { print }
  ')"

{
  printf '%s\n' "$filtered_crontab"
  printf '%s\n' "$CRON_BEGIN" "$MONITOR_CRON_ENTRY" "$BACKUP_CRON_ENTRY" "$CRON_END"
} | awk 'NF || previous_nonempty { print } { previous_nonempty = NF }' | crontab -

OPS_DIR="$OPS_DIR" "$OPS_DIR/monitor-security-hardening.sh"
"$OPS_DIR/backup-production-database.sh" >>"$OPS_DIR/database-backup.log" 2>&1

echo "Security monitor installed."
echo "monitor_schedule=$MONITOR_CRON_ENTRY"
echo "backup_schedule=$BACKUP_CRON_ENTRY"
echo "log=$OPS_DIR/security-monitor.log"
echo "failure_marker=$OPS_DIR/security-monitor.failed"
