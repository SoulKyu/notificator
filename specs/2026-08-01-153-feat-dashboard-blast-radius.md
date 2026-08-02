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
   (3)`, `alertname=<same> (5 firing)`). A group's headline number is **counted over the
   alerts the dashboard itself lists** carrying that label value — so it equals the row
   count the table reports for a label-equality filter on that value, across pages, not
   just the visible page. The open alert is counted exactly when the dashboard lists it,
   which is **not always**: the modal's own **Acknowledge** button
   (`modal_components.templ:1065`), muting the open alert, and deep-linking to an alert
   the active filter excludes all leave the modal open over an alert the table no longer
   shows. The count is therefore never `others + 1` — see "Count is measured, not
   derived". (The **Filter dashboard on this** action of goal 4 reaches that view through
   the free-text `search` filter, which can over-match; the count is the exact
   label-equality number, the action is the approximation — see Risks.) The expanded list
   under the header holds the *other* alerts — headline minus one when the open alert is
   in that set, all of them when it is not, since the open alert is already on screen.
2. Only labels that actually correlate are shown: a label shared with fewer than two
   other alerts is dropped as non-signal. A label shared by *every* other alert in the
   current view is not dropped — it is ranked last, because on an unfiltered estate it
   is usually noise (`alertmanager=prod`) but during a storm it is the finding itself.
   The surviving labels are capped to the top few by count.
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
   never appears in a group, and never inflates a group's count. **This includes the open
   alert itself**, which in the Hidden view is muted by definition: it is counted only if
   the set being reported contains it, and that set never contains muted alerts. This
   holds in *every* display mode, including the dashboard's own **Hidden** view
   (`internal/webui/templates/pages/NewDashboard.templ:142`), whose table is a
   mute-management list of exactly the muted alerts and whose rows open this same modal.
   Related is a triage panel over live *unmuted* alerts; it never inverts with the table
   (see "Hidden display mode" below).

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
   it doesn't exist, **and 404 the same way on `alert.IsResolved`**. Two paths reach a
   resolved source and the check must cover both: `GetAlert` falls through to the
   backend's resolved-alert store on a cache miss (`alert_cache.go:820-830`, setting
   `IsResolved = true` at `:825`), *and* a cache-resident alert can be resolved in place —
   the `bulk-action` handler's `resolve` case mutates the cached struct
   (`dashboard_handlers.go:1016-1024`), so a resolved alert is a perfectly ordinary cache
   hit. Test `IsResolved` on the returned snapshot, never "did it come from the cache".
   Related only makes sense for a live alert.
4. Build the candidate set **through the dashboard's own pipeline, not a parallel one**
   (see "Count parity" below): `filters := parseDashboardFilters(c)`
   (`dashboard_handlers.go:223`, which already defaults `displayMode` to `classic` at
   `:231`), then the single mode override of step 4b below,
   `sessionID := middleware.GetSessionID(c)` (as `HandleGetAlertHistory` does),
   the same `hiddenAlertsService.LoadUserData(sessionID)` warm-up `GetDashboardData` runs
   first (`dashboard_handlers.go:139-141`, nil- and empty-session-guarded there), then
   `candidates := applyDashboardFilters(alertsForDisplayMode(filters), filters, sessionID)`
   (`applyDashboardFilters` at `dashboard_handlers.go:422`).
5. Drop `IsResolved` alerts from the result of step 4. **Do not remove the source alert's
   fingerprint here** — the candidate slice is handed to the helper exactly as the
   dashboard produced it, so it contains the source alert if and only if the table does,
   and that is what makes the counts the table's numbers (see "Count is measured, not
   derived"). The helper excludes the source from the *listed* alerts by fingerprint
   itself. Nothing else is filtered by hand here — hidden alerts, hidden rules,
   search, severity, team, alertname, status and the acknowledged filter are all already
   applied by `applyDashboardFilters` (its hidden check is `dashboard_handlers.go:434`,
   `hiddenAlertsService.IsAlertHidden` at
   `internal/webui/services/hidden_alerts_service.go:194`, nil-guarded in place because
   the service can be nil per `SetHiddenAlertsService`, `dashboard_handlers.go:117`).

   **4b. Hidden display mode is overridden to `classic`, server-side.**
   `applyDashboardFilters` branches on the display mode at `dashboard_handlers.go:446`:
   in `DisplayModeHidden` it keeps **only** hidden alerts (`:446-450`) and in every other
   mode it drops them (`:451-456`). Passing `hidden` through would therefore build a
   candidate set made entirely of muted alerts and invert goal 6 — every group member and
   every count a muted alert. So the handler does, immediately after parsing:

   ```go
   if filters.DisplayMode == webuimodels.DisplayModeHidden {
       filters.DisplayMode = webuimodels.DisplayModeClassic
   }
   ```

   Server-side, not frontend-side, deliberately: `buildAlertQueryParams()` puts the
   dashboard's current `displayMode` on every request by construction (see Frontend), so
   the only place the guarantee can be made unbypassable is the handler. This is the one
   place goal 6 outranks count parity: from the Hidden view, Related answers "what else,
   unmuted, is firing like this" and its counts match the *classic* table, not the mute
   list on screen. That equality is exact, including for the source alert: the open alert
   is muted in this mode, so the classic candidate set does not contain it, so nothing
   counts it — which is only true because `Count` is measured over that set rather than
   derived as `others + 1`. The panel header says which set is reported (same copy path as
   the filtered-view notice below). The three other modes — `classic`, `acknowledge`,
   `full` — pass through untouched and keep full parity.
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
- Exactly two deliberate divergences, both bounded and both named here; there are no
  others, and no hand-written filter beyond them:
  1. Step 5 drops `IsResolved` alerts, which the `full` display mode does list in the
     table. That is this feature's stated non-goal — live firing alerts only.
  2. Step 4b rewrites `displayMode=hidden` to `classic`, so the Hidden view's Related
     panel reports unmuted alerts. That is goal 6, which outranks parity in that one
     mode; the previous revision claimed divergence 1 was the *only* one and thereby
     asserted an invariant it broke.
- Consequence to make visible in the UI copy: with a dashboard filter active, Related
  reports what correlates *within the filtered view*, matching the table rather than the
  raw cache.

#### Count is measured, not derived

Selecting the right candidate set is necessary but not sufficient, and this is where the
previous revisions kept failing: each one fixed *which alerts* are candidates and left
`Count = len(others) + 1` standing. That `+1` is a second, hand-written model of the
table — it asserts "the open alert is one of the rows" — and the handler has no basis for
that assertion. The modal outlives the row. All three of these keep the modal open over an
alert the dashboard no longer lists, without a reload:

- **Acknowledge the open alert** from the modal's own button
  (`modal_components.templ:1065`). In `classic` mode `getStandardAlerts()`
  (`dashboard_handlers.go:353`) drops `IsAcknowledged` at `:358`, so the row is gone and
  the `+1` reports a row that is not there.
- **Mute the open alert**, or open it from the **Hidden** view — where step 4b reports the
  classic set and the open alert is muted *by definition*, so the `+1` is always exactly
  the muted alert that goal 6 promises never inflates a count.
- **Deep-link** to `/dashboard/alert/<fp>` while a dashboard filter excludes that alert.

So the source alert is not special-cased out of the arithmetic; it is simply **not removed
from the candidate set**, and `Count` is the number of candidates carrying the label value
— the open alert among them or not, whichever the dashboard decided. The helper still
excludes the source from the *listed* alerts, by fingerprint
(`webuimodels.DashboardAlert.Fingerprint`, `internal/webui/models/dashboard.go:14`),
because it is already on screen. Two distinct quantities, one measurement each:

| | derived from | equals |
|---|---|---|
| `Count` (header) | candidates matching the value | the table's row count for that value |
| `Alerts` (expansion) | those candidates minus the source fingerprint | `Count` or `Count-1` |

The invariant is now stateable without an "if": **`Count` is a count of rows the dashboard
lists.** Nothing adds to it. The one-line rule for any future edit to this helper — if you
find yourself writing `+ 1`, the set is wrong, not the arithmetic.

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
	// Count is how many of the dashboard's own candidates carry this label value —
	// counted, never computed from len(Alerts). The source alert is in that number
	// exactly when the dashboard lists it (acknowledged, muted or filtered-out source
	// alerts are not). So Count is the table's row count for a label-equality filter
	// on this value, with no case analysis. Alerts holds the others only, capped.
	Count int `json:"count"`
	// WholeSet is true when this label value covers every other alert in the current
	// view: still shown, but ranked below every discriminating group and captioned as
	// such. See "Why the whole-set label is demoted, never dropped".
	WholeSet bool                          `json:"wholeSet"`
	Alerts   []*webuimodels.DashboardAlert `json:"alerts"`
}

// correlatingLabelGroups groups candidates by the label values they share with source
// and returns the top maxGroups groups, count descending. candidates is the dashboard's
// own filtered live set exactly as the table built it — the source alert included if and
// only if the table lists it. correlatingLabelGroups excludes the source from the listed
// alerts by fingerprint, and every threshold below is expressed over those *other*
// alerts, so the caller has no ±1 bookkeeping to get wrong.
func correlatingLabelGroups(source *webuimodels.DashboardAlert, candidates []*webuimodels.DashboardAlert, maxGroups, maxAlertsPerGroup int) []relatedAlertGroup
```

