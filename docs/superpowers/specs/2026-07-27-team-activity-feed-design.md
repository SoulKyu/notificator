# Spec: team activity feed (`/activity`)

- Issue: [SoulKyu/notificator#77](https://github.com/SoulKyu/notificator/issues/77)
- Date: 2026-07-27
- Status: designed (brainstormed, POC-validated)

## Problem

Everything the team does to alerts — acknowledgements, comments, silences, resolves — is
recorded, but only readable **one alert at a time** inside the alert detail modal
(`GetComments`/`GetAcknowledgments` are keyed by `alert_key`). There is no cross-alert view.

Consequences during a multi-alert incident:
- No way to answer "is anyone already on this?" without opening alert modals one by one.
- Someone paged in mid-incident cannot catch up on the preceding activity.
- The post-mortem timeline exists in the database but must be reassembled by hand.

## Key discovery that shapes this design

**Every collaboration action already writes a row in `comments`:**

| Action (dashboard) | Writes |
|---|---|
| acknowledge | `acknowledgments` row **and** comment `🔔 Alert acknowledged: <reason>` |
| unacknowledge | deletes `acknowledgments` row **and** comment `🔕 Alert unacknowledged: <reason>` |
| resolve | comment `✅ Alert resolved: <reason>` (resolve is cache-only UI state, no table) |
| silence (from an alert) | comment `🔇 Alert silenced for <d>: <reason>` |
| human comment | plain comment |

So the feed can be built from a **single source, the `comments` table**, if each comment
carries a structured `kind`. This avoids merging two tables and avoids the double-count an
acknowledgement would otherwise produce (its `acknowledgments` row *and* its `🔔` comment
represent the same act). This supersedes the original issue's two-source (`comments` +
`acknowledgments`) merge design.

Today, system comments are distinguished only by an emoji prefix in free text, and the
modal's `comment.isSystem` badge (`modal_components.templ:1300`) is **dead code** — the
field is never populated anywhere. The `kind` column fixes both: it powers the feed and
finally makes that badge work.

## Goals

1. A dedicated **`/activity`** page (a `PageNavigator` entry alongside Alerts / Silences /
   Statistics), showing one chronological, newest-first log of every ack, unack, comment,
   silence and resolve across all alerts.
2. The five event kinds are visually distinct (color + icon), system events (ack / unack /
   silence / resolve) distinguishable from human comments.
3. Filters: reuse the dashboard's alert-level filter UI and matching semantics
   (`alertmanagers`, `severities`, `teams`, `alertNames`), plus activity-specific filters
   (time window, action type, Mine/Everyone, text search).
4. Clicking a row opens that alert's modal via the existing `/dashboard/alert/:fingerprint`
   deep link.
5. The page refreshes while open (30s poll, only when visible) so it works as a live
   war-room log and doubles as a copyable post-mortem timeline.

## Non-goals / out of scope

- **No new audit-log table, no new write path** — the feed is a read over existing comments.
- **No new SSE channel or WebSocket, no browser notifications** — polling while the page is
  open is enough for the first cut.
