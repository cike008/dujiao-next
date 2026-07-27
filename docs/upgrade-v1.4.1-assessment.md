# Dujiao-Next v1.4.1 Upgrade Assessment

Date: 2026-07-27

## Official Target

- Latest upstream release checked: `v1.4.1`
- Release page: https://github.com/dujiao-next/dujiao-next/releases
- Official upgrade guide: https://dujiao-next.com/deploy/upgrade

`v1.4.x` is a deployment and architecture upgrade, not a routine patch. The upstream Docker image changes from separate API/user/admin services to one fullstack image that embeds both storefront and admin SPAs into the backend binary.

## Current Production Topology

Current VPS deployment is still v1.3.x style plus local custom services:

- `dujiaonext-api`: `dujiaonext/api:teamgenie-sync`, `127.0.0.1:8081->8080`
- `dujiaonext-admin`: `dujiaonext/admin:v1.3.1`, `127.0.0.1:8082->80`
- `unicard-themes`: `nginx:alpine`, `127.0.0.1:8083->80`
- `dujiaonext-postgres`: `postgres:16-alpine`
- `dujiaonext-redis`: `redis:7-alpine`
- `teamgenie-sync`: `teamgenie-sync:latest`

Important mounts:

- `/opt/dujiao-next/config/config.yml -> /app/config.yml`
- `/opt/dujiao-next/data/uploads -> /app/uploads`
- `/opt/dujiao-next/data/logs -> /app/logs`
- `/opt/unicard-themes/dist -> /usr/share/nginx/html`
- `/home/deploy/runtime/teamgenie-sync-service -> /app/runtime`

## Local Baseline

Backend repo:

- Path: `/Users/cike/dujiao-next/dujiao-next-upgrade-rehearsal-v1.3.0`
- Current branch: `upgrade-rehearsal-v1.4.1`
- Base custom branch before rehearsal: `main-teamgenie-v1.3.0`

Storefront repo:

- Path: `/Users/cike/dujiao-next/unicard-themes`
- Current branch: `main`
- Upgrade baseline commit: `b566689 fix(storefront): stabilize captcha and product detail interactions`

The storefront baseline commit contains recent fixes for captcha loading/layout, business 401 handling, checkout error display, product rich-text copy controls, nav icon behavior, and product detail rendering.

## Merge Rehearsal Result

A dry merge from current custom backend to upstream `v1.4.1` was attempted with `git merge --no-commit --no-ff v1.4.1` and then aborted.

The merge conflicts confirm that this cannot be handled as a normal fast upgrade. Main conflict areas:

- `.gitignore`, `go.mod`, `go.sum`
- old `internal/dto/product.go` deleted upstream but modified locally
- old `internal/http/handlers/*` deleted or relocated upstream
- `internal/provider/container.go` removed upstream, but locally used for TeamGenie wiring
- `internal/router/router.go` removed upstream, but locally used for custom route registration
- `internal/worker/asynq_worker.go` removed upstream, but locally used for TeamGenie async task handling
- `internal/service/teamgenie_sync_service.go` needs relocation into the new module architecture
- `internal/modules/fulfillment/application/service.go` conflicts with our fulfillment hook
- `internal/modules/settings/application/general.go` conflicts with contact/settings normalization

## Custom Features To Port

Required custom backend features:

- TeamGenie fulfilled-order sync
- TeamGenie async queue task and worker handler
- TeamGenie config block in `config.yml`
- custom contact channels in site settings/public config
- custom contact admin endpoint or admin UI support
- product sort order exposure for custom storefront
- upstream stock sync hardening already added locally
- runtime version suffix such as `v1.4.1-teamgenie`

Likely new target locations in upstream `v1.4.1`:

- Dependency wiring: `internal/app/container/*`
- Worker registration: `internal/app/jobs/consumer/*`
- Queue task constants/payloads: `internal/queue/tasks.go` and `internal/constants/constants.go`
- Fulfillment hook: `internal/modules/fulfillment/application/service.go`
- Public config contact/nav output: `internal/modules/settings/transport/http/public/handler.go`
- Settings normalization: `internal/modules/settings/application/*` and `internal/modules/settings/schema/*`
- Product read/sort/stock fields: `internal/modules/catalog/product/*`

## Frontend Strategy

Recommended first phase: keep `unicard-themes` as the production storefront and upgrade backend/admin first.

Reason:

- Current storefront is already heavily customized and deployed separately.
- Upstream `v1.4.1` embedded storefront is Vue-based under `frontend/user`.
- Porting the React storefront into upstream fullstack build would be a larger second project.
- A hybrid deployment preserves the current customer-facing UI while letting backend/admin move forward.

Implication:

- Do not remove `unicard-themes` in the first production cutover.
- New backend fullstack image can still serve embedded admin at `web.admin_path`.
- Nginx should route root storefront to `unicard-themes`, and route API/admin/upload paths to the new Dujiao-Next backend.

## Backup Requirements Before Any Production Upgrade

Create a timestamped backup directory, for example:

```sh
TS=$(date +%Y%m%d%H%M%S)
BACKUP_DIR=/opt/backups/dujiao-next-$TS
sudo mkdir -p "$BACKUP_DIR"
```

Required backups:

- PostgreSQL dump from `dujiaonext-postgres`
- `/opt/dujiao-next/config/config.yml`
- `/opt/dujiao-next/docker-compose.yml`
- `/opt/dujiao-next/.env`
- `/opt/dujiao-next/data/uploads`
- `/opt/unicard-themes/dist`
- `/opt/unicard-themes/nginx.conf`
- `/home/deploy/runtime/teamgenie-sync-service`
- active Nginx site config
- current `docker ps` and compose image list

PostgreSQL dump should be verified after creation with `pg_restore -l` if custom format is used.

## Recommended Implementation Order

1. Keep production untouched.
2. Push/store current storefront baseline commit.
3. Create a real v1.4.1 custom backend branch from upstream `v1.4.1`.
4. Port TeamGenie config, service, queue task, worker handler, and fulfillment hook into the new module structure.
5. Port custom contact channel support into the new settings/public-config structure.
6. Build a custom fullstack backend image, for example `dujiaonext/dujiao-next:teamgenie-v1.4.1`.
7. Run local/unit tests and compile the Docker image.
8. Prepare a staging compose on non-production ports with copied config and a database backup restore.
9. Validate login, captcha, product list/detail, stock display, sort order, checkout/payment, callbacks, uploads, admin settings, and TeamGenie sync.
10. Back up production.
11. Deploy production using a reviewed compose and Nginx change.

## Rollback Notes

Rollback must restore database backup if the `v1.4.1` process has run migrations against production.

Keep these old runtime pieces until the new version is stable:

- `dujiaonext/api:teamgenie-sync`
- `dujiaonext/admin:v1.3.1`
- `nginx:alpine` storefront container
- old `/opt/dujiao-next/docker-compose.yml`
- old Nginx config
- database dump and uploads backup

## Current Recommendation

Proceed with custom backend porting and staging rehearsal first. Do not apply the official production upgrade steps directly to this VPS yet.
