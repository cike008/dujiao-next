#!/usr/bin/env bash

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/home/deploy/backups/dujiao-next-daily}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-dujiaonext-postgres}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
LOCK_FILE="$BACKUP_DIR/database-backup.lock"
STAMP="$(date -u +%Y%m%d%H%M%S)"
FINAL_DUMP="$BACKUP_DIR/postgres-$STAMP.dump"
TEMP_DUMP="$BACKUP_DIR/.postgres-$STAMP.dump.tmp"

umask 077
mkdir -p "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"
exec 9>"$LOCK_FILE"
flock -n 9 || exit 0

cleanup() {
  rm -f "$TEMP_DUMP"
}
trap cleanup EXIT

docker inspect "$POSTGRES_CONTAINER" --format '{{.State.Status}}' | grep -qx running
docker exec "$POSTGRES_CONTAINER" sh -ec \
  'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' >"$TEMP_DUMP"
test -s "$TEMP_DUMP"
docker exec -i "$POSTGRES_CONTAINER" sh -ec \
  'pg_restore --list >/dev/null' <"$TEMP_DUMP"

mv "$TEMP_DUMP" "$FINAL_DUMP"
sha256sum "$FINAL_DUMP" >"$FINAL_DUMP.sha256"

find "$BACKUP_DIR" -maxdepth 1 -type f \
  \( -name 'postgres-*.dump' -o -name 'postgres-*.dump.sha256' \) \
  -mtime "+$RETENTION_DAYS" -delete

echo "database_backup=ok"
echo "backup_file=$FINAL_DUMP"
echo "retention_days=$RETENTION_DAYS"
