#!/usr/bin/env bash
# 把最新的每日数据库备份加密后同步到异地对象存储（rclone，适用于 Cloudflare R2 / B2 / S3 等），
# 校验上传结果，清理过期的远端备份，任何一步失败都会发 Telegram 告警。
#
# 前置条件（在服务器上由你完成一次，密钥不进仓库）:
#   1. 安装 rclone，并配置一个远端，例如名为 offsite:  rclone config
#   2. 创建密码文件（加密口令，务必另存一份到密码管理器，丢失则备份无法解密）:
#        install -m 600 /dev/null /home/deploy/ops/offsite-backup.pass && nano /home/deploy/ops/offsite-backup.pass
#   3. 创建配置文件 /home/deploy/ops/offsite-backup.env（权限 600），内容示例:
#        RCLONE_REMOTE=offsite:my-bucket/dujiao-next
#        TELEGRAM_BOT_TOKEN=123456:ABC...
#        TELEGRAM_CHAT_ID=123456789
#
# 用法:
#   backup-offsite-sync.sh            同步最新备份（适合放进 cron，在每日备份之后运行）
#   backup-offsite-sync.sh --selftest 只测试配置：列远端目录并发一条 Telegram 测试消息，不上传
#
# cron 示例（每日备份 19:43 UTC，之后 20:15 同步）:
#   15 20 * * * /home/deploy/ops/dujiao-security/backup-offsite-sync.sh >>/home/deploy/ops/dujiao-security/offsite-backup.log 2>&1
set -euo pipefail
umask 077

ENV_FILE="${ENV_FILE:-/home/deploy/ops/offsite-backup.env}"
BACKUP_DIR="${BACKUP_DIR:-/home/deploy/backups/dujiao-next-daily}"
PASS_FILE="${PASS_FILE:-/home/deploy/ops/offsite-backup.pass}"
REMOTE_RETENTION_DAYS="${REMOTE_RETENTION_DAYS:-30}"
RCLONE_BIN="${RCLONE_BIN:-rclone}"
MAX_AGE_HOURS="${MAX_AGE_HOURS:-30}"
ALERT_DISABLED="${ALERT_DISABLED:-0}"

if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$ENV_FILE"
fi
: "${RCLONE_REMOTE:?RCLONE_REMOTE is required (set it in $ENV_FILE)}"

for tool in "$RCLONE_BIN" openssl curl; do
  command -v "$tool" >/dev/null || { echo "missing command: $tool" >&2; exit 2; }
done
host="$(hostname)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

alert() {
  local message="$1"
  echo "ALERT: $message" >&2
  if [[ "$ALERT_DISABLED" == 1 || -z "${TELEGRAM_BOT_TOKEN:-}" || -z "${TELEGRAM_CHAT_ID:-}" ]]; then
    return 0
  fi
  curl -fsS -m 15 -o /dev/null \
    "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
    --data-urlencode "chat_id=${TELEGRAM_CHAT_ID}" \
    --data-urlencode "text=[$host] Dujiao offsite backup: $message" || true
}
on_error() {
  local code=$?
  alert "FAILED (exit $code) at line ${BASH_LINENO[0]}. Check offsite-backup.log."
  exit "$code"
}
trap on_error ERR

if [[ "${1:-}" == "--selftest" ]]; then
  "$RCLONE_BIN" lsd "$RCLONE_REMOTE" >/dev/null 2>&1 || "$RCLONE_BIN" mkdir "$RCLONE_REMOTE"
  "$RCLONE_BIN" lsf "$RCLONE_REMOTE" >/dev/null
  test -s "$PASS_FILE"
  alert "selftest OK: remote reachable, passphrase file present."
  echo "selftest=ok"
  exit 0
fi

test -s "$PASS_FILE"
latest="$(find "$BACKUP_DIR" -maxdepth 1 -type f -name 'postgres-*.dump' -print | sort | tail -n 1)"
if [[ -z "$latest" ]]; then
  alert "no daily dump found in $BACKUP_DIR."
  exit 1
fi
age_hours=$(( ( $(date +%s) - $(stat -c %Y "$latest" 2>/dev/null || stat -f %m "$latest") ) / 3600 ))
if (( age_hours > MAX_AGE_HOURS )); then
  alert "latest daily dump is ${age_hours}h old ($latest); the daily backup may have stopped."
  exit 1
fi
(cd "$(dirname "$latest")" && sha256sum --check --quiet "$(basename "$latest").sha256" 2>/dev/null) \
  || (cd "$(dirname "$latest")" && shasum -a 256 --check --quiet "$(basename "$latest").sha256")

name="$(basename "$latest").enc"
openssl enc -aes-256-cbc -pbkdf2 -iter 200000 -salt \
  -pass "file:$PASS_FILE" -in "$latest" -out "$tmpdir/$name"
test -s "$tmpdir/$name"
# 解密还原示例: openssl enc -d -aes-256-cbc -pbkdf2 -iter 200000 -pass file:PASS_FILE -in postgres-XXXX.dump.enc -out postgres-XXXX.dump

"$RCLONE_BIN" copyto "$tmpdir/$name" "$RCLONE_REMOTE/$name"
local_size="$(wc -c <"$tmpdir/$name" | tr -d ' ')"
remote_size="$("$RCLONE_BIN" lsl "$RCLONE_REMOTE/$name" | awk '{print $1}' | head -n 1)"
if [[ "$remote_size" != "$local_size" ]]; then
  alert "upload size mismatch for $name (local $local_size, remote ${remote_size:-none})."
  exit 1
fi

"$RCLONE_BIN" delete "$RCLONE_REMOTE" --min-age "${REMOTE_RETENTION_DAYS}d" --include 'postgres-*.dump.enc'

echo "offsite_backup=ok"
echo "uploaded=$name size=$local_size"
echo "remote_retention_days=$REMOTE_RETENTION_DAYS"
