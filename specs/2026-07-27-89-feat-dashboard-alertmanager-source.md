# Spec: Alertmanager source freshness — live/stale/unreachable indicator

- Issue: [SoulKyu/notificator#89](https://github.com/SoulKyu/notificator/issues/89)
- Date: 2026-07-27
- Status: planned

## Problem

`AlertCache.refreshAlerts` (`internal/webui/services/alert_cache.go:206`) polls every
configured Alertmanager via `FetchAllAlertsDetailed`
(`internal/alertmanager/client.go:749`) and, when a source fails, deliberately keeps its
last known alerts and only logs it (`alert_cache.go:212`, and the mass-resolve guard at
`alert_cache.go:274`, the fix for #45). That's the right behavior, but it's invisible in
the browser:

- A source down for 20 minutes still shows 20-minute-old alerts as if live — nothing
  marks them stale.
- A source unreachable since startup contributes zero alerts. An empty dashboard looks
  exactly like "all quiet".
- The header's "Last updated" (`dashboard_core.templ:410`, driven by
  `metadata.lastUpdate`) is cache-wide and keeps ticking as long as *one* Alertmanager
  answers.
- `GetHealthStatus()` (`client.go:896`) exists but only backs the unused
  `GET /health/alertmanager` probe endpoint (`handlers.go:356`, `AlertmanagerHealthCheck`)
  — it does a fresh synchronous `TestConnection()` per instance, no UI consumes it, and
  it says nothing about poll freshness.

No part of the WebUI can currently answer "am I seeing everything right now?"

## Goals

- Header **Sources** pill: `N/N live` (green), degraded amber, all-down red. Click opens
  a popover: per-source name, state (`live`/`stale`/`unreachable`), last successful poll
  as relative time, current alert count, last error.
- Dismissible banner above the alerts table when a source has missed several
  consecutive polls, naming the source and its alert count. Reappears if the condition
  persists across a reload.
- Stale marker on rows in the existing **Alertmanager** column (`col_source`,
  `dashboard_utilities.templ:662`).
- All of the above arrives with the initial page load and updates live over the
  existing SSE channel — no new endpoint, no browser-side polling.

## Non-goals

- No active probing. Status is derived only from the polls `refreshAlerts` already
  performs — no extra `TestConnection()` calls, `GET /health/alertmanager` unchanged.
- No alerting/paging on a dead source (no browser notification, no email).
- No per-user staleness threshold configuration.
- No change to the mass-resolve protection (#45) — keep-stale-alerts stays exactly as
  is; this only surfaces it.
- No backend/gRPC connectivity status.

## Approach

### 1. Track per-source status in the cache

`alert_cache.go`: add next to `ac.alerts` (same struct, guarded by the same `ac.mu`):

```go
type SourceStatus struct {
    Name                 string    `json:"name"`
    LastSuccessAt        time.Time `json:"lastSuccessAt"`
    LastError            string    `json:"lastError,omitempty"`
    ConsecutiveFailures  int       `json:"consecutiveFailures"`
    AlertCount           int       `json:"alertCount"`
}
```

`AlertCache` gets `sourceStatuses map[string]*SourceStatus`, initialized in
`NewAlertCache` (`alert_cache.go:102`) from `alertmanagerClient.GetClientNames()`
(`client.go:864`) so every configured source starts `unreachable` (zero-value
`LastSuccessAt`), not `live`, before the first poll completes.

In `refreshAlerts` (`alert_cache.go:206`), right after `FetchAllAlertsDetailed` returns
`(alertsWithSource, fetchErrors)` and under the existing `ac.mu.Lock()` section that
already builds `currentFingerprints`:

- For each configured source: if present in `fetchErrors`, increment
  `ConsecutiveFailures` and set `LastError`; otherwise reset `ConsecutiveFailures = 0`,
  clear `LastError`, stamp `LastSuccessAt = time.Now()`.
- `AlertCount` per source: derived from `ac.alerts` grouped by `.Source` after the
  fingerprint loop — no separate accounting needed, it's the same data
  `buildDashboardMetadata` already walks.

Derive state on read, not stored:
- `unreachable`: `LastSuccessAt.IsZero()`.
- `stale`: `ConsecutiveFailures >= 3` (three consecutive polls — same shape as the
  mass-resolve guard's reasoning, rides on `ac.refreshInterval`
  (`alert_cache.go:84`/`170`), no new config key).
- `live`: otherwise.

Add `func (ac *AlertCache) GetSourceStatuses() map[string]SourceStatusView` (RLock,
snapshot copy, same pattern as `GetAllAlerts` at `alert_cache.go:694`) returning the
derived state alongside the raw fields.

### 2. Surface it in the initial payload

`internal/webui/models/dashboard.go`: add `Sources map[string]SourceStatusView
\`json:"sources,omitempty"\`` to both `DashboardResponse` (line 141) and
`DashboardIncrementalUpdate` (line 130), next to the existing `Colors` field — same
"embedded so first render is correct" rationale already used for colors.

`dashboard_handlers.go`: `GetDashboardData` (line 114) sets
`response.Sources = alertCache.GetSourceStatuses()` next to the existing
`response.Colors = ...` line (~206).

### 3. Push it over SSE, including status-only changes

This is the part that's easy to get wrong: `refreshAlerts` only calls
`ac.notifySubscribers(update)` when `len(newAlertsForSSE) > 0 ||
len(updatedAlertsForSSE) > 0 || len(removedFingerprints) > 0`
(`alert_cache.go:319`). A source going from `live` to `stale` produces **no** alert
diff — its cached alerts don't change, only `ConsecutiveFailures` does. Left as-is, the
pill would never update live when a source silently degrades; it would only refresh on
the next full-alert-changing poll or page reload, defeating the point of the feature.

Fix: compute the source-status snapshot before the notify-condition check, and extend
the condition to also fire when any source's derived state changed since the previous
snapshot (compare against the pre-refresh copy). Attach `Sources:
ac.GetSourceStatuses()` to the `update` struct unconditionally whenever `notifySubscribers`
does fire, so the client always has the latest view alongside whatever alert diff
triggered it.

`sse_handler.go` needs no change — it forwards whatever `DashboardIncrementalUpdate` it
receives; `Sources` rides along automatically once the model field exists.

### 4. Client: pill, popover, banner

New `internal/webui/templates/scripts/dashboard_sources.templ`, following the existing
mixin convention (`dashboard_core.templ:309-314`, `window.dashboardXMixin` +
`Object.assign`):

```js
window.dashboardSourcesMixin = {
    sources: {},
    dismissedStaleBanner: false,
    sourceSummary() { /* {live, total} from Object.values(this.sources) */ },
    sourcePillClass() { /* green / amber / red from sourceSummary() */ },
    relativeTime(iso) { /* reuse existing relative-time helper if one exists in dashboard_utilities.templ; else small helper here */ },
};
```

Wire into `dashboard_core.templ`'s `Object.assign` list and into `dashboard_data.templ`:
- initial load (`dashboard_core.templ` init path, ~line 66): `this.sources =
  result.data.sources || {}`.
- SSE merge (`dashboard_data.templ:419`, alongside `if (update.metadata)`): `if
  (update.sources) this.sources = update.sources`.

Markup: a `components/source_status.templ` component rendered from
`NewDashboard.templ`'s header, next to the existing "Secondary Stats Dropdown"
(`NewDashboard.templ:88-99` — same `x-data="{ open: false }"` + `@click.away` popover
pattern, reused rather than invented). Banner is a conditionally-rendered block above
the alerts table, dismissal state kept in `dismissedStaleBanner` (component-local, not
persisted — reappears on reload if the condition still holds, per acceptance criteria).

`renderText`'s `field_path === 'source'` branch in `dashboard_utilities.templ:705`
(`renderText`) gets a stale-dot suffix when `this.sources[value]?.state !== 'live'` —
no new column, no column-preference migration.

Regenerate with `make webui-templates` after editing `.templ` files; `make webui-css`
if new Tailwind classes are introduced. Never hand-edit `*_templ.go`.

### Files touched

- `internal/webui/services/alert_cache.go` — `SourceStatus` tracking, `GetSourceStatuses()`,
  notify-on-status-change.
- `internal/webui/services/alert_cache_test.go` — new coverage (see Validation).
- `internal/webui/models/dashboard.go` — `Sources` field on `DashboardResponse` and
  `DashboardIncrementalUpdate`.
- `internal/webui/handlers/dashboard_handlers.go` — populate `response.Sources`.
- `internal/webui/templates/scripts/dashboard_sources.templ` — new mixin.
- `internal/webui/templates/scripts/dashboard_core.templ` — register mixin, initial load wiring.
- `internal/webui/templates/scripts/dashboard_data.templ` — SSE merge wiring.
- `internal/webui/templates/scripts/dashboard_utilities.templ` — stale marker in `renderText`.
- `internal/webui/templates/components/source_status.templ` — new pill/popover component.
- `internal/webui/templates/pages/NewDashboard.templ` — mount the component in the header.
- Generated `*_templ.go` via `make webui-templates` (not hand-edited).

## Risks & trade-offs

- **Silent-status-change blind spot**: without the fix in step 3, the SSE path never
  reflects a source going stale until an unrelated alert diff happens to piggyback the
  same refresh cycle. Flagged explicitly above because it's the one part of this
  feature that's easy to ship broken while every acceptance criterion *looks* satisfied
  in a demo where alerts are also changing.
- **Threshold choice (`ConsecutiveFailures >= 3`)**: ties staleness to poll count, not
  wall-clock time, so it scales automatically with `refreshInterval` without a new
  config key — but a source flapping on every other poll never crosses the threshold.
  Acceptable per the issue's explicit non-goal (no per-user threshold config); can be
  revisited if flapping sources turn out to be common.
- **`sourceStatuses` map keyed by source name**: relies on Alertmanager source names
  being stable identifiers (already true — it's the same key `DashboardAlert.Source`
  and `fetchErrors` use). Renaming a source in config resets its history, which is
  correct (it's effectively a different source).
- **Popover pattern reuse**: copying the existing `x-data="{ open: false }"` +
  `@click.away` popover (already used for "More" stats) avoids a new interaction
  pattern, but means the Sources pill and the More dropdown are independent
  `x-data` scopes — acceptable, they don't need to coordinate.

## Validation

- `GetSourceStatuses()` unit test in `alert_cache_test.go`: success → failure →
  recovery transition, asserting `live` → `stale` after 3 consecutive `fetchErrors`
  entries → back to `live` with `ConsecutiveFailures` reset on the next success; and a
  source absent from any successful poll since cache creation reads `unreachable`.
- `make webui-templates && go build ./...` passes.
- Manual check via `make test` (docker-compose stack, multiple Alertmanager sources
  configured):
  - All sources up: header shows `N/N live`; popover rows show a last-successful-poll
    under one sync interval.
  - Stop one Alertmanager container: within 3 sync intervals the pill turns amber,
    that source's popover row shows `stale` with its last error, the banner names the
    source and its alert count, and its rows in the table carry the stale marker in
    the Alertmanager column — without a page reload (SSE path).
  - Restart the stopped Alertmanager: state clears on the next successful poll, no
    reload needed.
  - A source that never answers from a fresh `make test` startup shows `unreachable`,
    not `live`, contributing zero alerts.
  - Reload the page while the banner condition still holds: banner reappears (not
    permanently dismissed).
  - Initial `GET /api/v1/dashboard/data` response already contains the correct
    `sources` block — no flash of "all green" before the first SSE update.
