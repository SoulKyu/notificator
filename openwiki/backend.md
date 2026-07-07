# Backend

The backend (`notificator backend`, `internal/backend/`) is the system's single source of
truth: it owns the database, authentication, alert-collaboration state, and the statistics
engine. Everything is exposed over **gRPC** on `:50051`; a small plain-HTTP server on `:8080`
serves only `/health` and `/metrics`.

## Bootstrap

`Server.Start()` (`internal/backend/server.go:48`) does, in order: init DB → `AutoMigrate` →
`initServices()` → start gRPC → start HTTP → start two background cleanup tickers → block on
graceful shutdown. gRPC registers three services plus reflection (grpcurl-friendly):

| Service | Impl | Proto |
|---------|------|-------|
| `AuthService` | `AuthServiceGorm` | `proto/auth.proto` |
| `AlertService` | `AlertServiceGorm` | `proto/alert.proto` |
| `StatisticsService` | `StatisticsServiceGorm` | `proto/alert.proto` |

`OAuthService` is initialized only when `config.OAuth.Enabled`. Statistics capture is offloaded
to a `StatisticsWorkerPool` (10 workers, queue 1000; `server.go:131`). A single
`grpc.UnaryInterceptor` logs method/duration/status (its `getClientIP` is a stub that always
returns `"unknown"`).

## Authentication & sessions {#auth}

The entire auth model is: **every RPC takes a `session_id` string and calls
`db.GetUserBySession(sessionId)`** (`internal/backend/database/gorm_db.go`, joins
`sessions.expires_at > now()`). Key consequences:

- **No auth interceptor.** Each handler validates the session by hand. A new RPC that forgets
  the check has *no* auth. This is the single most important thing to know before adding an RPC.
- `Login` creates a bcrypt-checked `User` session with a random hex `session_id`, 7-day expiry
  (`internal/backend/services/services.go`). `User` supports both local password and OAuth
  identity (`OAuthProvider`/`OAuthID`, `internal/backend/models/models.go`).
- **No enforced RBAC.** `models/oauth_models.go` defines `UserRole` and OAuth group-sync
  machinery, but nothing gates an RPC by role. `GetConnectedUsers` is commented "admin only"
  yet only checks that *some* valid session exists. Do not trust "admin only" comments.

## Database

GORM over **SQLite or PostgreSQL**, selected by `config.Backend.Database.Type` (or the
`--db-type` flag). `NewGormDB` (`database/gorm_db.go:27`) picks the dialect; dialect-specific
SQL is guarded by `IsSQLite()` / `IsPostgreSQL()`.

`AutoMigrate()` runs `RunCustomMigrations()` **first** (`database/migrate.go`: dedupe
`alert_statistics` before adding a unique index, add `column_configs` to `filter_presets`,
create `user_column_preferences`, backfill `fix_time_seconds`), then GORM `AutoMigrate` over
~20 models: users, sessions, comments, acknowledgments, resolved_alerts, notification prefs,
hidden alerts/rules, filter presets, the full OAuth table set, Sentry config, alert statistics
+ aggregates, statistics views, annotation-button configs. On Postgres it additionally creates
GIN indexes on `alert_statistics.metadata` (best-effort — failure only warns).

**Two independent expiry mechanisms** (don't confuse them when debugging "my data vanished"):

- **Resolved-alert TTL** — `ResolvedAlert.ExpiresAt = now + ttlHours` (default 24h if the caller
  omits it). Reads filter `expires_at > now()`; an hourly job physically deletes expired rows
  (`server.go` `performResolvedAlertCleanup`).
- **Statistics retention** — `alert_statistics` rows older than `config.Statistics.RetentionDays`
  (default 90d) are purged daily (`server.go` statistics cleanup).

## Statistics engine

This is the largest backend subsystem. See [domain concepts](domain.md#statistics) for the
metric definitions (MTTR / MTTA / fix-time / on-call rules).

- **Capture** (`services/statistics_capture.go`): `CaptureAlertFired` upserts an
  `AlertStatistic` keyed idempotently on `(fingerprint, fired_at)`. `UpdateAlertResolved` sets
  `ResolvedAt` and computes **MTTR = resolved − fired** and **FixTime = resolved − acknowledged**.
  `UpdateAlertAcknowledged` computes **MTTA = acknowledged − fired** and now writes it to the
  `MTTASeconds` column (it previously mis-wrote MTTA into `MTTRSeconds` — a data-correctness bug,
  now fixed). Capture also records `silenced_at_fire`, driving the `include_silenced` query filters.
- **Async path** (`services/statistics_worker.go`): jobs run through the worker pool and are
  **silently dropped if the queue (1000) is full** — only a warning is logged, no metric today.
- **Query** (`services/statistics_query.go`): `QueryStatistics` filters by time range, optional
  severity/team (multi-select OR), optional on-call time-of-day window, `include_silenced`, and an
  explicit `timezone`, grouped by `severity | team | alert_name | period | none`. Two additional
  analytics RPCs are **PostgreSQL-only** (they use `EXTRACT(DOW …)` / `array_agg` and error on
  SQLite): **`QueryHeatmap`** (day-of-week × hour counts + avg MTTR, in the user's tz) and
  **`QueryFlappingAlerts`** (fingerprints with `COUNT(*) ≥ min_fires`, default 3, scored by
  fire-rate/gap). `QueryFlappingAlerts` is exposed at `/api/v1/statistics/flapping` but has **no
  current UI** — see [statistics](statistics.md#metrics-and-charts).

### On-call overlap filter (regression-sensitive)

The most subtle code in the backend. The on-call time-of-day filter used to match on `fired_at`
clock time only, which **dropped daytime-fired alerts that ran into the night window**. The fix
treats each alert's **active interval `[fired_at, COALESCE(resolved_at, NOW())]`**
and includes it if that interval *overlaps* any daily occurrence of the on-call window — with
cross-midnight support (e.g. 18:00→08:00) and separate Postgres (`make_interval`) and SQLite
(`julianday`/`strftime`) implementations (`statistics_query.go` `onCallOverlapClause`).
`weekendMode` is `exclude | same_hours | full_weekends`.

**`internal/backend/services/statistics_oncall_test.go` is the reference for the intended
semantics — read it before touching the overlap clause.**

## Real-time collaboration push

`SubscribeToAlertUpdates` is a server-streaming RPC backed by an in-memory
`subscriptions map[alertKey][]*Subscription` guarded by a mutex (`services/services.go`).
Mutating RPCs call `broadcastUpdate` after a successful DB write; a failed `Stream.Send`
auto-unsubscribes that client. Scoped **per alert key** (no global stream) and **single-process
only** (no cross-replica fan-out). See [architecture](architecture.md#real-time).

## Gotchas {#gotchas}

- **No auth interceptor** — forgetting the per-RPC `session_id` check = an unauthenticated RPC.
- **"Admin only" is not enforced** anywhere despite the `UserRole`/group infra existing.
- **Dead code:** `services/comment_service.go` and `services/acknowledgment_service.go` define
  `CommentService`/`AcknowledgmentService` whose constructors are never called — the real logic
  is inline in `AlertServiceGorm`.
- **Silent job drops:** statistics worker pool drops events when full, with no alerting.
- **Single-replica constraint:** in-memory subscriptions break under horizontal scaling.
- **Encryption key fallback:** Sentry token storage falls back to a hardcoded dev key if
  `NOTIFICATOR_ENCRYPTION_KEY` is unset — see [configuration](configuration.md#sentry).
