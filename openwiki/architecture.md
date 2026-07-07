# Architecture

Notificator is a Go monorepo that builds into **one binary with subcommands**. The supported
deployment runs two of those subcommands as separate processes plus a database.

## Components and ports

| Component | Command | Listens | Talks to |
|-----------|---------|---------|----------|
| **Backend** | `notificator backend` | gRPC `:50051`, HTTP `:8080` (`/health`, `/metrics`) | Database (SQLite/Postgres) |
| **WebUI** | `notificator webui` | HTTP `:8081` | Backend (gRPC), Alertmanager(s) (HTTP poll) |
| Alertmanager(s) | external | `:9093` (convention) | — |
| Database | external | Postgres `:5432` or embedded SQLite | — |

The browser only ever talks to the WebUI. The WebUI is a **gRPC client** of the backend; the
backend is the only process with database access. Entrypoints:
`main.go` → `cmd.Execute()` (`cmd/root.go:25`) → `runBackend` (`cmd/backend.go:41`) /
`runWebUI` (`cmd/webui.go:39`).

```
                 ┌────────────────────────┐
 Browser ◄──────►│  WebUI  (:8081)        │
  HTTP + SSE     │  Gin + templ + HTMX    │
                 │  + Alpine.js           │
                 └───┬───────────────┬────┘
              gRPC   │               │ HTTP poll (every ~10s)
                     ▼               ▼
        ┌────────────────────┐   ┌──────────────────┐
        │ Backend (:50051)   │   │ Alertmanager(s)  │
        │ AuthService        │   │  /api/v2/alerts  │
        │ AlertService       │   └──────────────────┘
        │ StatisticsService  │
        └─────────┬──────────┘
                  ▼
        ┌────────────────────┐
        │ SQLite / Postgres  │
        └────────────────────┘
```

## Request flow (a browser hit)

1. Gin middleware chain runs: `CORSMiddleware` → `LoggingMiddleware` → `gin.Recovery` →
   `SessionMiddleware` (`internal/webui/router.go:130-134`).
2. Route-group auth wrappers (`RequireAuth` / `OptionalAuth` / `RedirectIfNotAuth`) call
   `backendClient.ValidateSession(sessionID)` — **a gRPC round-trip on every request**, no
   local session cache (`internal/webui/middleware/auth.go`).
3. The handler either renders a `templ` component to the response writer, or translates the
   HTTP request into a backend gRPC call and marshals the proto reply back to JSON / templ props.
4. Handlers reach their dependencies (backend client, alert cache, services) through
   **package-level singletons** set once at startup via `handlers.Set*` functions
   (`internal/webui/handlers/handlers.go`), not per-request injection.

Details: [webui](webui.md) and [backend](backend.md).

## Real-time — two distinct mechanisms {#real-time}

Notificator has **two independent real-time paths**; do not conflate them.

**1. Live alerts → browser (SSE, polling-backed).** The WebUI owns an in-memory
`AlertCache` (`internal/webui/services/alert_cache.go`) that polls Alertmanager every
~10s (`Polling.SyncInterval`), diffs the result, and fans changes out to browsers over
**Server-Sent Events** (`GET /api/v1/dashboard/stream`, `internal/webui/handlers/sse_handler.go`).
Subscriber channels are buffered and **non-blocking** — a slow browser silently misses
updates rather than stalling the poll loop. The backend is *not* in this path.

**2. Collaboration updates (backend gRPC streaming).** The backend exposes
`SubscribeToAlertUpdates` (server-streaming gRPC, `internal/backend/services/services.go`),
an **in-memory pub/sub keyed per alert key**. Mutating RPCs (add comment/ack, resolve)
broadcast to subscribers after the DB write. Because subscriptions live in process memory,
**this only works with a single backend replica** — there is no Redis/NATS fan-out.

As a side effect of path 1, when the alert cache sees an alert appear/resolve it
fire-and-forgets statistics events to the backend (`CaptureAlertFired`,
`UpdateAlertResolved`) and archives fully-resolved alerts with their comments/acks.

## Build variants and tags {#build-variants}

The single `main.go` compiles into different command sets via Go build tags:

| Build command | Commands present | Used for |
|---------------|------------------|----------|
| `go build .` (no tags) | `backend` only | — (default build is not useful on its own) |
| `go build -tags nogui .` | `backend`, `desktop` (error stub) | `Dockerfile.backend` |
| `go build -tags "nogui,webui" .` | `backend`, `webui`, `desktop` (stub) | `Dockerfile.webui` |

Tag gates: `cmd/backend.go` + `cmd/root.go` have **no tag** (always built);
`cmd/webui.go` requires `//go:build webui`; `cmd/desktop_stub.go` requires `//go:build nogui`
and only registers a `desktop` command that prints "not available" and exits.

> **There is no working desktop GUI in this checkout.** No non-stub `desktop.go` exists in
> `cmd/`. `cmd/root.go:36-38` still defaults dispatch to `"desktop"` and the `Makefile` still
> has `go-*-desktop` targets, but they resolve to the error stub (or "unknown command" in a
> plain build). The Fyne desktop app has effectively been removed; the leftover Fyne-dependent
> packages (`internal/notifier`, `internal/audio`), `FyneApp.toml`, and the `fyne.io/fyne/v2`
> dependency are **dead code slated for cleanup**. Treat backend + webui as the whole product.

CGO is enabled in both Docker images for SQLite support. See [operations](operations.md).
