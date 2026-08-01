# Spec: blast-radius panel in the alert modal

- Issue: [SoulKyu/notificator#153](https://github.com/SoulKyu/notificator/issues/153)
- Date: 2026-08-01
- Status: planned

## Problem

The alert modal (`internal/webui/templates/scripts/dashboard_modal.templ` +
`internal/webui/templates/components/alert_modal_shared.templ`) answers "what is this
alert" — Overview, Labels, Annotations, Comments, Acknowledgments, History (with a
30-day sparkline), Details. It never answers "is this one alert, or one of many firing
on the same cluster/namespace/node right now". Finding that out today means closing the
modal, going back to the table, and hand-building a filter — losing the modal context at
the exact moment (a storm) when that context matters most.

Distinguishing "single noisy target" from "whole cluster degraded" changes the response
(silence one vs. page the infra team), so it needs to be visible without leaving the
modal.

## Goals

1. A **Related** tab in the alert modal, alongside Comments/History, showing the other
   currently-firing alerts that share a label value with the open alert — grouped by
   label (`cluster=prod-eu (12 firing)`, `namespace=payments (7)`, `instance=node-14
   (3)`, `alertname=<same> (5 firing)`). A group's headline number is the number of
   alerts the **dashboard** currently lists for that label value, the open alert
   included — so it equals what the table shows when filtered on that value. The
   expanded list under the header holds the *other* alerts (headline minus one), since
   the open alert is already on screen.
2. Only labels that actually correlate are shown: a label shared with fewer than two
   other alerts, or shared by essentially the whole active set, is dropped as
   non-signal. The remaining correlating labels are capped to the top few by count.
3. Each group expands to its matching alerts (name, severity, target, age). Clicking one
   opens that alert in the modal, reusing the existing deep-link/history-stack
   navigation (`pushAlertHistoryEntry` / `syncModalWithLocation` in
   `dashboard_modal.templ`) so the browser Back button returns to the alert the user
   came from.
4. Each group has a **Filter dashboard on this** action that closes the modal and
   applies that label value as the dashboard's search filter.
5. An alert that is the only one firing shows an explicit empty state, not a spinner or
   an empty box.
6. Hidden alerts and hidden rules (`HiddenAlertsService`) are respected — a muted alert
   never appears in a group, and never inflates a group's count.

## Non-goals

- Cross-alert correlation over *resolved* history or statistics. Live firing alerts
  only — no backend RPC, no schema/proto change.
- Causality, root-cause ranking, or any scoring beyond "shares a label value".
- Bulk-acting on the related set from inside the modal. Bulk actions stay on the table.
- Persisting a per-user choice of which labels to correlate on.
- A dedicated structured label filter on the dashboard. "Filter dashboard on this"
  reuses the existing free-text `search` filter (see Approach) — it does not add a new
  filter type to `DashboardFilters`.

## Approach

### Backend: one new read-only endpoint, cache-only

**`GET /api/v1/dashboard/alert/:fingerprint/related`**, registered in
`internal/webui/router.go` next to the existing
`dashboard.GET("/alert/:fingerprint/history", handlers.HandleGetAlertHistory)`
(`internal/webui/router.go:266`), inside the same `RequireAuth()`-guarded `dashboard`
group.

New handler `HandleGetRelatedAlerts` in `internal/webui/handlers/dashboard_handlers.go`,
following the shape of `HandleGetAlertHistory` (`dashboard_handlers.go:2464`) and
`GetAlertDetails` (`dashboard_handlers.go:1342`):

1. Read `fingerprint` from the path param; 400 if empty.
2. 503 if `alertCache == nil` (same guard `GetAvailableFields` uses at
   `dashboard_handlers.go:2546`).
3. Look up the source alert with `alertCache.GetAlert(fingerprint)`
   (`internal/webui/services/alert_cache.go:807`) — snapshot, not a live pointer. 404 if
   it doesn't exist. `GetAlert` only returns a cache hit for a still-firing alert (it
   falls through to the backend's resolved-alert store otherwise); a resolved-alert hit
   is treated the same as "not found" for this endpoint, since Related only makes sense
   for a live alert.
