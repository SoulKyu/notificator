# Spec: Alertmanager source freshness — live/stale/unreachable indicator

- Issue: [SoulKyu/notificator#89](https://github.com/SoulKyu/notificator/issues/89)
- Date: 2026-07-27
- Status: planned

## Problem

`AlertCache.refreshAlerts` (`internal/webui/services/alert_cache.go:206`) polls every
configured Alertmanager via `FetchAllAlertsDetailed`
(`internal/alertmanager/client.go:749`) and, when a source fails, deliberately keeps its
last known alerts and only logs it (`alert_cache.go:213`, and the mass-resolve guard at
`alert_cache.go:281`, the fix for #45). That's the right behavior, but it's invisible in
the browser:

- A source down for 20 minutes still shows 20-minute-old alerts as if live — nothing
  marks them stale.
- A source unreachable since startup contributes zero alerts. An empty dashboard looks
  exactly like "all quiet".
- The header's "Last updated" (driven by `metadata.lastUpdate`) is cache-wide and keeps
  ticking as long as *one* Alertmanager answers.
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
    Name                string    `json:"name"`
    LastSuccessAt       time.Time `json:"lastSuccessAt"`
    LastError           string    `json:"lastError,omitempty"`
    ConsecutiveFailures int       `json:"consecutiveFailures"`
    AlertCount          int       `json:"alertCount"`
}

// SourceStatusView is what leaves the cache: SourceStatus plus the derived
// State the client and the JSON payload actually consume. This is the type
// GetSourceStatuses() returns and the type the `sources` field on
// DashboardResponse / DashboardIncrementalUpdate (§2) is keyed by.
type SourceStatusView struct {
    Name                string    `json:"name"`
    State               string    `json:"state"` // "live" | "stale" | "unreachable"
    LastSuccessAt       time.Time `json:"lastSuccessAt"`
    LastError           string    `json:"lastError,omitempty"`
    ConsecutiveFailures int       `json:"consecutiveFailures"`
    AlertCount          int       `json:"alertCount"`
}
```

`AlertCache` gets `sourceStatuses map[string]*SourceStatus`, initialized in
`NewAlertCache` (`alert_cache.go:102`).

**Nil-client guard (fixes the panic in 6 existing tests):** `NewAlertCache` already
has an established pattern for this exact problem — it never stores a typed-nil
`*MultiClient` in the `alertFetcher` interface field because that would defeat a
plain `!= nil` check (`alert_cache.go:115-120`). `sourceStatuses` init must follow
the same guard, one level earlier: only call `amClient.GetClientNames()`
(`client.go:868`) when `amClient != nil`. `GetClientNames()` takes
`mc.mutex.RLock()`, so calling it on a nil `*MultiClient` panics regardless of the
interface trick — the guard has to happen before the call, not after.

```go
sourceStatuses := make(map[string]*SourceStatus)
if amClient != nil {
    for _, name := range amClient.GetClientNames() {
        sourceStatuses[name] = &SourceStatus{Name: name} // zero-value LastSuccessAt -> unreachable
    }
}
```

With a nil client, `sourceStatuses` stays an empty map — no configured sources,
nothing to report, and `refreshAlerts`'s existing `if ac.alertmanagerClient == nil { return }`
guard (`:207-209`) already keeps it that way for the life of the cache. This is
exactly the shape `NewAlertCache(nil, nil, 0, 0)` is called with in
`dashboard_handlers_test.go:70,81,92,116,145,169` — those tests must keep passing
unmodified.

When `amClient != nil`, every configured source starts `unreachable` (zero-value
`LastSuccessAt`), not `live`, before the first poll completes.

Derive `State` on read, not stored:
- `unreachable`: `LastSuccessAt.IsZero()`.
- `stale`: `ConsecutiveFailures >= 3` (three consecutive polls — same shape as the
  mass-resolve guard's reasoning, rides on `ac.refreshInterval`
  (`alert_cache.go:84` field, `:170` setter), no new config key).
- `live`: otherwise.

Add `func (ac *AlertCache) GetSourceStatuses() map[string]SourceStatusView` (RLock,
snapshot copy, same pattern as `GetAllAlerts` at `alert_cache.go:691`): for each
entry in `ac.sourceStatuses`, compute `State` per the rules above and `AlertCount`
by walking `ac.alerts` and counting by `.Source` (no separate accounting needed,
it's the same data `buildDashboardMetadata` already walks), and copy the raw
fields into a `SourceStatusView`.

The bookkeeping that populates `ConsecutiveFailures` / `LastError` /
`LastSuccessAt` on every poll, and its placement relative to the early return at
`alert_cache.go:215-218`, is specified in §3 below — that placement is the core of
this feature and gets its own section rather than being folded in here.

### 2. Surface it in the initial payload

`internal/webui/models/dashboard.go`: add `Sources map[string]SourceStatusView
\`json:"sources,omitempty"\`` to both `DashboardResponse` (line 143) and
`DashboardIncrementalUpdate` (line 132), next to the existing `Colors` field — same
"embedded so first render is correct" rationale already used for colors.

`dashboard_handlers.go`: `GetDashboardData` (line 121) sets
`response.Sources = alertCache.GetSourceStatuses()` next to the existing
`response.Colors = ...` line (line 218).

**Pre-existing dead field, don't confuse it with `Sources`:** `DashboardMetadata`
(`models/dashboard.go:167`) already has an `AlertmanagerStatus map[string]bool`
field. It's always set to an empty map (`dashboard_handlers.go:828`, comment:
"would be populated based on health check") — nothing populates it today. It stays
untouched by this spec; `Sources` is a new, separate field on `DashboardResponse`
itself, not a replacement for it. A future cleanup can remove
`AlertmanagerStatus` once `Sources` covers its intended purpose, but that's out of
scope here.

