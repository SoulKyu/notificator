# Live Alert Dashboard

The live dashboard (`/dashboard`) is the app's primary operational surface: the real-time table
of firing alerts with acknowledge / comment / silence / hide actions. This page documents its
internals. For the page's place in the wider system see [webui](webui.md); for the alert-cache /
SSE plumbing it rides on see [architecture](architecture.md#real-time).

**Source:** page `internal/webui/templates/pages/NewDashboard.templ`; logic in
`internal/webui/templates/scripts/dashboard_*.templ`; server in
`internal/webui/handlers/dashboard_handlers.go`. Never edit `*_templ.go` — see
[operations](operations.md#codegen).

## Client architecture: one component + manual mixins

The whole page is a single Alpine component, `newDashboard()`
(`scripts/dashboard_core.templ:5`), mounted with `x-data` on the outer div
(`NewDashboard.templ:14`). Its logic is split across script files that each attach methods to a
`window.dashboard*Mixin` global; `init()` merges them onto the one instance with
`Object.assign(this, window.dashboardDataMixin || {})` etc. (`dashboard_core.templ:287`). This is
a **hand-rolled mixin pattern, not an Alpine plugin.**

The scripts included at the bottom of the page (`NewDashboard.templ:986`):
`NotificationService`, `DashboardFilterPresetsMixin`, `DashboardResolvedAlertsMixin`,
`DashboardCore`, `DashboardData`, `DashboardActions`, `DashboardUtilities`, `DashboardModal`,
`DashboardSettings`.

`init()` (`dashboard_core.templ:287`) also sets **`window.dashboardInstance = this`** — a
load-bearing global that modals, dropdowns, the Sentry loader, and the logout handler all reach
into. Removing it silently breaks unrelated features. `init()` further installs a `fetch`
interceptor that redirects to `/login` on any 401, tracks first-load state in `sessionStorage`,
loads settings/columns/user/annotation-buttons, applies a default filter preset **or** URL
filters, starts SSE (or polling), and opens the alert modal if the URL is `/dashboard/alert/:id`.

## Data lifecycle

### Full load

`loadDashboardData()` (`scripts/dashboard_data.templ:6`) → `GET /api/v1/dashboard/data` →
`GetDashboardData` (`dashboard_handlers.go:112`). The server reads from the in-memory
**`alertCache`** (not a per-request Alertmanager call — see [webui](webui.md#alert-cache)); the
subset depends on `displayMode`:

| `displayMode` | Source |
|---------------|--------|
| `classic` | cached alerts that are **not** acknowledged and **not** resolved |
| `acknowledge` | acknowledged alerts |
| `resolved` | statistics "recently resolved" (see [statistics](statistics.md)) |
| `hidden` / `full` | active + resolved combined |

The server then applies filters (`applyDashboardFilters`, `dashboard_handlers.go:356`), sorting,
pagination, and either flat-list or group-by rendering. Filter **dropdown option lists** and
counters (`buildDashboardMetadata:628`) are computed from the *full* alert set for that display
mode, not the filtered page — so dropdowns don't shrink as you filter (intentional, looks like a
bug but isn't).

### Live updates (SSE) and the client-side merge

`initSSE()` (`dashboard_core.templ:458`) opens `EventSource('/api/v1/dashboard/stream')`; `update`
events go to `applyIncrementalUpdate()`. Server side, `SSEStream`
(`internal/webui/handlers/sse_handler.go:25`) subscribes to `alertCache.Subscribe()` and forwards
each change plus a 30s `ping`. On SSE error the client falls back to polling
`/api/v1/dashboard/incremental`. Both paths converge on **one merge function**,
`applyIncrementalUpdate(update, source)` — the `source` (`'sse'` vs `'poll'`) matters: on the SSE
path (genuine Alertmanager resolves) it evicts removed fingerprints from the notification seen-set
so re-fires re-notify; the poll path does not, because its `removedAlerts` conflate resolve with
filter-out. See [notifications](notifications.md#what-triggers-a-notification).

> ⚠️ **SSE broadcasts unfiltered, colorless data to every subscriber.** The server does no
> per-user filtering on the SSE path (unlike the polling path). Therefore the client re-applies
> filters itself via **`alertMatchesFilters()`** (`dashboard_data.templ:421`) — a full JS
> reimplementation of the server's `applyDashboardFilters`. **These two must be kept in sync:**
> any new server-side filter dimension that isn't mirrored in `alertMatchesFilters` silently
> breaks live updates for that filter. Colors aren't in SSE payloads either, so the client
> debounces a `loadAlertColors(true)` refetch after an SSE update — but the **initial** full-load
> response now embeds a `colors` map (`GetDashboardData` → `response.Colors`, applied before
> `this.alerts` is set), fixing first-paint color lag; the standalone `GET /alert-colors` endpoint
> remains as a fallback.

**Adaptive polling** (only when not on SSE, `dashboard_core.templ:506`): every 10 polls it slows
toward 60s if <10% of polls saw changes, or speeds toward 5s if >50% did.

## Filtering and grouping

Filter dimensions: free-text `searchQuery`; multi-select `severities`, `statuses`, `teams`,
`alertmanagers`, `alertNames`. Filtering runs **on both sides** — server for the authoritative
page, client for SSE/incremental merges.

> The **acknowledged / comments** checkbox filters were **removed from the UI** (their checkbox
> values never matched the comparison code, and multi-checkbox-to-one-`x-model` is invalid Alpine).
> The backend still parses `?acknowledged=` / `?hasComments=` in `applyDashboardFilters` — the
> capability is dormant (unreachable from the UI) but intact if called directly.

**Hidden alerts are two-tier:** global per-user hidden alerts/rules (via `hiddenAlertsService`,
Settings → Hidden tab) *and* filter-scoped hides stored inside a filter preset's `filter_data`.
See [domain](domain.md#collaboration-state-persisted-by-the-backend).

**Grouping** (`viewMode = 'group'`) renders `AlertsGroupView`; the group-by field
(`groupByLabel`, default `alertname`) is grouped server-side by `groupAlertsByLabel`
(`dashboard_handlers.go:559`) — supports `alertname`/`severity`/`team`/`instance` plus any
arbitrary label key (fallback bucket `"Other"`). Each group carries a `WorstSeverity`.

## The dynamic table and configurable columns

The table (`components/dynamic_alerts_table.templ`) is fully driven by `this.columns` /
`this.visibleColumns`. Cells are rendered by `renderCell(alert, column)`
(`dashboard_utilities.templ:655`), dispatching on `column.formatter`
(`checkbox|text|badge|duration|timestamp|count|actions`) to functions that emit HTML strings
inserted via `x-html`. `getFieldValue` resolves dotted `field_path` (e.g. `labels.environment`).

11 default columns (`getDefaultColumns:639`). The Column Config modal
(`components/column_config_modal.templ`) supports drag-reorder, visibility, width (50–800px),
and creating **custom columns** backed by any label/annotation. Preferences persist via
`GET/PUT /api/v1/dashboard/column-preferences`, and column configs can also travel inside a filter
preset. Server validates configs (unique id/order, width bounds, allowed formatter/field_type) in
`filter_preset_handlers.go:92`.

## Alert actions

Every single and bulk action funnels through **`POST /api/v1/dashboard/bulk-action`** →
`BulkActionAlerts` (`dashboard_handlers.go:759`) → per-fingerprint `processAlertAction`.

| Action | Path |
|--------|------|
| Acknowledge / Unack | `backendClient` gRPC + auto audit comment |
| Comment add/delete | `backendClient` gRPC (`/alert/:fp/comments`) |
| **Silence / Unsilence** | **bypass the backend** — call `alertmanagerClient` directly on **every** configured Alertmanager |
| **Resolve** | **local-only** — flips `IsResolved`/`Status.State` in the cache + audit comment; does **not** touch Alertmanager |
| Hide (global) | `hiddenAlertsService` via `/hidden-alerts` |
| Hide in filter | client-only mutation of the active preset's `filterHiddenAlerts` |

> ⚠️ Two mental models coexist and are easy to confuse: **collaboration** (ack/comment/resolve)
> goes through the backend gRPC client; **silence** goes straight to Alertmanager(s).
> **"Resolve" is cosmetic** — the alert reappears on the next Alertmanager sync if still firing
> upstream. **"Hide in Filter" is lost on reload** unless the user re-saves the preset via
> `updateActiveFilterPreset()`. Comment/ack counts on rows are maintained by incrementing
> `alert.CommentCount` in the cache, not re-queried.

## Alert detail modal

`AlertDetailsModal` (`components/modal_components.templ:921`), opened by
`showAlertDetails(fingerprint)` (`dashboard_modal.templ:6`), which `pushState`s the URL to
`/dashboard/alert/:fingerprint` so deep links work (`router.go:354` re-renders the page and
`checkAlertFromURL()` reopens the modal). Data comes from `GET /api/v1/dashboard/alert/:fingerprint`
→ `GetAlertDetails` (`dashboard_handlers.go:1231`): the cached alert plus, if the backend is
connected, its comments and acknowledgments.

Tabs: **Overview**, **Details** (fingerprint, generator URL), **Labels** / **Annotations**
(copy-to-clipboard), **Acknowledgments**, **Comments** (add/delete; system comments show a badge),
**History** (lazy `GET /alert/:fp/history` → up to 50 fire/resolve/ack occurrences with MTTR/MTTA),
and **Sentry** (only if the alert carries a `sentry` annotation/label; lazy-loaded). Header offers
Silence/Unsilence, configurable per-user **annotation buttons**, Ack/Unack, "Source"
(`generatorURL`), and "Copy as Issue" (builds a Markdown issue and copies it).

> ⚠️ The modal's `Silences` field is **always empty** (`dashboard_handlers.go:1289`, not
> implemented) — only `status.silencedBy` IDs are available.

## Filter presets, resolved view, colors

- **Filter presets** (`scripts/dashboard_filter_presets.templ`): `captureCurrentFilterState()`
  serializes search, all filter arrays, ack/comment filters, display/view mode, group-by, sort,
  page size, optionally column configs, and filter-scoped hides — into `FilterPresetData`. CRUD via
  `/api/v1/dashboard/filter-presets` (gRPC-backed), with shared-vs-personal (`is_shared`) and a
  per-user default that loads at boot.
- **Resolved alerts view** (`displayMode==='resolved'`) is **not** from the cache — it queries the
  statistics subsystem (`POST /api/v1/statistics/recently-resolved`) and reconciles historical
  "resolved" against live cache status. Full detail in [statistics](statistics.md#recently-resolved).
- **Colors**: `getAlertColor()` (`dashboard_utilities.templ:540`) returns per-row colors from
  either rule-based **user color preferences** (server-computed, `/alert-colors`) or a hardcoded
  severity-fallback palette. See [domain](domain.md#collaboration-state-persisted-by-the-backend).

## Notification hooks

The dashboard doesn't implement notifications; it calls `window.notificationService`. On first
load it seeds the "seen" set (`initializeSeenAlerts`); on every merge it calls
`processNewAlerts(this.alerts, this.filters, this.currentUser.id)` (`dashboard_data.templ:367`). A
dismissible banner prompts for browser-notification permission. Full behavior in
[notifications](notifications.md).

## Gotchas {#gotchas}

- **`alertMatchesFilters()` (client) must mirror `applyDashboardFilters()` (server)** — or live
  updates go stale for the unmirrored filter. This is the #1 dashboard footgun.
- **Dead legacy components:** `components/table_components.templ` (`AlertsTable`,
  `SortableHeader`, `ResizableHeader`) and `column_config_tab.templ` (`ColumnConfigTab`) are
  defined but **never referenced** — the live page uses `DynamicAlertsTable` and the standalone
  `ColumnConfigModal`. Grep before editing.
- **Two column-width systems:** the modern dynamic `columns[]` (server-persisted) vs. a vestigial
  `localStorage['dashboardColumnWidths']` used only by the dead `table_components.templ`. Only
  touch the former.
- **Resolve is local-only; Silence bypasses the backend** (see actions above).
- **`window.dashboardInstance`** is relied on across the codebase — don't rename/remove it.
- Classic-mode ack/resolved counters are special-cased with a second pass
  (`dashboard_handlers.go:681`) — account for it when changing counter logic.
