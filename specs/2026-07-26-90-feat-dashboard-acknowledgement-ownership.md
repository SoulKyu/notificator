# feat(dashboard): acknowledgement ownership and ack age

Issue: [SoulKyu/notificator#90](https://github.com/SoulKyu/notificator/issues/90)

## Problem

`GetDashboardData` (`internal/webui/handlers/dashboard_handlers.go:114`) excludes acknowledged
alerts from `classic` mode (`getStandardAlerts`, `dashboard_handlers.go:335`) — the default view
everyone lives in. The moment an alert is acked it disappears behind the `acknowledge` display
mode, and nothing in the UI surfaces what happened to it:

- `AcknowledgedBy` / `AcknowledgedAt` / `AcknowledgeReason` are already on every cached alert
  (`internal/webui/models/dashboard.go:24-27`, populated in
  `internal/webui/services/alert_cache.go:602-604`) but none of the 11 default columns
  (`getDefaultColumns`, `internal/webui/templates/scripts/dashboard_utilities.templ:650-663`)
  render them.
- `Duration` (`dashboard.go:42`) is fire-time, not ack age — an alert acked 8h ago and one acked
  2m ago look identical.
- The acked view (`getAcknowledgedAlerts`, `dashboard_handlers.go:359`) has no ordering or
  worklist semantics — no way to answer "what did the last shift take and never finish?".
- `processAlertAction` stores the acking **user ID** (`AcknowledgedBy = userID`,
  `dashboard_handlers.go:875`), while the cache refresh path later overwrites it with the
  **username** (`acknowledgment.Username`, `alert_cache.go:602`). Any column built on this field
  shows a numeric ID for ~10s after acking, then a username — a bug independent of this feature,
  but one this feature makes visible and must fix first.

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

## Approach

WebUI-only. `DashboardAlert` already carries every field needed
(`internal/webui/models/dashboard.go:24-27`) and is already serialized to the browser — no proto
change, no new RPC, no new persistence table.

### 1. Prerequisite fix — ack username at ack time

`processAlertAction` (`dashboard_handlers.go:866`, case `"acknowledge"`) sets
`a.AcknowledgedBy = userID` where `userID` comes from `getCurrentUserID(c)`
(`dashboard_handlers.go:307`), which returns `middleware.GetEffectiveUserID(c)` — a numeric/opaque
ID, not a display name. Resolve the acting username the same way
`middleware.GetCurrentUser(c)` does (session `username` key, `internal/webui/middleware/session.go:56`),
respecting impersonation (`GetImpersonatedUsername`, `middleware/session.go:110`), and store that
in `AcknowledgedBy` instead. This makes the field consistent with what `alert_cache.go:602`
already writes 10s later on the next cache refresh, so nothing downstream needs to special-case
"ID now, username later".

### 2. Two new opt-in system columns

`getDefaultColumns()` (`dashboard_utilities.templ:650`) gets two entries appended, both
`visible: false` so existing saved configs are unaffected:

```js
{id: "col_owner", label: "Owner", field_type: "system", field_path: "acknowledgedBy",
 formatter: "text", width: 150, sortable: true, visible: false, order: 12, resizable: true, critical: false},
{id: "col_ack_age", label: "Ack Age", field_type: "system", field_path: "acknowledgedAt",
 formatter: "ackage", width: 130, sortable: true, visible: false, order: 13, resizable: true, critical: false},
```

`mergeSystemColumns()` (`dashboard_utilities.templ:638`) already back-fills newly added system
columns into saved configs by `id` — no extra work needed for existing users to see them appear
(hidden) in the Column Config modal.

Add an `ackage` branch to `renderCell()`'s formatter switch (`dashboard_utilities.templ:669`):
renders relative age (reuse the existing `formatDuration`-style logic already used client-side for
`Triggered At` / `Duration`, e.g. "3m", "8h", "2d") from `acknowledgedAt`, blank for
non-acknowledged alerts. On hover, a `title` attribute with `alert.acknowledgeReason`.

The stale marker (amber, on the Ack Age cell only) is computed client-side against
`window.currentSettingsModal`'s threshold (see §4) — no new field needed on `DashboardAlert`.

### 3. Sorting by ack age

`applySorting()` (`dashboard_handlers.go:532`) gets one more `case` in the `sorting.Field` switch:

```go
case "acknowledgedAt":
    less = sorted[i].AcknowledgedAt.Before(sorted[j].AcknowledgedAt)
```

This is server-side, so it composes with the existing pagination (`applyPagination`,
`dashboard_handlers.go:515`) without special-casing.

Default sort when entering `acknowledge` mode: sort by `acknowledgedAt` ascending (oldest/stalest
first) — set client-side in `setDisplayMode('acknowledge')` (`dashboard_actions.templ`), the same
place `DefaultSorting` (`dashboard.go` `DashboardSettings.DefaultSorting`) is normally read from.

### 4. Stale threshold — per-user setting, no new table

`DashboardSettings` (`dashboard.go:112-121`) gets one new field:

```go
StaleAckThresholdMinutes int `json:"staleAckThresholdMinutes"` // 0 = never stale
```

Persisted exactly like `RefreshInterval` today — in the existing in-memory `userSettings` map
(`dashboard_handlers.go:29`, keyed by user ID, written by `SaveDashboardSettings`,
`dashboard_handlers.go:1019`). No new table, no new endpoint. Default `240` (4h); `0` disables
staleness everywhere. Add the input to the Settings modal
(`internal/webui/templates/scripts/dashboard_settings.templ`) next to the existing refresh-interval
control, and read it into `window.currentSettingsModal` the same way sound paths are today.

### 5. Stale count on the Acknowledged mode button

`buildDashboardMetadata()` (`dashboard_handlers.go:665`) computes counters over the acked set
already (`counters.Acknowledged`, `dashboard_handlers.go:709` and the classic-mode special case at
`:718-727`). Add `StaleAcknowledged int` to `DashboardCounters` (`dashboard.go:170-180`) and
increment it in the same loop(s) using the user's `StaleAckThresholdMinutes` (threshold `0` →
never increment). This keeps the button badge and the in-view stale markers coming from one
authoritative pass instead of two divergent client/server computations.

