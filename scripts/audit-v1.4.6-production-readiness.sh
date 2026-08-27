#!/usr/bin/env bash

set -euo pipefail

HOST="${1:-dujiao-vps}"
REMOTE_SCRIPT="/tmp/dujiao-next-v1.4.6-production-readiness.sh"

echo "Uploading production readiness audit helper to ${HOST}:${REMOTE_SCRIPT}..."

ssh "$HOST" "cat > '$REMOTE_SCRIPT' && chmod 700 '$REMOTE_SCRIPT'" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "This audit helper must run with sudo/root on the VPS." >&2
  echo "Run: sudo bash /tmp/dujiao-next-v1.4.6-production-readiness.sh" >&2
  exit 1
fi

CONFIG_FILE="${CONFIG_FILE:-/opt/dujiao-next/config/config.yml}"
COMPOSE_DIR="${COMPOSE_DIR:-/opt/dujiao-next}"
IMAGE_TAG="${IMAGE_TAG:-dujiaonext/dujiao-next:teamgenie-v1.4.6}"

echo "== Dujiao-Next v1.4.6 Production Readiness Audit =="
echo "config_file=${CONFIG_FILE}"
echo "compose_dir=${COMPOSE_DIR}"
echo "image_tag=${IMAGE_TAG}"
echo

if [ ! -f "$CONFIG_FILE" ]; then
  echo "FAIL config_missing: $CONFIG_FILE"
  exit 1
fi

python3 - "$CONFIG_FILE" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
lines = path.read_text().splitlines()

sections = {}
current = None
for line in lines:
    stripped = line.strip()
    if not stripped or stripped.startswith("#"):
        continue
    if not line.startswith((" ", "\t")) and stripped.endswith(":"):
        current = stripped[:-1].strip()
        sections.setdefault(current, [])
        continue
    if current:
        sections.setdefault(current, []).append(line)

def block(name: str) -> list[str]:
    return sections.get(name, [])

def get_scalar(section: str, key: str) -> str:
    prefix = f"{key}:"
    for raw in block(section):
        stripped = raw.strip()
        if stripped.startswith(prefix):
            return stripped[len(prefix):].strip().strip('"').strip("'")
    return ""

def bool_value(section: str, key: str) -> str:
    val = get_scalar(section, key).lower()
    if val in {"true", "false"}:
        return val
    return val or "<missing>"

def length_of(section: str, key: str) -> int:
    return len(get_scalar(section, key))

required_sections = ["database", "redis", "queue", "jwt", "user_jwt", "web", "teamgenie_sync"]
for section in required_sections:
    print(("OK" if section in sections else "WARN") + f" section_{section}={'present' if section in sections else 'missing'}")

app_secret_len = length_of("app", "secret_key")
if "app" not in sections:
    print("FAIL app_section_missing: add persistent app.secret_key before v1.4.6 production cutover")
elif app_secret_len < 32:
    print(f"FAIL app_secret_weak: length={app_secret_len}")
else:
    print(f"OK app_secret_present: length={app_secret_len}")

for section, key in [("jwt", "secret"), ("user_jwt", "secret")]:
    length = length_of(section, key)
    status = "OK" if length >= 32 else "FAIL"
    print(f"{status} {section}_{key}_length={length}")

admin_path = get_scalar("web", "admin_path")
if not admin_path:
    print("FAIL web_admin_path_missing")
elif admin_path == "/admin":
    print("WARN web_admin_path_default=/admin")
else:
    print(f"OK web_admin_path_set={admin_path}")

print(f"INFO teamgenie_enabled={bool_value('teamgenie_sync', 'enabled')}")
print(f"INFO teamgenie_webhook_url_present={'yes' if get_scalar('teamgenie_sync', 'webhook_url') else 'no'}")
print(f"INFO teamgenie_shared_secret_length={length_of('teamgenie_sync', 'shared_secret')}")
print(f"INFO server_mode={get_scalar('server', 'mode') or '<missing>'}")
print(f"INFO database_driver={get_scalar('database', 'driver') or '<missing>'}")
print(f"INFO captcha_provider_config_section={'present' if 'captcha' in sections else 'runtime_or_db'}")
PY

echo
echo "== Docker inventory =="
docker image inspect "$IMAGE_TAG" --format 'OK image_present={{.RepoTags}} id={{.Id}} created={{.Created}} size={{.Size}}' 2>/dev/null || echo "FAIL image_missing=${IMAGE_TAG}"
docker ps --format 'container={{.Names}} image={{.Image}} status={{.Status}} ports={{.Ports}}' | grep -E 'dujiao|unicard|teamgenie' || true

echo
echo "== Compose inventory =="
if [ -d "$COMPOSE_DIR" ]; then
  (
    cd "$COMPOSE_DIR"
    docker compose ps || true
    echo "--- services ---"
    docker compose config --services || true
    echo "--- images ---"
    docker compose config --images || true
  )
else
  echo "WARN compose_dir_missing=${COMPOSE_DIR}"
fi

echo
echo "== Latest backup candidates =="
find /opt/backups -maxdepth 1 -type d -name 'dujiao-next-v1.4.6-preflight-*' -printf '%TY-%Tm-%Td %TH:%TM:%TS %p\n' 2>/dev/null | sort | tail -5 || true

echo
echo "Audit completed. No production files were modified."
REMOTE

echo
echo "The helper has been uploaded. Run this on the VPS:"
echo
echo "  sudo bash $REMOTE_SCRIPT"
