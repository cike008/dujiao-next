#!/usr/bin/env bash

set -euo pipefail

HOST="${1:-dujiao-vps}"
REMOTE_SCRIPT="/tmp/dujiao-next-v1.4.6-patch-production-config.sh"

echo "Uploading production config patch helper to ${HOST}:${REMOTE_SCRIPT}..."

ssh "$HOST" "cat > '$REMOTE_SCRIPT' && chmod 700 '$REMOTE_SCRIPT'" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "This config patch helper must run with sudo/root on the VPS." >&2
  echo "Run: sudo bash /tmp/dujiao-next-v1.4.6-patch-production-config.sh [admin_path]" >&2
  exit 1
fi

CONFIG_FILE="${CONFIG_FILE:-/opt/dujiao-next/config/config.yml}"
ADMIN_PATH="${1:-${DJ_ADMIN_PATH:-}}"

if [ ! -f "$CONFIG_FILE" ]; then
  echo "config file not found: $CONFIG_FILE" >&2
  exit 1
fi

if [ -z "$ADMIN_PATH" ]; then
  ADMIN_PATH="/console-$(openssl rand -hex 4)"
fi
if [[ "$ADMIN_PATH" != /* ]]; then
  ADMIN_PATH="/$ADMIN_PATH"
fi
if [ "$ADMIN_PATH" = "/" ] || [ "$ADMIN_PATH" = "/admin" ]; then
  echo "refusing unsafe admin_path: $ADMIN_PATH" >&2
  exit 1
fi

BACKUP_FILE="${CONFIG_FILE}.bak-v141-config-$(date +%Y%m%d%H%M%S)"
cp -a "$CONFIG_FILE" "$BACKUP_FILE"

APP_SECRET="$(openssl rand -hex 32)"

python3 - "$CONFIG_FILE" "$APP_SECRET" "$ADMIN_PATH" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
new_app_secret = sys.argv[2]
admin_path = sys.argv[3]
text = path.read_text()

def split_sections(src: str):
    lines = src.splitlines()
    result = []
    current_name = None
    current_lines = []
    for line in lines:
        stripped = line.strip()
        is_top = bool(stripped) and not stripped.startswith("#") and not line.startswith((" ", "\t")) and stripped.endswith(":")
        if is_top:
            if current_name is not None:
                result.append((current_name, current_lines))
            current_name = stripped[:-1].strip()
            current_lines = [line]
        else:
            if current_name is None:
                result.append((None, [line]))
            else:
                current_lines.append(line)
    if current_name is not None:
        result.append((current_name, current_lines))
    return result

def scalar_value(section_lines, key: str) -> str:
    prefix = f"{key}:"
    for raw in section_lines[1:]:
        stripped = raw.strip()
        if stripped.startswith(prefix):
            return stripped[len(prefix):].strip().strip('"').strip("'")
    return ""

sections = split_sections(text)
seen = set()
out = []
app_status = "added"
web_status = "added"

for name, lines in sections:
    if name is None:
        out.extend(lines)
        continue
    if name in seen:
        continue
    seen.add(name)
    if name == "app":
        current = scalar_value(lines, "secret_key")
        if len(current) >= 32:
            out.extend(lines)
            app_status = "kept_existing"
        else:
            out.extend([
                "app:",
                f'  secret_key: "{new_app_secret}"',
                '  totp_issuer: "MoshShop"',
            ])
            app_status = "replaced_weak"
        continue
    if name == "web":
        current_path = scalar_value(lines, "admin_path")
        final_path = current_path if current_path and current_path not in {"/", "/admin"} else admin_path
        out.extend([
            "web:",
            f'  admin_path: "{final_path}"',
        ])
        web_status = "kept_existing" if final_path == current_path else "replaced_or_added"
        continue
    out.extend(lines)

if "app" not in seen:
    out.extend(["", "app:", f'  secret_key: "{new_app_secret}"', '  totp_issuer: "MoshShop"'])
if "web" not in seen:
    out.extend(["", "web:", f'  admin_path: "{admin_path}"'])

path.write_text("\n".join(out).rstrip() + "\n")
print(f"app_secret={app_status}")
print(f"web_admin_path={admin_path if 'web' not in seen else 'see_config'}")
PY

echo "backup_file=${BACKUP_FILE}"
echo "admin_path=$(python3 - "$CONFIG_FILE" <<'PY'
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
echo "Config patch completed. No containers were restarted."
REMOTE

echo
echo "The helper has been uploaded. Run this on the VPS:"
echo
echo "  sudo bash $REMOTE_SCRIPT"
echo
echo "Or pass a custom private admin path:"
echo
echo "  sudo bash $REMOTE_SCRIPT /your-private-admin-path"
