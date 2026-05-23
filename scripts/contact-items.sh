#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-https://share.aimosh.com/api/v1}"
ENDPOINT="${BASE_URL%/}/admin/settings/contact-items"

usage() {
  cat <<'EOF'
Usage:
  ADMIN_TOKEN=... scripts/contact-items.sh list
  ADMIN_TOKEN=... scripts/contact-items.sh apply path/to/contact-items.json

Environment:
  BASE_URL      API base URL, default: https://share.aimosh.com/api/v1
  ADMIN_TOKEN  Admin bearer token with system_admin permission

JSON format:
  {
    "items": [
      {
        "type": "wechat_work",
        "label": { "zh-CN": "企业微信", "en-US": "WeCom" },
        "value": "https://work.weixin.qq.com/...",
        "qr_code": "/uploads/contact/wecom.png",
        "enabled": true,
        "sort_order": 10
      }
    ]
  }
EOF
}

require_token() {
  if [[ -z "${ADMIN_TOKEN:-}" ]]; then
    echo "ADMIN_TOKEN is required." >&2
    exit 2
  fi
}

pretty_print() {
  if command -v jq >/dev/null 2>&1; then
    jq .
  else
    cat
  fi
}

cmd="${1:-}"
case "$cmd" in
  list)
    require_token
    curl -fsS \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      "$ENDPOINT" | pretty_print
    ;;
  apply)
    require_token
    file="${2:-}"
    if [[ -z "$file" || ! -f "$file" ]]; then
      echo "A readable JSON file is required." >&2
      usage >&2
      exit 2
    fi
    curl -fsS \
      -X PUT \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      -H "Content-Type: application/json" \
      --data-binary "@${file}" \
      "$ENDPOINT" | pretty_print
    ;;
  -h|--help|help|"")
    usage
    ;;
  *)
    echo "Unknown command: $cmd" >&2
    usage >&2
    exit 2
    ;;
esac
