# Production v1.2.1 TeamGenie Notes

Current production baseline:

- API branch: `main-teamgenie-v1.1.0`
- API version: `v1.2.1-teamgenie`
- API image: `dujiaonext/api:teamgenie-sync`
- Admin image: `dujiaonext/admin:v1.2.1`
- Theme source: `/Users/cike/dujiao-next/unicard-themes`

Deploy API from this worktree:

```bash
scripts/deploy-production-api.sh
```

Check runtime:

```bash
scripts/check-production-runtime.sh dujiao-vps
```

Important VPS note:

`/opt/dujiao-next/.env` still needs root access to persist:

```env
TAG=v1.2.1
```

Suggested root command:

```bash
cd /opt/dujiao-next
cp .env .env.bak-$(date +%Y%m%d%H%M%S)
sed -i 's/^TAG=.*/TAG=v1.2.1/' .env
```

The admin container can run `dujiaonext/admin:v1.2.1` even if `.env` still says
`TAG=v1.0.1`, but a future plain `docker compose up` may recreate it with the old
tag unless `.env` is updated.

Go365 upstream handling:

- If Go365 returns `403 invalid_api_key`, the API marks the connection `disabled`
  and stops retrying stock sync for that connection.
- After fixing Go365 upstream sync/API access, set the connection back to
  `active` and run Ping from the admin UI.
