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
   (3)`, `alertname=<same> (5 other targets)`).
2. Only labels that actually correlate are shown: a label whose value is shared by 1
   other alert, or by essentially the whole active set, is dropped as non-signal. The
   remaining correlating labels are capped to the top few by count.
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
4. Get the full live snapshot with `alertCache.GetAllAlerts()`
   (`alert_cache.go:691`) — the same call `GetAvailableFields` uses, and the same source
   `GetDashboardData` filters for the table, so counts naturally match what the
   dashboard shows.
5. Drop alerts hidden for the current session: `sessionID :=
   middleware.GetSessionID(c)` (used by `HandleGetAlertHistory`), then skip any alert
   where `hiddenAlertsService.IsAlertHidden(sessionID, alert)` is true
   (`internal/webui/services/hidden_alerts_service.go:194`) — same check
   `GetDashboardData` applies at `dashboard_handlers.go:434`. Guard `hiddenAlertsService
   == nil` (it can be, per `SetHiddenAlertsService`) by treating "nothing hidden" as the
   fallback, matching how `IsAlertHidden`'s callers already handle a nil service.
6. Call the new pure helper (below) with the source alert, the filtered snapshot, and a
   `maxGroups` cap, excluding the source alert's own fingerprint from every group.
7. Return `webuimodels.SuccessResponse` with the group list; empty `groups: []` (not an
   error) when nothing correlates — the frontend renders the empty state on an empty
   array, no separate "empty" flag needed.

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
	Label  string                       `json:"label"`
	Value  string                       `json:"value"`
	Count  int                          `json:"count"`
	Alerts []*webuimodels.DashboardAlert `json:"alerts"`
}

// correlatingLabelGroups groups candidates (the live set minus the source alert and
// minus hidden alerts) by shared label value with source, drops degenerate labels
// (shared by 1 other alert, or by >= the whole candidate set), and returns the top
// maxGroups groups ordered by count descending, each capped to maxAlertsPerGroup
// alerts.
func correlatingLabelGroups(source *webuimodels.DashboardAlert, candidates []*webuimodels.DashboardAlert, maxGroups, maxAlertsPerGroup int) []relatedAlertGroup
```

Algorithm:

- For every label key present on `source` (including `alertname`, treated as a label
  key for this purpose even though it's a struct field on `DashboardAlert`, matching how
  `matchesSearch` already folds `alert.AlertName` into its label-style comparison), scan
  `candidates` and collect those whose value for that key equals the source's value.
- A label is **degenerate** and dropped if the matching count is `< 2` (only the source
  itself, i.e. nothing correlates) or if it covers the entire candidate set *and* the
  candidate set is large (count `== len(candidates)` and `len(candidates) > 5`, e.g.
  every alert on a large estate shares `alertmanager=prod` — that's the whole estate,
  not signal). In a small homogeneous storm (2–5 identical alerts), all labels are kept
  as signal, ensuring at least one group survives to distinguish "only alert firing"
  from "cluster ablaze". `__name__` and any key starting with `__` are skipped outright
  — they're Prometheus/Alertmanager internal labels, never operator-meaningful.
- Sort the surviving groups by count descending, take the top `maxGroups` (5, matching
  the issue's "top few").
- Within a group, sort alerts by severity then name for a stable, scannable order, and
  cap the list to `maxAlertsPerGroup` (20) — the group's `Count` still reports the true
  total so "12 firing" isn't silently truncated to "5 firing" in the header, only the
  expanded list is capped.

This mirrors the label-counting loop `GetAvailableFields` already runs
(`dashboard_handlers.go:2560-2577`) but keyed by matching *value* against the source
alert instead of counting key occurrence.

### Frontend: Related tab, following the Comments/History tab pattern

**`internal/webui/templates/components/modal_components.templ`** (the `AlertDetailsModal`
template used by the live dashboard modal): add a `Related` `AlertModalTabButton` next to
`history`, alongside the existing `@AlertModalTabButton(..., "currentAlertTab", ...)` calls
(`modal_components.templ:1120-1125`), and a `x-show="currentAlertTab === 'related'"` panel
next to the History panel (`modal_components.templ:1662`) rendering the group list — each
group is a disclosure/accordion header (`label=value (N firing)`) that expands to a compact
alert list (name, severity badge, instance, age), reusing `AlertModalSeverityBadge` and
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
- The Related tab button's click handler (or an `x-init`/watcher on `currentAlertTab`) calls
  `loadRelatedAlerts()` the first time `currentAlertTab` becomes `'related'` and
  `relatedAlerts === null`, so re-clicking the tab doesn't re-fetch.
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
- **Degenerate-label thresholds are heuristic.** "Shared by 1 other alert" and "covers
  the whole candidate set" are the two cheap, obvious no-signal cases; there's no
  statistical significance test. Good enough for "what else is firing", not a general
  correlation engine — consistent with the issue's explicit non-goal on scoring/ranking.
- **Hidden-alerts filtering happens at read time, not cached.** Every `/related` call
  re-evaluates `IsAlertHidden` for the whole candidate set. Same cost model
  `GetDashboardData` already accepts for the main table; not a new risk, just inherited.

## Validation

- `go build -tags "nogui,webui" -o notificator .` succeeds.
- `make webui-templates` regenerates `alert_modal_shared_templ.go` and
  `dashboard_modal_templ.go` cleanly (no manual edits to either `*_templ.go`).
- New unit test in `internal/webui/handlers/dashboard_handlers_test.go` (or a sibling
  `dashboard_related_alerts_test.go`, matching the existing
  `dashboard_filter_predicate_test.go` split-by-concern style) covering
  `correlatingLabelGroups`:
  - a label shared by exactly one other alert is dropped (degenerate: count < 2 signal).
  - a label shared by every candidate is dropped (degenerate: whole estate).
  - `__name__`/`__`-prefixed labels are never considered.
  - groups are ordered by count descending, and truncated to `maxGroups`.
  - a group's alert list is capped to `maxAlertsPerGroup` while `Count` still reports
    the untruncated total.
- Manual verification against `docker-compose up -d` (per project convention — `make
  test` for the full stack): open an alert that's the only one firing on its labels and
  confirm the empty state; open an alert during a simulated storm (several alerts
  sharing `cluster`/`namespace`) and confirm the groups, counts, and per-alert list match
  what filtering the dashboard table on that label shows; click a related alert and
  confirm Back returns to the original; use "Filter dashboard on this" and confirm the
  modal closes with the table filtered.
- Confirm a hidden alert never appears in a group and never counts toward a group's
  total (hide an alert that would otherwise correlate, re-open Related, confirm it drops
  out and the count decrements).
