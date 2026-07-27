# Statistics Features 1, 2, 4 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Add 3 features to the statistics dashboard: (1) Alert Noise Heatmap (7×24 DOW×hour), (2) Alert Flapping Detector (top noisy alerts), (4) Shareable filter state via URL.

**Architecture:** Features 1 & 2 are full-stack: new gRPC RPCs on `StatisticsService`, query-layer GORM aggregations reusing existing timezone helpers, client wrappers, Gin handlers, routes, and frontend sections. Feature 4 is frontend-only. Heatmap renders as a CSS grid (NO new chart dependency). Flapping renders as an HTML table.

**Tech Stack:** Go, GORM/PostgreSQL, gRPC/protobuf, Gin, templ, Alpine.js, Chart.js (existing).

## Global Constraints

- Proto regen: run `./scripts/generate_proto.sh` directly (NOT `make proto` — it's a no-op because a `proto/` directory shadows the phony target). Generated files: `internal/backend/proto/alert/alert.pb.go`, `alert_grpc.pb.go`. Stage them.
- `.templ` edits: run `make webui-templates` then `go build ./...`. Never hand-edit `*_templ.go`. Stage source + generated.
- `go build ./...` must pass after every task.
- Commits: Conventional Commits (`feat:`), NO `Co-Authored-By` trailer. One atomic commit per task.
- Reuse existing patterns — copy the `QueryStatistics` / `QueryRecentlyResolved` analog at every layer (see the wiring references inline). Do not invent new abstractions.
- All new endpoints require auth (mounted under the existing `statistics` group which already applies `RequireAuth()`).
- Timezone: reuse `localFiredAt(tz)` (statistics_query.go:580) and `dowExpr(tz)` (statistics_query.go:307). Validate tz via the existing `validateTimezone` (statistics_grpc_service.go:47).

---

### Task F-A: Backend for Heatmap + Flapping (proto → query → service → client → handler → route)

**Files:**
- Modify: `proto/alert.proto` (add 2 RPCs + 6 messages inside/after `service StatisticsService`)
- Regenerate: `internal/backend/proto/alert/alert.pb.go`, `alert_grpc.pb.go` (via `./scripts/generate_proto.sh`)
- Modify: `internal/backend/services/statistics_query.go` (add `QueryHeatmap`, `QueryFlappingAlerts` methods)
- Modify: `internal/backend/services/statistics_grpc_service.go` (add 2 RPC methods on `*StatisticsServiceGorm`)
- Modify: `internal/webui/client/backend_client.go` (add 2 wrapper methods on `*BackendClient`)
- Modify: `internal/webui/handlers/statistics_handlers.go` (add 2 handlers)
- Modify: `internal/webui/router.go` (add 2 routes in the `statistics` group, ~line 321)

**Interfaces produced (later frontend tasks rely on these JSON shapes):**
- `POST /api/v1/statistics/heatmap` body `{start_date, end_date, severities[], teams[], include_silenced, timezone}` → `{success, data:{cells:[{dow,hour,count,avg_mttr_seconds}]}}`
- `POST /api/v1/statistics/flapping` body `{start_date, end_date, severities[], teams[], include_silenced, min_fires, limit, timezone}` → `{success, data:{alerts:[{fingerprint,alert_name,team,severity,fire_count,avg_gap_seconds,fires_per_hour,avg_mttr_seconds,flap_score}]}}`

- [ ] **Step 1: Add proto messages + RPCs**

In `proto/alert.proto`, add these two lines inside `service StatisticsService { ... }` (next to the existing `rpc QueryStatistics`):
```proto
  rpc QueryHeatmap(QueryHeatmapRequest) returns (QueryHeatmapResponse);
  rpc QueryFlappingAlerts(QueryFlappingAlertsRequest) returns (QueryFlappingAlertsResponse);
```
And add these messages (place near the other statistics messages; `google.protobuf.Timestamp` is already imported — confirm the import line exists):
```proto
message QueryHeatmapRequest {
  string session_id = 1;
  google.protobuf.Timestamp start_date = 2;
  google.protobuf.Timestamp end_date = 3;
  repeated string severities = 4;
  repeated string teams = 5;
  bool include_silenced = 6;
  string timezone = 7;
}
message HeatmapCell {
  int32 dow = 1;   // 0=Sunday .. 6=Saturday (PostgreSQL EXTRACT(DOW))
  int32 hour = 2;  // 0..23
  int64 count = 3;
  double avg_mttr_seconds = 4;
}
message QueryHeatmapResponse {
  bool success = 1;
  string message = 2;
  repeated HeatmapCell cells = 3;
}
message QueryFlappingAlertsRequest {
  string session_id = 1;
  google.protobuf.Timestamp start_date = 2;
  google.protobuf.Timestamp end_date = 3;
  repeated string severities = 4;
  repeated string teams = 5;
  bool include_silenced = 6;
  int32 min_fires = 7;
  int32 limit = 8;
  string timezone = 9;
}
message FlappingAlert {
  string fingerprint = 1;
  string alert_name = 2;
  string team = 3;
  string severity = 4;
  int64 fire_count = 5;
  double avg_gap_seconds = 6;
  double fires_per_hour = 7;
  double avg_mttr_seconds = 8;
  int64 flap_score = 9;
}
message QueryFlappingAlertsResponse {
  bool success = 1;
  string message = 2;
  repeated FlappingAlert alerts = 3;
}
```

- [ ] **Step 2: Regenerate proto**

Run: `./scripts/generate_proto.sh`
Expected: "✅ Proto generation completed!". Then `go build ./...` will fail until the service implements the new RPCs (the generated `*_grpc.pb.go` adds them to the `UnimplementedStatisticsServiceServer`, so build still passes — verify).

- [ ] **Step 3: Add query-layer methods**

In `internal/backend/services/statistics_query.go`, add two methods on `*StatisticsQueryService`. Reuse the base-query construction from `QueryStatistics` (the `fired_at BETWEEN`, severity/team, and `silenced_at_fire=false` filters — copy that filter block). Define small internal result structs.

Heatmap (PostgreSQL; `dowExpr(tz)` and `localFiredAt(tz)` already exist):
```go
type HeatmapCellResult struct {
	Dow     int     `gorm:"column:dow"`
	Hour    int     `gorm:"column:hour"`
	Count   int64   `gorm:"column:count"`
	AvgMTTR float64 `gorm:"column:avg_mttr"`
}
// SELECT <dowExpr(tz)> as dow, EXTRACT(HOUR FROM <localFiredAt(tz)>) as hour,
//        COUNT(*) as count, COALESCE(AVG(mttr_seconds),0) as avg_mttr
// FROM alert_statistics WHERE <filters> GROUP BY dow, hour
```
Use `EXTRACT(HOUR FROM ...)::int` on PostgreSQL. Cast dow to int. Return `[]HeatmapCellResult`.

Flapping — aggregate in SQL, compute gap/rate/score in Go:
```go
type FlappingResult struct {
	Fingerprint string    `gorm:"column:fingerprint"`
	AlertName   string    `gorm:"column:alert_name"`
	Team        string    `gorm:"column:team"`
	Severity    string    `gorm:"column:severity"`
	FireCount   int64     `gorm:"column:fire_count"`
	AvgMTTR     float64   `gorm:"column:avg_mttr"`
	FirstFired  time.Time `gorm:"column:first_fired"`
	LastFired   time.Time `gorm:"column:last_fired"`
}
// SELECT fingerprint,
//   (array_agg(alert_name ORDER BY fired_at DESC))[1] as alert_name,
//   (array_agg(metadata->'labels'->>'team' ORDER BY fired_at DESC))[1] as team,
//   (array_agg(severity ORDER BY fired_at DESC))[1] as severity,
//   COUNT(*) as fire_count, COALESCE(AVG(mttr_seconds),0) as avg_mttr,
//   MIN(fired_at) as first_fired, MAX(fired_at) as last_fired
// FROM alert_statistics WHERE <filters> GROUP BY fingerprint
// HAVING COUNT(*) >= <minFires>
```
Then in Go, for each row compute:
- `spanSec := LastFired.Sub(FirstFired).Seconds()`
- `avgGap := 0.0; if FireCount > 1 { avgGap = spanSec / float64(FireCount-1) }`
- `firesPerHour := 0.0; if spanSec > 0 { firesPerHour = float64(FireCount) / (spanSec/3600) }`
- `flapScore := int64(0); if avgGap > 0 { flapScore = int64(math.Round(float64(FireCount) / (avgGap/3600))) } else if FireCount > 1 { flapScore = FireCount * 100 }`
Sort by `flapScore` desc, truncate to `limit` (default 20 if <=0). `team` may be empty string → leave as-is (frontend shows "unknown"). Method signatures: accept the same internal request shape you build in the service layer (your choice — a small struct with Start, End, Severities, Teams, IncludeSilenced, Timezone, MinFires, Limit).

NOTE: the SQL uses PostgreSQL-only constructs (`array_agg`, `EXTRACT`). The existing query layer already branches Postgres/SQLite in helpers; these two methods may be PostgreSQL-only — if the codebase must support SQLite too, add a simple fallback or guard. Check `localFiredAt`'s dialect handling; if SQLite support is required and non-trivial, report DONE_WITH_CONCERNS rather than shipping a broken SQLite path.

- [ ] **Step 4: Add service RPC methods**

In `statistics_grpc_service.go`, add `QueryHeatmap` and `QueryFlappingAlerts` on `*StatisticsServiceGorm`, copying the `QueryStatistics` method shape: validate `req.SessionId`, `s.db.GetUserBySession`, validate dates non-nil, `tz := validateTimezone(req.Timezone)`, call the query-layer method, map results to the proto response. Return `success:true` with the cells/alerts.

- [ ] **Step 5: Add client wrappers**

In `backend_client.go`, add `QueryHeatmap(sessionID string, req *alertpb.QueryHeatmapRequest) (*alertpb.QueryHeatmapResponse, error)` and `QueryFlappingAlerts(...)` copying the `QueryStatistics` wrapper (10s context, nil-guard `c.statisticsClient`, set `req.SessionId`).

- [ ] **Step 6: Add handlers**

In `statistics_handlers.go`, add `QueryHeatmap(c *gin.Context)` and `QueryFlappingAlerts(c *gin.Context)` copying the `QueryStatistics` handler: get sessionID, `IsConnected` check, bind JSON body, validate timezone (`time.LoadLocation`), build proto req, call the client, return `webuimodels.SuccessResponse(gin.H{"cells": ...})` / `{"alerts": ...}`. Convert proto cells/alerts to plain maps/structs for JSON (snake_case keys matching the Interfaces block above).

- [ ] **Step 7: Add routes**

In `router.go` statistics group (~line 321, after `alerts-by-name`):
```go
		statistics.POST("/heatmap", handlers.QueryHeatmap)
		statistics.POST("/flapping", handlers.QueryFlappingAlerts)
```

- [ ] **Step 8: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 9: Commit**

```bash
git add proto/alert.proto internal/backend/proto/alert/ internal/backend/services/statistics_query.go internal/backend/services/statistics_grpc_service.go internal/webui/client/backend_client.go internal/webui/handlers/statistics_handlers.go internal/webui/router.go
git commit -m "feat(statistics): add heatmap and flapping-alerts backend RPCs"
```

---

### Task F-B: Frontend — Alert Noise Heatmap section

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (markup section + Alpine `loadHeatmap()` + `heatmapColor()` helper + field)
- Regenerate: `StatisticsDashboard_templ.go`

**Interfaces consumed:** `POST /api/v1/statistics/heatmap` (see F-A).

- [ ] **Step 1: Add Alpine state + loader**

In `statisticsDashboardPage()` data object add: `heatmapData: [],` and `heatmapMetric: 'count',`. Add a method `async loadHeatmap()` that POSTs to `/api/v1/statistics/heatmap` with `{start_date, end_date, severities, teams, include_silenced, timezone}` (reuse the exact same start_date/end_date/timezone construction as `queryStatistics()` — factor it or copy it), and stores `this.heatmapData = data.data?.cells || []`. Call `this.loadHeatmap()` inside `queryStatistics()` by adding it to the existing `Promise.all([...])` block (alongside `loadTimeSeriesData` etc.).

- [ ] **Step 2: Add a color helper**

Add method:
```js
heatmapValue(dow, hour) {
    const c = this.heatmapData.find(x => x.dow === dow && x.hour === hour);
    return c ? (this.heatmapMetric === 'count' ? c.count : c.avg_mttr_seconds) : 0;
},
heatmapMax() {
    if (!this.heatmapData.length) return 1;
    return Math.max(1, ...this.heatmapData.map(c => this.heatmapMetric === 'count' ? c.count : c.avg_mttr_seconds));
},
heatmapCellStyle(dow, hour) {
    const v = this.heatmapValue(dow, hour), t = v / this.heatmapMax();
    // slate(30,41,59) -> amber(252,211,77) -> red(220,38,38)
    const lerp = (a,b,u) => a.map((x,i)=>Math.round(x+(b[i]-x)*u));
    const rgb = t < 0.5 ? lerp([30,41,59],[252,211,77], t/0.5) : lerp([252,211,77],[220,38,38], (t-0.5)/0.5);
    return `background-color: rgb(${rgb.join(',')})`;
},
```
NOTE: PostgreSQL `EXTRACT(DOW)` returns 0=Sunday..6=Saturday. The grid rows should be Mon..Sun; map accordingly (row order `[1,2,3,4,5,6,0]`).

- [ ] **Step 3: Add the markup section**

Add a new card (after the existing charts section, gated on `x-show="hasData"`). Follow the existing card styling in the file (`bg-white dark:bg-dark-bg-secondary rounded-xl ...`). Render a 7×24 CSS grid:
```html
<div class="grid" style="grid-template-columns: 2rem repeat(24, 1fr); gap: 3px">
  <template x-for="(dow, ri) in [1,2,3,4,5,6,0]" :key="dow">
    <template x-for="hour in 24" :key="hour">
      ...
    </template>
  </template>
</div>
```
Implementer: design the grid so each row has a day label (Mon..Sun) then 24 cells; each cell uses `:style="heatmapCellStyle(dow, hour-1)"` and a `:title` tooltip like `Mon 14:00 — 42 alerts`. Add a metric toggle (Volume/MTTR) bound to `heatmapMetric`. Add a small legend. Keep it dark-mode compatible. Empty state when `heatmapData.length === 0`.

- [ ] **Step 4: Regenerate + build**

Run: `make webui-templates && go build ./...`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(statistics): add alert noise heatmap section"
```

---

### Task F-C: Frontend — Alert Flapping Detector section

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ`
- Regenerate: `StatisticsDashboard_templ.go`

**Interfaces consumed:** `POST /api/v1/statistics/flapping` (see F-A).

- [ ] **Step 1: Add Alpine state + loader**

Add `flappingData: [],` to the data object. Add `async loadFlapping()` POSTing to `/api/v1/statistics/flapping` with `{start_date, end_date, severities, teams, include_silenced, min_fires: 3, limit: 20, timezone}` (same date/tz construction as queryStatistics). Store `this.flappingData = data.data?.alerts || []`. Add `loadFlapping()` to the `Promise.all` in `queryStatistics()`.

- [ ] **Step 2: Add formatting helpers (reuse existing if present)**

For gap/MTTR display reuse the existing duration formatter in the file (grep `formatDuration` or `formatSeconds`); if none, add a small `formatGap(sec)` → `"3m 10s"`. For the flap-score badge color: `>=50 red, 20-49 amber, else slate`.

- [ ] **Step 3: Add the markup section**

Add a card "Top Flapping Alerts" (gated `x-show="hasData"`), an HTML table with columns: # / Alert (name + team pill + severity badge) / Fires / Avg gap / Rate (/h) / MTTR / Score. Use `<template x-for="(a, i) in flappingData" :key="a.fingerprint">`. Reuse the severity badge styling already used elsewhere in the dashboard (grep for existing severity color classes). Empty state: "No flapping alerts detected ✓" when `flappingData.length === 0`. Row click is optional (skip unless an `openAlertModal` already exists and is trivial to wire).

- [ ] **Step 4: Regenerate + build**

Run: `make webui-templates && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(statistics): add flapping-alerts section"
```

---

### Task F-D: Frontend — Shareable filter state via URL

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ`
- Regenerate: `StatisticsDashboard_templ.go`

**Interfaces consumed:** none (frontend-only).

- [ ] **Step 1: Add URL serialization**

Add a method `syncFiltersToURL()` that builds `URLSearchParams` from the meaningful `filters` fields (`startDate, endDate, groupBy, periodType, chartPeriodType, limit, includeSilenced, timeOfDayStart, timeOfDayEnd, weekendMode, timeRangeMode, absoluteFromTime, absoluteUntilTime` and the arrays `severities`, `teams` joined by comma) plus `searchQuery` if present, then `window.history.replaceState(null, '', '/statistics?' + params.toString())`. Call `syncFiltersToURL()` at the END of `queryStatistics()` (after a successful response). Use the existing `updateURL()` pattern in `dashboard_utilities.templ` if it already does this — grep first; if a generic URL helper exists, extend it rather than duplicating.

- [ ] **Step 2: Add URL hydration on init**

Add `hydrateFiltersFromURL()` that reads `new URLSearchParams(window.location.search)` and, for each present param, assigns it into `this.filters` (split comma arrays for severities/teams; parse booleans/ints). Call it in the component init (find the existing `init()` / `x-init` / `initViews()` entry; call hydrate BEFORE the initial auto-query so the query uses the restored filters). Only override a filter when the URL param is actually present (don't clobber defaults with empty strings).

- [ ] **Step 3: Add the Share button**

Add a "Share" button in the header action bar next to Export (grep for the Export button markup and mirror its sty­ling). On click: `navigator.clipboard.writeText(window.location.href)` then show a transient "Copied!" state (reuse the existing toast/transient pattern, e.g. `showExportSuccess` style, but for the link). Keep it minimal — a 1.5s label swap is enough.

- [ ] **Step 4: Regenerate + build**

Run: `make webui-templates && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(statistics): shareable filter state via URL deep-link"
```

---

## Self-Review

- **Coverage:** Feature 1 (heatmap) → F-A backend + F-B frontend; Feature 2 (flapping) → F-A backend + F-C frontend; Feature 4 (share URL) → F-D. ✓
- **Sequencing:** F-A first (backend contract). F-B, F-C, F-D each edit the same `StatisticsDashboard.templ` → strictly sequential, never parallel. ✓
- **No new deps:** heatmap is CSS grid, flapping is HTML table — Chart.js untouched. ✓
- **Dialect risk:** flagged in F-A Step 3 — the SQL is PostgreSQL-oriented; implementer must check SQLite support requirement and escalate if non-trivial.
- **Type consistency:** JSON keys (`avg_mttr_seconds`, `fires_per_hour`, `flap_score`, `dow`, `hour`) defined once in F-A Interfaces block and consumed verbatim in F-B/F-C.

## Verification (manual, after all tasks)

- `go build ./...` passes; `./scripts/generate_proto.sh` leaves no unexpected diff beyond the new messages.
- Optional smoke: run the app, open `/statistics`, run a query → heatmap grid populates, flapping table populates; copy Share link, open in a new tab → filters restored.