#### Why the whole-set label is demoted, never dropped

Three revisions of this section treated "this label covers every other candidate" as a
boolean *drop*, and each revision only moved the boundary: first an unconditional drop,
then a `len(candidates) > 5` carve-out, then — when the candidate set was narrowed to the
dashboard's own filtered view — a cliff at seven rows. The predicate is what is wrong, not
its constant. Once candidates are the *filtered* view (which is what buys count parity),
narrowing the dashboard makes whole-set coverage the **normal** case: filter on
`alertNames=PodCrashLoop` during an 8-alert storm and every single label covers the whole
set, so a drop rule empties the panel in exactly the situation the feature exists for.

A whole-set label is not "no signal" — it is *less discriminating within the current
view*. On an unfiltered estate that makes it noise (`alertmanager=prod` is on everything);
during a storm it is the finding. Those are the same measurement, so the design cannot
decide between them with a threshold — it decides with **rank**: whole-set groups sort
after every discriminating group, and `maxGroups` alone decides whether they are shown.
No estate-size constant, no cliff, and one stateable invariant: *the only way the panel is
empty is that no correlatable label of the source is shared with two other alerts* (goal
5) — "correlatable" excluding only `__`-prefixed keys and empty values, per below.

Algorithm — written as code rather than prose, because prior revisions were ambiguous
about whether the threshold counted the source alert or not. `candidates` *includes* the
source when the dashboard lists it (handler step 5), so the split between "how many rows"
and "which other rows" is made once, here, and nowhere else.
`sortBySeverityThenName`, `sortGroups` and `capSlice` are helpers to write alongside it,
not existing functions in the tree:

