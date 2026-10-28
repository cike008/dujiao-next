#!/usr/bin/env bash
# 分销自定义域名只读健康检查（可在任意机器上运行，不需要服务器权限，不修改任何东西）。
#
# 用法:
#   EXPECT_IP=104.224.159.190 scripts/check-reseller-domain.sh store.example.com [more.example.com ...]
#
# 环境变量:
#   EXPECT_IP     期望的 A 记录（留空则只要求存在 A 记录）
#   MIN_CERT_DAYS 证书剩余天数低于该值时报失败（默认 21）
#   RESOLVER      查询 DNS 的公共解析器（默认 1.1.1.1，避开本机代理的假 IP）
#
# 退出码: 全部通过为 0；任一域名有失败项为 1。适合放进 cron / CI 做证书到期和接入监控。
set -u

EXPECT_IP="${EXPECT_IP:-}"
MIN_CERT_DAYS="${MIN_CERT_DAYS:-21}"
RESOLVER="${RESOLVER:-1.1.1.1}"
CURL=(curl -s -m 15)

if [[ $# -lt 1 ]]; then
  echo "Usage: EXPECT_IP=<ip> $0 <domain> [domain...]" >&2
  exit 2
fi
for cmd in openssl curl python3; do
  command -v "$cmd" >/dev/null || { echo "missing command: $cmd" >&2; exit 2; }
done

# 查 DNS：有 dig 用 dig，没有就退回 Cloudflare DNS-over-HTTPS（只需要 curl + python3）。
# 输出格式与 `dig +short` 一致：每行一条记录，TXT 带双引号。
dns_short() { # 记录类型 名称
  if command -v dig >/dev/null; then
    dig @"$RESOLVER" +short "$1" "$2"
    return
  fi
  curl -s -m 10 -H 'accept: application/dns-json' "https://cloudflare-dns.com/dns-query?name=$2&type=$1" |
    python3 -c 'import json,sys
try:
    for a in json.load(sys.stdin).get("Answer", []):
        print(a.get("data", ""))
except Exception:
    pass'
}

overall=0
for domain in "$@"; do
  domain="$(printf '%s' "$domain" | tr '[:upper:]' '[:lower:]')"
  if [[ ! "$domain" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]]; then
    echo "== $domain: invalid domain" >&2
    overall=1
    continue
  fi
  echo "== $domain"
  failed=0
  pass() { echo "  PASS  $1"; }
  fail() { echo "  FAIL  $1"; failed=1; }

  # DNS
  ips="$(dns_short A "$domain" | grep -E '^[0-9.]+$' | tr '\n' ' ')"
  if [[ -z "$ips" ]]; then
    fail "no A record via $RESOLVER"
  elif [[ -n "$EXPECT_IP" && " $ips" != *" $EXPECT_IP "* ]]; then
    fail "A record is [$ips], expected $EXPECT_IP"
  else
    pass "A record: $ips"
  fi
  txt="$(dns_short TXT "_moshshop-verify.$domain" | tr -d '"')"
  if [[ "$txt" == moshshop-verification=* ]]; then
    pass "ownership TXT present"
  else
    fail "ownership TXT _moshshop-verify.$domain missing"
  fi

  # Certificate
  cert="$(echo | timeout 15 openssl s_client -connect "$domain:443" -servername "$domain" 2>/dev/null | openssl x509 -noout -enddate -checkhost "$domain" 2>/dev/null)"
  if grep -q "does match certificate" <<<"$cert"; then
    pass "certificate covers hostname"
  else
    fail "certificate does not cover $domain (or TLS failed)"
  fi
  end="$(sed -n 's/^notAfter=//p' <<<"$cert")"
  if [[ -n "$end" ]]; then
    days="$(python3 - "$end" <<'PY'
import sys, datetime
end = datetime.datetime.strptime(sys.argv[1].strip(), "%b %d %H:%M:%S %Y %Z").replace(tzinfo=datetime.timezone.utc)
print((end - datetime.datetime.now(datetime.timezone.utc)).days)
PY
)"
    if [[ "$days" =~ ^-?[0-9]+$ ]] && (( days >= MIN_CERT_DAYS )); then
      pass "certificate valid for $days more days"
    else
      fail "certificate expires in ${days:-?} days (minimum $MIN_CERT_DAYS)"
    fi
  else
    fail "could not read certificate expiry"
  fi

  # HTTP behaviour
  redirect="$("${CURL[@]}" -o /dev/null -w '%{http_code} %{redirect_url}' "http://$domain/")"
  if [[ "$redirect" == "301 https://$domain/"* ]]; then pass "http redirects to https"; else fail "http redirect unexpected: $redirect"; fi
  headers="$("${CURL[@]}" -I "https://$domain/")"
  for h in x-content-type-options strict-transport-security; do
    if grep -qi "^$h:" <<<"$headers"; then pass "header $h"; else fail "header $h missing"; fi
  done
  code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' "https://$domain/api/v1/admin/")"
  [[ "$code" == 404 ]] && pass "/api/v1/admin/ hidden (404)" || fail "/api/v1/admin/ returned $code (expected 404)"
  code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' "https://$domain/api/v1/reseller/profile")"
  [[ "$code" == 404 ]] && pass "/api/v1/reseller/ hidden (404)" || fail "/api/v1/reseller/ returned $code (expected 404)"
  "${CURL[@]}" "https://$domain/health" | grep -q '"status":"ok"' && pass "health ok" || fail "health check failed"

  # Tenant recognition
  tenant="$("${CURL[@]}" "https://$domain/api/v1/public/config" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)["data"]
    t = d.get("tenant") or {}
    print(t.get("mode", ""), t.get("host", ""), d.get("app_version", ""))
except Exception:
    print("")
' 2>/dev/null)"
  if [[ "$tenant" == "reseller $domain "* ]]; then
    pass "recognised as reseller tenant (${tenant##* })"
  else
    fail "tenant not recognised as reseller for this host: [$tenant]"
  fi

  (( failed )) && overall=1
done
exit "$overall"