4. Build the candidate set **through the dashboard's own pipeline, not a parallel one**
   (see "Count parity" below): `filters := parseDashboardFilters(c)`
   (`dashboard_handlers.go:223`, which already defaults `displayMode` to `classic` at
   `:231`), `sessionID := middleware.GetSessionID(c)` (as `HandleGetAlertHistory` does),
   the same `hiddenAlertsService.LoadUserData(sessionID)` warm-up `GetDashboardData` runs
   first (`dashboard_handlers.go:139-141`, nil- and empty-session-guarded there), then
   `candidates := applyDashboardFilters(alertsForDisplayMode(filters), filters, sessionID)`
   (`applyDashboardFilters` at `dashboard_handlers.go:422`).
5. Drop `IsResolved` alerts from the result of step 4, and drop the source alert's own
   fingerprint. Nothing else is filtered by hand here — hidden alerts, hidden rules,
   search, severity, team, alertname, status and the acknowledged filter are all already
   applied by `applyDashboardFilters` (its hidden check is `dashboard_handlers.go:434`,
   `hiddenAlertsService.IsAlertHidden` at
   `internal/webui/services/hidden_alerts_service.go:194`, nil-guarded in place because
   the service can be nil per `SetHiddenAlertsService`, `dashboard_handlers.go:117`).
6. Call the new pure helper (below) with the source alert, that candidate slice, and the
   `maxGroups` / `maxAlertsPerGroup` caps.
7. Return `webuimodels.SuccessResponse` with the group list; empty `groups: []` (not an
   error) when nothing correlates — the frontend renders the empty state on an empty
   array, no separate "empty" flag needed.

#### Count parity: share the selection code, don't re-enumerate the filters

The issue requires the group counts to match what the dashboard shows for the same
filter. A `/related` handler that starts from `alertCache.GetAllAlerts()` and re-applies
a hand-written subset of the dashboard's filters **cannot** hold that property: in its
default `classic` display mode the table does not come from `GetAllAlerts()` at all, it
comes from `getStandardAlerts()` (`dashboard_handlers.go:353`, called from
`GetDashboardData`'s `default:` branch at `:176`), which drops
`IsAcknowledged || IsResolved` *before* any other filter runs. Acknowledging an alert is
a first-class workflow here (it has its own display mode), and an acknowledged alert
stays live and firing in the cache — so `GetAllAlerts()`-based counts read
`alertname=DiskSpaceLow (3 firing)` next to a table listing 2. Every filter enumerated
by hand is a list that can be — and was — incomplete.

The fix is structural: **both paths select and filter through the same functions**.

- Extract `GetDashboardData`'s display-mode switch (`dashboard_handlers.go:146-177`)
  verbatim into `func alertsForDisplayMode(filters webuimodels.DashboardFilters)
  []*webuimodels.DashboardAlert`, and have `GetDashboardData` call it. Pure move, no
  behaviour change — the switch already only reads `filters.DisplayMode` and
  `filters.ResolvedAlertsLimit`.
- `/related` calls that same function with filters parsed from its own query string, then
  the same `applyDashboardFilters`. The frontend sends the dashboard's current query
  params (see Frontend), so the candidate set is the table's set by construction, whatever
  the display mode and whatever filters are active.
- The one deliberate divergence: step 5 drops `IsResolved` alerts, which `full`/`hidden`
  display modes do list in the table. That is this feature's stated non-goal (live firing
  alerts only), it is bounded and named here, and it is the only filter `/related` applies
  that `GetDashboardData` doesn't.
- Consequence to make visible in the UI copy: with a dashboard filter active, Related
  reports what correlates *within the filtered view*, matching the table rather than the
  raw cache.

No Alertmanager call, no gRPC call, no lock held across the aggregation (the cache calls
already return copies) — this is an in-memory scan over the current live set, which the
issue caps at "a few thousand active alerts", comfortably under the 100ms target.

### Correlation helper — pure function, unit-testable in isolation

New unexported function in `dashboard_handlers.go` (co-located with the handler, same
pattern as `matchesSearch` at `dashboard_handlers.go:487` and
`alertPassesAlertLevelFilters` covered by
`internal/webui/handlers/dashboard_filter_predicate_test.go`):

```go
type relatedAlertGroup struct {
	Label string `json:"label"`
	Value string `json:"value"`
	// Count is len(others)+1 — every dashboard alert carrying this label value,
	// the source alert included, so it equals the table's row count when filtered
	// on that value. Alerts below holds only the others, capped.
	Count  int                           `json:"count"`
	Alerts []*webuimodels.DashboardAlert `json:"alerts"`
}

// correlatingLabelGroups groups candidates by the label values they share with source
// and returns the top maxGroups groups, count descending. candidates is the dashboard's
// own filtered live set with the source alert already removed, so every threshold below
// is expressed over *other* alerts only.
func correlatingLabelGroups(source *webuimodels.DashboardAlert, candidates []*webuimodels.DashboardAlert, maxGroups, maxAlertsPerGroup int) []relatedAlertGroup
```