```go
groups := []relatedAlertGroup{}

// The denominator for WholeSet: candidates other than the source. Computed from the
// same slice as everything else, so it is right whether or not the source is in it.
otherCandidates := 0
for _, candidate := range candidates {
	if candidate.Fingerprint != source.Fingerprint {
		otherCandidates++
	}
}

for key, sourceValue := range source.Labels {
	if strings.HasPrefix(key, "__") {
		continue // __name__ & friends: Alertmanager internals, never operator-meaningful
	}
	if sourceValue == "" {
		continue // an empty value would correlate with absence — see below
	}
	matched := 0 // every candidate carrying the value: this is the table's number
	var others []*webuimodels.DashboardAlert
	for _, candidate := range candidates {
		value, ok := candidate.Labels[key]
		if !ok || value != sourceValue {
			continue // comma-ok: a missing key is not a match, whatever sourceValue is
		}
		matched++
		if candidate.Fingerprint != source.Fingerprint {
			others = append(others, candidate) // the open alert is already on screen
		}
	}
	if len(others) < 2 {
		continue // goal 2: fewer than two other alerts share it — not a blast radius
	}
	sortBySeverityThenName(others) // stable, scannable order
	groups = append(groups, relatedAlertGroup{
		Label: key,
		Value: sourceValue,
		Count: matched, // counted over the dashboard's own set — never len(others)+1
		// WholeSet: covers every other alert in the current view. Kept, ranked last.
		WholeSet: len(others) == otherCandidates,
		Alerts:   capSlice(others, maxAlertsPerGroup),
	})
}
// Discriminating groups first, then whole-set ones; count descending within each,
// label name ascending as the tiebreak so the order is deterministic.
sortGroups(groups, func(a, b relatedAlertGroup) bool {
	if a.WholeSet != b.WholeSet {
		return !a.WholeSet
	}
	if a.Count != b.Count {
		return a.Count > b.Count
	}
	return a.Label < b.Label
})
groups = capSlice(groups, maxGroups)
```