Render `Acked · 3 stale` on the existing button (`NewDashboard.templ:119-123`) — append the count
only when `metadata.counters.staleAcknowledged > 0`.

### 6. Mine / Everyone toggle

New filter dimension, mirroring the existing boolean-filter pattern (`Acknowledged *bool`,
`dashboard.go:89`):

```go
OwnedByMe *bool `json:"ownedByMe,omitempty"` // nil = everyone, true = only current user's acks
```

- Parsed in `parseDashboardFilters()` (`dashboard_handlers.go:216`, alongside the existing
  `acknowledged` / `hasComments` bool parsing at `:228-239`).
- Applied in `applyDashboardFilters()` (`dashboard_handlers.go:356+`): when set, drop alerts where
  `alert.AcknowledgedBy != currentUsername`. Needs the *username*, not the user ID that
  `applyDashboardFilters` currently has available — plumb it through the same way `sessionID` is
  already threaded into this function today.
- **Must be mirrored in `alertMatchesFilters()`** (`dashboard_data.templ:501`), the client-side
  predicate used to accept/reject SSE updates (`dashboard_data.templ:372`, `:399`) — this is the
  documented #1 dashboard footgun (see `openwiki/dashboard.md`): a filter that only exists
  server-side goes stale on every live update, because SSE pushes arrive unfiltered and the client
  decides locally whether to keep them. This is called out explicitly in the acceptance criteria
  below.
- Included in `captureCurrentFilterState()` (`dashboard_filter_presets.templ:287`) as `owned_by_me`
  so it survives saved filter presets, same treatment as every other filter field there.
- UI: a small toggle in the acked-view toolbar, visible only when `displayMode === 'acknowledge'`.

### Build step

`make webui-templates` after every `.templ` edit. Never hand-edit the generated `*_templ.go`
files.

## Risks / trade-offs

- **Settings are in-memory, not durable.** `userSettings` (`dashboard_handlers.go:29`) already
  doesn't survive a webui process restart today (refresh interval, sound paths, etc. all reset to
  defaults) — the new stale threshold inherits that existing limitation. Explicitly not fixing
  this now; it's pre-existing behavior for the whole settings modal, not a regression introduced
  here.
- **Username resolution at ack time under impersonation.** Must use the *impersonated* username
  (`GetImpersonatedUsername`), matching what `getCurrentUserID`/`GetEffectiveUserID` already do for
  the ID side, or an admin acking while impersonating will attribute the ack to themselves instead
  of the impersonated identity — inconsistent with how the rest of the ack path already treats
  impersonation.
- **`OwnedByMe` needs a username, `applyDashboardFilters` currently only has a user ID/session.**
  Small plumbing change (pass username alongside `sessionID`) rather than a redesign — same shape
  as the existing `sessionID` parameter.
- **Client/server stale computation must not drift.** Both the row-level amber marker (client,
  computed against `acknowledgedAt` + settings threshold) and the button badge (server, in
  `buildDashboardMetadata`) implement "is this ack stale" independently. Any change to the
  threshold semantics (e.g. what "stale" means) has to be made in both places or the acceptance
  criterion "stale count matches marked rows" breaks.

## Validation

- `go build -tags "nogui,webui" .` and `go vet ./...` after backend changes.
- `make webui-templates` regenerates cleanly with no diff drift in unrelated templates.
- Manual pass against the acceptance criteria in the issue:
  - Enable Owner + Ack Age columns → correct username + relative age, flat and grouped views.
  - Ack an alert from the dashboard → Owner shows the right username immediately, not a numeric ID
    that later changes.
  - Sort by Ack Age both directions, across a page boundary.
  - Threshold at 4h: an alert acked 5h ago is marked stale, one acked 5m ago is not; threshold `0`
    → no markers anywhere.
  - Acknowledged mode button's stale count equals the number of amber rows in that mode.
  - Mine shows only the current user's acks, and stays correct after a live SSE update lands
    (verify `alertMatchesFilters` was actually updated, not just the server side).
  - Both new columns default to invisible for an existing saved column layout, and appear
    (unchecked) in the Column Config modal without disturbing the rest of that layout.
