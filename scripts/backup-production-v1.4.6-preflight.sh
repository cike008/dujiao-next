#!/usr/bin/env bash

set -euo pipefail

HOST="${1:-dujiao-vps}"
REMOTE_SCRIPT="/tmp/dujiao-next-v1.4.6-preflight-backup.sh"

echo "Uploading pre-upgrade backup helper to ${HOST}:${REMOTE_SCRIPT}..."

ssh "$HOST" "cat > '$REMOTE_SCRIPT' && chmod 700 '$REMOTE_SCRIPT'" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "This backup helper must run with sudo/root on the VPS." >&2
  echo "Run: sudo bash /tmp/dujiao-next-v1.4.6-preflight-backup.sh" >&2
  exit 1
fi

TS="$(date +%Y%m%d%H%M%S)"
BACKUP_DIR="/opt/backups/dujiao-next-v1.4.6-preflight-${TS}"

echo "backup_dir=${BACKUP_DIR}"
install -d -m 750 "$BACKUP_DIR"

echo "--- capture runtime inventory ---"
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}' | tee "$BACKUP_DIR/docker-ps.txt" >/dev/null
if [ -d /opt/dujiao-next ]; then
  (
    cd /opt/dujiao-next
    docker compose ps
    printf '\n--- services ---\n'
    docker compose config --services
    printf '\n--- images ---\n'
    docker compose config --images
  ) | tee "$BACKUP_DIR/dujiao-compose-inventory.txt" >/dev/null
fi

echo "--- copy dujiao config files ---"
install -d -m 750 "$BACKUP_DIR/dujiao-next"
for file in /opt/dujiao-next/.env /opt/dujiao-next/docker-compose.yml /opt/dujiao-next/config/config.yml; do
  if [ -f "$file" ]; then
    cp -a "$file" "$BACKUP_DIR/dujiao-next/"
  fi
done

echo "--- dump postgres ---"
if docker ps --format '{{.Names}}' | grep -qx 'dujiaonext-postgres'; then
  docker exec dujiaonext-postgres pg_dump -U dujiao -d dujiao -Fc > "$BACKUP_DIR/postgres-dujiao.dump"
  docker exec -i dujiaonext-postgres pg_restore -l < "$BACKUP_DIR/postgres-dujiao.dump" > "$BACKUP_DIR/postgres-dujiao.dump.list"
else
  echo "WARN: dujiaonext-postgres container not found; database dump skipped" | tee "$BACKUP_DIR/WARN-postgres.txt" >/dev/null
fi

echo "--- archive uploads and logs metadata ---"
if [ -d /opt/dujiao-next/data/uploads ]; then
  tar -C /opt/dujiao-next/data -czf "$BACKUP_DIR/uploads.tar.gz" uploads
fi
if [ -d /opt/dujiao-next/data/logs ]; then
  find /opt/dujiao-next/data/logs -maxdepth 1 -type f -printf '%TY-%Tm-%Td %TH:%TM %s %p\n' | tee "$BACKUP_DIR/log-files.txt" >/dev/null || true
fi

echo "--- archive storefront runtime ---"
if [ -d /opt/unicard-themes ]; then
  tar -C /opt -czf "$BACKUP_DIR/unicard-themes.tar.gz" unicard-themes
fi

echo "--- archive teamgenie runtime ---"
if [ -d /home/deploy/runtime/teamgenie-sync-service ]; then
  tar -C /home/deploy/runtime -czf "$BACKUP_DIR/teamgenie-sync-runtime.tar.gz" teamgenie-sync-service
fi

echo "--- capture nginx config ---"
if command -v nginx >/dev/null 2>&1; then
  nginx -T > "$BACKUP_DIR/nginx-full.conf" 2> "$BACKUP_DIR/nginx-full.stderr" || true
fi

echo "--- final backup summary ---"
find "$BACKUP_DIR" -maxdepth 2 -type f -printf '%s %p\n' | sort -n
echo "Backup completed: $BACKUP_DIR"
REMOTE

echo
echo "The helper has been uploaded. Run this on the VPS to create the backup:"
echo
echo "  sudo bash $REMOTE_SCRIPT"