`WholeSet` is also serialised (`json:"wholeSet"`) so the panel can caption those groups
("covers every alert in the current view") instead of letting the operator mistake an
estate-wide label for a blast radius.

#### Why an empty label value is skipped, and why the lookup is comma-ok

Label values are **alert-author input**, not trusted config: anything that can `POST
/api/v2/alerts` can set `cluster=""`, and `convertToDashboardAlert` copies it into
`Labels` verbatim (`alert_cache.go:340-346`). In Go, `m[k]` on a missing key returns the
zero value, so a naive `candidate.Labels[key] == sourceValue` makes an empty source value
match **every candidate that does not carry that label at all** — inverting the
correlation into an anti-correlation. Measured on a 23-row estate with one such alert
posted, that yields `cluster= (20 firing)` as the *top-ranked* group over a table showing
2, and it burns a `maxGroups` slot ahead of every real group.

Two independent guards, because this block is meant to be copied verbatim:

- `sourceValue == ""` → skip the label. `label=<nothing>` is not a blast radius under any
  reading; there is no useful group to build from it.
- comma-ok on the candidate lookup → "absent" and "present but empty" stay distinct
  regardless of the first guard, so the loop is still correct if someone later relaxes it.

The same reasoning applies to any group whose value would render as `key=` in the header:
that string is unactionable in the UI and untypeable into "Filter dashboard on this".

Consequences, spelled out so no reader has to re-derive them:

- Exactly two alerts firing in total (`otherCandidates == 1`): `len(others) == 1` for
  every label, every group is dropped, and the Related tab shows the goal-5 empty state.
  That is the intended reading of goal 2, not an accident — one other alert is a pair,
  not a blast radius, and the alert list is one row away in the table behind the modal.
  Note the threshold and `WholeSet` both key off `otherCandidates`, never
  `len(candidates)`, so they do not shift when the source alert leaves the dashboard's set.
- **The open alert acknowledged, muted or filtered out** while the modal stays open: it is
  absent from `candidates`, so `matched` does not count it and every header equals its
  table row count — the previously-broken case. `others` and therefore the group
  membership threshold are unaffected either way, since they never included the source.
- **Any homogeneous storm, at any size, filtered or not** — 3, 8, 80 identical alerts:
  every label is whole-set, so all groups tie on the first sort key and the top
  `maxGroups` by count are shown. The panel never blanks, and there is no estate size or
  dashboard filter (`severities=`, `teams=`, `alertNames=`, `search=`, or enough hides)
  that can push it into an empty state while three or more alerts correlate. This is the
  behaviour the previous two revisions failed to deliver.
