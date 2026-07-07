# Notificator — Quickstart

Notificator is a **team-oriented web UI for Prometheus Alertmanager**. It connects to
one or more Alertmanagers (including multi-tenant Grafana Mimir / Cortex) and shows every
firing alert in a single dashboard, with real-time collaboration: acknowledgements,
comments, assignments, hidden alerts, and historical statistics (MTTR / MTTA / on-call
analytics).

The project is a Go monorepo. The supported runtime is **two cooperating processes**:

- **Backend** (`notificator backend`) — a gRPC + HTTP server that owns the database, auth,
  sessions, OAuth, alert-collaboration state, and the statistics engine.
- **WebUI** (`notificator webui`) — a Gin web server that renders the browser UI, polls
  Alertmanager for live alerts, and calls the backend over gRPC for everything persistent.

> ⚠️ **Deprecated: the Fyne desktop GUI.** The project once shipped a third variant — a Fyne
> desktop app (`notificator desktop`). It is **unmaintained and its command has already been
> removed** from this checkout: a plain `go build .` produces a binary with only the `backend`
> command (a `desktop` stub appears only under `-tags nogui`, and it just prints an error).
> What remains is dead cruft — the `internal/notifier` and `internal/audio` packages (both
> import Fyne but are wired to nothing), the `gui` / `notifications` config sections,
> `FyneApp.toml`, the `fyne.io/fyne/v2` dependency in `go.mod`, and the `go-*-desktop` Makefile
> targets. These should be deleted. See [architecture](architecture.md#build-variants).

## Run it locally

```bash
# Server build (excludes the desktop GUI)
go build -tags "nogui,webui" -o notificator .

./notificator backend    # gRPC :50051, HTTP :8080 (health/metrics)
./notificator webui      # http://localhost:8081
```

Log in at `http://localhost:8081` with `admin:admin` (change it). The WebUI defaults to an
Alertmanager at `localhost:9093`; point it at yours via config or env
(see [configuration](configuration.md)).

Docker Compose brings up the whole stack (Postgres + fake Alertmanager + backend + webui):

```bash
docker-compose up -d      # WebUI at http://localhost:8081
```

See [operations](operations.md) for Compose, Helm, and build/codegen details.

## Architecture at a glance

```
Browser ──HTTP/SSE──► WebUI (:8081) ──gRPC──► Backend (:50051) ──► DB (SQLite / Postgres)
                        │                         ▲
                        └──HTTP poll──► Alertmanager(s) :9093
```

Live alerts flow **WebUI ← Alertmanager** (polling, fanned out to browsers via SSE). Everything
persistent (users, comments, acks, resolved-alert history, statistics, preferences) flows
**WebUI → Backend → DB** over gRPC. The two real-time mechanisms are distinct — see
[architecture](architecture.md#real-time).

## Repository map

| Path | What lives there |
|------|------------------|
| `main.go`, `cmd/` | Cobra entrypoints: `backend`, `webui`, `desktop` (stub in server builds) |
| `config/` | Config structs + Viper loading (JSON + env + flags); OAuth config |
| `proto/` | `alert.proto` (AlertService + StatisticsService), `auth.proto` (AuthService) |
| `internal/backend/` | gRPC server, services, GORM database, domain models |
| `internal/webui/` | Gin router, middleware, handlers, gRPC client, `templ` templates, alert cache |
| `internal/alertmanager/` | Multi-Alertmanager HTTP client (multi-tenant headers) |
| `internal/models/` | Core `Alert` domain model + fingerprinting |
| `internal/{notifier,audio}/` | ⚠️ desktop-only (Fyne) — deprecated |
| `charts/notificator-app/` | Helm chart (backend, webui, alertmanager, ingress) |
| `alertmanager/fake/` | Python fake Alertmanager for local dev/testing |
| `docs/` | OAuth setup guides, design/implementation plans, Alertmanager OpenAPI |

## Where to go next

- [Architecture](architecture.md) — components, ports, request flow, real-time, build variants
- [Backend](backend.md) — gRPC services, auth/sessions, database, statistics engine
- [WebUI](webui.md) — routing, templ/HTMX/Alpine, SSE, auth, dashboards
- [Live dashboard](dashboard.md) — the alert table: SSE merge, filters, actions, columns, modal
- [Statistics dashboard](statistics.md) — analytics: time ranges, on-call filtering, charts, saved views
- [Notification system](notifications.md) — browser notifications + sound (and the dead desktop notifier)
- [Domain concepts](domain.md) — alerts, fingerprints, acks/comments, resolved alerts, on-call rules
- [Configuration](configuration.md) — config layering, env vars, multi-tenant, OAuth, Sentry
- [Operations](operations.md) — deploy, build, codegen, database retention, health

## For future agents — where to be careful

- **Never edit `*_templ.go` or `*.pb.go`** — they are generated. Edit the `.templ` / `.proto`
  source and regenerate (`make webui-templates`, `make proto`). Same for `output.css`
  (`make webui-css`). See [operations](operations.md#codegen).
- The backend has **no auth interceptor** — every gRPC handler validates `session_id` by hand.
  A new RPC that forgets the check is wide open. See [backend](backend.md#auth).
- Several "admin only" endpoints are **not actually enforced**; some code paths are dead or
  broken (`profile` timezone update panics). Known-issue list in [backend](backend.md#gotchas)
  and [webui](webui.md#gotchas).
