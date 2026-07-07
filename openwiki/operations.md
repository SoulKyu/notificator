# Operations

How to build, run, test, deploy, and maintain Notificator. All Make targets live in `Makefile`
(`make help` lists them).

## Local testing — `make test` (the primary dev loop)

The maintainer's normal way to test a new feature is a full docker-compose rebuild:

```bash
make test
```

`make test` (`Makefile:209`) runs `webui-full-rebuild` + `docker-build-all`, then
`docker-compose down && docker-compose up -d`. It rebuilds the WebUI assets (CSS + templates),
builds all three images (backend, webui, fake-alertmanager), and restarts the stack. This is the
recommended way to exercise changes end-to-end against a realistic environment (Postgres + fake
Alertmanager + backend + webui). See the Compose topology below.

For faster inner-loop iteration without Docker:

```bash
make run-all          # runs backend then webui via `go run`, concurrently
# or individually:
make dev-backend
make dev-webui        # regenerates templates first, then `go run . webui`
```

## Build variants

| Target / command | Output | Tags |
|------------------|--------|------|
| `make go-build-backend` | `bin/backend` | (Docker uses `-tags nogui`) |
| `make go-build-webui` | `bin/webui` | (Docker uses `-tags "nogui,webui"`) |
| `Dockerfile.backend` | server backend image | `-tags nogui` |
| `Dockerfile.webui` | server webui image | `-tags "nogui,webui"` |

See [architecture](architecture.md#build-variants). The `go-*-desktop` targets and the default
tagless build are **not useful** — the desktop GUI command has been removed.

## Codegen — never edit generated files {#codegen}

Three generated artifact families. **Edit the source, run the target, never touch the output:**

| Generated (do NOT edit) | Source | Regenerate with |
|-------------------------|--------|-----------------|
| `internal/webui/**/*_templ.go` | `internal/webui/templates/**/*.templ` | `make webui-templates` (`templ generate`) |
| `internal/webui/static/css/output.css` | `internal/webui/static/css/input.css` | `make webui-css` (dev) / `make webui-css-prod` |
| `internal/backend/proto/**/*.pb.go` | `proto/alert.proto`, `proto/auth.proto` | `make proto` (`scripts/generate_proto.sh`) |

`make webui-full-rebuild` does clean + npm install + CSS build + template generate in one shot
(this is what the WebUI Docker image runs).

> LSP tip: after `make proto` your editor's Go language server may report stale "unknown field"
> errors on the regenerated `.pb.go`. Trust `go build`, not the LSP, until it re-indexes.

## Deployment

### Docker Compose (`docker-compose.yml`)

Four services on one bridge network:

| Service | Image | Ports |
|---------|-------|-------|
| `postgres` | `postgres:15-alpine` | 5432 |
| `alertmanager` | `soulkyu/notificator-alertmanager` (fake) | 9093 |
| `backend` | `soulkyu/notificator-backend` | 50051 (gRPC), 8080 (HTTP) |
| `webui` | `soulkyu/notificator-webui` | 8081 |

Dependency order is enforced by healthchecks (backend waits for postgres+alertmanager; webui
waits for backend). All config is passed as inline env vars (OAuth and Sentry disabled by
default). Set `NOTIFICATOR_SESSION_SECRET` in your host env for session persistence across
restarts — it defaults empty.

### Kubernetes (`charts/notificator-app/`)

Helm chart (chart `v0.1.0`). Renders backend + webui **Deployments** (1 replica each, no HPA),
Services, ServiceAccounts, and a webui **Ingress** (nginx class, optional TLS). The in-cluster
Alertmanager deployment is off by default (`alertmanager.enabled: false`) — you point at an
external Alertmanager via the top-level `alertmanagerConfig` list.

```bash
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set webui.ingress.host=notificator.example.com
```

`values.yaml` exposes per-service `image`, `labels`/`annotations`, `securityContext`
(**empty/unhardened by default** — set `runAsNonRoot`, drop capabilities, etc. for production),
`serviceAccount`, and free-form `env: {}` maps to inject any `NOTIFICATOR_*` / `OAUTH_*` var.

> ⚠️ **Single backend replica only.** Real-time collaboration subscriptions are in-process
> (see [backend](backend.md#real-time-collaboration-push)) — do not scale the backend `Deployment`
> above 1 replica without adding an external pub/sub.

## Health & metrics

The backend serves `GET /health` and `GET /metrics` on `:8080`. **`/metrics` is JSON, not
Prometheus exposition format** — don't point a Prometheus scraper at it expecting text metrics.
The WebUI serves `/health` on `:8081`. Compose/Helm use these for readiness.

## Database maintenance

- **Migrations** run automatically on backend start (`--migrate`, default on). Custom migrations
  run before GORM `AutoMigrate` (see [backend](backend.md#database)).
- **Retention** is automatic: resolved-alert rows expire by TTL (hourly cleanup); statistics rows
  are purged after `Statistics.RetentionDays` (daily, default 90d).
- **SQLite vs Postgres**: SQLite is fine for local/dev (default `./notificator.db`); use Postgres
  for production. CGO must be enabled to build with SQLite (both Docker images do this). Note the
  statistics **heatmap and flapping** queries are **PostgreSQL-only** (see
  [backend](backend.md#statistics-engine)) — they error on SQLite.
- **Timezones**: the binary blank-imports `time/tzdata` (`main.go`), so IANA zones resolve even in
  alpine-based images that ship no `/usr/share/zoneinfo` — required for timezone-aware statistics
  period/heatmap bucketing.

## Fake Alertmanager (`alertmanager/fake/`)

A Python (uv/pyproject) mock implementing Alertmanager's `/api/v2/*` surface, so you can run and
test the whole stack without a real Prometheus/Alertmanager. It ships its own `Dockerfile` and is
the `alertmanager` service in Compose.