- Unfiltered mixed estate: `cluster=prod-eu (8)` and `namespace=payments (8)` are
  discriminating and rank above `alertmanager=prod (19)`, which is whole-set and lands
  last — the noise case the old drop rule was reaching for, now handled by ordering. It
  still occupies a slot if fewer than `maxGroups` discriminating groups exist, which is
  the deliberate trade: something true and low-ranked beats an empty panel.
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
`Count`, i.e. the table's row count for that value) that expands to the group's `alerts`
array as a compact list (name, severity badge, instance, age). Render the header from
`Count` and the list from `alerts.length` — never `Count - 1`, since the open alert is in
`Count` only when the dashboard still lists it (acknowledge it from the modal and the two
converge). Reuse `AlertModalSeverityBadge` and
`formatDuration` already used elsewhere in the modal. A group with `wholeSet: true`
renders a muted "covers every alert in the current view" caption on its header — it is
last in the list by construction (see the ranking above), and the caption is what keeps a
storm's whole-set groups from reading as a narrower blast radius than they are. The panel
header states which set is being reported: the dashboard's current filtered view, or —
when the dashboard is in **Hidden** mode, which the backend overrides per step 4b — the
live unmuted set. New template functions go in
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
  already built inline three times in `dashboard_data.templ` as byte-identical blocks —
  `loadDashboardData` (`:6`, block at `:10-44`), `loadDashboardIncremental` (`:94`, block
  at `:102-136`, which only appends `lastUpdate` afterwards) and `loadAlertColors`
  (`:176`, block at `:193-227`) — so
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
- **One heuristic threshold is left, and it is the only way to get an empty panel.**
  "Fewer than two other alerts share this label" is the single *heuristic* drop rule; the
  whole-set case is a rank, not a drop, so it cannot blank anything. (The two structural
  skips — `__`-prefixed keys and empty values — are not heuristics: neither can name a
  blast radius under any reading.) No statistical significance
  test — good enough for "what else is firing", consistent with the issue's non-goal on
  scoring. The visible cost is the two-alerts-total case: a pair shows the empty state.
  Lowering it to one other alert is a one-character change if triage feedback says pairs
  matter.
- **Ranking is by raw count, so coarse labels can outrank the interesting ones.**
  Measured on a 19-row estate: `severity=critical (14)` is discriminating (not whole-set)
  and outranks `cluster=prod-eu (8)`, and with `environment`/`job`/`service`/`team` also
  correlating it can push `cluster`/`namespace` off the five slots. Accepted for now: an
  exclusion list or a per-label weight is a scoring mechanism, which the issue explicitly
  puts out of scope. Named here so the first triage complaint has an obvious lever — the
  comparator is one function and takes a label-class key ahead of `Count` without
  touching anything else.
- **`alertsForDisplayMode` is extracted from `GetDashboardData` only.** The same
  display-mode switch also exists at `dashboard_handlers.go:1127`
  (`getFilteredAndSortedAlerts`), `:1163` (`getDashboardMetadata`) and `:1851`
  (`GetAlertColors`). Converting those three is a pure follow-up refactor with no bearing
  on this feature's parity (Related and the table both go through `GetDashboardData`'s
  copy), but leaving four copies keeps the drift this spec's own argument warns about —
  so it is a named follow-up, not an oversight.
