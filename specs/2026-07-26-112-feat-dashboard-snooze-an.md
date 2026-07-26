# Spec: dashboard snooze — hide an alert for a chosen duration instead of forever

- Issue: [SoulKyu/notificator#112](https://github.com/SoulKyu/notificator/issues/112)
- Date: 2026-07-26
- Status: planned

## Problem

The webui dashboard has two ways to make an alert go away, and neither fits "not
actionable right now":

- **Silence** (`processSilenceAction`,
  `internal/webui/handlers/dashboard_handlers.go:1937`) loops over
  `GetAllClients()` and writes to every configured Alertmanager. Team-wide,
  and it tells the rest of the team the alert is handled when it isn't.
- **Hide** (`UserHiddenAlert`, `internal/backend/models/user_hidden_alerts.go:10`)
  is correctly scoped to the user, but permanent: the model has `CreatedAt`/
  `UpdatedAt` and no expiry, and nothing ever removes a row. Today there isn't
  even a single-click "hide this one alert" entry point on a row or in the
  alert detail modal — `hideAlertInFilter()` only hides within an active
  filter preset, and the personal, filter-independent `hideSelected()`
  (`internal/webui/templates/scripts/dashboard_actions.templ:100`) exists but
  is not wired to any button. The only way to clear a personally-hidden alert
  today is to remember to go to Settings → Hidden.

Result: hiding an alert is both under-exposed and, once used, a standing
blind spot with no reminder.

## Goals

- A personal hide with an expiry ("snooze"): 1h / 4h / 8h / until tomorrow
  09:00 / forever, defaulting to `1h`.
- Add the missing single-alert hide entry points — a row action and a button
  in the alert detail modal — since neither exists today, and wire the
  duration picker into them.
- A snoozed alert behaves exactly like a hidden alert today (gone from the
  dashboard) and comes back on its own when the snooze expires, notifications
  included, without a page reload.
- Settings → Hidden shows remaining time per entry and supports extend /
  wake-now.
- A "N snoozed" counter near the filter bar, clicking it opens the Hidden tab.

## Non-goals

- Silences or anything that writes to Alertmanager — out of scope, this stays
  a per-user view concern.
- Team-wide/shared snoozes, or snoozing an entire `UserHiddenRule` (label-based
  rules stay permanent, no expiry field added there).
- Re-notification or reminder email on expiry — the alert simply re-enters
  the normal alert list and notification path.
- A background sweeper to enforce expiry in real time — reads filter it out
  (see Approach); a periodic purge only bounds table growth.

## Approach

### Proto

`proto/alert.proto`:
- `UserHiddenAlert` (line 367): add `google.protobuf.Timestamp expires_at = 9;`
- `HideAlertRequest` (line 330): add `google.protobuf.Timestamp expires_at = 7;`

Absent/zero `expires_at` means "forever" — existing rows and older clients
keep working unchanged. Regenerate with `make proto`; never hand-edit the
generated `*.pb.go`.

### Model & migration

`internal/backend/models/user_hidden_alerts.go:10`: add
`ExpiresAt *time.Time` with a GORM index (`gorm:"index"`) to `UserHiddenAlert`.
`AutoMigrate` (`internal/backend/database/gorm_db.go:122`) already lists
`&models.UserHiddenAlert{}`, so the nullable column is picked up automatically
— no custom migration in `migrate.go`/`RunCustomMigrations`.

### Backend (gRPC service + GORM)

- `internal/backend/database/gorm_db.go`:
  - `CreateUserHiddenAlert` (line 710) and `SaveHiddenAlert` (line 727): accept
    and persist `expiresAt *time.Time`.
  - `GetUserHiddenAlerts` (line 774): add
    `.Where("expires_at IS NULL OR expires_at > ?", time.Now())` to the query.
    This one filter serves both consumers — the dashboard's hidden-set lookup
    and the Settings → Hidden list — so an expired-but-not-yet-purged row
    naturally stops appearing in either place with no separate code path.
  - Add `PurgeExpiredHiddenAlerts(olderThan time.Duration) (int64, error)`
    deleting rows where `expires_at` is non-null and older than the cutoff
    (e.g. 7 days past expiry, matching the existing pattern in
    `CleanupExpiredResolvedAlerts`).
- `internal/backend/services/services.go`: `HideAlert` (line 1836) passes
  `req.ExpiresAt.AsTime()` (or nil when unset) through to
  `CreateUserHiddenAlert`, and includes `ExpiresAt` in the `UserHiddenAlert`
  response.
- `internal/backend/server.go`: `startResolvedAlertCleanup`/
  `performResolvedAlertCleanup` (line 298) already run hourly via
  `cleanupTicker` — this is "the existing retention path" referenced in the
  issue. Add a call to `PurgeExpiredHiddenAlerts` alongside
  `CleanupExpiredResolvedAlerts` there; no new ticker.

### WebUI service & handlers

- `internal/webui/client/backend_client.go:1030` `HideAlert`: add an
  `expiresAt *time.Time` parameter, set on the proto request.
- `internal/webui/services/hidden_alerts_service.go`:
  - `userHiddenAlerts` is currently `map[string]map[string]bool` (fingerprint
    → hidden), populated in `LoadUserData` (line 140) and mutated in
    `HideAlert`/`UnhideAlert` (lines 325, 348). Change the value type to carry
    expiry, e.g. `map[string]map[string]time.Time` (zero `time.Time` = never
    expires), and evaluate expiry at read time in `IsAlertHidden` (line 194)
    rather than trusting the cached boolean. This avoids needing the cache TTL
    (`hiddenAlertsCacheTTL`, line 24) to track the nearest expiry — the entry
    itself carries its own answer, and the 30s TTL still bounds how quickly a
    genuinely new/removed hide propagates.
  - `HideAlert` (line 326) takes and stores the chosen `expiresAt`.
  - `GetUserHiddenAlerts` (line 369) already round-trips through the backend
    query (which now filters expiry) — just carry `ExpiresAt` into
    `models.UserHiddenAlert`.
- `internal/webui/handlers/hidden_alerts_handlers.go` `HideAlert` (line 36):
  accept a `duration` field on the request struct, an enum of exactly
  `1h` / `4h` / `8h` / `tomorrow_9am` / `forever`, and translate it into a
  concrete `*time.Time` server-side (server clock is the source of truth, not
  the client's). A missing `duration` field defaults to `1h` (matching the
  UI picker's pre-selected option, see Goals). An unrecognized value is
  rejected with a 400 validation error — it must never silently fall back to
  `forever`, since that would turn a bounded snooze into an accidental
  permanent hide.

### Client (templ + Alpine)

- Row hide action: `internal/webui/templates/components/table_components.templ`
  (near the existing "Hide in Filter" button, line 119) — add a personal
  "Hide" button (this is new; only the filter-scoped variant exists today)
  that opens a small duration picker rather than hiding immediately.
- Alert detail modal: `internal/webui/templates/components/modal_components.templ`
  (action button row starting line 1016, alongside Silence/Acknowledge) — add
  the same personal hide action with the duration picker.
- Reuse the existing modal-flag pattern (`showAckModal`, `showSilenceModal`,
  …) for a `showHideModal` + duration select, following
  `internal/webui/templates/scripts/dashboard_actions.templ`'s
  `hideSelected()` (line 100) as the base for the fetch call, extended to
  send the chosen duration.
- Settings → Hidden list (`modal_components.templ:362`, `hiddenAlerts` array
  from `dashboard_settings.templ`'s `loadHiddenAlerts()`): render "expires in
  Xh" / "never" per entry from `expiresAt`, and add extend / wake-now actions
  (wake-now = call `unhideSpecificAlert`, already at
  `dashboard_settings.templ:578`; extend = a small `HideAlert` call reusing
  the fingerprint with a new duration).
- Snoozed counter: near the "Filters & Search" section
  (`internal/webui/templates/pages/NewDashboard.templ:357`), a small badge
  reading `window.currentSettingsModal.hiddenAlerts` (already loaded eagerly
  on page init via `settingsModalData().init()`,
  `dashboard_settings.templ:79`) filtered to non-expired entries; clicking it
  sets `showSettings = true; activeTab = 'hidden'`.
- **Client filter mirror**: `alertMatchesFilters`-adjacent hidden check in
  `internal/webui/templates/scripts/dashboard_data.templ` (the
  `isGlobalHidden`/`isHidden` block starting line 559, used to decide whether
  an SSE-pushed alert is shown) currently does
  `hiddenAlerts.some(hidden => hidden.fingerprint === alert.fingerprint)`.
  This must become expiry-aware (`hidden.fingerprint === ... && (!hidden.expiresAt
  || new Date(hidden.expiresAt) > new Date())`), or an expired snooze will
  keep hiding SSE-delivered alerts while the server-side path (which already
  filters expiry in the DB query) shows them — the two paths would disagree.
  Since expiry is wall-clock-driven with no push event, also add a light
  periodic re-evaluation (e.g. a 60s interval calling `applyFilters()` again)
  so a snooze that expires while the tab is open surfaces the alert without
  waiting for the next SSE message or manual refresh.
- Templ workflow: edit `.templ` files, run `make webui-templates`; never hand
  -edit `*_templ.go`.

### Files touched

- `proto/alert.proto` (+ regenerated `*.pb.go` via `make proto`)
- `internal/backend/models/user_hidden_alerts.go`
- `internal/backend/database/gorm_db.go`
- `internal/backend/services/services.go`
- `internal/backend/server.go`
- `internal/webui/client/backend_client.go`
- `internal/webui/services/hidden_alerts_service.go`
- `internal/webui/handlers/hidden_alerts_handlers.go`
- `internal/webui/templates/components/table_components.templ`
- `internal/webui/templates/components/modal_components.templ`
- `internal/webui/templates/scripts/dashboard_actions.templ`
- `internal/webui/templates/scripts/dashboard_settings.templ`
- `internal/webui/templates/scripts/dashboard_data.templ`
- `internal/webui/templates/pages/NewDashboard.templ`
- Generated `*_templ.go` files via `make webui-templates` (not hand-edited)

## Risks & trade-offs

- **Cache shape change**: `userHiddenAlerts` moves from
  `map[string]bool` to a type that carries expiry. Every read site
  (`IsAlertHidden` and any other direct map access in
  `hidden_alerts_service.go`) must be updated together, or a stale bool check
  will silently ignore expiry on one path.
- **Clock trust**: duration keywords (`1h`, `tomorrow 09:00`, …) must resolve
  to an absolute timestamp on the server, not the browser, so a wrong client
  clock cannot under/over-snooze. This branch's base (`main`) does not have
  the tz-aware handling that `feat/production-readiness` carries, so
  `tomorrow_9am` resolves against the server process's UTC clock (Go's
  `time.Now().UTC()`), not the container's local time or a per-user
  timezone — call this out as a known limitation until the production-
  readiness tz work lands on `main`.
- **Two independent expiry checks** (server DB query + client SSE mirror)
  must agree. Tested by the acceptance criterion below; drifting them apart
  is the most likely regression path for this feature.
- **No real-time push on expiry**: relying on the 60s client poll / next SSE
  message / next full reload means expiry is eventually-consistent within
  about a minute, not instant. Acceptable per the issue (no reminder
  requirement), but worth calling out — do not oversell it as instant.
- **Purge cadence**: hooking into the hourly `performResolvedAlertCleanup` job
  means expired rows can persist up to ~1h + retention window before
  deletion; this is fine since the read-side filter already hides them, the
  purge only bounds table size.

## Validation

- `make proto && make webui-templates && go build ./...` passes.
- Backend test (new, e.g. in `gorm_db_test.go`) covering the `GetUserHiddenAlerts`
  expiry boundary: a row with `expires_at` in the past is excluded, a null
  `expires_at` is included, a future `expires_at` is included.
- Handler test: missing `duration` defaults to `1h`; an unrecognized value
  (e.g. `"bogus"`) returns a 400 and does not create a "forever" hide.
- `tomorrow_9am` resolves against the server's UTC clock on this branch
  (known limitation, see Risks) — not a per-user or container-local
  timezone; this is testable/expected until the production-readiness tz
  work lands on `main`.
- Manual check via `make test` (docker-compose stack):
  - Hide an alert with a 1h snooze from a row action → alert disappears from
    the dashboard immediately.
  - Same from the alert detail modal.
  - Manually back-date the row's `expires_at` in the DB (or snooze for a very
    short duration) and confirm the alert reappears on the next data load and
    via SSE, without a page reload, and re-notifies as a normal new alert
    (not suppressed as a re-fire of the old incident).
  - Hide with "forever" → behaves exactly as before this change.
  - Settings → Hidden shows "expires in Xh" / "never" per entry; extend and
    wake-now both work.
  - The "N snoozed" counter matches the non-expired count and drops when an
    entry expires while the tab is open; clicking it opens Settings → Hidden.
  - Confirm the client SSE mirror and the server-side dashboard load agree:
    an alert whose snooze just expired is not hidden by one path and shown by
    the other.
