#!/usr/bin/env bash
set -euo pipefail
umask 077

MODE="${1:---dry-run}"
if [[ $# -gt 1 || "$MODE" != --dry-run && "$MODE" != --apply ]]; then
  echo 'Usage: enable-reseller-production.sh [--dry-run|--apply]' >&2
  exit 1
fi
if [[ $(id -u) != 0 ]]; then
  echo 'Run with sudo on the VPS.' >&2
  exit 1
fi
if [[ "$MODE" == --apply && "${MAIN_HOSTS_REVIEWED:-false}" != true ]]; then
  echo 'Apply requires MAIN_HOSTS_REVIEWED=true after reviewing dry-run inventory.' >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELPER="$ROOT/prepare-reseller-enable.py"
APP_DIR=/opt/dujiao-next
CONFIG="$APP_DIR/config/config.yml"
CONTAINER=dujiaonext
BACKUP_DIR="$(mktemp -d /opt/backups/dujiao-next-reseller-enable-XXXXXXXX)"
echo "mode=$MODE"
echo "backup_dir=$BACKUP_DIR"
python3 -c 'import yaml' >/dev/null
test -s "$HELPER"
test -s "$CONFIG"
docker inspect "$CONTAINER" --format '{{.State.Health.Status}}' | grep -qx healthy
docker inspect "$CONTAINER" --format '{{.Config.Image}}' > "$BACKUP_DIR/backend-image.txt"
docker inspect "$CONTAINER" --format '{{json .Config.Env}}' > "$BACKUP_DIR/container-env.json"
trap 'rm -f "$BACKUP_DIR/container-env.json"' EXIT
install -m 600 "$CONFIG" "$BACKUP_DIR/config.yml.before"
nginx -T > "$BACKUP_DIR/nginx-inventory.txt" 2> "$BACKUP_DIR/nginx-check.txt"
HOST_ARGS=(--main-host share.aimosh.com --main-host moshskmgr.aimosh.com
  --main-host localhost --main-host 127.0.0.1 --main-host ::1 --main-host dujiaonext)
if [[ -n "${EXTRA_MAIN_HOSTS:-}" ]]; then
  read -r -a extra_hosts <<< "$EXTRA_MAIN_HOSTS"
  for host in "${extra_hosts[@]}"; do HOST_ARGS+=(--main-host "$host"); done
fi
python3 "$HELPER" --source "$BACKUP_DIR/config.yml.before" --candidate "$BACKUP_DIR/config.yml.candidate" \
  --container-env "$BACKUP_DIR/container-env.json" "${HOST_ARGS[@]}"
echo "Review backend proxy hostnames in $BACKUP_DIR/nginx-inventory.txt before applying."

check_main_hosts() {
  for host in share.aimosh.com moshskmgr.aimosh.com 127.0.0.1 dujiaonext; do
    curl -fsS --connect-timeout 3 --max-time 10 -H "Host: $host" \
      http://127.0.0.1:8081/api/v1/public/config | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["status_code"] == 0 and d["data"]["tenant"]["mode"] == "main"'
  done
}
check_main_hosts
if [[ "$MODE" == --dry-run ]]; then
  echo 'Dry run complete. Live config, containers, database, DNS and Nginx were not changed.'
  echo 'YAML comments are not retained in the candidate; the exact original is backed up.'
  echo "After review and explicit approval: sudo env MAIN_HOSTS_REVIEWED=true bash $ROOT/enable-reseller-production.sh --apply"
  exit 0
fi

cmp -s "$CONFIG" "$BACKUP_DIR/config.yml.before" || { echo 'Live config changed during preflight; refusing to apply.' >&2; exit 1; }
OWNER="$(stat -c '%u:%g' "$CONFIG")"
PERMISSIONS="$(stat -c '%a' "$CONFIG")"
replace_config() {
  local source="$1" temporary
  temporary="$(mktemp "$APP_DIR/config/.reseller-config-XXXXXXXX")"
  install -m "$PERMISSIONS" -o "${OWNER%:*}" -g "${OWNER#*:}" "$source" "$temporary"
  mv -f "$temporary" "$CONFIG"
}
applied=false
rollback_on_exit() {
  local result=$?
  rm -f "$BACKUP_DIR/container-env.json"
  if [[ "$result" != 0 && "$applied" == true ]]; then
    echo 'Enable verification failed; restoring the original configuration.' >&2
    replace_config "$BACKUP_DIR/config.yml.before"
    docker restart "$CONTAINER" >/dev/null
  fi
  exit "$result"
}
trap rollback_on_exit EXIT
applied=true
replace_config "$BACKUP_DIR/config.yml.candidate"
docker restart "$CONTAINER" >/dev/null
ready=false
for ((attempt=0; attempt<45; attempt++)); do
  if curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:8081/health >/dev/null 2>&1; then ready=true; break; fi
  sleep 2
done
[[ "$ready" == true ]]
check_main_hosts
unknown_status="$(curl -sS --connect-timeout 3 --max-time 10 -o "$BACKUP_DIR/unknown-host-response.json" -w '%{http_code}' \
  -H 'Host: reseller-enable-check.invalid' http://127.0.0.1:8081/api/v1/public/config)"
[[ "$unknown_status" == 404 ]]
[[ "$(docker inspect "$CONTAINER" --format '{{.Config.Image}}')" == "$(cat "$BACKUP_DIR/backend-image.txt")" ]]
echo 'Reseller mode and applications enabled; approval, DNS, certificates and domain bindings remain manual.'
echo 'No database restore, Nginx change or automatic domain approval was performed.'
