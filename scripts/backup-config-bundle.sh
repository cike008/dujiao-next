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
name="dujiao-config-$stamp.tar.gz"

[[ $(id -u) == 0 ]] || { echo "Run as root." >&2; exit 1; }
test -f "$APP_DIR/config/config.yml"
[[ "$KEEP" =~ ^[1-9][0-9]*$ ]] || { echo "Invalid KEEP" >&2; exit 1; }
user="${OWNER%%:*}"

files=("${APP_DIR#/}/config/config.yml" "${APP_DIR#/}/.env" "${APP_DIR#/}/docker-compose.yml")
nginx_files=()
for item in nginx/nginx.conf nginx/conf.d nginx/sites-available nginx/sites-enabled nginx/snippets; do
  [[ -e "/etc/$item" ]] && nginx_files+=("-C" "/etc" "$item")
done
work="$(mktemp -d /var/tmp/dujiao-config.XXXXXXXX)"
trap 'rm -rf -- "$work"' EXIT
tmp="$work/bundle.tar.gz"
tar czf "$tmp" -C / "${files[@]}" "${nginx_files[@]}"
test -s "$tmp"
listing="$(tar tzf "$tmp")"
grep -q "config/config.yml$" <<<"$listing"
digest="$(sha256sum "$tmp" | awk '{print $1}')"
# Only the unprivileged recipient opens paths in its writable directory.
runuser -u "$user" -- bash -c '
set -euo pipefail
umask 077
dir=$1 name=$2 digest=$3 keep=$4
mkdir -p -- "$dir"
chmod 0700 "$dir"
cd -- "$dir"
stage=$(mktemp -d .publish.XXXXXXXX)
trap '\''rm -rf -- "$stage"'\'' EXIT
cat >"$stage/$name"
printf "%s  %s\n" "$digest" "$name" >"$stage/$name.sha256"
(cd "$stage" && sha256sum -c "$name.sha256" >/dev/null)
mv -T -- "$stage/$name" "$name"
mv -T -- "$stage/$name.sha256" "$name.sha256"
mapfile -t old < <(find . -maxdepth 1 -type f -name "dujiao-config-*.tar.gz" -printf "%f\n" | sort -r | tail -n +"$((keep + 1))")
for file in "${old[@]}"; do rm -f -- "$file" "$file.sha256"; done
' _ "$OUT_DIR" "$name" "$digest" "$KEEP" <"$tmp"
echo "config_bundle=ok file=$OUT_DIR/$name"