- **Standalone `/silences` silences are not in the feed.** Silences authored on the `/silences`
  page (#130, not tied to an alert) do not write an alert-scoped comment, so they do not
  appear. Only silences created from an alert (the dashboard bulk *silence* action) do. This
  is intentional: the feed is "what the team did *to these alerts*".
- No `kind` backfill migration for pre-existing comments (see Legacy handling).
- No @mentions, replies, reactions, presence, or export/post-mortem file generation.
- No retention/purging beyond whatever already applies to comments.
- The `statuses` dashboard filter (active/suppressed/silenced) is not reused — it is a live
  alert state, meaningless on a historical event.

## Design

### 1. Data model — single source `comments` + `kind`

- Add `kind` to the `Comment` GORM model (`internal/backend/models/models.go`) and the
  `Comment` proto message (`proto/alert.proto`). Values: `comment` (human, default), `ack`,
  `unack`, `silence`, `resolve`.
- Add `kind` to `AddCommentRequest` (proto). The webui client keeps
  `AddComment(sessionID, fingerprint, content)` defaulting `kind="comment"`, and gains
  `AddSystemComment(sessionID, fingerprint, kind, content)` used by the four audit call sites
  in `processAlertAction` / `processSilenceAction` (`dashboard_handlers.go`).
- **Modal badge**: repoint `comment.isSystem` rendering onto `kind != "comment"` so the
  existing (currently dead) "System" badge starts working. The webui `Comment` wire model /
  handler that feeds the modal gains the `kind` field.
- **Legacy handling (no backfill)**: rows written before this change have `kind=""`. At read
  time only, when `kind==""`, derive it from the emoji prefix (`🔔`→ack, `🔕`→unack,
  `🔇`→silence, `✅`→resolve, else comment). New rows use the authoritative column; old rows
  degrade gracefully. Emoji-sniffing is confined to this legacy read fallback, never the
  primary path.

### 2. Backend — one read-only RPC

`AlertService` (`proto/alert.proto`, `internal/backend/services/services.go`):

```
rpc GetRecentActivity(GetRecentActivityRequest) returns (GetRecentActivityResponse);

GetRecentActivityRequest { string session_id; google.protobuf.Timestamp since;
                           int32 limit; repeated string kinds; repeated string alert_keys; }
ActivityEvent { string id; string alert_key; string kind; string user_id;
                string username; string content; google.protobuf.Timestamp created_at; }
GetRecentActivityResponse { repeated ActivityEvent events; }
```

- **Session validation is manual and mandatory.** The backend has no auth interceptor —
  `GetComments` today validates nothing. This is the first `AlertService` RPC to gate on
  identity: an empty/invalid `session_id` returns `codes.Unauthenticated`. (A broader "the
  whole AlertService is unauthenticated" hardening is noted as a separate follow-up, not part
  of this issue.)
- `limit` clamped server-side: default 100, max 200, so a hostile/buggy client cannot pull
  the whole table.
- Query: single indexed select over `comments` — `WHERE created_at >= since [AND kind IN
  kinds] [AND alert_key IN alert_keys] ORDER BY created_at DESC LIMIT n`, joined to `users`
  for `username`. `kind` resolved via the column, falling back to emoji-prefix derivation for
  legacy `kind==""` rows.

### 3. Migration

`internal/backend/database/migrate.go`: add an index on `comments.created_at`. Today only
`alert_key` is indexed, so a time-ordered global scan would table-scan. Nothing on
`acknowledgments` (single-source = comments).

### 4. Filters — genuine reuse, honest semantics

- **UI**: reuse `@components.FilterDropdown(filterType, title)`
  (`internal/webui/templates/components/filter_components.templ`) verbatim for
  `alertmanagers` / `severities` / `teams` / `alertNames`. The `/activity` Alpine component
  exposes the same `filters{}` shape, `availableFilters` metadata, and `clearFilter` /
  `applyFilters` methods the dropdown reads through `$parent`.
- **Matching predicate**: extract the per-alert "does this alert pass these filters?" logic
  out of `applyDashboardFilters` (`dashboard_handlers.go:400`) into a shared function called
  by both the dashboard and the activity handler. Guarantees identical semantics, no copy.
- **Uncached events**: alert-level filters are resolved by looking up each event's `alert_key`
  in `alertCache`. When **no** alert-level filter is active, all events pass (full history,
  including events whose alert has since resolved/expired and left the cache). When an
  alert-level filter **is** active, events whose alert is no longer cached are hidden (they
  cannot be evaluated). Predictable and stated in the UI.
- **Activity-specific filters** (no dashboard equivalent): time window (15m / 1h / 8h / 24h,
  drives `since`), action type (`kinds`), Mine/Everyone (by `user_id`), and text search over
  comment content + username + resolved alert name.

### 5. WebUI — handler + route

- `GET /api/v1/dashboard/activity?since=&limit=&kinds=&alertmanagers=&severities=&teams=&alertNames=&scope=&search=`
  in a new `internal/webui/handlers/activity_handlers.go`, registered in the existing
  `dashboard` group in `internal/webui/router.go`.
- Calls the backend `GetRecentActivity`, then for each event resolves `alert_key` →
  alert name + labels via `alertCache` (feed reads in alert names, not fingerprints; degrades
  to the raw key with an `uncached` marker when the alert is gone). Applies the shared
  alert-level filter predicate + the activity-specific filters.
- Thin `GetRecentActivity(...)` wrapper in `internal/webui/client/backend_client.go` following
  the existing `GetComments` shape.

### 6. Frontend — `/activity` page, log-table layout (POC variant C)

- New page templ following `Silences.templ` / `StatisticsDashboard.templ`: sticky header,
  `PageNavigator` entry, then the **log table** — columns Time · User · Action · Alert ·
  Detail, grouped by day, newest first. Action is a color-coded pill (ack green, unack gray,
  comment blue, silence amber, resolve emerald). Rows are clickable → `/dashboard/alert/:fingerprint`.
  Uncached alerts render the raw key with an `uncached` tag.
- Filter bar above the table: the reused `FilterDropdown`s + the activity-specific controls
  (time-window segmented control, action-type chips, Mine/Everyone toggle, search box) + a
  "live · refreshes every 30s" indicator.
- Alpine mixin following the existing pattern
  (`Object.assign(this, window.dashboardActivityMixin || {})`, cf. `dashboard_core.templ:287`)
  in a new `dashboard_activity.templ` script; a panel/page component.
- **Polling**: every 30s, **only while the page is visible** (`document.visibilityState`),
  stopped otherwise. Deliberately no second SSE channel; the existing alert SSE stream is
  untouched.
- `.templ` sources only; regenerate with `make webui-templates` (never hand-edit `*_templ.go`).
  Regenerate proto with `make proto` (never hand-edit `*.pb.go`).

### 7. Error handling

- Backend query failure → `codes.Internal`; the webui handler surfaces a non-OK and the page
  shows an error banner (it does not silently render an empty feed — same lesson as #126).
- Missing/invalid session → `codes.Unauthenticated` → the page treats it as a normal
  auth-expiry redirect.
- Alert no longer cached → not an error: render the raw `alert_key` with the `uncached` tag.

### 8. Testing

- Backend: `GetRecentActivity` returns events newest-first within the window; `limit` clamped
  to the server max; missing/invalid `session_id` rejected; `kinds` filter narrows results;
  legacy `kind==""` row resolved via emoji-prefix fallback.
- Shared filter predicate: one test asserting the dashboard path and the activity path match
  identically for the same alert + filter set (proves the extraction did not change behavior).
- WebUI handler: alert-name resolution from cache, `uncached` fallback, pass-through vs.
  hide-on-active-filter for uncached events.

## Acceptance criteria

- [ ] `kind` column added to comments (model + proto + `AddCommentRequest`); the four audit
      call sites write their kind via `AddSystemComment`; human comments default to `comment`.
- [ ] The modal's `isSystem` badge renders for `kind != "comment"` (previously dead).
- [ ] `GetRecentActivity` rejects a missing/invalid `session_id` and clamps `limit` to a
      server-side maximum.
- [ ] `GET /api/v1/dashboard/activity` returns acks, unacks, comments, silences and resolves
      from all alerts within the requested window, newest first, each with username, alert key,
      resolved alert name and timestamp.
- [ ] Legacy comments (no `kind`) are categorised via the emoji-prefix read fallback.
- [ ] The `/activity` page renders the log-table layout, is reachable from `PageNavigator`,
      and shows the last hour by default.
- [ ] The reused `FilterDropdown`s filter the feed by alertmanager/severity/team/alertName
      using the shared predicate; a Mine/Everyone toggle, action-type filter, time-window
      control and text search all work.
- [ ] With an alert-level filter active, events whose alert is no longer cached are hidden;
      with none active, they are shown with the `uncached` fallback label.
- [ ] Clicking a row opens the corresponding alert modal via `/dashboard/alert/:fingerprint`.
- [ ] The page polls every 30s while visible and stops when hidden/closed (verifiable in the
      network tab).
- [ ] Index on `comments.created_at` created by the migration; the feed query does not scan
      the whole table.
- [ ] `make proto` and `make webui-templates` regenerated; no hand-edited generated files.
- [ ] Standalone `/silences` silences are confirmed absent from the feed (documented limitation).
