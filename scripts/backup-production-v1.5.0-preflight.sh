#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $(id -u) != 0 ]]; then
  echo 'Run with sudo on the VPS.' >&2
  exit 1
fi

APP_DIR=/opt/dujiao-next
BACKUP_DIR="/opt/backups/dujiao-next-v1.5.0-preflight-$(date -u +%Y%m%d%H%M%S)"
for file in "$APP_DIR/docker-compose.yml" "$APP_DIR/.env" "$APP_DIR/config/config.yml"; do
  test -s "$file"
done
docker inspect dujiaonext --format '{{.State.Health.Status}}' | grep -qx healthy
docker inspect dujiaonext-postgres --format '{{.State.Status}}' | grep -qx running
install -d -m 700 "$BACKUP_DIR/dujiao-next"
echo "backup_dir=$BACKUP_DIR"

docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}' > "$BACKUP_DIR/docker-ps.txt"
docker inspect dujiaonext --format '{{.Config.Image}}' > "$BACKUP_DIR/backend-image.txt"
install -m 600 "$APP_DIR/docker-compose.yml" "$APP_DIR/.env" "$APP_DIR/config/config.yml" "$BACKUP_DIR/dujiao-next/"
(
  cd "$APP_DIR"
  docker compose config --services
  docker compose config --images
) > "$BACKUP_DIR/compose-inventory.txt"

echo 'Creating and validating PostgreSQL backup.'
docker exec dujiaonext-postgres sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$BACKUP_DIR/postgres-dujiao.dump.tmp"
test -s "$BACKUP_DIR/postgres-dujiao.dump.tmp"
docker exec -i dujiaonext-postgres pg_restore --list < "$BACKUP_DIR/postgres-dujiao.dump.tmp" > "$BACKUP_DIR/postgres-dujiao.dump.list"
mv "$BACKUP_DIR/postgres-dujiao.dump.tmp" "$BACKUP_DIR/postgres-dujiao.dump"
test -s "$BACKUP_DIR/postgres-dujiao.dump.list"

tar -C "$APP_DIR/data" -czf "$BACKUP_DIR/uploads.tar.gz" uploads
tar -h -C /opt/unicard-themes -czf "$BACKUP_DIR/storefront-current.tar.gz" dist
readlink -f /opt/unicard-themes/dist > "$BACKUP_DIR/storefront-release.txt"
tar -C /home/deploy/runtime -czf "$BACKUP_DIR/teamgenie-sync-runtime.tar.gz" teamgenie-sync-service
nginx -T > "$BACKUP_DIR/nginx-full.conf" 2> "$BACKUP_DIR/nginx-full.stderr"
(
  cd "$BACKUP_DIR"
  sha256sum postgres-dujiao.dump uploads.tar.gz storefront-current.tar.gz teamgenie-sync-runtime.tar.gz > SHA256SUMS
  sha256sum --check SHA256SUMS
)
printf 'v1.5.0 preflight backup completed\n' > "$BACKUP_DIR/COMPLETE"
echo "Backup completed: $BACKUP_DIR"
echo 'Production configuration and containers were not changed.'
