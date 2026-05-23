# Custom Contact Items

The storefront supports extra contact channels through `site_config.contact.items`.
Use this for WeCom, QQ, email, QR-code support, or any future channel that is not
part of the official Telegram/WhatsApp fields.

## Manage Items

List current items:

```bash
ADMIN_TOKEN=... scripts/contact-items.sh list
```

Apply a JSON file:

```bash
ADMIN_TOKEN=... scripts/contact-items.sh apply examples/contact-items.example.json
```

`ADMIN_TOKEN` must be an admin bearer token with `system_admin` permission for
updates. Read-only admins can call `GET`, but cannot update.

## JSON Shape

```json
{
  "items": [
    {
      "type": "wechat_work",
      "label": { "zh-CN": "企业微信", "en-US": "WeCom" },
      "value": "https://work.weixin.qq.com/...",
      "qr_code": "/uploads/contact/wecom.png",
      "target": "_blank",
      "enabled": true,
      "sort_order": 10
    }
  ]
}
```

Supported fields:

- `type`: `wechat_work`, `wechat`, `qq`, `email`, or any custom string.
- `label`: localized text object, such as `zh-CN`, `zh-TW`, `en-US`.
- `value`: raw value or URL. `email` becomes `mailto:`, numeric `qq` becomes a QQ chat URL.
- `href` / `url`: explicit link; overrides `value` when present.
- `qr_code`: image URL. If present, the storefront opens a QR modal.
- `target`: `_blank` or `_self`; defaults to `_blank`.
- `enabled`: set `false` to hide an item.
- `sort_order`: smaller values appear earlier.

The update endpoint only touches `contact.items`; it keeps brand, Telegram,
WhatsApp, SEO, legal, and other `site_config` fields unchanged.