### 3. Restructure `refreshAlerts` so status changes reach SSE, including through the early return

This is the headline problem the previous attempt got wrong: `alert_cache.go:215-218`

```go
if len(alertsWithSource) == 0 && len(fetchErrors) > 0 {
    // No source returned anything usable; leave the cache as-is.
    return
}
```

is a **function-level `return`**. Nothing between `:220` and the end of the function
runs on that path — not the alert-diff bookkeeping, not `notifySubscribers` at `:323`.
No amount of doing status bookkeeping earlier in the function makes `:323` execute
after a `return` at `:218`; the two statements are mutually exclusive by construction.
The fix has to put a notify call *on the early-return path itself*, not just move
bookkeeping before it.

**Boundary case to design for, not special-case:** the condition
`len(alertsWithSource) == 0 && len(fetchErrors) > 0` is not "all sources down". One
source that's up but legitimately returns zero alerts, plus one source that fails,
also trips it — `alertsWithSource` is empty and `fetchErrors` is non-empty either way.
The fix below must treat this identically to the true all-down case: "status may have
changed, check and notify" — not assume every early return means every source failed.

**New flow for `refreshAlerts` (`alert_cache.go:206-337`), in order:**

1. `:207-209` nil-client guard — unchanged.
2. `:211` `alertsWithSource, fetchErrors := ac.alertmanagerClient.FetchAllAlertsDetailed()` — unchanged.
3. `:212-214` fetch-error logging loop — unchanged.
4. **NEW, before anything touches `sourceStatuses`:** take the pre-refresh snapshot —
   `statusesBefore := ac.snapshotSourceStates()`. This is a small RLock'd helper that
   reads `ac.sourceStatuses` and returns `map[string]string` of the *currently derived*
   state (`live`/`stale`/`unreachable`) per source, computed from the untouched
   `SourceStatus` values. Taking this before step 5 is what fixes the snapshot-timing
   defect: a snapshot taken after the bookkeeping mutation would just be comparing the
   new state against itself, a permanent no-op.
5. **NEW, still before the early return, replacing the loop described in §1's old
   draft:** `ac.updateSourceStatuses(fetchErrors)` — `ac.mu.Lock()`, for every source
   name already in `ac.sourceStatuses`: if present in `fetchErrors`, increment
   `ConsecutiveFailures` and set `LastError`; otherwise reset `ConsecutiveFailures = 0`,
   clear `LastError`, stamp `LastSuccessAt = time.Now()`. `ac.mu.Unlock()`.
6. **NEW helper used by both branches below, so they can't drift from each other:**
   `func (ac *AlertCache) sourceStatesChanged(before map[string]string) bool` — RLock,
   recompute current derived state per source, return `true` if it differs from
   `before` for any source (added/removed sources count as changed).
7. **`:215-218` early return, REPLACED:**
   ```go
   if len(alertsWithSource) == 0 && len(fetchErrors) > 0 {
       // No source returned anything usable; leave the cached alerts as-is, but a
       // source's live/stale/unreachable state may have changed even when its
       // alerts didn't — notify before bailing out, or the pill never updates.
       if ac.sourceStatesChanged(statusesBefore) {
           ac.notifySubscribers(&webuimodels.DashboardIncrementalUpdate{
               Sources:        ac.GetSourceStatuses(),
               LastUpdateTime: time.Now().Unix(),
           })
       }
       return
   }
   ```
   This branch is reached for the true all-sources-down case *and* the
   healthy-empty-source-plus-failing-source boundary case above, and handles both the
   same way: check whether derived state actually changed, notify only if it did. No
   "are all sources down" branching is needed because `sourceStatesChanged` already
   answers the only question that matters.
