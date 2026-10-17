#!/usr/bin/env bash
# 生产镜像升级（在 VPS 上以 root 运行）：备份 → 切换镜像 → 健康检查 → 失败自动回滚。
#
# 用法:
#   sudo NEW_IMAGE=dujiaonext/dujiao-next:teamgenie-v1.5.0-security-<rev> \
#        bash scripts/upgrade-production-image.sh --dry-run     # 只检查，不改任何东西
#   sudo NEW_IMAGE=... bash scripts/upgrade-production-image.sh --apply
#
# 环境变量:
#   NEW_IMAGE         必填，已构建好的新镜像标签
#   OLD_IMAGE         可选，默认取当前运行容器的镜像
#   EXPECTED_VERSION  可选，默认由 NEW_IMAGE 标签推导（teamgenie-v1.5.0-security-REV -> v1.5.0-teamgenie-security-REV）
# 其余路径/容器名见下方默认值，可用同名环境变量覆盖。
#
# 数据库只做备份，从不覆盖；失败时只恢复 compose 文件和镜像。
set -euo pipefail
umask 077

MODE="${1:---dry-run}"
if [[ $# -gt 1 || "$MODE" != --dry-run && "$MODE" != --apply ]]; then
  echo 'Usage: NEW_IMAGE=<image> upgrade-production-image.sh [--dry-run|--apply]' >&2
  exit 1
fi
if [[ $(id -u) != 0 ]]; then
  echo 'Run with sudo on the VPS.' >&2
  exit 1
fi

APP_DIR="${APP_DIR:-/opt/dujiao-next}"
SERVICE="${SERVICE:-dujiaonext}"
CONTAINER="${CONTAINER:-dujiaonext}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-dujiaonext-postgres}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8081/health}"
CONFIG_URL="${CONFIG_URL:-http://127.0.0.1:8081/api/v1/public/config}"
NEW_IMAGE="${NEW_IMAGE:?NEW_IMAGE is required}"
OLD_IMAGE="${OLD_IMAGE:-$(docker inspect "$CONTAINER" --format '{{.Config.Image}}')}"
if [[ -z "${EXPECTED_VERSION:-}" ]]; then
  if [[ "$NEW_IMAGE" =~ :teamgenie-(v[0-9]+\.[0-9]+\.[0-9]+)-security-([A-Za-z0-9]+)$ ]]; then
    EXPECTED_VERSION="${BASH_REMATCH[1]}-teamgenie-security-${BASH_REMATCH[2]}"
  else
    echo 'Cannot derive EXPECTED_VERSION from NEW_IMAGE; set EXPECTED_VERSION explicitly.' >&2
    exit 1
  fi
fi
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

echo "== production image upgrade ($MODE) =="
echo "old_image=$OLD_IMAGE"
echo "new_image=$NEW_IMAGE"
echo "expected_version=$EXPECTED_VERSION"

if [[ "$OLD_IMAGE" == "$NEW_IMAGE" ]]; then
  echo 'OLD_IMAGE and NEW_IMAGE are identical; nothing to do.' >&2
  exit 1
fi
test -f "$COMPOSE_FILE"
docker image inspect "$NEW_IMAGE" >/dev/null
docker inspect "$CONTAINER" --format '{{.State.Health.Status}}' | grep -qx healthy
old_count="$(grep -Fxc "    image: $OLD_IMAGE" "$COMPOSE_FILE" || true)"
if [[ "$old_count" != "1" ]]; then
  echo "Expected exactly one production image reference, found $old_count." >&2
  exit 1
fi
mkdir -p "$ROLLOUT_DIR"
chmod 0700 "$ROLLOUT_DIR"
sed "s#    image: $OLD_IMAGE#    image: $NEW_IMAGE#" "$COMPOSE_FILE" >"$COMPOSE_CANDIDATE"
(cd "$APP_DIR" && docker compose --env-file "$APP_DIR/.env" -f "$COMPOSE_CANDIDATE" config -q)

if [[ "$MODE" == --dry-run ]]; then
  rm -rf "$ROLLOUT_DIR"
  echo 'Checks passed. Nothing was changed. Re-run with --apply to upgrade.'
  exit 0
fi

mkdir -p "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"
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

cp "$COMPOSE_CANDIDATE" "$COMPOSE_FILE"
changed=1
(cd "$APP_DIR" && docker compose up -d --no-deps "$SERVICE")

echo "Waiting for the backend to become healthy."
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

echo "Upgrade completed."
echo "backup_dir=$BACKUP_DIR"
echo "rollout_dir=$ROLLOUT_DIR"
echo "image=$NEW_IMAGE"
echo "Rollback: restore $BACKUP_DIR/docker-compose.yml to $COMPOSE_FILE, then: cd $APP_DIR && docker compose up -d --no-deps $SERVICE"
