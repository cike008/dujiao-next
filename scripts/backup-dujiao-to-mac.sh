#!/usr/bin/env bash
# 把 Dujiao 生产站的备份拉取到这台 Mac（异地备份）。由 Mac 主动连接服务器，服务器不需要知道 Mac 的任何信息。
#   1) 每日数据库备份 postgres-*.dump（只拉新增，拉完校验 sha256，并用 pg_restore --list 检查可读）
#   2) 配置包 dujiao-config-*.tar.gz（config.yml / .env / compose / nginx，由服务器 root 定时生成）
#   3) 上传文件 uploads（只增不删，服务器误删文件不会同步清空本地）
# 备份含用户数据和密钥配置：目录 700、文件 600，依赖 Mac 的 FileVault 磁盘加密。
# 用法: bash scripts/backup-dujiao-to-mac.sh      定时运行见 scripts/launchd/com.aimosh.dujiao-backup.plist
set -euo pipefail
umask 077

REMOTE="${BACKUP_REMOTE:-dujiao-vps}"
DEST="${BACKUP_DEST:-$HOME/Backups/dujiao-next}"
REMOTE_DB_DIR="${REMOTE_DB_DIR:-/home/deploy/backups/dujiao-next-daily}"
REMOTE_CONFIG_DIR="${REMOTE_CONFIG_DIR:-/home/deploy/backups/dujiao-config}"
REMOTE_UPLOADS="${REMOTE_UPLOADS:-/opt/dujiao-next/data/uploads}"
KEEP_DB="${BACKUP_KEEP_DB:-30}"
KEEP_CONFIG="${BACKUP_KEEP_CONFIG:-14}"
MAX_DB_AGE_HOURS="${BACKUP_MAX_DB_AGE_HOURS:-30}"
MAX_CONFIG_AGE_HOURS="${BACKUP_MAX_CONFIG_AGE_HOURS:-30}"
PG_RESTORE="${PG_RESTORE:-$(command -v pg_restore || echo /opt/homebrew/opt/postgresql@16/bin/pg_restore)}"
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=15 -o ServerAliveInterval=10 -o ServerAliveCountMax=3)
stamp="$(date -u +%Y%m%dT%H%M%SZ)"

for number in "$KEEP_DB" "$KEEP_CONFIG" "$MAX_DB_AGE_HOURS" "$MAX_CONFIG_AGE_HOURS"; do
  [[ "$number" =~ ^[1-9][0-9]*$ ]] || { echo "Invalid backup retention or age setting" >&2; exit 1; }
done

mkdir -p "$DEST/db" "$DEST/config" "$DEST/uploads"
chmod 700 "$DEST" "$DEST/db" "$DEST/config" "$DEST/uploads"

notify() { /usr/bin/osascript -e "display notification \"$1\" with title \"Dujiao 备份\"" >/dev/null 2>&1 || true; }
fail() {
  echo "备份失败：$1" >&2
  notify "备份失败：$1"
  printf '{"ok":false,"at":"%s","error":"%s"}\n' "$stamp" "$1" >"$DEST/status.json" 2>/dev/null || true
  exit 1
}
rsync_pull() { rsync -a --ignore-existing -e "ssh ${SSH_OPTS[*]}" "$@"; }

# 1) 数据库
rsync_pull --include='postgres-*.dump' --include='postgres-*.dump.sha256' --exclude='*' \
  "$REMOTE:$REMOTE_DB_DIR/" "$DEST/db/" || fail "拉取数据库备份失败"
chmod 600 "$DEST"/db/postgres-* 2>/dev/null || true
latest_db="$(ls -1t "$DEST"/db/postgres-*.dump 2>/dev/null | head -1 || true)"
[[ -n "$latest_db" && -s "$latest_db" ]] || fail "没有可用的数据库备份"
# .sha256 里记的是服务器上的绝对路径，所以只比对哈希值本身
expected_sum="$(awk '{print $1}' "$latest_db.sha256" 2>/dev/null || true)"
actual_sum="$(shasum -a 256 "$latest_db" | awk '{print $1}')"
[[ -n "$expected_sum" && "$expected_sum" == "$actual_sum" ]] || fail "数据库备份校验和不一致"
if [[ -x "$PG_RESTORE" ]]; then
  "$PG_RESTORE" --list "$latest_db" >/dev/null 2>&1 || fail "数据库备份无法被 pg_restore 读取"
  restore_check="ok"
else
  restore_check="skipped"
fi
db_age_hours=$(( ( $(date +%s) - $(stat -f %m "$latest_db") ) / 3600 ))
(( db_age_hours >= 0 && db_age_hours <= MAX_DB_AGE_HOURS )) || fail "最新数据库备份已过期或时间异常"

# 2) 配置包（服务器 root 定时生成；缺失视为失败，因为没有 config.yml 就恢复不出可用站点）
rsync_pull --include='dujiao-config-*.tar.gz' --include='dujiao-config-*.tar.gz.sha256' --exclude='*' "$REMOTE:$REMOTE_CONFIG_DIR/" "$DEST/config/" \
  || fail "拉取配置包失败（服务器是否已安装 dujiao-config-bundle？）"
chmod 600 "$DEST"/config/dujiao-config-*.tar.gz 2>/dev/null || true
latest_config="$(ls -1t "$DEST"/config/dujiao-config-*.tar.gz 2>/dev/null | head -1 || true)"
[[ -n "$latest_config" ]] || fail "没有配置包"
expected_config_sum="$(awk '{print $1}' "$latest_config.sha256" 2>/dev/null || true)"
actual_config_sum="$(shasum -a 256 "$latest_config" | awk '{print $1}')"
[[ -n "$expected_config_sum" && "$expected_config_sum" == "$actual_config_sum" ]] || fail "配置包校验和不一致或缺失"
config_age_hours=$(( ( $(date +%s) - $(stat -f %m "$latest_config") ) / 3600 ))
(( config_age_hours >= 0 && config_age_hours <= MAX_CONFIG_AGE_HOURS )) || fail "配置包已过期或时间异常，服务器配置备份可能停了"
config_listing="$(tar tzf "$latest_config")" || fail "配置包损坏"
for required in config/config.yml .env docker-compose.yml; do
  grep -Fqx "${REMOTE_APP_DIR:-opt/dujiao-next}/$required" <<<"$config_listing" || fail "配置包缺少必要文件"
done

# 3) 上传文件（不加 --delete）
rsync_pull "$REMOTE:$REMOTE_UPLOADS/" "$DEST/uploads/" || fail "拉取上传文件失败"
uploads_count="$(find "$DEST/uploads" -type f | wc -l | tr -d ' ')"

# 4) 保留策略
ls -1t "$DEST"/db/postgres-*.dump | tail -n +"$((KEEP_DB + 1))" | while read -r f; do rm -f -- "$f" "$f.sha256"; done
ls -1t "$DEST"/config/dujiao-config-*.tar.gz | tail -n +"$((KEEP_CONFIG + 1))" | while read -r f; do rm -f -- "$f" "$f.sha256"; done

printf '{"ok":true,"at":"%s","latest_db":"%s","db_age_hours":%s,"pg_restore_list":"%s","latest_config":"%s","uploads_files":%s}\n' \
  "$stamp" "$(basename "$latest_db")" "$db_age_hours" "$restore_check" "$(basename "$latest_config")" "$uploads_count" >"$DEST/status.json"
echo "备份完成：$(basename "$latest_db")（${db_age_hours} 小时前，pg_restore 检查 ${restore_check}）；配置包 $(basename "$latest_config")；上传文件 $uploads_count 个"