Algorithm — written as code rather than prose, because the two prior revisions of this
section were ambiguous about whether the threshold counted the source alert or not.
`candidates` excludes the source (handler step 5), so `others` never contains it and no
±1 reading is possible:

```go
groups := []relatedAlertGroup{}
for key, sourceValue := range source.Labels {
	if strings.HasPrefix(key, "__") {
		continue // __name__ & friends: Alertmanager internals, never operator-meaningful
	}
	var others []*webuimodels.DashboardAlert
	for _, candidate := range candidates {
		if candidate.Labels[key] == sourceValue {
			others = append(others, candidate)
		}
	}
	if len(others) < 2 {
		continue // goal 2: fewer than two other alerts share it — not a blast radius
	}
	if len(others) == len(candidates) && len(candidates) > 5 {
		continue // covers every other alert on a large estate (alertmanager=prod) — no signal
	}
	sortBySeverityThenName(others) // stable, scannable order
	groups = append(groups, relatedAlertGroup{
		Label:  key,
		Value:  sourceValue,
		Count:  len(others) + 1, // the open alert counts too — this is the table's number
		Alerts: capSlice(others, maxAlertsPerGroup),
	})
}
sortByCountDesc(groups)
groups = capSlice(groups, maxGroups)
```

Consequences of pinning the threshold to `others`, spelled out so no reader has to
re-derive them:

- Exactly two alerts firing in total (`len(candidates) == 1`): `len(others) == 1` for
  every label, every group is dropped, and the Related tab shows the goal-5 empty state.
  That is the intended reading of goal 2, not an accident — one other alert is a pair,
  not a blast radius, and the alert list is one row away in the table behind the modal.
- Three to six identical alerts: `len(others) >= 2` and the `len(candidates) > 5`
  carve-out keeps the whole-estate rule from firing, so a small homogeneous storm still
  produces groups. This is the case the carve-out exists for.