8. `:220-321` unchanged (alert-diff bookkeeping under `ac.mu.Lock()`).
9. `:323` existing notify-condition check, EXTENDED (not replaced) with the same
   helper from step 6, reusing `statusesBefore` from step 4 — not a fresh snapshot,
   which is what the previous attempt's self-contradictory instruction got wrong:
   ```go
   if len(newAlertsForSSE) > 0 || len(updatedAlertsForSSE) > 0 ||
       len(removedFingerprints) > 0 || ac.sourceStatesChanged(statusesBefore) {
       update := &webuimodels.DashboardIncrementalUpdate{
           NewAlerts:      newAlertsForSSE,
           UpdatedAlerts:  updatedAlertsForSSE,
           RemovedAlerts:  removedFingerprints,
           Sources:        ac.GetSourceStatuses(),
           LastUpdateTime: time.Now().Unix(),
       }
       ac.notifySubscribers(update)
   }
   ```
10. `:333-336` `loadBackendData()` / `RefreshAllCachedColors()` — unchanged.

Steps 7 and 9 are mutually exclusive (step 7 returns), so there is never a double
notify for the same refresh cycle. `refreshAlerts` is only ever invoked sequentially —
`Start()` calls it once synchronously, then `backgroundRefresh()`'s `for`/`select` loop
(`:181-194`) calls it again only after the previous call returned — so `statusesBefore`
cannot be invalidated by a concurrent refresh between steps 4 and 7/9.

`sse_handler.go` needs no change — it forwards whatever `DashboardIncrementalUpdate` it
receives; `Sources` rides along automatically once the model field exists.

### 4. Client: pill, popover, banner

