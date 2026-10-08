#!/usr/bin/env bash

set -euo pipefail
umask 077

APP_DIR="${APP_DIR:-/opt/dujiao-next}"
SERVICE="${SERVICE:-dujiaonext}"
CONTAINER="${CONTAINER:-dujiaonext}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-dujiaonext-postgres}"
OLD_IMAGE="${OLD_IMAGE:-dujiaonext/dujiao-next:teamgenie-v1.4.8-security-20260929}"
NEW_IMAGE="${NEW_IMAGE:-dujiaonext/dujiao-next:teamgenie-v1.4.9-security-20261006}"
EXPECTED_VERSION="${EXPECTED_VERSION:-v1.4.9-teamgenie-security-20261006}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8081/health}"
CONFIG_URL="${CONFIG_URL:-http://127.0.0.1:8081/api/v1/public/config}"
STAMP="$(date -u +%Y%m%d%H%M%S)"
BACKUP_DIR="${BACKUP_DIR:-/opt/backups/dujiao-next-security-$STAMP}"
ROLLOUT_DIR="$APP_DIR/security-rollout-$STAMP"
COMPOSE_FILE="$APP_DIR/docker-compose.yml"
COMPOSE_BACKUP="$ROLLOUT_DIR/docker-compose.yml.before"
COMPOSE_CANDIDATE="$ROLLOUT_DIR/docker-compose.yml.candidate"
changed=0

rollback() {
  local status=$?
  if [[ $status -eq 0 || $changed -eq 0 ]]; then
    return
  fi

  echo "Deployment failed; restoring the previous compose file and backend image." >&2
  cp "$COMPOSE_BACKUP" "$COMPOSE_FILE"
  (cd "$APP_DIR" && docker compose up -d --no-deps "$SERVICE") || true
}
trap rollback EXIT

echo "== security hardening production deployment =="
echo "old_image=$OLD_IMAGE"
echo "new_image=$NEW_IMAGE"

test -f "$COMPOSE_FILE"
docker image inspect "$NEW_IMAGE" >/dev/null
docker inspect "$CONTAINER" --format '{{.State.Health.Status}}' | grep -qx healthy

mkdir -p "$BACKUP_DIR" "$ROLLOUT_DIR"
chmod 0700 "$BACKUP_DIR" "$ROLLOUT_DIR"
install -m 0600 "$COMPOSE_FILE" "$COMPOSE_BACKUP"
install -m 0600 "$COMPOSE_FILE" "$BACKUP_DIR/docker-compose.yml"
install -m 0600 "$APP_DIR/.env" "$BACKUP_DIR/.env"
install -m 0600 "$APP_DIR/config/config.yml" "$BACKUP_DIR/config.yml"

echo "Creating a PostgreSQL backup."
docker exec "$POSTGRES_CONTAINER" sh -ec \
  'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  >"$BACKUP_DIR/postgres.dump"
chmod 0600 "$BACKUP_DIR/postgres.dump"
test -s "$BACKUP_DIR/postgres.dump"
docker exec -i "$POSTGRES_CONTAINER" sh -ec \
  'pg_restore --list >/dev/null' <"$BACKUP_DIR/postgres.dump"

old_count="$(grep -Fxc "    image: $OLD_IMAGE" "$COMPOSE_FILE" || true)"
if [[ "$old_count" != "1" ]]; then
  echo "Expected exactly one production image reference, found $old_count." >&2
  exit 1
fi

sed "s#    image: $OLD_IMAGE#    image: $NEW_IMAGE#" \
  "$COMPOSE_FILE" >"$COMPOSE_CANDIDATE"
(cd "$APP_DIR" && docker compose --env-file "$APP_DIR/.env" -f "$COMPOSE_CANDIDATE" config -q)

cp "$COMPOSE_CANDIDATE" "$COMPOSE_FILE"
changed=1
(cd "$APP_DIR" && docker compose up -d --no-deps "$SERVICE")

echo "Waiting for the hardened backend to become healthy."
for _ in $(seq 1 60); do
  state="$(docker inspect "$CONTAINER" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' 2>/dev/null || true)"
  if [[ "$state" == "healthy" ]] && curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null; then
    break
  fi
  sleep 2
done

docker inspect "$CONTAINER" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' | grep -qx healthy
curl -fsS --max-time 10 "$HEALTH_URL" | grep -q '"status":"ok"'
curl -fsS --max-time 10 "$CONFIG_URL" | grep -Fq "\"app_version\":\"$EXPECTED_VERSION\""
docker inspect "$CONTAINER" --format '{{.Config.Image}}' | grep -Fqx "$NEW_IMAGE"

changed=0
trap - EXIT

echo "Deployment completed."
echo "backup_dir=$BACKUP_DIR"
echo "rollout_dir=$ROLLOUT_DIR"
echo "image=$NEW_IMAGE"