- **Related is scoped to the dashboard's current filters, not the whole cache.** That is
  what makes the counts match the table (the issue's acceptance criterion), but it also
  means a filtered dashboard yields a narrower blast radius than the operator might
  expect. The panel says so in its header copy rather than silently reporting a
  filter-dependent number. The one mode where the scoping is *not* inherited is `hidden`,
  overridden to `classic` by handler step 4b so goal 6 holds; the same header copy says
  which set is being reported.
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
  `correlatingLabelGroups`. Every case must be built **both ways** — `candidates` with the
  source alert present and the identical slice with it absent — because that is the axis
  every prior revision got wrong; group membership and thresholds must be byte-identical
  across the pair, and `Count` must differ by exactly one:
  - **`Count` is a count of candidates, not `len(others)+1`.** Source present: three other
    alerts share `cluster=prod-eu` → `Count == 4`. Same call with the source removed from
    `candidates` (the acknowledged / muted / filtered-out-source case) → `Count == 3`,
    same group, same `Alerts`, `len(Alerts) == 3` in both. A `+1` implementation passes the
    first assertion and fails the second; this is the whole point of the helper's design
    and the single test that must never be relaxed.
  - two alerts firing in total (one candidate besides the source, present or not): no
    groups at all — the pair case resolves to the empty state.
  - a label with exactly two other alerts sharing the value survives, with `Count == 3`
    when the source is in `candidates` and `Count == 2` when it is not.
  - **an empty source label value never produces a group.** Source carries `cluster=""`;
    candidates are a mix of alerts with no `cluster` label at all and a couple with
    `cluster=""`. Assert no `cluster` group is returned, and in particular that the
    label-less candidates are not counted — the `m[k]`-returns-`""` trap. Add the mirror
    case: a candidate lacking the key never matches a non-empty source value either.
  - **a label shared by every candidate is always kept, at every candidate-set size.**
    Sweep the number of other candidates from 2 to 10 with one whole-set label plus one
    discriminating label and assert a non-empty group list at every size — the boundary
    sweep that caught the old `> 5` cliff at seven rows. Assert the whole-set group
    carries `WholeSet == true` and sorts *after* the discriminating one even when its
    `Count` is higher. Run the sweep with the source absent from `candidates` too:
    `WholeSet` keys off `otherCandidates`, so it must not flip.
  - a fully homogeneous candidate set (every label whole-set, the filtered-storm case):
    groups are returned, ordered by `Count` descending, not an empty list.
  - `__name__`/`__`-prefixed labels are never considered.
  - `alertname` yields exactly one group, not two, when the source carries an
    `alertname` label (no duplicate from the `AlertName` struct field).
  - groups are ordered by `Count` descending, and truncated to `maxGroups`.
  - a group's alert list is capped to `maxAlertsPerGroup` while `Count` still reports
    the untruncated candidate total.
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
- **Source-not-in-the-table check — acknowledge *the open alert*, not a bystander.** The
  check above only ever acknowledges a bystander, so it cannot surface this. Open an alert, note the
  Related header for its `alertname` group, then **without closing the modal** press the
  modal's own Acknowledge button (`modal_components.templ:1065`), reopen the Related tab
  and confirm the header dropped by one and equals the table's row count for that value —
  not header = table + 1. Repeat twice more for the other two ways the source leaves the
  set: mute the open alert from the modal, and load `/dashboard/alert/<fp>` directly while
  a dashboard filter (`severities=`, `alertNames=`) excludes that alert. In all three the
  open alert must be absent from every count while the groups themselves are unchanged.
- **Hostile-label check.** `POST /api/v2/alerts` an alert whose labels include an
  empty-valued one (`{"alertname":"EmptyLabelProbe","cluster":"", ...}`), open it, and
  confirm Related shows **no** `cluster` group at all — in particular not a top-ranked
  `cluster= (N firing)` where `N` counts every alert that simply has no `cluster` label.
  Cross-check `N` against the table filtered on that value before and after.
- **Storm regression check, the other one that has bitten this design.** With eight
  alerts sharing `alertname`/`cluster`/`namespace`, filter the dashboard on
  `alertNames=<that name>` so the table shows only the storm, open one of them, and
  confirm Related still lists groups (previously: an empty "nothing correlates" state).
  Repeat at 3, 6, 7 and 8 rows — the old cliff was at 7 — and confirm the panel is
  non-empty at every size, with the whole-set groups captioned and ranked last.
- **Hidden-mode check (goal 6).** Hide five firing alerts that share labels, switch the
  dashboard to **Hidden**, open one of them from that table, and confirm Related lists
  only *unmuted* alerts and that none of the five muted ones appears in any group or
  count — **including the one that is open**, which is the muted alert the old `+1`
  silently added back to every header. Read each header against the *classic* table's row
  count for that value and require exact equality, not off-by-one — that is the whole of
  goal 6. The panel is the classic-mode answer, per step 4b, and never a
  mirror of the mute list. Also confirm a hidden alert never appears in a group in classic mode
  (hide an alert that would otherwise correlate, re-open Related, confirm it drops out
  and the count decrements). Same for a search/severity filter left active on the
  dashboard.
