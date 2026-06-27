# Production v1.3.0 TeamGenie Notes

Current production baseline:

- API branch: `main-teamgenie-v1.3.0`
- API version: `v1.3.0-teamgenie`
- API image: `dujiaonext/api:teamgenie-sync`
- Admin image: `dujiaonext/admin:v1.3.0`
- Theme source: `/Users/cike/dujiao-next/unicard-themes`

Deploy API from this worktree:

```bash
scripts/deploy-production-api.sh
```

Upgrade admin:

```bash
cd /opt/dujiao-next
TAG=v1.3.0 docker compose pull dujiaonext-admin
TAG=v1.3.0 docker compose up -d --force-recreate dujiaonext-admin
```

Persist the admin tag with root/sudo:

```bash
sudo sed -i.bak-v130 's/^TAG=.*/TAG=v1.3.0/' /opt/dujiao-next/.env
```

Check runtime:

```bash
scripts/check-production-runtime.sh dujiao-vps
```

Backup location used before the upgrade:

```text
/home/deploy/backups/dujiao-next/
```

Go365 upstream handling:

- Go365 Card may intentionally remain `disabled` when upstream sync is not needed.
- If Go365 returns `403 invalid_api_key`, the API marks the connection `disabled`
  and stops retrying stock sync for that connection.
