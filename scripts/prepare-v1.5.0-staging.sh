#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $(id -u) != 0 ]]; then
  echo 'Run with sudo on the VPS.' >&2
  exit 1
fi
BACKUP_DIR="${1:-}"
IMAGE_TAG="${2:-}"
EXPECTED_VERSION="${3:-}"
if [[ ! "$BACKUP_DIR" =~ ^/opt/backups/dujiao-next-v1\.5\.0-preflight-[0-9]{14}$ ]] ||
   [[ ! "$IMAGE_TAG" =~ ^dujiaonext/dujiao-next:teamgenie-v1\.5\.0-[a-zA-Z0-9._-]+$ ]] ||
   [[ ! "$EXPECTED_VERSION" =~ ^v1\.5\.0-teamgenie-[a-zA-Z0-9._-]+$ ]]; then
  echo 'Usage: sudo bash prepare-v1.5.0-staging.sh BACKUP_DIR IMAGE_TAG EXPECTED_VERSION' >&2
  exit 1
fi
test -s "$BACKUP_DIR/COMPLETE"
test -s "$BACKUP_DIR/postgres-dujiao.dump"
(cd "$BACKUP_DIR" && sha256sum --check SHA256SUMS)
docker image inspect "$IMAGE_TAG" >/dev/null

STAGE_DIR=/opt/dujiao-next-staging-v1.5.0
if [[ -e "$STAGE_DIR" ]] || docker inspect dujiaonext-v150-staging >/dev/null 2>&1 ||
   docker inspect dujiaonext-postgres-v150-staging >/dev/null 2>&1 ||
   docker inspect dujiaonext-redis-v150-staging >/dev/null 2>&1 ||
   docker network inspect dujiao-next-v150-staging >/dev/null 2>&1; then
  echo 'Staging already exists; refusing to overwrite or delete it.' >&2
  exit 1
fi
if [[ -n $(ss -ltnH 'sport = :18081') ]]; then
  echo 'Port 18081 is already in use.' >&2
  exit 1
fi

install -d -m 700 "$STAGE_DIR/config" "$STAGE_DIR/data/postgres" "$STAGE_DIR/data/redis" "$STAGE_DIR/data/uploads" "$STAGE_DIR/data/logs"
DB_PASSWORD="$(openssl rand -hex 32)"
APP_SECRET="$(openssl rand -hex 32)"
ADMIN_SECRET="$(openssl rand -hex 32)"
USER_SECRET="$(openssl rand -hex 32)"

# Generate an isolated config instead of copying production integration secrets.
cat > "$STAGE_DIR/config/config.yml" <<YAML
app:
  secret_key: "$APP_SECRET"
  totp_issuer: "MoshShop Staging"
server:
  host: 0.0.0.0
  port: 8080
  mode: release
  trusted_proxies: []
database:
  driver: postgres
  dsn: "host=dujiaonext-postgres-v150-staging port=5432 user=dujiao password=$DB_PASSWORD dbname=dujiao sslmode=disable"
  pool:
    max_open_conns: 10
    max_idle_conns: 5
jwt:
  secret: "$ADMIN_SECRET"
  expire_hours: 24
user_jwt:
  secret: "$USER_SECRET"
  expire_hours: 24
  remember_me_expire_hours: 168
bootstrap:
  default_admin_username: ""
  default_admin_password: ""
redis:
  enabled: true
  host: dujiaonext-redis-v150-staging
  port: 6379
  db: 0
  prefix: dj_v150_staging
queue:
  enabled: false
teamgenie_sync:
  enabled: false
email:
  enabled: false
telegram_auth:
  enabled: false
google_auth:
  enabled: false
web:
  admin_path: /staging-admin-v150
YAML

cat > "$STAGE_DIR/docker-compose.yml" <<YAML
services:
  postgres:
    image: postgres:16-alpine
    container_name: dujiaonext-postgres-v150-staging
    environment:
      POSTGRES_DB: dujiao
      POSTGRES_USER: dujiao
      POSTGRES_PASSWORD: "$DB_PASSWORD"
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    networks: [staging]
  redis:
    image: redis:7-alpine
    container_name: dujiaonext-redis-v150-staging
    command: [redis-server, --appendonly, "yes"]
    volumes:
      - ./data/redis:/data
    networks: [staging]
  backend:
    image: "$IMAGE_TAG"
    container_name: dujiaonext-v150-staging
    depends_on: [postgres, redis]
    ports:
      - "127.0.0.1:18081:8080"
    volumes:
      - ./config/config.yml:/app/config.yml:ro
      - ./data/uploads:/app/uploads
      - ./data/logs:/app/logs
    networks: [staging]
networks:
  staging:
    name: dujiao-next-v150-staging
    internal: true
YAML
cd "$STAGE_DIR"
docker compose config -q
docker compose up -d postgres redis
for _ in $(seq 1 60); do
  if docker exec dujiaonext-postgres-v150-staging pg_isready -U dujiao -d dujiao >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
docker exec dujiaonext-postgres-v150-staging pg_isready -U dujiao -d dujiao

echo 'Restoring backup into isolated PostgreSQL (transactional, fail-fast).'
docker exec -i dujiaonext-postgres-v150-staging pg_restore -U dujiao -d dujiao --no-owner --no-privileges --single-transaction --exit-on-error < "$BACKUP_DIR/postgres-dujiao.dump"
tar -C "$STAGE_DIR/data" -xzf "$BACKUP_DIR/uploads.tar.gz"

counts() {
  docker exec dujiaonext-postgres-v150-staging psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc \
    "SELECT 'users',count(*) FROM users UNION ALL SELECT 'orders',count(*) FROM orders UNION ALL SELECT 'payments',count(*) FROM payments UNION ALL SELECT 'member_level_prices',count(*) FROM member_level_prices ORDER BY 1;"
}
counts > counts-before.txt
docker compose up -d backend
for _ in $(seq 1 90); do
  if curl -fsS --max-time 3 http://127.0.0.1:18081/health >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
curl -fsS --max-time 10 http://127.0.0.1:18081/health
curl -fsS --max-time 10 http://127.0.0.1:18081/api/v1/public/config > public-config.json
grep -Fq "\"app_version\":\"$EXPECTED_VERSION\"" public-config.json
counts > counts-after.txt
diff -u counts-before.txt counts-after.txt
columns="$(docker exec dujiaonext-postgres-v150-staging psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND (table_name,column_name) IN (('users','invite_code_id'),('invite_codes','code'),('invite_codes','used_count'));")"
[[ "$columns" == 3 ]]
echo
echo 'migration_columns=3/3; core_table_row_counts=unchanged'
echo 'Staging ready: http://127.0.0.1:18081/staging-admin-v150/'
echo 'Queue and TeamGenie disabled; Docker network has no external egress.'
echo "staging_dir=$STAGE_DIR"
echo 'Production was not modified. Keep staging private; it contains customer data.'
