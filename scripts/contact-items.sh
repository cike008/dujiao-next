#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-https://share.aimosh.com/api/v1}"
ENDPOINT="${BASE_URL%/}/admin/settings/contact-items"
BACKUP_DIR="${BACKUP_DIR:-contact-items-backups}"

usage() {
  cat <<'EOF'
Usage:
  ADMIN_TOKEN=... scripts/contact-items.sh list
  ADMIN_TOKEN=... scripts/contact-items.sh backup
  scripts/contact-items.sh validate path/to/contact-items.json
  ADMIN_TOKEN=... scripts/contact-items.sh apply path/to/contact-items.json

Environment:
  BASE_URL              API base URL, default: https://share.aimosh.com/api/v1
  ADMIN_TOKEN          Admin bearer token with system_admin permission
  BACKUP_DIR           Backup directory, default: contact-items-backups
  SKIP_BACKUP=1        Skip automatic backup before apply

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

require_jq() {
  if ! command -v jq >/dev/null 2>&1; then
    echo "jq is required for this command." >&2
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

validate_file() {
  local file="$1"
  if [[ -z "$file" || ! -f "$file" ]]; then
    echo "A readable JSON file is required." >&2
    exit 2
  fi
  require_jq
  jq -e '
    type == "object"
    and (.items | type == "array")
    and all(.items[]?;
      type == "object"
      and ((.label // {}) | type == "object")
      and (((.label // {}) | to_entries | map(.value | tostring | length > 0) | any) == true)
      and (((.value // "") | tostring | length > 0)
        or ((.href // "") | tostring | length > 0)
        or ((.url // "") | tostring | length > 0)
        or ((.qr_code // "") | tostring | length > 0))
    )
  ' "$file" >/dev/null
}

backup_current() {
  require_token
  mkdir -p "$BACKUP_DIR"
  local ts file
  ts="$(date +%Y%m%d%H%M%S)"
  file="${BACKUP_DIR}/contact-items-${ts}.json"

  if command -v jq >/dev/null 2>&1; then
    curl -fsS -H "Authorization: Bearer ${ADMIN_TOKEN}" "$ENDPOINT" \
      | jq '.data' > "$file"
  else
    file="${BACKUP_DIR}/contact-items-${ts}.raw.json"
    curl -fsS -H "Authorization: Bearer ${ADMIN_TOKEN}" "$ENDPOINT" > "$file"
    echo "jq not found; saved raw API response instead of apply-ready JSON." >&2
  fi
  echo "$file"
}

cmd="${1:-}"
case "$cmd" in
  list)
    require_token
    curl -fsS \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      "$ENDPOINT" | pretty_print
    ;;
  backup)
    backup_current
    ;;
  validate)
    validate_file "${2:-}"
    echo "ok - contact items JSON is valid"
    ;;
  apply)
    require_token
    file="${2:-}"
    if [[ -z "$file" || ! -f "$file" ]]; then
      echo "A readable JSON file is required." >&2
      usage >&2
      exit 2
    fi
    validate_file "$file"
    if [[ "${SKIP_BACKUP:-0}" != "1" ]]; then
      backup_file="$(backup_current)"
      echo "backup saved: ${backup_file}" >&2
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
