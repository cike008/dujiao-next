#!/usr/bin/env bash

set -euo pipefail

HOST="${1:-dujiao-vps}"
REMOTE_SCRIPT="/tmp/dujiao-next-v1.4.1-production-cutover.sh"

echo "Uploading production cutover helper to ${HOST}:${REMOTE_SCRIPT}..."

ssh "$HOST" "cat > '$REMOTE_SCRIPT' && chmod 700 '$REMOTE_SCRIPT'" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "This cutover helper must run with sudo/root on the VPS." >&2
  echo "Dry run: sudo bash /tmp/dujiao-next-v1.4.1-production-cutover.sh" >&2
  echo "Apply:   sudo bash /tmp/dujiao-next-v1.4.1-production-cutover.sh --apply" >&2
  exit 1
fi

APPLY=0
if [ "${1:-}" = "--apply" ]; then
  APPLY=1
fi

APP_DIR="/opt/dujiao-next"
CONFIG_FILE="${APP_DIR}/config/config.yml"
COMPOSE_FILE="${APP_DIR}/docker-compose.yml"
IMAGE_TAG="dujiaonext/dujiao-next:teamgenie-v1.4.1"
TS="$(date +%Y%m%d%H%M%S)"
CUTOVER_DIR="${APP_DIR}/cutover-v1.4.1-${TS}"
SHARE_NGINX="/etc/nginx/sites-available/share.aimosh.com"
ADMIN_NGINX="/etc/nginx/sites-available/moshskmgr.aimosh.com"

require_file() {
  if [ ! -f "$1" ]; then
    echo "required file missing: $1" >&2
    exit 1
  fi
}

require_file "$CONFIG_FILE"
require_file "$COMPOSE_FILE"
require_file "$SHARE_NGINX"
require_file "$ADMIN_NGINX"

ADMIN_PATH="$(python3 - "$CONFIG_FILE" <<'PY'
import pathlib, sys
lines = pathlib.Path(sys.argv[1]).read_text().splitlines()
inside = False
for line in lines:
    stripped = line.strip()
    if stripped == "web:":
        inside = True
        continue
    if inside and stripped and not line.startswith((" ", "\t", "#")):
        inside = False
    if inside and stripped.startswith("admin_path:"):
        print(stripped.split(":", 1)[1].strip().strip('"').strip("'"))
        break
PY
)"

if [ -z "$ADMIN_PATH" ] || [ "$ADMIN_PATH" = "/" ] || [ "$ADMIN_PATH" = "/admin" ]; then
  echo "unsafe or missing web.admin_path: ${ADMIN_PATH:-<missing>}" >&2
  exit 1
fi

echo "== v1.4.1 production cutover helper =="
echo "mode=$([ "$APPLY" -eq 1 ] && echo apply || echo dry-run)"
echo "cutover_dir=${CUTOVER_DIR}"
echo "admin_path=${ADMIN_PATH}"
echo "image_tag=${IMAGE_TAG}"
echo

docker image inspect "$IMAGE_TAG" >/dev/null

install -d -m 750 "$CUTOVER_DIR"
cp -a "$COMPOSE_FILE" "$CUTOVER_DIR/docker-compose.yml.before"
cp -a "$SHARE_NGINX" "$CUTOVER_DIR/share.aimosh.com.before"
cp -a "$ADMIN_NGINX" "$CUTOVER_DIR/moshskmgr.aimosh.com.before"

python3 - "$COMPOSE_FILE" "$CUTOVER_DIR/docker-compose.yml.candidate" "$IMAGE_TAG" <<'PY'
import pathlib
import re
import sys

src = pathlib.Path(sys.argv[1])
dst = pathlib.Path(sys.argv[2])
image = sys.argv[3]
lines = src.read_text().splitlines()

service_re = re.compile(r"^  [A-Za-z0-9_-]+:\s*$")
out = []
i = 0
inserted = False

new_service = f"""  dujiaonext:
    image: {image}
    container_name: dujiaonext
    restart: unless-stopped
    extra_hosts:
      - "host.docker.internal:host-gateway"
    ports:
      - "127.0.0.1:8081:8080"
    volumes:
      - /opt/dujiao-next/config/config.yml:/app/config.yml:ro
      - /opt/dujiao-next/data/uploads:/app/uploads
      - /opt/dujiao-next/data/logs:/app/logs
    networks:
      - dujiaonext
    depends_on:
      dujiaonext-postgres:
        condition: service_healthy
      dujiaonext-redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:8080/health || exit 1"]
      interval: 15s
      timeout: 5s
      retries: 5""".splitlines()

while i < len(lines):
    line = lines[i]
    if line == "  dujiaonext-api:":
        out.extend(new_service)
        inserted = True
        i += 1
        while i < len(lines) and not service_re.match(lines[i]):
            i += 1
        continue
    out.append(line)
    i += 1

if not inserted:
    raise SystemExit("dujiaonext-api service block not found")

dst.write_text("\n".join(out).rstrip() + "\n")
PY

python3 - "$SHARE_NGINX" "$CUTOVER_DIR/share.aimosh.com.candidate" "$ADMIN_PATH" <<'PY'
import pathlib
import sys

src = pathlib.Path(sys.argv[1])
dst = pathlib.Path(sys.argv[2])
admin_path = sys.argv[3].rstrip("/")
text = src.read_text()

