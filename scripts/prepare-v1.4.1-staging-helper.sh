#!/usr/bin/env bash

set -euo pipefail

HOST="${1:-dujiao-vps}"
REMOTE_SCRIPT="/tmp/dujiao-next-v1.4.1-staging-restore.sh"

echo "Uploading staging restore helper to ${HOST}:${REMOTE_SCRIPT}..."

ssh "$HOST" "cat > '$REMOTE_SCRIPT' && chmod 700 '$REMOTE_SCRIPT'" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "This staging helper must run with sudo/root on the VPS." >&2
  echo "Run: sudo bash /tmp/dujiao-next-v1.4.1-staging-restore.sh /opt/backups/dujiao-next-v1.4.1-preflight-YYYYmmddHHMMSS" >&2
  exit 1
fi

BACKUP_DIR="${1:-}"
if [ -z "$BACKUP_DIR" ] || [ ! -f "$BACKUP_DIR/postgres-dujiao.dump" ] || [ ! -f "$BACKUP_DIR/dujiao-next/config.yml" ]; then
  echo "Usage: sudo bash /tmp/dujiao-next-v1.4.1-staging-restore.sh /opt/backups/dujiao-next-v1.4.1-preflight-YYYYmmddHHMMSS" >&2
  echo "The backup directory must contain postgres-dujiao.dump and dujiao-next/config.yml." >&2
  exit 1
fi

STAGE_DIR="/opt/dujiao-next-staging-v1.4.1"
STAGE_DB_PASSWORD="dujiao_staging_only_local"
IMAGE_TAG="dujiaonext/dujiao-next:teamgenie-v1.4.1"

echo "staging_dir=${STAGE_DIR}"
install -d -m 750 "$STAGE_DIR/config" "$STAGE_DIR/data/postgres" "$STAGE_DIR/data/redis" "$STAGE_DIR/data/uploads" "$STAGE_DIR/data/logs"

cp -a "$BACKUP_DIR/dujiao-next/config.yml" "$STAGE_DIR/config/config.yml"
if [ -d /opt/dujiao-next/data/uploads ]; then
  rm -rf "$STAGE_DIR/data/uploads"
  tar -C /opt/dujiao-next/data -cf - uploads | tar -C "$STAGE_DIR/data" -xf -
elif [ -f "$BACKUP_DIR/uploads.tar.gz" ]; then
  rm -rf "$STAGE_DIR/data/uploads"
  tar -C "$STAGE_DIR/data" -xzf "$BACKUP_DIR/uploads.tar.gz"
fi

python3 - "$STAGE_DIR/config/config.yml" "$STAGE_DB_PASSWORD" <<'PY'
import pathlib
import re
import sys

path = pathlib.Path(sys.argv[1])
db_password = sys.argv[2]
text = path.read_text()

def replace_block(src: str, name: str, block: str) -> str:
    pattern = re.compile(rf"^{name}:\n(?:^[ \t].*\n?)*", re.M)
    if pattern.search(src):
        return pattern.sub(block.rstrip() + "\n", src)
    return src.rstrip() + "\n\n" + block.rstrip() + "\n"

text = replace_block(text, "database", f"""
database:
  driver: postgres
  dsn: "host=dujiaonext-postgres-staging port=5432 user=dujiao password={db_password} dbname=dujiao sslmode=disable"
  pool:
    max_open_conns: 10
    max_idle_conns: 5
    conn_max_lifetime_seconds: 3600
    conn_max_idle_time_seconds: 600
""")

text = replace_block(text, "redis", """
redis:
  enabled: true
  host: dujiaonext-redis-staging
  port: 6379
  password: ""
  db: 0
  prefix: "dj_staging"
""")

text = replace_block(text, "queue", """
queue:
  enabled: true
  host: dujiaonext-redis-staging
  port: 6379
  password: ""
  db: 1
  concurrency: 2
  queues:
    default: 10
    critical: 5
  upstream_sync_interval: "30m"
""")

text = replace_block(text, "teamgenie_sync", """
teamgenie_sync:
  enabled: false
  webhook_url: ""
  shared_secret: ""
  channel: ""
  source_platform: ""
  source_site: ""
  timeout_ms: 3000
""")

text = replace_block(text, "server", """
server:
  host: 0.0.0.0
  port: 8080
  mode: release
""")

text = replace_block(text, "web", """
web:
  admin_path: "/staging-admin-v141"
""")

path.write_text(text)
PY

cat > "$STAGE_DIR/docker-compose.yml" <<YAML
services:
  dujiaonext-postgres-staging:
    image: postgres:16-alpine
    container_name: dujiaonext-postgres-staging
    restart: unless-stopped
    environment:
      POSTGRES_DB: dujiao
      POSTGRES_USER: dujiao
      POSTGRES_PASSWORD: ${STAGE_DB_PASSWORD}
    volumes:
      - ${STAGE_DIR}/data/postgres:/var/lib/postgresql/data
    networks:
      - dujiao-next-staging

  dujiaonext-redis-staging:
    image: redis:7-alpine
    container_name: dujiaonext-redis-staging
    restart: unless-stopped
    command: ["redis-server", "--appendonly", "yes"]
    volumes:
      - ${STAGE_DIR}/data/redis:/data
    networks:
      - dujiao-next-staging

  dujiaonext-staging:
    image: ${IMAGE_TAG}
    container_name: dujiaonext-staging
    restart: unless-stopped
    depends_on:
      - dujiaonext-postgres-staging
      - dujiaonext-redis-staging
    ports:
      - "127.0.0.1:18081:8080"
    volumes:
      - ${STAGE_DIR}/config/config.yml:/app/config.yml:ro
      - ${STAGE_DIR}/data/uploads:/app/uploads
      - ${STAGE_DIR}/data/logs:/app/logs
    networks:
      - dujiao-next-staging

networks:
  dujiao-next-staging:
    name: dujiao-next-staging
YAML

cd "$STAGE_DIR"
docker compose down --remove-orphans || true
rm -rf "$STAGE_DIR/data/postgres" "$STAGE_DIR/data/redis"
install -d -m 750 "$STAGE_DIR/data/postgres" "$STAGE_DIR/data/redis" "$STAGE_DIR/data/logs"
docker compose up -d dujiaonext-postgres-staging dujiaonext-redis-staging

echo "--- waiting for staging postgres ---"
for _ in $(seq 1 60); do
  if docker exec dujiaonext-postgres-staging pg_isready -U dujiao -d dujiao >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec dujiaonext-postgres-staging pg_isready -U dujiao -d dujiao

echo "--- restoring production dump into isolated staging database ---"
cat "$BACKUP_DIR/postgres-dujiao.dump" | docker exec -i dujiaonext-postgres-staging pg_restore -U dujiao -d dujiao --no-owner --role=dujiao

echo "--- starting v1.4.1 TeamGenie staging backend ---"
docker compose up -d dujiaonext-staging

echo "--- staging status ---"
docker compose ps
sleep 5
curl -fsS http://127.0.0.1:18081/health || true
echo
echo "Staging prepared. Local VPS URL: http://127.0.0.1:18081"
echo "Admin path: http://127.0.0.1:18081/staging-admin-v141/"
echo "Logs: docker logs --tail=200 dujiaonext-staging"
REMOTE

echo
echo "The helper has been uploaded. Run this on the VPS:"
echo
echo "  sudo bash $REMOTE_SCRIPT /opt/backups/dujiao-next-v1.4.1-preflight-YYYYmmddHHMMSS"
