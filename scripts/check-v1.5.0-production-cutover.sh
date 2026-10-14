#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $(id -u) != 0 ]]; then
  echo 'Run with sudo on the VPS.' >&2
  exit 1
fi
MODE="${1:---dry-run}"
if [[ $# -gt 1 || "$MODE" != --dry-run && "$MODE" != --apply ]]; then
  echo 'Usage: check-v1.5.0-production-cutover.sh [--dry-run|--apply]' >&2
  exit 1
fi
APP_DIR=/opt/dujiao-next
STAGE_DIR=/opt/dujiao-next-staging-v1.5.0
OLD_IMAGE=dujiaonext/dujiao-next:teamgenie-v1.4.9-guest-guard-20261008
NEW_IMAGE=dujiaonext/dujiao-next:teamgenie-v1.5.0-security-aa1ec721
EXPECTED_VERSION=v1.5.0-teamgenie-security-aa1ec721
CORE_SCRIPT=/tmp/dujiao-next-v1.5.0-deploy-core.sh

echo '== v1.5.0 production cutover readiness =='
echo "mode=$MODE"
docker inspect dujiaonext --format '{{.Config.Image}}' | grep -Fqx "$OLD_IMAGE"
docker inspect dujiaonext --format '{{.State.Health.Status}}' | grep -qx healthy
docker image inspect "$NEW_IMAGE" >/dev/null
docker inspect dujiaonext-v150-staging --format '{{.Config.Image}}' | grep -Fqx "$NEW_IMAGE"
docker inspect dujiaonext-v150-staging --format '{{.State.Status}}' | grep -qx running
docker network inspect dujiao-next-v150-staging --format '{{.Internal}}' | grep -qx true
docker inspect dujiaonext-postgres-v150-staging --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Source}}{{end}}{{end}}' | grep -Fqx "$STAGE_DIR/data/postgres"
docker exec dujiaonext-v150-staging wget -qO- -T 10 http://127.0.0.1:8080/health | grep -q '"status":"ok"'
docker exec dujiaonext-v150-staging wget -qO- -T 10 http://127.0.0.1:8080/api/v1/public/config | python3 -c '
import json,sys
d=json.load(sys.stdin)["data"]
assert d["app_version"] == sys.argv[1], "staging version mismatch"
assert d.get("invite_only_enabled") is False, "leave invite-only registration off during cutover"
print("staging_version=ok; invite_only_enabled=false")
' "$EXPECTED_VERSION"
test -s "$STAGE_DIR/counts-before.txt"
docker exec dujiaonext-postgres-v150-staging psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc \
  "SELECT 'users',count(*) FROM users UNION ALL SELECT 'orders',count(*) FROM orders UNION ALL SELECT 'payments',count(*) FROM payments UNION ALL SELECT 'member_level_prices',count(*) FROM member_level_prices ORDER BY 1;" | diff -u "$STAGE_DIR/counts-before.txt" -
columns="$(docker exec dujiaonext-postgres-v150-staging psql -U dujiao -d dujiao -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND (table_name,column_name) IN (('users','invite_code_id'),('invite_codes','code'),('invite_codes','used_count'));")"
[[ "$columns" == 3 ]]
test -s "$APP_DIR/docker-compose.yml"
test -s "$APP_DIR/.env"
test -s "$APP_DIR/config/config.yml"
[[ $(grep -Fxc "    image: $OLD_IMAGE" "$APP_DIR/docker-compose.yml" || true) == 1 ]]
(
  cd "$APP_DIR"
  docker compose config -q
)
nginx -t
test -s "$CORE_SCRIPT"
bash -n "$CORE_SCRIPT"
echo 'Checks passed: production healthy, candidate image present, staging migrations and row counts verified.'
if [[ "$MODE" == --dry-run ]]; then
  echo 'Readiness check only. Production files, containers and database were not modified.'
  echo 'Apply only after explicit approval: sudo bash /tmp/dujiao-next-v1.5.0-production-cutover.sh --apply'
  exit 0
fi

# The deployment core creates a fresh production dump immediately before switching.
# Failure rolls back the image/Compose file, never overwrites the live database.
exec env APP_DIR="$APP_DIR" OLD_IMAGE="$OLD_IMAGE" NEW_IMAGE="$NEW_IMAGE" \
  EXPECTED_VERSION="$EXPECTED_VERSION" bash "$CORE_SCRIPT"
