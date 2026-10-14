#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $(id -u) != 0 ]]; then
  echo 'Run with sudo on the VPS.' >&2
  exit 1
fi
BACKUP_DIR="${1:-}"
IMAGE_TAG=dujiaonext/dujiao-next:teamgenie-v1.5.0-security-aa1ec721
EXPECTED_VERSION=v1.5.0-teamgenie-security-aa1ec721
STAGE_DIR=/opt/dujiao-next-staging-v1.5.0
DB_CONTAINER=dujiaonext-postgres-v150-staging
BACKEND_CONTAINER=dujiaonext-v150-staging
if [[ ! "$BACKUP_DIR" =~ ^/opt/backups/dujiao-next-v1\.5\.0-preflight-[0-9]{14}$ ]]; then
  echo 'Usage: sudo bash resume-v1.5.0-staging.sh BACKUP_DIR' >&2
  exit 1
fi
test -s "$BACKUP_DIR/COMPLETE"
(cd "$BACKUP_DIR" && sha256sum --check SHA256SUMS)
test -s "$STAGE_DIR/config/config.yml"
test -s "$STAGE_DIR/docker-compose.yml"
docker image inspect "$IMAGE_TAG" >/dev/null
docker network inspect dujiao-next-v150-staging --format '{{.Internal}}' | grep -qx true
docker inspect "$DB_CONTAINER" --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' | grep -Fqx "$STAGE_DIR"
docker inspect "$DB_CONTAINER" --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Source}}{{end}}{{end}}' | grep -Fqx "$STAGE_DIR/data/postgres"
if docker inspect "$BACKEND_CONTAINER" >/dev/null 2>&1; then
  echo 'Backend already exists; refusing to restore over an active staging database.' >&2
  exit 1
fi
cd "$STAGE_DIR"
docker compose config -q
docker compose config --images | grep -Fqx "$IMAGE_TAG"

# The initialization-only PostgreSQL server listens on a Unix socket, not TCP.
for _ in $(seq 1 60); do
  if docker exec "$DB_CONTAINER" pg_isready -h 127.0.0.1 -U dujiao -d dujiao >/dev/null 2>&1; then break; fi
  sleep 2
done
docker exec "$DB_CONTAINER" pg_isready -h 127.0.0.1 -U dujiao -d dujiao
tables="$(docker exec "$DB_CONTAINER" psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc "SELECT count(*) FROM pg_tables WHERE schemaname='public';")"
if [[ "$tables" != 0 ]]; then
  echo "Staging database already has $tables tables; refusing to overwrite it." >&2
  exit 1
fi
echo 'Restoring backup into the existing empty staging database.'
docker exec -i "$DB_CONTAINER" pg_restore -U dujiao -d dujiao --no-owner --no-privileges --single-transaction --exit-on-error < "$BACKUP_DIR/postgres-dujiao.dump"
tar -C "$STAGE_DIR/data" -xzf "$BACKUP_DIR/uploads.tar.gz"
counts() {
  docker exec "$DB_CONTAINER" psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc \
    "SELECT 'users',count(*) FROM users UNION ALL SELECT 'orders',count(*) FROM orders UNION ALL SELECT 'payments',count(*) FROM payments UNION ALL SELECT 'member_level_prices',count(*) FROM member_level_prices ORDER BY 1;"
}
counts > counts-before.txt
cat > docker-compose.api.yml <<'YAML'
services:
  backend:
    command: ["./dujiao-next", "-mode", "api"]
YAML
docker compose -f docker-compose.yml -f docker-compose.api.yml up -d backend
for _ in $(seq 1 90); do
  if docker exec "$BACKEND_CONTAINER" wget -qO- -T 3 http://127.0.0.1:8080/health >/dev/null 2>&1; then break; fi
  sleep 2
done
docker exec "$BACKEND_CONTAINER" wget -qO- -T 10 http://127.0.0.1:8080/health
docker exec "$BACKEND_CONTAINER" wget -qO- -T 10 http://127.0.0.1:8080/api/v1/public/config > public-config.json
grep -Fq "\"app_version\":\"$EXPECTED_VERSION\"" public-config.json
counts > counts-after.txt
diff -u counts-before.txt counts-after.txt
columns="$(docker exec "$DB_CONTAINER" psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND (table_name,column_name) IN (('users','invite_code_id'),('invite_codes','code'),('invite_codes','used_count'));")"
[[ "$columns" == 3 ]]
echo
echo 'migration_columns=3/3; core_table_row_counts=unchanged'
echo 'Staging ready (internal network only).'
echo 'Production was not modified. Queue and TeamGenie remain disabled.'
