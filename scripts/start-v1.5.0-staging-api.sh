#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $(id -u) != 0 ]]; then
  echo 'Run with sudo on the VPS.' >&2
  exit 1
fi
STAGE_DIR=/opt/dujiao-next-staging-v1.5.0
CONTAINER=dujiaonext-v150-staging
DB_CONTAINER=dujiaonext-postgres-v150-staging
IMAGE_TAG=dujiaonext/dujiao-next:teamgenie-v1.5.0-security-aa1ec721
EXPECTED_VERSION=v1.5.0-teamgenie-security-aa1ec721
test -s "$STAGE_DIR/docker-compose.yml"
test -s "$STAGE_DIR/counts-before.txt"
docker inspect "$CONTAINER" --format '{{.Config.Image}}' | grep -Fqx "$IMAGE_TAG"
docker inspect "$CONTAINER" --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' | grep -Fqx "$STAGE_DIR"
docker inspect "$DB_CONTAINER" --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Source}}{{end}}{{end}}' | grep -Fqx "$STAGE_DIR/data/postgres"
docker network inspect dujiao-next-v150-staging --format '{{.Internal}}' | grep -qx true
cd "$STAGE_DIR"

# Leave the restored database and disabled queue untouched; override startup only.
cat > docker-compose.api.yml <<'YAML'
services:
  backend:
    command: ["./dujiao-next", "-mode", "api"]
YAML
docker compose -f docker-compose.yml -f docker-compose.api.yml config -q
docker compose -f docker-compose.yml -f docker-compose.api.yml up -d --no-deps backend
for _ in $(seq 1 90); do
  if docker exec "$CONTAINER" wget -qO- -T 3 http://127.0.0.1:8080/health >/dev/null 2>&1; then break; fi
  state="$(docker inspect "$CONTAINER" --format '{{.State.Status}}')"
  if [[ "$state" == exited || "$state" == dead ]]; then
    echo 'Staging backend exited. Check its private app.log; database was not restored again.' >&2
    exit 1
  fi
  sleep 2
done
docker exec "$CONTAINER" wget -qO- -T 10 http://127.0.0.1:8080/health
docker exec "$CONTAINER" wget -qO- -T 10 http://127.0.0.1:8080/api/v1/public/config > public-config.json
grep -Fq "\"app_version\":\"$EXPECTED_VERSION\"" public-config.json
docker exec "$DB_CONTAINER" psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc \
  "SELECT 'users',count(*) FROM users UNION ALL SELECT 'orders',count(*) FROM orders UNION ALL SELECT 'payments',count(*) FROM payments UNION ALL SELECT 'member_level_prices',count(*) FROM member_level_prices ORDER BY 1;" > counts-after.txt
diff -u counts-before.txt counts-after.txt
columns="$(docker exec "$DB_CONTAINER" psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND (table_name,column_name) IN (('users','invite_code_id'),('invite_codes','code'),('invite_codes','used_count'));")"
[[ "$columns" == 3 ]]
echo
echo "app_version=$EXPECTED_VERSION"
echo 'migration_columns=3/3; core_table_row_counts=unchanged'
echo 'Staging ready (internal network only).'
echo 'API-only mode; queue and TeamGenie remain disabled. Production was not modified.'