block = f"""
  location = /health {{ proxy_pass http://127.0.0.1:8081/health; proxy_http_version 1.1; proxy_set_header Host $host; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; proxy_set_header X-Forwarded-Proto $scheme; }}
  location ^~ {admin_path}/ {{ proxy_pass http://127.0.0.1:8081; proxy_http_version 1.1; proxy_set_header Host $host; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; proxy_set_header X-Forwarded-Proto $scheme; proxy_set_header X-Forwarded-Host $host; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade"; }}
  location = {admin_path} {{ return 301 {admin_path}/; }}
"""

if f"location ^~ {admin_path}/" not in text:
    marker = "\n  location / {"
    idx = text.rfind(marker)
    if idx == -1:
        raise SystemExit("could not find root location marker")
    text = text[:idx] + block + text[idx:]

dst.write_text(text)
PY

python3 - "$ADMIN_NGINX" "$CUTOVER_DIR/moshskmgr.aimosh.com.candidate" "$ADMIN_PATH" <<'PY'
import pathlib
import sys

src = pathlib.Path(sys.argv[1])
dst = pathlib.Path(sys.argv[2])
admin_path = sys.argv[3].rstrip("/")
text = src.read_text()

block = f"""
  location = /health {{ proxy_pass http://127.0.0.1:8081/health; proxy_http_version 1.1; proxy_set_header Host $host; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; proxy_set_header X-Forwarded-Proto $scheme; }}
  location ^~ {admin_path}/ {{ proxy_pass http://127.0.0.1:8081; proxy_http_version 1.1; proxy_set_header Host $host; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; proxy_set_header X-Forwarded-Proto $scheme; proxy_set_header X-Forwarded-Host $host; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade"; }}
  location = {admin_path} {{ return 301 {admin_path}/; }}
"""

if f"location ^~ {admin_path}/" not in text:
    marker = "\n  location / {"
    idx = text.rfind(marker)
    if idx == -1:
        raise SystemExit("could not find root location marker")
    text = text[:idx] + block + text[idx:]

dst.write_text(text)
PY

echo "== candidate validation =="
docker compose -f "$CUTOVER_DIR/docker-compose.yml.candidate" config --services
docker compose -f "$CUTOVER_DIR/docker-compose.yml.candidate" config --images
nginx -t
echo
echo "Candidate files:"
echo "  $CUTOVER_DIR/docker-compose.yml.candidate"
echo "  $CUTOVER_DIR/share.aimosh.com.candidate"
echo "  $CUTOVER_DIR/moshskmgr.aimosh.com.candidate"

if [ "$APPLY" -ne 1 ]; then
  echo
  echo "Dry run completed. No production files were modified."
  echo "To apply after review: sudo bash /tmp/dujiao-next-v1.4.1-production-cutover.sh --apply"
  exit 0
fi

echo
echo "== applying cutover =="
cp -a "$CUTOVER_DIR/docker-compose.yml.candidate" "$COMPOSE_FILE"
cp -a "$CUTOVER_DIR/share.aimosh.com.candidate" "$SHARE_NGINX"
cp -a "$CUTOVER_DIR/moshskmgr.aimosh.com.candidate" "$ADMIN_NGINX"

if ! nginx -t; then
  echo "nginx candidate failed; restoring previous nginx files"
  cp -a "$CUTOVER_DIR/share.aimosh.com.before" "$SHARE_NGINX"
  cp -a "$CUTOVER_DIR/moshskmgr.aimosh.com.before" "$ADMIN_NGINX"
  nginx -t || true
  exit 1
fi

docker stop dujiaonext-api >/dev/null 2>&1 || true
(
  cd "$APP_DIR"
  docker compose up -d dujiaonext
)

echo "== waiting for new backend health =="
ok=0
for _ in $(seq 1 40); do
  if curl -fsS http://127.0.0.1:8081/health >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 2
done

if [ "$ok" -ne 1 ]; then
  echo "new backend failed health check; rolling back"
  docker logs --tail=120 dujiaonext || true
  docker rm -f dujiaonext >/dev/null 2>&1 || true
  cp -a "$CUTOVER_DIR/docker-compose.yml.before" "$COMPOSE_FILE"
  cp -a "$CUTOVER_DIR/share.aimosh.com.before" "$SHARE_NGINX"
  cp -a "$CUTOVER_DIR/moshskmgr.aimosh.com.before" "$ADMIN_NGINX"
  docker start dujiaonext-api >/dev/null 2>&1 || true
  nginx -t && nginx -s reload
  exit 1
fi

nginx -s reload
curl -fsS http://127.0.0.1:8081/health
echo
curl -fsS http://127.0.0.1:8081/api/v1/public/config | grep -o '"app_version":"[^"]*"' || true
echo
docker ps --format 'container={{.Names}} image={{.Image}} status={{.Status}} ports={{.Ports}}' | grep -E 'dujiao|unicard|teamgenie' || true
echo "Cutover completed. Previous files are in: $CUTOVER_DIR"
REMOTE

echo
echo "The helper has been uploaded. Dry-run on VPS:"
echo
echo "  sudo bash $REMOTE_SCRIPT"
echo
echo "Apply on VPS after reviewing dry-run output:"
echo
echo "  sudo bash $REMOTE_SCRIPT --apply"