New `internal/webui/templates/scripts/dashboard_sources.templ`, following the existing
mixin convention (`dashboard_core.templ:322-327`, `window.dashboardXMixin` +
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

**Load the mixin, or `Object.assign` silently no-ops:** every existing mixin is loaded
by an explicit `@scripts.X()` call in the fixed block at `NewDashboard.templ:960-968`
(`@scripts.DashboardFilterPresetsMixin()`, `@scripts.DashboardCore()`,
`@scripts.DashboardData()`, etc.) — that's what makes `window.dashboardXMixin` exist
before `Object.assign` reads it in `dashboard_core.templ`'s `init()`. Add
`@scripts.DashboardSources()` to that same block. Without this line,
`window.dashboardSourcesMixin` stays `undefined`, `Object.assign(this,
window.dashboardSourcesMixin || {})` silently assigns nothing (no error, no console
warning), and the pill renders permanently empty.

Wire into `dashboard_core.templ`'s `Object.assign` list (line 322-327) and into
`dashboard_data.templ`:
- initial load (`dashboard_data.templ:61-72`, in `loadDashboardData()` next to existing
  `result.data.colors` handling): `if (result.data.sources) { this.sources =
  result.data.sources; }`.
- SSE merge (`dashboard_data.templ` at the update merge point, alongside status handling):
  `if (update.sources) this.sources = update.sources`.

Markup: a `components/source_status.templ` component rendered from
`NewDashboard.templ`'s header, next to the existing "Secondary Stats Dropdown"
(line 88 — same `x-data="{ statsOpen: false }"` + `@click.away` popover pattern, reused
rather than invented; note the state var is `statsOpen`, not `open`). Banner is a
conditionally-rendered block above the alerts table,
dismissal state kept in `dismissedStaleBanner` (component-local, not persisted —
reappears on reload if the condition still holds, per acceptance criteria).

In `dashboard_utilities.templ:705`, the `renderText(value, fieldPath)` function gains a
`field_path === 'source'` branch that suffixes a stale-dot visual marker when
`this.sources[value]?.state !== 'live'` — no new column, no column-preference migration
(leverages existing `col_source` at line 662).

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
- `internal/webui/templates/pages/NewDashboard.templ` — mount the component in the
  header (line 88 area) and add `@scripts.DashboardSources()` to the script-loading
  block (lines 960-968), or the mixin never loads.
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
- **Popover pattern reuse**: copying the existing `x-data="{ statsOpen: false }"` +
  `@click.away` popover (already used for "More" stats) avoids a new interaction
  pattern, but means the Sources pill and the More dropdown are independent
  `x-data` scopes — acceptable, they don't need to coordinate.

## Validation

- `GetSourceStatuses()` unit test in `alert_cache_test.go`: success → failure →
  recovery transition, asserting `live` → `stale` after 3 consecutive `fetchErrors`
  entries → back to `live` with `ConsecutiveFailures` reset on the next success; and a
  source absent from any successful poll since cache creation reads `unreachable`.
- `refreshAlerts` early-return notify test: seed a source at `ConsecutiveFailures == 2`
  (still `live`, one failure short of `stale`), then have the fake `alertFetcher`
  return `(nil, {sourceA: err})` — the third consecutive failure, crossing into
  `stale` and tripping the `len(alertsWithSource) == 0 && len(fetchErrors) > 0`
  early return. Assert a subscriber still receives an update with
  `Sources[sourceA].State == "stale"` despite zero alert diff and the early
  `return` — this is the regression test for the early-return control-flow bug.
  Also assert a poll that does *not* cross a state boundary (e.g. a lone failure
  going from `ConsecutiveFailures` 0 to 1, still `live`) produces no notify on the
  early-return path, confirming the fix isn't just "always notify on early return".
- Boundary-case test: two sources, one returns zero alerts successfully, the other
  is already at `ConsecutiveFailures == 2` and fails again this poll (so
  `len(alertsWithSource) == 0 && len(fetchErrors) > 0` is true even though only one
  of the two sources actually failed) — assert the failing source's `live`→`stale`
  transition still reaches subscribers via the early-return path.
- `NewAlertCache(nil, nil, 0, 0)` does not panic and its `GetSourceStatuses()` returns
  an empty map — regression test guarding the 6 existing callers in
  `dashboard_handlers_test.go`.
- `make webui-templates && go build ./...` passes.
- Manual check via `make test` (docker-compose stack, **with at least 2 Alertmanager
  sources configured** — the second source is commented out by default; **uncomment**
  `NOTIFICATOR_ALERTMANAGERS_1_NAME` / `_URL` at `docker-compose.yml:68-69` (backend
  service) and `NOTIFICATOR_ALERTMANAGERS_1_NAME` / `_URL` /
  `_OAUTH_ENABLED` / `_OAUTH_PROXY_MODE` at `docker-compose.yml:128-131` (webui
  service) to enable it. Lines 52-53 (`NOTIFICATOR_BACKEND_ENABLED` /
  `NOTIFICATOR_BACKEND_GRPC_LISTEN`) are unrelated — commenting those out disables
  the backend entirely and must not be touched):
  - All sources up: header shows `N/N live`; popover rows show a last-successful-poll
    under one sync interval.
  - Stop one Alertmanager container: within 3 sync intervals the pill turns amber,
    that source's popover row shows `stale` with its last error, the banner names the
    source and its alert count, and its rows in the table carry the stale marker in
    the Alertmanager column — without a page reload (SSE path).
  - Restart the stopped Alertmanager: state clears on the next successful poll, no
    reload needed.
  - **All sources down:** after stopping all configured Alertmanager containers, the
    pill turns red within 3 sync intervals, and all sources show `stale` in the
    popover (no `unreachable` — only the startup zero-value triggers that). The banner
    appears and persists — over SSE, without a page reload. This step verifies §3's
    restructured `refreshAlerts`: every poll in this scenario takes the early-return
    path at `:215-218` (`alertsWithSource` is empty, `fetchErrors` is full), so the
    pill only updates live if the early-return branch itself calls
    `notifySubscribers` before its `return` — bookkeeping running earlier in the
    function is necessary but not sufficient.
  - **Partial outage (the boundary case):** with 2 sources configured, stop only one.
    Confirm the still-healthy source keeps polling normally (its `AlertCount` and
    `LastSuccessAt` keep advancing) while the down source's popover row independently
    reaches `stale`, even on refresh cycles where the healthy source happens to
    return zero alerts (which also trips the `len(alertsWithSource) == 0` early
    return) — the down source's state must still update live, not just on cycles
    where the healthy source also has alerts to report.
  - A source that never answers from a fresh `make test` startup shows `unreachable`,
    not `live`, contributing zero alerts.
  - Reload the page while the banner condition still holds: banner reappears (not
    permanently dismissed).
  - Initial `GET /api/v1/dashboard/data` response already contains the correct
    `sources` block — no flash of "all green" before the first SSE update.
