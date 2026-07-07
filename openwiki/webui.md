# WebUI

The WebUI (`notificator webui`, `internal/webui/`) is a server-rendered web app:
**Gin** for HTTP, the **`templ`** templating library for HTML, **HTMX** + **Alpine.js** for
interactivity, **Tailwind** for styling. No JS build pipeline beyond Tailwind — client-side
logic is hand-written JS embedded inside `.templ` files. The WebUI is a **gRPC client** of the
backend and an **HTTP client** of Alertmanager.

## Startup and wiring

`SetupRouter(backendAddress)` (`internal/webui/router.go:21`) loads config, builds the
Alertmanager `MultiClient`, dials the backend gRPC client (**mandatory** — `log.Fatalf` if the
dial target is malformed), fetches OAuth config from the backend, and wires everything into
handlers via **package-level singleton setters** (`handlers.SetBackendClient`,
`SetAlertCache`, `SetColorService`, …). This global-singleton pattern (not per-request DI) is
fine for a single-instance server but blocks parallel/multi-tenant handler testing.

Middleware order (`router.go:130-134`): `CORSMiddleware` → `LoggingMiddleware` →
`gin.Recovery` → `SessionMiddleware`.

## Sessions, auth, OAuth, impersonation {#auth}

- **Session store:** `gin-contrib/sessions` cookie store, secret from
  `NOTIFICATOR_SESSION_SECRET` or a **per-process random fallback** — the fallback means
  sessions don't survive a restart (logged as a warning). Cookie `notificator-session`,
  7-day, `HttpOnly: true`, **`Secure: false` hardcoded** (`middleware/session.go`) — must be
  fixed for HTTPS-only production.
- The cookie holds only an opaque `session_id` (+ cached user fields); validity is always
  re-checked against the backend via `ValidateSession` **on every request** — no local TTL
  cache, so backend load scales 1:1 with UI traffic.
- **Classic login/register** (`handlers/handlers.go`) POST form creds to the backend; can be
  disabled via OAuth `DisableClassicAuth`.
- **OAuth** (`handlers/oauth_handlers.go`): CSRF `state` stored in session, provider auth URL
  fetched from backend, `OAuthCallback` validates `state` before the backend exchanges the code.
  See [configuration](configuration.md#oauth).
- **Impersonation** (`handlers/impersonation_handlers.go`, `middleware/session.go`): admin-listed
  users (`config.Admin.ImpersonationAllowedUsers`) can "view as" another user. Implemented as
  extra session keys; `GetEffectiveUserID` resolves *whose data* to load, while
  `GetEffectiveSessionID` deliberately keeps the **admin's** session for backend auth — backend
  RPCs then carry an explicit `impersonate_user_id` to act on the target user's data server-side.

## The backend gRPC client

`internal/webui/client/backend_client.go` wraps every backend RPC the UI needs (auth, comments,
acks, resolved alerts, statistics, color/column/hidden/filter preferences, OAuth, Sentry config).
Handlers call these methods and re-marshal proto replies into `internal/webui/models` structs.

## Alert cache + SSE (live alerts) {#alert-cache}

`internal/webui/services/alert_cache.go` (~900 lines) is the source of truth for **live firing
alerts** and the SSE hub:

- Polls Alertmanager every `Polling.SyncInterval` (default ~10s) via the `MultiClient`, diffs
  the in-memory `map[fingerprint]*DashboardAlert`, and normalizes upstream quirks
  (`suppressed`→`silenced`, `information`→`info`).
- Tracks `UpdatedAt` only on *meaningful* change (`hasAlertChanged`) to avoid UI churn — this is
  what `alert_cache_test.go` primarily verifies.
- Maintains a **per-user color cache** (rule-based alert coloring) refreshed after each poll.
- On resolve, asynchronously archives the alert (with comments/acks) to the backend and fires
  `CaptureAlertFired`/`UpdateAlertResolved` statistics events.
- **SSE fan-out:** `Subscribe`/`Unsubscribe`/`notifySubscribers` push buffered (10),
  **non-blocking** updates. `handlers/sse_handler.go` (`GET /api/v1/dashboard/stream`) sets
  `text/event-stream` (+ `X-Accel-Buffering: no` for nginx), streams `update` events and 30s
  `ping` heartbeats until the client disconnects.

This is separate from the backend's gRPC collaboration stream — see
[architecture](architecture.md#real-time).

## Handlers (by feature)

`internal/webui/handlers/`: `dashboard_handlers` (live dashboard + bulk actions),
`statistics_handlers` + `statistics_view_handlers` (analytics + saved views),
`oauth_handlers`, `profile_handlers`, `notification_handlers`, `hidden_alerts_handlers`,
`filter_preset_handlers`, `impersonation_handlers`, `sentry_handlers`, `sse_handler`,
`connected_users_handlers`.

## Templates and the two dashboards

`internal/webui/templates/` structure:

- `layouts/Base.templ` — shared HTML shell; loads `output.css`, HTMX, Alpine (+ collapse),
  Day.js from CDN; contains inline dark-mode, impersonation-banner, and connected-users JS.
- `pages/` — `NewDashboard` (the **live operational dashboard**, `/dashboard` — deep-dived in
  [dashboard](dashboard.md)), `StatisticsDashboard` (the **historical analytics dashboard**,
  `/statistics` — deep-dived in [statistics](statistics.md)), `Login`, `Register`, `Profile`,
  `OAuthCallback`, `Index`, `Playground` (dev/demo landing when `WebUI.Playground` is on).
  Browser/sound alerts are covered in [notifications](notifications.md).
- `components/` — tables, modals, filter/group views, timezone selector, page navigator, etc.
- `scripts/` — **`.templ` files whose entire body is a `<script>` of hand-written Alpine.js**
  (e.g. `dashboard_core.templ` defines `function newDashboard(){…}`). Included into pages via
  `@scripts.DashboardCore()`. Editing client behavior means editing these `.templ` files, not
  files under `static/`.

### templ workflow {#templ}

Each `.templ` compiles to a sibling `*_templ.go`. **Never edit the generated `*_templ.go`.**
Edit the `.templ`, then run `make webui-templates` (`templ generate`). Tailwind output is
`static/css/output.css`, regenerated with `make webui-css` — never hand-edit it either. See
[operations](operations.md#codegen).

## Gotchas {#gotchas}

- **`profile_handlers.go` `UpdateTimezone`** calls `c.MustGet("db").(*gorm.DB)`, but no
  middleware ever sets `"db"` on the gin context — the route (`PUT /profile/timezone`) will
  **panic** (surfaced as a 500). Leftover direct-DB code in an otherwise all-gRPC UI.
- **`middleware/session.go` `Secure: false`** is hardcoded — patch for HTTPS.
- **`middleware/cors.go`** sets `AllowOrigins: ["*"]` with `AllowCredentials: true`, which the
  Fetch spec disallows together — verify real browser behavior before relying on cross-origin
  cookies.
- **No local session cache** — every request hits the backend `ValidateSession`; add a
  short-TTL cache here if it becomes a bottleneck.
- **Package-level handler globals** block parallel/multi-instance handler wiring without a refactor.
