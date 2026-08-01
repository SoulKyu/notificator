# feat(dashboard): acknowledgement ownership and ack age

Issue: [SoulKyu/notificator#90](https://github.com/SoulKyu/notificator/issues/90)

## Problem

`GetDashboardData` (`internal/webui/handlers/dashboard_handlers.go:121`) excludes acknowledged
alerts from `classic` mode (`getStandardAlerts`, `dashboard_handlers.go:353`) — the default view
everyone lives in. The moment an alert is acked it disappears behind the `acknowledge` display
mode, and nothing in the UI surfaces what happened to it:

- The ack block (`IsAcknowledged` / `AcknowledgedBy` / `AcknowledgedAt` / `AcknowledgeReason`,
  `internal/webui/models/dashboard.go:26-29`, populated in
  `internal/webui/services/alert_cache.go:602-604`) is already on every cached alert, but none of
  the 12 default columns (`getDefaultColumns`,
  `internal/webui/templates/scripts/dashboard_utilities.templ:649-663`, orders `0-11`) render them.
- `Duration` (`models/dashboard.go:44`) is fire-time, not ack age — an alert acked 8h ago and one
  acked 2m ago look identical.
- The acked view (`getAcknowledgedAlerts`, `dashboard_handlers.go:366`) has no ordering or
  worklist semantics — no way to answer "what did the last shift take and never finish?".
- `processAlertAction` (`dashboard_handlers.go:936`) stores the acking **user ID**
  (`a.AcknowledgedBy = userID`, `dashboard_handlers.go:970`), while the cache refresh path later
  overwrites it with the **username** (`acknowledgment.Username`, `alert_cache.go:602`). Any column
  built on this field shows an opaque ID for ~10s after acking, then a username — a bug independent
  of this feature, but one this feature makes visible and must fix first.

## Goals

1. Surface **who** acknowledged an alert and **how long ago**, as opt-in columns, without
   changing anyone's existing saved column layout.
2. Give the acked view a **stale-ack** signal (per-user threshold, default 4h) so an
   ack-and-forget alert doesn't silently sit for a whole shift.
3. Let a user filter the acked view down to **their own** acknowledgements (shift handover /
   "what am I on the hook for").
4. Fix the ID-vs-username inconsistency so the new Owner column is correct immediately, not
   10 seconds later.

## Non-goals (explicitly out of scope, per issue)

- Assigning/re-assigning an alert to another user, or any escalation. Acknowledgement stays the
  only ownership primitive.
- Notifications/paging for stale acks — this is a passive badge + sort, not an alert.
- Auto-expiring or auto-unacking stale acknowledgements.
- On-call schedule / roster integration.
- Changing `classic` mode's exclusion of acked alerts.
- Making the Settings modal persist general settings server-side (see §4 — the threshold is
  designed around what the modal actually does today, not around fixing it).

## Approach

WebUI-only. `DashboardAlert` already carries every field needed (`models/dashboard.go:26-29`) and
is already serialized to the browser — no proto change, no new RPC, no new persistence table.

Two client/server mirrors decide most of the design below, and both have burned this repo before:

- **Every filter predicate exists twice** — server `applyDashboardFilters`
  (`dashboard_handlers.go:422`) and client `alertMatchesFilters` (`dashboard_data.templ:514`).
  Documented as the #1 dashboard footgun (`openwiki/dashboard.md:244`).
- **Every sort exists twice** — server `applySorting` (`dashboard_handlers.go:541`) and client
  `sortAlerts` (`dashboard_data.templ:467`), the latter re-applied on every live update.

Anything this feature adds to one half must be added to the other half in the same change.

### 1. Prerequisite fix — ack username at ack time

`processAlertAction` (`dashboard_handlers.go:936`, case `"acknowledge"`) sets
`a.AcknowledgedBy = userID` (`:970`) where `userID` comes from `getCurrentUserID(c)`
(`dashboard_handlers.go:314`), which returns `middleware.GetEffectiveUserID(c)`
(`internal/webui/middleware/auth.go:194`) — an opaque ID, not a display name. Resolve the acting
username the same way `middleware.GetCurrentUser(c)` does (session `username` key,
`internal/webui/middleware/session.go:59-61`), respecting impersonation
(`GetImpersonatedUsername`, `middleware/session.go:111`), and store that in `AcknowledgedBy`
instead. This makes the field consistent with what `alert_cache.go:602` already writes 10s later on
the next cache refresh, so nothing downstream needs to special-case "ID now, username later".

### 2. Two new opt-in system columns

`getDefaultColumns()` (`dashboard_utilities.templ:649`) gets two entries appended, both
`visible: false` so existing saved configs are unaffected:

```js
{id: "col_owner", label: "Owner", field_type: "system", field_path: "acknowledgedBy",
 formatter: "text", width: 150, sortable: false, visible: false, order: 12, resizable: true, critical: false},
{id: "col_ack_age", label: "Ack Age", field_type: "system", field_path: "acknowledgedAt",
 formatter: "ackage", width: 130, sortable: true, visible: false, order: 13, resizable: true, critical: false},
```

`sortable: false` on `col_owner` is deliberate, not an oversight: `sortByColumn(column)`
(`dashboard_utilities.templ:899-914`) sends `column.field_path` verbatim as `sortField`, so a
sortable Owner header would issue `sortField=acknowledgedBy`, hit the `default:` branch of
`applySorting` (`dashboard_handlers.go:565-567`) and silently sort by **Duration**. The issue only
requires Ack Age to be sortable; adding an `acknowledgedBy` sort would mean a third pair of
server+client cases for no requirement.

`mergeSystemColumns()` (`dashboard_utilities.templ:638`) already back-fills newly added system
columns into saved configs by `id` — no extra work needed for existing users to see them appear
(hidden) in the Column Config modal.

Add an `ackage` branch to `renderCell()`'s formatter switch (`dashboard_utilities.templ:666`):
renders relative age (reuse the existing `formatDuration`-style logic already used client-side for
`Triggered At` / `Duration`, e.g. "3m", "8h", "2d") from `acknowledgedAt`, blank for
non-acknowledged alerts. **Guard against Go's zero time value** (`"0001-01-01T00:00:00Z"`) which is
truthy in JS — `omitempty` does not drop a zero `time.Time`, so the API really does emit it for
every un-acked alert, and the existing `renderTimestamp` guard (`dashboard_utilities.templ:809`,
`if (!timestamp)`) does not catch it. Use `if (!timestamp || timestamp.startsWith("0001"))` or
similar. On hover, a `title` attribute with `alert.acknowledgeReason`.

The amber stale marker (Ack Age cell only) is computed client-side from
`this.settings.staleAckThresholdMinutes` — the same number the browser sends to the server for the
§5 badge, so the two cannot disagree (see §4/§5).

### 2b. Grouped view

`renderCell` / `visibleColumns` exist **only** in `dynamic_alerts_table.templ` and the scripts. The
grouped view renders `@components.AlertsGroupView()` (`NewDashboard.templ:879`), whose per-group
table is a hardcoded 5-column `Alert / Instance / Status / Duration / Actions`
(`internal/webui/templates/components/group_components.templ:109-113`, rows at `:117`). It never
touches the column system, so §2 alone has **zero** effect there — yet the issue's AC 1 asks for
Owner + Ack Age "in both flat and grouped views".

Add the two cells explicitly to `group_components.templ`, gated on the user's own opt-in so the
grouped table stays 5 columns for anyone who hasn't enabled them:

```html
<th x-show="visibleColumns.some(c => c.id === 'col_owner')" …>Owner</th>
<th x-show="visibleColumns.some(c => c.id === 'col_ack_age')" …>Ack Age</th>
```

with the matching `<td>`s in the `x-for` at `:117` reusing the same `renderCell(alert, column)`
helper (`x-html`), so the ack-age formatting and the zero-time guard have exactly one
implementation. If AC 1 gets amended to scope grouped views out, delete this section — but do not
leave §Validation asking for a grouped-view check that the code cannot satisfy.

### 3. Sorting by ack age — both halves

**Server.** `applySorting()` (`dashboard_handlers.go:541`) gets one more `case` in the
`sorting.Field` switch (`:548-568`):

```go
case "acknowledgedAt":
    less = sorted[i].AcknowledgedAt.Before(sorted[j].AcknowledgedAt)
```

This runs before `applyPagination` (`dashboard_handlers.go:524`, called at `:189` after
`applySorting` at `:183`), so it composes with pagination without special-casing.

**Client — mandatory, not optional.** `sortAlerts()` (`dashboard_data.templ:467`) is an independent
re-implementation of the same switch, and its `case 'duration':` falls through from `default:`
(`:497-498`). It is re-applied to `this.alerts` on **every** live update that adds rows
(`dashboard_data.templ:404` and `:415`). Without the mirrored case, an acked-mode worklist sorted
`acknowledgedAt asc` silently re-orders itself by duration the first time an SSE push lands — the
same footgun class as §6's filters, one function away:

```js
case 'acknowledgedAt':
    aVal = new Date(a.acknowledgedAt).getTime();
    bVal = new Date(b.acknowledgedAt).getTime();
    break;
```

Both halves order zero times (`0001-01-01`) first in `asc`. That is only reachable outside
`acknowledge` mode (where every row is acked by construction), so no extra guard is needed — but
don't "fix" it in one half only.

**Default sort when entering `acknowledge` mode:** `acknowledgedAt` ascending (oldest/stalest
first), set client-side in `setDisplayMode('acknowledge')` (`dashboard_core.templ:457-482`) next to
the existing `currentPage = 1` reset, before the `loadDashboardData()` call at `:474`. This is a
one-time initialization; unlike `DefaultSorting` (a stored `DashboardSettings` field), ack-mode
sorting is a fixed UX choice.

### 4. Stale threshold — a client-side setting, like `resolvedAlertsLimit`

**Do not add a field to `DashboardSettings`** (`models/dashboard.go:115-124`) and do not route this
through `SaveDashboardSettings`. That path does not work today and this feature is not fixing it:

- The "💾 Save All Settings" button (`modal_components.templ:638`) resolves to the settings modal's
  own `saveSettings()` (`dashboard_settings.templ:455`), which writes
  `localStorage.setItem('dashboardSettings', …)` (`:468`) and then only POSTs colors, hidden rules
  and notification preferences. It never calls `POST /api/v1/dashboard/settings`.
- The dashboard's other `saveSettings()` (`dashboard_utilities.templ:199`) is the one that would
  POST — and it has no caller.
- Consequence: `getUserSettings(userID)` (`dashboard_handlers.go:325`) returns its hard-coded
  defaults (`:333-345`) forever, and `userSettings` (`dashboard_handlers.go:29`) is never written
  for a real user. A Go field here would be a field the server never receives.

Instead, follow `resolvedAlertsLimit`, which is exactly this shape and already works end to end:

| step | `resolvedAlertsLimit` | `staleAckThresholdMinutes` |
|---|---|---|
| client default | `dashboard_core.templ:35` (`settings` object, `:32-36`) | add `staleAckThresholdMinutes: 240` |
| modal input | `modal_components.templ:91-93`, labelled "(stored locally)" `:96` | new number input next to it, same label wording |
| persisted | `localStorage` via `dashboard_settings.templ:468`, re-read by `loadSettings()` (`dashboard_utilities.templ:185-197`) | identical, no code needed |
| reaches the server | query param, set at `dashboard_data.templ:34-36` | same, in all three query builders |
| parsed | `dashboard_handlers.go:249-253` | same block |
| carried | `DashboardFilters.ResolvedAlertsLimit` (`models/dashboard.go:95`) | `StaleAckThresholdMinutes int \`json:"staleAckThresholdMinutes,omitempty"\`` |

Notes:

- Use `x-model.number` (or coerce with `Number(...)` on read). Plain `x-model` on an
  `<input type="number">` stores a **string** — `settings.refreshInterval` is `"60"` today.
- Default `240` (4h); `0` = never stale, everywhere. `0` must survive the round-trip, so send the
  param unconditionally rather than behind an `if (value > 0)` truthiness test — this is the one
  place where copying `resolvedAlertsLimit`'s param code verbatim (`dashboard_data.templ:34`) would
  be wrong.
- All three client query builders must set it, or the badge flips between the user's value and the
  default depending on which request last landed: `loadDashboardData` (`dashboard_data.templ:10`),
  `loadDashboardIncremental` (`:102`), `loadAlertColors` (`:193` — harmless but keeps the three in
  sync).
- `this.settings` is overwritten by the server on every load
  (`this.settings = { ...this.settings, ...result.data.settings }`, `dashboard_data.templ:68`).
  Keeping the threshold **off** `DashboardSettings` is what stops the server from clobbering the
  user's value with a default — the same reason `resolvedAlertsLimit` isn't a Go settings field.

Putting it on `DashboardFilters` rather than threading a new parameter through
`buildDashboardMetadata` / `getDashboardMetadata` / `processIncremental` costs one line in the
guard test (see §6) and zero signature changes, since `filters` already reaches every metadata
call site.

### 5. Stale count on the Acknowledged mode button

The badge must be server-computed: the button is visible from `classic` mode, where the browser
holds **no** acked alerts at all, and elsewhere it only holds the current page — the client
physically cannot count the acked set. `buildDashboardMetadata()` (`dashboard_handlers.go:735`)
already receives `filteredAlerts` **pre-pagination** (`:180-189`, call at `:212`) plus `filters`,
so it has both the rows and the threshold.

Add `StaleAcknowledged int` to `DashboardCounters` (`models/dashboard.go:173`) and increment it
alongside `counters.Acknowledged` (`dashboard_handlers.go:767`) and in the classic-mode recount
(`:776-784`), using `filters.StaleAckThresholdMinutes` (`0` → never increment). Both metadata call
sites are covered without a signature change: `GetDashboardData` (`:212`) and the incremental path
`getDashboardMetadata` (`:1159` → `:1187`, called from `processIncremental` at `:1319`, shared by
`PostDashboardIncremental` `:1190` and `GetDashboardIncremental` `:1235`). SSE pushes carry no
metadata (`sse_handler.go`), so there is no fourth path.

**Why this cannot drift from §2's client-side markers:** there is one value, owned by the browser.
The client renders amber rows from `this.settings.staleAckThresholdMinutes` and sends that same
number on the request whose response produces the badge. Threshold `0` disables both at once. The
server never stores a threshold, so it cannot hold a stale one.

Render `Acked · 3 stale` on the display-mode button (`NewDashboard.templ:137-141`) — append the
count only when `metadata.counters.staleAcknowledged > 0`.

### 6. Mine / Everyone toggle

New filter dimension, mirroring the existing boolean-filter pattern (`Acknowledged *bool`,
`models/dashboard.go:91`):

```go
OwnedByMe *bool `json:"ownedByMe,omitempty"` // nil = everyone, true = only current user's acks
```

- Parsed in `parseDashboardFilters()` (`dashboard_handlers.go:223`, alongside the existing
  `acknowledged` / `hasComments` bool parsing at `:236-246`).
- Applied in `applyDashboardFilters()` (`dashboard_handlers.go:422`): when set, drop alerts where
  `alert.AcknowledgedBy != currentUsername`. Needs the *username*, not the user ID that
  `applyDashboardFilters` currently has available — plumb it through the same way `sessionID` is
  already threaded into this function today.
- **Must be mirrored in FOUR places:**
  1. `alertMatchesFilters()` (`dashboard_data.templ:514`), the client-side predicate used to
     accept/reject SSE updates at `:376` / `:384` / `:410` — filters that only exist server-side go
     stale on every live update because SSE pushes arrive unfiltered and the client decides locally
     whether to keep them (`openwiki/dashboard.md:244`).
  2. `filtersAffectResolvedCount()` (`dashboard_handlers.go:383`) — its own code comment says
     *"Mirrors every rejection in applyDashboardFilters."* `OwnedByMe` **can** exclude a resolved
     alert, so it belongs in the predicate.
  3. `TestFiltersAffectResolvedCountCoversEveryFilterField`
     (`internal/webui/handlers/dashboard_resolved_count_test.go:79`) reflects over
     `DashboardFilters` against a hand-written `known` map (`:80-87`). It hard-fails on **any** new
     field, so mirroring `filtersAffectResolvedCount` alone will not turn CI green — both
     `OwnedByMe` (predicate) and `StaleAckThresholdMinutes` (non-predicate, next to
     `"ResolvedAlertsLimit"` on `:86`) must be added to that map in the same commit.
  4. Filter presets, **both directions**: `captureCurrentFilterState()`
     (`dashboard_filter_presets.templ:287-306`) as `owned_by_me`, **and** the restore side
     `applyFilterPreset()` (`:163-210`, filter assignments at `:172-177`). Capture without restore
     saves a preset that silently drops the toggle on load. Note there is no precedent here for a
     `*bool`: today's presets round-trip only strings and arrays, so pick and document a nil
     encoding (omit the key for "Everyone") rather than assuming `false` means nil.
- UI: a small toggle in the acked-view toolbar, visible only when `displayMode === 'acknowledge'`.

### Build step

`make webui-templates` after every `.templ` edit. Never hand-edit the generated `*_templ.go`
files.

## Risks / trade-offs

- **The threshold is per-browser, not per-account.** It lives in `localStorage`, so the same user
  on a second machine gets the `240` default until they set it there too. Accepted: it matches
  `resolvedAlertsLimit`, it is the only storage the Settings modal actually writes (§4), and it is
  what makes the client marker and the server badge provably agree. Making settings durable is a
  separate change to the whole modal, out of scope here.
- **Threshold `0` and truthiness.** `0` is the "never stale" value and is falsy in JS. Every
  read/send path must test `!== undefined` / `!== ''`, not truthiness, or "off" degrades into "4h"
  on the server while the client shows no markers — the exact mismatch AC "threshold off yields no
  markers anywhere" is written to catch.
- **Username resolution at ack time under impersonation.** Must use the *impersonated* username
  (`GetImpersonatedUsername`), matching what `getCurrentUserID`/`GetEffectiveUserID` already do for
  the ID side, or an admin acking while impersonating attributes the ack to themselves.
- **`OwnedByMe` needs a username, `applyDashboardFilters` currently only has a user ID/session.**
  Small plumbing change (pass username alongside `sessionID`) rather than a redesign — same shape
  as the existing `sessionID` parameter.
- **Two more entries in the client/server mirror inventory** (`sortAlerts` §3, `alertMatchesFilters`
  §6). They are listed explicitly above precisely because "server-side only" reads as done in
  review and breaks on the first live update.

## Validation

- `go build -tags "nogui,webui" .`, `go vet ./...`, and **`go test ./internal/webui/...`** after
  backend changes. `TestFiltersAffectResolvedCountCoversEveryFilterField` will fail until both new
  `DashboardFilters` fields are added to its `known` map — that failure is expected and is the
  §6 checklist enforcing itself.
- `make webui-templates` regenerates cleanly with no diff drift in unrelated templates.
- Manual pass against the acceptance criteria in the issue:
  - Enable Owner + Ack Age columns → correct username + relative age, in the flat view **and** in
    the grouped view (§2b — if this section was dropped, drop this check too).
  - Ack an alert from the dashboard → Owner shows the right username immediately, not an opaque ID
    that changes ~10s later.
  - Sort by Ack Age both directions, across a page boundary, **then leave the tab open until an SSE
    update lands and re-check the order** (this is what catches a missing `sortAlerts` case).
  - Owner column header is not clickable (`sortable: false`) — clicking it must not re-sort by
    Duration.
  - Threshold at 4h: an alert acked 5h ago is marked stale, one acked 5m ago is not.
  - Set the threshold to `0` → no amber markers **and** no `· N stale` on the Acknowledged button,
    from `classic` mode as well as from `acknowledge` mode.
  - Acknowledged mode button's stale count equals the number of amber rows in that mode, including
    when the acked set spans more than one page.
  - Mine shows only the current user's acks, and stays correct after a live SSE update lands
    (verify `alertMatchesFilters` was actually updated, not just the server side).
  - Save a preset with Mine on, switch it off, re-apply the preset → Mine comes back on.
  - Both new columns default to invisible for an existing saved column layout, and appear
    (unchecked) in the Column Config modal without disturbing the rest of that layout.