- `maxGroups` is 5 (the issue's "top few"), `maxAlertsPerGroup` is 20. Truncating the
  list never touches `Count`, so "12 firing" stays 12 in the header even when the
  expansion shows 11 rows and stops.
- `alertname` needs no special handling: `AlertCache.convertToDashboardAlert` copies
  every Alertmanager label into `Labels` (`alert_cache.go:340-346`) and the
  `DashboardAlert.AlertName` struct field is just `Labels["alertname"]` read back
  (`alert_cache.go:378` → `models.Alert.GetAlertName`, `internal/models/alert.go:53`).
  Folding the struct field in as well would emit a duplicate `alertname=…` group and burn
  a `maxGroups` slot.

The inner loop mirrors the label-counting loop `GetAvailableFields` already runs
(`dashboard_handlers.go:2560-2577`) but keyed by matching *value* against the source
alert instead of counting key occurrence.

### Frontend: Related tab, following the Comments/History tab pattern

**`internal/webui/templates/components/modal_components.templ`** (the `AlertDetailsModal`
template used by the live dashboard modal): add the `Related` tab as a **hand-written
`<button>` next to the History one** (`modal_components.templ:1131-1132`), copying its
`@click="currentAlertTab = 'related'; if (!relatedAlerts && !relatedLoading) loadRelatedAlerts()"`
shape and its `:class` pill styling. Not `@AlertModalTabButton` — that helper
(`alert_modal_shared.templ:302`) takes four strings and hardcodes
`@click="<tabVar> = '<tab>'"`, so it cannot carry the lazy-load call; that is exactly why
History is hand-written while the six purely-declarative tabs above it
(`modal_components.templ:1120-1125`) use the helper. Then add a
`x-show="currentAlertTab === 'related'"` panel
next to the History panel (`modal_components.templ:1662`) rendering the group list — each
group is a disclosure/accordion header (`label=value (N firing)`, `N` being the group's
`Count`, the open alert included, so it reads as the table's row count for that value)
that expands to the other `N-1` alerts as a compact list (name, severity badge, instance,
age), reusing `AlertModalSeverityBadge` and
`formatDuration` already used elsewhere in the modal. New template functions go in
`internal/webui/templates/components/alert_modal_shared.templ` beside the existing
`AlertModalHistoryTable` (`alert_modal_shared.templ:375`) and `AlertModalCommentsReadonly`
(`alert_modal_shared.templ:454`) — same file, same declaration style, so they pick up
the file's existing dark-mode/Tailwind conventions instead of inventing new ones — and are
called from `modal_components.templ` using `currentAlertTab`, matching the tab buttons
above.

Note: the read-only Statistics/resolved-alerts modal (`AlertModalReadonly` in
`alert_modal_shared.templ`, `x-data="{ currentTab: 'overview' }"` at
`alert_modal_shared.templ:604`, tab buttons at `alert_modal_shared.templ:686-698`) has its
own separate tab scaffolding and no live `dashboardModalMixin` state to call
`loadRelatedAlerts` from. It is out of scope for this spec; adding a Related tab there, if
wanted, is a separate follow-up.

**`internal/webui/templates/scripts/dashboard_modal.templ`**: unlike History, which
`showAlertDetails` loads eagerly right after the alert details fetch
(`dashboard_modal.templ:79`, `this.loadAlertHistory()`), Related loads lazily — only
when the tab is opened for the first time, per the issue. Add:

- `relatedAlerts: null` and `relatedLoading: false` to the modal's Alpine state (next to
  `alertHistory` / `historyLoading`), reset in `showAlertDetails` alongside
  `this.alertHistory = null` (`dashboard_modal.templ:57`) so switching alerts doesn't
  leak the previous alert's groups.
- `window.dashboardModalMixin.loadRelatedAlerts`, mirroring `loadAlertHistory`
  (`dashboard_modal.templ:594-633`) including its fingerprint-guard-after-await pattern
  (the alert can change while the fetch is in flight; a stale response must not
  overwrite the now-current alert's state).
- The lazy-load trigger is the tab button's own `@click` guard
  (`if (!relatedAlerts && !relatedLoading) loadRelatedAlerts()`), byte-for-byte the
  History precedent at `modal_components.templ:1132` — no watcher, no `x-init`, and
  re-clicking the tab does not re-fetch.
- `loadRelatedAlerts` must send the dashboard's **current filter query string** so the
  backend can rebuild the table's exact alert set (see "Count parity"). That query is
  already built inline three times in `dashboard_data.templ` — `loadDashboardData`
  (`:10-22`), `loadDashboardIncremental` (`:94`) and `loadAlertColors` (`:176`) — so
  extract those identical lines into a `buildAlertQueryParams()` method on
  `window.dashboardDataMixin`, have the three existing callers use it, and call it from
  `loadRelatedAlerts` too. The data mixin and the modal mixin are `Object.assign`-ed onto
  the same dashboard component (`dashboard_core.templ:318` and `:321`), so
  `this.buildAlertQueryParams()` resolves from inside `loadRelatedAlerts`; `displayMode`
  lives on that same shared scope (`dashboard_core.templ:45`).
  Net: one fewer copy of the block than today, and parity can't drift because there is
  only one place left to drift in.
- Clicking a related alert in the list calls the existing
  `this.showAlertDetails(fingerprint)` (`dashboard_modal.templ:52`) — no new navigation
  code needed, it already pushes history state and the browser Back button already
  restores the previous alert via `syncModalWithLocation`
  (`dashboard_modal.templ:26-50`).
- **Filter dashboard on this**: sets `this.searchQuery = value` (the label's value, not
  `key=value` — `matchesSearch` at `dashboard_handlers.go:487` already substring-matches
  free text against both label keys and label values, so this is a correct, if
  approximate, reuse of the existing filter rather than a new filter primitive) and
  calls `this.closeAlertModal()` then the dashboard's existing filter-apply path (the
  same one the search box's own input triggers). Documented as approximate: a label
  value that also happens to be a substring of an unrelated field's text will over-match,
  same as manually typing that value into the search box would today.

Regenerate templates with `make webui-templates` after editing the `.templ` files —
never hand-edit the generated `*_templ.go` output.

## Risks & trade-offs

- **Search-based "Filter dashboard on this" is approximate, not exact.** Reusing the
  free-text `search` filter instead of adding a real label-equality filter keeps this
  WebUI-only with no new filter primitive, matching the issue's explicit scope, but a
  value like `db` as a `namespace` could also match an unrelated alert whose summary
  contains "db". Accepted because it's no worse than the manual workaround this feature
  replaces, and a real structured label filter is a separate, larger feature.
- **O(labels × candidates) scan per request.** For each of the source alert's labels, a
  full pass over the live candidate set. At "a few thousand active alerts" and a typical
  handful of labels per alert, this is a few thousand comparisons — well within the
  sub-100ms budget — but a pathological alert with dozens of labels on a very large
  estate would scale linearly with both. No pagination or streaming needed at current
  scale; flagged here so a future regression (very high label cardinality) has a named
  cause to look for.
- **Degenerate-label thresholds are heuristic.** "Fewer than two other alerts" and
  "covers every other alert on a large estate" are the two cheap, obvious no-signal
  cases; there's no statistical significance test. Good enough for "what else is firing",
  not a general correlation engine — consistent with the issue's explicit non-goal on
  scoring/ranking. The visible cost is the two-alerts-total case: a pair shows the empty
  state. Lowering the threshold to one other alert is a one-character change if triage
  feedback says pairs matter.
- **Related is scoped to the dashboard's current filters, not the whole cache.** That is
  what makes the counts match the table (the issue's acceptance criterion), but it also
  means a filtered dashboard yields a narrower blast radius than the operator might
  expect. The panel says so in its header copy rather than silently reporting a
  filter-dependent number.
- **Extracting `alertsForDisplayMode` touches `GetDashboardData`.** A pure move of an
  existing switch, but it is production code on the dashboard's hot path, so it lands as
  its own commit with `go test ./internal/webui/...` green before the handler is added.
- **Hidden-alerts filtering happens at read time, not cached.** Every `/related` call
  re-runs `applyDashboardFilters` over the whole live set, `IsAlertHidden` included. Same
  cost model `GetDashboardData` already accepts for the main table; not a new risk, just
  inherited — and paying it is what buys count parity.

## Validation

- `go build -tags "nogui,webui" -o notificator .` succeeds.
- `make webui-templates` regenerates `alert_modal_shared_templ.go` and
  `dashboard_modal_templ.go` cleanly (no manual edits to either `*_templ.go`).
- New unit test in `internal/webui/handlers/dashboard_handlers_test.go` (or a sibling
  `dashboard_related_alerts_test.go`, matching the existing
  `dashboard_filter_predicate_test.go` split-by-concern style) covering
  `correlatingLabelGroups` (thresholds are over `others`, i.e. `candidates` with the
  source already removed):
  - `len(candidates) == 1` (two alerts firing in total): no groups at all — the pair case
    resolves to the empty state.
  - a label with exactly two other alerts sharing the value survives, with `Count == 3`.
  - a label shared by every candidate is dropped when `len(candidates) > 5`, and kept at
    `len(candidates) <= 5` (the small-homogeneous-storm carve-out).
  - `__name__`/`__`-prefixed labels are never considered.
  - `alertname` yields exactly one group, not two, when the source carries an
    `alertname` label (no duplicate from the `AlertName` struct field).
  - groups are ordered by `Count` descending, and truncated to `maxGroups`.
  - a group's alert list is capped to `maxAlertsPerGroup` while `Count` still reports
    the untruncated total, source included.
- `go test ./internal/webui/...` passes — in particular the existing dashboard handler
  tests, which cover the `GetDashboardData` path that the `alertsForDisplayMode`
  extraction moves code out of.
- Manual verification against `docker-compose up -d` (per project convention — `make
  test` for the full stack): open an alert that's the only one firing on its labels and
  confirm the empty state; open an alert during a simulated storm (several alerts
  sharing `cluster`/`namespace`) and confirm the groups, counts, and per-alert list match
  what filtering the dashboard table on that label shows; click a related alert and
  confirm Back returns to the original; use "Filter dashboard on this" and confirm the
  modal closes with the table filtered.
- **Count-parity regression check, the one that has bitten this design twice.** With
  several alerts sharing an `alertname`, acknowledge one of them, stay in the default
  `classic` display mode, and confirm the Related group header drops by one in lockstep
  with the table's row count (an acknowledged alert is still live and firing in
  `AlertCache`, so a `GetAllAlerts()`-based implementation would keep counting it). Then
  switch the dashboard to `acknowledge` and to `full` mode and confirm Related tracks the
  table in each — modulo the one documented exception, resolved alerts, which `full`
  lists and Related never does.
- Confirm a hidden alert never appears in a group and never counts toward a group's
  total (hide an alert that would otherwise correlate, re-open Related, confirm it drops
  out and the count decrements). Same for a search/severity filter left active on the
  dashboard.
