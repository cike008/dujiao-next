#!/usr/bin/env bash
# 把恢复站点必需、但 deploy 用户读不到的配置打包到 deploy 可读的目录，供异地备份拉取。
# 在服务器上以 root 运行（由 /etc/cron.d 每天触发一次）。内容：
#   config.yml（root 600，含会话签名密钥和数据库密码）、.env、docker-compose.yml、
#   nginx 配置（不含证书私钥）。
#
# 安装（只需一次，root 执行；脚本必须由 root 拥有，不能放在 deploy 可写的目录里）:
#   sudo install -m 755 -o root -g root backup-config-bundle.sh /usr/local/sbin/dujiao-config-bundle
#   echo '20 19 * * * root /usr/local/sbin/dujiao-config-bundle >>/var/log/dujiao-config-bundle.log 2>&1' \
#     | sudo tee /etc/cron.d/dujiao-config-bundle
#   sudo /usr/local/sbin/dujiao-config-bundle      # 立即生成一份
set -euo pipefail
umask 077

APP_DIR="${APP_DIR:-/opt/dujiao-next}"
OUT_DIR="${OUT_DIR:-/home/deploy/backups/dujiao-config}"
OWNER="${OWNER:-deploy:deploy}"
KEEP="${KEEP:-14}"
stamp="$(date -u +%Y%m%d%H%M%S)"
out="$OUT_DIR/dujiao-config-$stamp.tar.gz"

[[ $(id -u) == 0 ]] || { echo "Run as root." >&2; exit 1; }
test -f "$APP_DIR/config/config.yml"
mkdir -p "$OUT_DIR"
chmod 0700 "$OUT_DIR"
chown "$OWNER" "$OUT_DIR"

files=("${APP_DIR#/}/config/config.yml" "${APP_DIR#/}/.env" "${APP_DIR#/}/docker-compose.yml")
nginx_files=()
for item in nginx/nginx.conf nginx/conf.d nginx/sites-available nginx/sites-enabled nginx/snippets; do
  [[ -e "/etc/$item" ]] && nginx_files+=("-C" "/etc" "$item")
done
tmp="$(mktemp "$OUT_DIR/.config-bundle.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
tar czf "$tmp" -C / "${files[@]}" "${nginx_files[@]}"
test -s "$tmp"
listing="$(tar tzf "$tmp")"
grep -q "config/config.yml$" <<<"$listing"
mv "$tmp" "$out"
chown "$OWNER" "$out"
chmod 0600 "$out"
trap - EXIT

ls -1t "$OUT_DIR"/dujiao-config-*.tar.gz | tail -n +"$((KEEP + 1))" | xargs -r rm -f --
echo "config_bundle=ok file=$out"
