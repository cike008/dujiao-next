#!/usr/bin/env bash
# 分销自定义域名定时体检（在生产服务器上以 deploy 用户运行，只读）。
# 从数据库取「启用中的自定义域名」，逐个运行 check-reseller-domain.sh（DNS、所有权 TXT、证书与剩余天数、
# 接入、后台接口隐藏、分销站识别）。状态变化时发 Telegram：首次失败、每 24 小时一次的未恢复提醒、恢复。
#
# 安装（只需一次，deploy 用户）:
#   cp check-reseller-domain.sh monitor-reseller-domains.sh /home/deploy/ops/dujiao-security/
#   (crontab -l; echo '23 */6 * * * /home/deploy/ops/dujiao-security/monitor-reseller-domains.sh >/dev/null 2>&1') | crontab -
#   /home/deploy/ops/dujiao-security/monitor-reseller-domains.sh      # 立即跑一次（通过时静默，不发消息）
#
# 环境变量: EXPECT_IP（默认生产 IP）、MIN_CERT_DAYS（默认 21）、ALERT_ENV（Telegram 凭据文件）、DOMAINS（空格分隔，
# 设置后不查数据库，便于手动演练）、DRY_NOTIFY=1（只打印将发送的消息，不真正发送）。
set -euo pipefail
umask 077

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK_SCRIPT="${CHECK_SCRIPT:-$HERE/check-reseller-domain.sh}"
ALERT_ENV="${ALERT_ENV:-$HERE/alert.env}"
STATE_FILE="${STATE_FILE:-$HERE/reseller-domains.state}"
LOG_FILE="${LOG_FILE:-$HERE/reseller-domains.log}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-dujiaonext-postgres}"
export EXPECT_IP="${EXPECT_IP:-104.224.159.190}"
export MIN_CERT_DAYS="${MIN_CERT_DAYS:-21}"
REMIND_SECONDS="${REMIND_SECONDS:-86400}"
now="$(date +%s)"
stamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

notify() {
  if [[ "${DRY_NOTIFY:-0}" == 1 ]]; then printf '[notify] %s\n' "$1"; return 0; fi
  [[ -f "$ALERT_ENV" ]] || return 0
  local token chat
  token="$(grep -m1 '^TELEGRAM_BOT_TOKEN=' "$ALERT_ENV" | cut -d= -f2-)"
  chat="$(grep -m1 '^TELEGRAM_CHAT_ID=' "$ALERT_ENV" | cut -d= -f2-)"
  [[ -n "$token" && -n "$chat" ]] || return 0
  curl -fsS -m 15 -o /dev/null "https://api.telegram.org/bot${token}/sendMessage" \
    --data-urlencode "chat_id=${chat}" --data-urlencode "text=$1" >/dev/null 2>&1 \
    || echo "[$stamp] telegram notify failed" >>"$LOG_FILE"
}

if [[ -n "${DOMAINS:-}" ]]; then
  domains="$DOMAINS"
else
  # 只读查询；表与字段见 reseller_domains（type=custom, status=active, 未软删除）
  domains="$(docker exec -i "$POSTGRES_CONTAINER" sh -ec 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At' \
    <<<"select domain from reseller_domains where type='custom' and status='active' and deleted_at is null order by id;" \
    | tr '\n' ' ')" || { notify "❌ 分销域名体检无法读取域名列表（数据库不可用？）"; exit 1; }
fi
domains="$(echo "$domains" | xargs)"
if [[ -z "$domains" ]]; then
  echo "[$stamp] no active custom domains" >>"$LOG_FILE"
  rm -f "$STATE_FILE"
  exit 0
fi

# shellcheck disable=SC2086
output="$(bash "$CHECK_SCRIPT" $domains 2>&1)" && rc=0 || rc=$?
{ echo "[$stamp] rc=$rc domains=$domains"; [[ $rc -ne 0 ]] && printf '%s\n' "$output"; } >>"$LOG_FILE"
# 日志只保留最近 2000 行
if [[ $(wc -l <"$LOG_FILE") -gt 2000 ]]; then tail -n 2000 "$LOG_FILE" >"$LOG_FILE.tmp" && mv "$LOG_FILE.tmp" "$LOG_FILE"; fi

prev_status="ok"; prev_alert=0
if [[ -f "$STATE_FILE" ]]; then
  read -r prev_status prev_alert <"$STATE_FILE" || true
fi

if [[ $rc -eq 0 ]]; then
  if [[ "$prev_status" == "failed" ]]; then
    notify "✅ 分销域名体检已恢复正常（${domains}）"
  fi
  printf 'ok 0\n' >"$STATE_FILE"
  exit 0
fi

# 失败：只列出失败的域名与项目，避免消息过长
summary="$(printf '%s\n' "$output" | awk '/^== /{dom=$2} /FAIL/{sub(/^ +/,""); print dom ": " $0}' | head -12)"
if [[ "$prev_status" != "failed" || $(( now - prev_alert )) -ge $REMIND_SECONDS ]]; then
  notify "❌ 分销域名体检失败（$(hostname)）
${summary}
完整结果: ${LOG_FILE}"
  printf 'failed %s\n' "$now" >"$STATE_FILE"
else
  printf 'failed %s\n' "$prev_alert" >"$STATE_FILE"
fi
exit 1
