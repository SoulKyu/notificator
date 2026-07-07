# Statistics Dashboard

The statistics dashboard (`/statistics`) is the app's analytics surface: aggregate alert metrics
(counts, MTTR / MTTA / fix-time) over a time range, with grouping, on-call time-of-day filtering,
charts, drill-down, and saved views. This page covers the UI and what it sends/displays; the
backend query engine (capture, storage, the on-call overlap SQL) is documented in
[backend](backend.md#statistics-engine), and the metric definitions in
[domain](domain.md#statistics).

**Source:** page `internal/webui/templates/pages/StatisticsDashboard.templ` (component
`statisticsDashboardPage()`); saved-views mixin
`internal/webui/templates/scripts/statistics_views.templ`; handlers
`internal/webui/handlers/statistics_handlers.go` and `statistics_view_handlers.go`. Never edit
`*_templ.go` — see [operations](operations.md#codegen).

## Client architecture and bootstrap

The page scope spreads two independently-defined mixins onto one Alpine object
(`StatisticsDashboard.templ:43`): `statisticsDashboardPage()` (state + query/chart logic) and
`statisticsViewsMixin()` (saved views + timezone helpers). Boot order: Alpine calls the mixin's
`init()` → `initPage()` (sets the on-call time preset, watches) → registers `$watch` on every
filter so any edit sets **`needsQuery = true`** (pulsing the Query button) and clears the active
view → `initViews()` loads saved views, applies the user's default if any, then **always** fires
one `queryStatistics()` on first paint.

Default filters: `groupBy: 'team'`, `periodType: 'day'`, `limit: 20`,
`severities: ['critical','critical-daytime']`, relative range "last 7 days → now", time-of-day
overridden to the on-call schedule.

> Charts use **Chart.js 4.4.1 + the date-fns adapter, loaded per-page from a CDN**
> (`StatisticsDashboard.templ:19`) — separate from the app-wide Alpine/htmx/Day.js bundle in
> `layouts/Base.templ`. A CDN outage degrades only chart rendering on this page.

## Time range selection

Two modes (`filters.timeRangeMode`), plus quick presets (1h/24h/7d/30d/90d):

- **Relative** — numeric value + unit (minutes…years) for "From" and "Until", with "All Time" /
  "Now" toggles. Backed by `RelativeTimeConfig` (`value`, `unit`, `all_time`, `now`;
  `proto/alert.proto:1073`). Values are clamped per unit (max 365 days, 10 years).
- **Absolute** — `<input type=date>` + `<input type=time>` pairs.

Both modes compute `filters.startDate` / `endDate` as plain `YYYY-MM-DD` strings.

In **relative** mode the wire dates are day-boundary (`T00:00:00Z` / `T23:59:59Z`). In **absolute**
mode the custom `absoluteFromTime` / `absoluteUntilTime` **are now honored** — the start/end
timestamps reflect the chosen time-of-day rather than being forced to day boundaries. Relative
ranges also support a `years` granularity. Queries carry an explicit `timezone` (field 18), so
period/heatmap bucketing happens in the user's zone (the binary embeds `time/tzdata`, so IANA
zones resolve even in alpine containers — see [operations](operations.md)).

## Time-of-day / on-call filtering

UI (`StatisticsDashboard.templ:321`): lenient `HH:MM` inputs (accepts `0900`/`9`/`902`), a
24-segment visual timeline (correct overnight wrap), six preset chips (On-Call / Morning / Noon /
Business / Evening / Night), and a **weekend-mode** select (`exclude` / `same_hours` /
`full_weekends`). The On-Call preset reads the user's on-call schedule from
`localStorage.dashboardSettings.onCallSchedule` (default 18:00→08:00, weekends included) and locks
weekend-mode to `full_weekends`.

`isTimeFilterActive()` computes the `filter_by_time_of_day` flag: true unless the window is the
full day *and* weekend-mode is `same_hours`. Every query payload
(`queryStatistics`, the four chart loaders, the drill-down) sends the same five fields:
`filter_by_time_of_day`, `time_of_day_start`, `time_of_day_end`, `weekend_mode`, plus
`severities` / `teams`. See [domain](domain.md#on-call-rules--time-of-day-filtering) and the
regression-sensitive overlap semantics in [backend](backend.md#on-call-overlap-filter-regression-sensitive).

> ⚠️ **Time-of-day UTC asymmetry.** The live query sends raw `HH:MM`. The *saved-view* path
> converts to UTC on **save** (`convertTimeToUTC`) but does **not** convert back on **apply** —
> so reloading a saved view in a different timezone can shift the effective on-call window. There
> is no `use_on_call_period` boolean on the wire despite the proto field existing; on-call is
> implemented purely as a time-of-day preset.

## Grouping

`groupBy`: Overall (`''`) / `severity` / `team` / `alert_name` / `period`. When `period`, a
`periodType` (hour/day/week/month) appears; when `alert_name`, a Top-N input (`limit`, default 20).
`secondary_group_by` is **not** user-facing — it's set internally to break the time-series chart
into series within each bucket. Overall/severity/team/alert_name render the sortable **"Statistics
by Group"** table plus the four charts; `period` renders a **"Period Breakdown"** card list instead
(the resolution-times chart is hidden for period grouping). `group_by`/`period_type`/`weekend_mode`
are plain strings in the proto (not enums) — typos yield empty results, not errors.

## Metrics and charts

Metrics come from `AggregatedStatistics` (`count`, `avg_mttr_seconds`, `avg_mtta_seconds`,
`avg_fix_time_seconds`). Formatters: `formatDuration` ("1h 5m 3s"), `formatMTTR`/`formatTimeOrNA`
(render `N/A` for a `0` value, not "0s"), `formatNumber` (K/M). The overall KPI tiles are
**alert-count-weighted** (`sum(total_seconds)/sum(count)`, using `total_*_seconds` from the backend
when present) rather than an average-of-averages. Four Chart.js instances:

| Chart | Type | Purpose |
|-------|------|---------|
| Alerts Over Time | line | fired/resolved/both, or per-group series with a dashed "Total" |
| Distribution | doughnut | Top-10 + "Others", with a Chart/List toggle |
| Top 10 Items | horizontal bar | click a bar → drill-down (when grouped by alert_name) |
| Resolution Times | grouped horizontal bar | MTTR / MTTA / Fix Time per group |

Each chart has a fullscreen variant. `buildKeyColorMap()` assigns one stable color per group key
across all charts. Continuous time axes are zero-filled for missing buckets
(`fillMissingPeriods`). A **comparison mode** queries the immediately-preceding equal-length
period and shows % deltas (with inverted coloring so "down is good" for durations).

A fifth visualization, the **Alert Noise Heatmap** (`StatisticsDashboard.templ` ~1441), is a
custom 7×24 (day-of-week × hour) grid — not Chart.js — colored by a Volume/MTTR toggle
(`heatmapMetric`). It loads via `loadHeatmap()` → `POST /api/v1/statistics/heatmap`
(`QueryHeatmap`), which is **PostgreSQL-only** (uses `EXTRACT(DOW …)` / `AT TIME ZONE`) — see
[backend](backend.md#statistics-engine).

> There is also a `QueryFlappingAlerts` RPC (`POST /api/v1/statistics/flapping`), but the
> "Top Flapping Alerts" UI section that used it was **added then removed** — the backend RPC/endpoint
> still exist and work, they're just **unused by any current UI**.

**Export is 100% client-side CSV** from the already-loaded `statsData` — there is no backend
export endpoint.

## Sharing & URL state

The page serializes its full filter state into the URL after every query (`syncFiltersToURL` →
`history.replaceState('/statistics?…')`, covering dates, grouping, period, limit,
`includeSilenced`, time-of-day, weekend mode, time-range mode, severities/teams). A **Share**
button copies `window.location.href`; on load, `hydrateFiltersFromURL()` restores any present
params, and the default-saved-view auto-apply **skips itself when URL params exist** so a shared
link isn't clobbered by the user's default view. Every statistics query is also `include_silenced`-
and `timezone`-aware on the wire (`QueryStatisticsRequest` fields 17/18), backed by the
`silenced_at_fire` capture column.

## Severity & team filters

Two searchable multi-select dropdowns. Option lists are **derived, not authoritative**:
`loadAvailableSeverities()` / `loadAvailableTeams()` each issue their own statistics query grouped
by severity/team over a hardcoded **trailing 90-day window** and take the option list from the
result keys — so a severity/team appearing only in older data won't show up as an option. These
filters (`severities` / `teams`, `repeated string`) are sent on every statistics RPC.

## Drill-down

- **By alert name** — `openAlertDetailsModal(alertName)` → `POST /api/v1/statistics/alerts-by-name`
  (`GetAlertsByName`, `statistics_handlers.go:152`): a table of individual occurrences (severity,
  fired/resolved, MTTR/MTTA/fix-time, labels). Rows are themselves clickable.
- **Individual alert / history** — `openIndividualAlertDetails()` →
  `GET /api/v1/statistics/alert/:fingerprint` (`GetResolvedAlertDetails`,
  `statistics_handlers.go:372`): rendered by the shared `AlertModalReadonly` (also used by the
  dashboard's resolved view). The backend enriches it with comments/acks and reconstructs
  labels/annotations/source from the latest history record. The Comments tab is **no longer
  read-only** — it uses `AlertModalCommentsWritable` (`alert_modal_shared.templ`) with an add-comment
  box that POSTs to the dashboard's `/api/v1/dashboard/alert/:fingerprint/comments` endpoint (even
  though the modal lives on the Statistics page).

## Recently resolved {#recently-resolved}

> **Not on this page.** Despite hitting the same endpoint, "Recently Resolved" is a **tab of the
> live dashboard** (`displayMode==='resolved'`), implemented in
> `internal/webui/templates/scripts/dashboard_resolved_alerts.templ` and mixed into
> `NewDashboard.templ`. Don't look for it in `StatisticsDashboard.templ`.

It POSTs to `POST /api/v1/statistics/recently-resolved` (`QueryRecentlyResolved`,
`statistics_handlers.go:297`) with its own time range and only the
`severities`/`teams`/`alertNames`/`search` filters (not alertmanager/status/ack/comment filters).
It **always** requests `include_silenced: true`, then re-queries live cache status per fingerprint
to reconcile "historically resolved" against "currently firing/silenced" client-side. Returns
`ResolvedAlertItem` rows (occurrence_count, first_fired/last_resolved, avg MTTR/MTTA/fix-time,
labels, team). Limit default 100, cap 1000, offset paging.

## Saved statistics views

A saved view persists the entire analytics config (`StatisticsViewData`): time-range mode +
relative config, dates, time-of-day window (UTC-converted on save), grouping, period type,
severities/teams, limit. CRUD via `/api/v1/statistics/views` (create/update/delete/set-default),
shared-vs-personal (`is_shared`), with a per-user default applied at boot.

**Every saved-view handler is impersonation-aware** (`middleware.GetImpersonatedUserID`), so an
admin impersonating a user manages that user's views. (Impersonation exists only for saved views,
not for the query/drill-down endpoints.)

## Gotchas {#gotchas}

- **Heatmap & flapping are PostgreSQL-only** — `QueryHeatmap`/`QueryFlappingAlerts` use PG-specific
  SQL and error on SQLite. The flapping RPC has **no UI** (the section was removed).
- **Time-of-day UTC asymmetry in saved views** — converted on save, not on apply.
- **Weekend-mode does not round-trip through saved views.** `getCurrentViewData()`/`applyView()`
  read/write dead `filters.includeWeekends`/`applyRules`/`filterByTimeOfDay` properties that don't
  exist on `statisticsDashboardPage()`'s `filters` (which uses the 3-way `weekendMode` string
  instead) — leftover from a pre-`weekendMode` schema. A saved view won't restore weekend-mode.
- **Filter option lists are derived from a trailing 90-day query**, not authoritative.
- **Export and "Recently Resolved endpoint" have no dedicated backend surface** you'd expect —
  export is client-side; recently-resolved lives on the other page.
- `onFilterChange()` is bound to the severity/team checkboxes but its definition wasn't located —
  it may be a no-op/dead hook (the `$watch`ers already handle the side effects). Confirm before
  relying on it.
