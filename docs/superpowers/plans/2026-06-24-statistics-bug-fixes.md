# Statistics Dashboard Bug Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the 22 confirmed bugs found in the statistics feature (backend gRPC + query + handlers + client + frontend templ/JS).

**Architecture:** Go backend (GORM/PostgreSQL, gRPC) + Gin webui handlers + gRPC client + templ templates with embedded Alpine.js. Fixes are grouped into 5 tasks by file/layer so each task compiles and is independently reviewable. Most fixes are defensive nil-guards and validation; a few are real logic bugs (MTTA mislabel, weighted averages, infinite loop, absolute-time payload).

**Tech Stack:** Go, GORM, gRPC/protobuf, Gin, templ, Alpine.js, Chart.js.

## Global Constraints

- Build backend with: `go build ./...` — must pass after every task touching `.go`.
- Frontend `.templ` edits REQUIRE regeneration: `make webui-templates` (runs `templ generate`), then `go build ./...`. NEVER hand-edit `*_templ.go` — edit the `.templ` source.
- Commits: Conventional Commits (`fix:`), no `Co-Authored-By` trailer.
- Each task = one atomic commit.
- Do NOT fix the two agent-retracted findings (backend weekend-filter logic, resolved-alerts count query) — they were verified as non-bugs.
- Skipped as needing product decision, NOT in this plan: per-user scoping of `GetStatisticsSummary`/`GetAlertsByName` (the data model is single-tenant by design — confirm separately before touching).

---

### Task 1: Backend gRPC service — MTTA mislabel + unchecked type assertion

**Files:**
- Modify: `internal/backend/services/statistics_grpc_service.go:386-401` (H1), `:225` (H2)

**Interfaces:**
- Consumes: `models.AlertStatistic.MTTASeconds *int` (exists, line 34 of `models/statistics.go`).
- Produces: nothing downstream depends on new symbols.

- [ ] **Step 1: Fix H1 — write MTTA, not MTTR, in `UpdateAlertAcknowledged`**

Replace lines 386-390:
```go
	// Calculate MTTR (Mean Time To Resolve) in seconds
	// MTTR = time from alert firing to acknowledgment
	mttr := acknowledgedAt.Sub(stat.FiredAt)
	mttrSec := int(mttr.Seconds())
	stat.MTTRSeconds = &mttrSec
```
with:
```go
	// Calculate MTTA (Mean Time To Acknowledge) in seconds
	// MTTA = time from alert firing to acknowledgment
	mtta := acknowledgedAt.Sub(stat.FiredAt)
	mttaSec := int(mtta.Seconds())
	stat.MTTASeconds = &mttaSec
```
Also update the log line at 401 from `(MTTR: %ds)`/`mttrSec` to `(MTTA: %ds)`/`mttaSec`.

- [ ] **Step 2: Fix H2 — guard the `total_statistics` type assertion**

At line 225, replace:
```go
		TotalStatistics: summary["total_statistics"].(int64),
```
with:
```go
		TotalStatistics: func() int64 { v, _ := summary["total_statistics"].(int64); return v }(),
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success, no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/backend/services/statistics_grpc_service.go
git commit -m "fix(statistics): write MTTA on acknowledge and guard summary type assertion"
```

---

### Task 2: Backend query — infinite loop, type re-declaration, GROUP BY alias, date-range session

**Files:**
- Modify: `internal/backend/services/statistics_query.go` — period generators (~798, 822, weekly, monthly), `convertAggregatedToResolvedAlertItem` (~1315), `aggregateByTeam` (~426), `GetStatisticsSummary` date range (~917)
- Test: `internal/backend/services/statistics_query_test.go`

**Interfaces:**
- Consumes: existing `Period{Label, Start, End}` struct, `generateDailyPeriods/Hourly/Weekly/Monthly(start, end time.Time) []Period`.
- Produces: no new exported symbols.

- [ ] **Step 1: Write failing test for the infinite-loop guard (M1)**

Add to `internal/backend/services/statistics_query_test.go` (create file with package `services` if absent):
```go
package services

import (
	"testing"
	"time"
)

func TestGenerateDailyPeriods_EqualStartEnd_Terminates(t *testing.T) {
	sqs := &StatisticsQueryService{}
	d := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	done := make(chan []Period, 1)
	go func() { done <- sqs.generateDailyPeriods(d, d) }()
	select {
	case <-done: // returns without hanging
	case <-time.After(2 * time.Second):
		t.Fatal("generateDailyPeriods did not terminate for start == end")
	}
}
```

- [ ] **Step 2: Run it, confirm it hangs/fails**

Run: `go test ./internal/backend/services/ -run TestGenerateDailyPeriods_EqualStartEnd_Terminates -timeout 10s`
Expected: FAIL (test times out after 2s — loop never ends).

- [ ] **Step 3: Fix M1 — strict `Before(end)` in all four generators**

In `generateDailyPeriods`, `generateHourlyPeriods`, `generateWeeklyPeriods`, `generateMonthlyPeriods`, change every loop condition:
```go
	for current.Before(end) || current.Equal(end) {
```
to:
```go
	for current.Before(end) {
```

- [ ] **Step 4: Run the test, confirm pass**

Run: `go test ./internal/backend/services/ -run TestGenerateDailyPeriods_EqualStartEnd_Terminates -timeout 10s`
Expected: PASS.

- [ ] **Step 5: Fix M2 — remove the shadow `AggregatedResult` re-declaration**

In `convertAggregatedToResolvedAlertItem` (~line 1315), there is a local `type AggregatedResult struct {...}` identical to the one declared in `QueryResolvedAlerts`. Hoist a single definition to package scope (top of file, near other types) and delete BOTH local re-declarations so the `case AggregatedResult:` type switch actually matches the value passed in. Keep the reflection branch as-is (it becomes dead but harmless); do not delete it in this task to keep the diff minimal.

Verify the package-scope type matches the fields used by both call sites (`Fingerprint, AlertName, Severity, Count, FirstFiredAt, LastResolvedAt, ...`). Read both sites before editing.

- [ ] **Step 6: Fix M3 — GROUP BY full expression in `aggregateByTeam`**

At ~line 432, change:
```go
		Group("team").
```
to:
```go
		Group("COALESCE(metadata->'labels'->>'team', 'unknown')").
```

- [ ] **Step 7: Fix L (date-range query reuse) in `GetStatisticsSummary`**

At ~line 917, where `baseQuery` is reused after `.Count()` for the MIN/MAX date scan, insert a fresh session:
```go
	if err := baseQuery.Session(&gorm.Session{}).
		Select("MIN(fired_at) as min_date, MAX(fired_at) as max_date").
		Scan(&dateRange).Error; err != nil {
```
(Adjust to match the exact existing call; only add `.Session(&gorm.Session{})`. Confirm `gorm` is imported — it is, used elsewhere in the file.)

- [ ] **Step 8: Build + full package test**

Run: `go build ./... && go test ./internal/backend/services/ -timeout 60s`
Expected: build success, tests pass.

- [ ] **Step 9: Commit**

```bash
git add internal/backend/services/statistics_query.go internal/backend/services/statistics_query_test.go
git commit -m "fix(statistics): prevent period infinite loop, fix team GROUP BY, dedup AggregatedResult, reset date-range session"
```

---

### Task 3: WebUI handlers — nil-safe timestamps, validation, limit caps

**Files:**
- Modify: `internal/webui/handlers/statistics_handlers.go` — lines 97-100 (C1), `convertBreakdownItems` ~270 (C2), FiredAt/CreatedAt sites 229/460/490/509-510/527-528/546 (H4), QueryStatistics limit 69 + GetAlertsByName ~189 (M7), timezone 53/79 (M6), QueryRecentlyResolved required tags ~315 (M8)

**Interfaces:**
- Consumes: proto getters `resp.GetTimeRange()`, `item.GetStartTime()` etc. (protobuf-generated getters are nil-safe and return zero value).
- Produces: a small helper `tsToTime(ts *timestamppb.Timestamp) time.Time` used across this file.

- [ ] **Step 1: Add nil-safe timestamp helper**

Near the top of the file (after imports, before the first handler), add:
```go
// tsToTime converts a possibly-nil protobuf timestamp to time.Time (zero value if nil).
func tsToTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
```

- [ ] **Step 2: Fix C1 — guard `resp.TimeRange`**

Replace lines 96-103:
```go
	result := gin.H{
		"time_range": gin.H{
			"start": resp.TimeRange.Start.AsTime(),
			"end":   resp.TimeRange.End.AsTime(),
		},
		"total_alerts": resp.TotalAlerts,
		"statistics":   convertStatisticsMap(resp.Statistics),
	}
```
with:
```go
	result := gin.H{
		"total_alerts": resp.TotalAlerts,
		"statistics":   convertStatisticsMap(resp.Statistics),
	}
	if resp.TimeRange != nil {
		result["time_range"] = gin.H{
			"start": tsToTime(resp.TimeRange.Start),
			"end":   tsToTime(resp.TimeRange.End),
		}
	}
```

- [ ] **Step 3: Fix C2 + H4 — replace all unguarded `.AsTime()` with `tsToTime(...)`**

Read each site (lines ~229, ~270-271, ~460, ~490, ~509-510, ~527-528, ~546). For each `X.AsTime()` where `X` is a `*timestamppb.Timestamp` proto field, replace with `tsToTime(X)`. Verify by grepping: `rg -n '\.AsTime\(\)' internal/webui/handlers/statistics_handlers.go` — after this step, no proto-field `.AsTime()` should remain unguarded (a `tsToTime`-wrapped one is fine).

- [ ] **Step 4: Fix M6 — validate timezone before forwarding (both handlers)**

In `QueryStatistics` (after `ShouldBindJSON`, ~line 60) and in `QueryRecentlyResolved` similarly, add:
```go
	if request.Timezone != "" {
		if _, err := time.LoadLocation(request.Timezone); err != nil {
			c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid timezone"))
			return
		}
	}
```

- [ ] **Step 5: Fix M7 — cap Limit on QueryStatistics and GetAlertsByName**

After binding, before building the gRPC req, in both handlers:
```go
	if request.Limit > 1000 {
		request.Limit = 1000
	}
```
(Use the existing field name; `GetAlertsByName`'s request uses its own struct — match it.)

- [ ] **Step 6: Fix M8 — require start/end dates in QueryRecentlyResolved**

In the `QueryRecentlyResolved` request struct, add `binding:"required"` to the `start_date` and `end_date` string fields (mirror `GetAlertsByName`).

- [ ] **Step 7: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 8: Commit**

```bash
git add internal/webui/handlers/statistics_handlers.go
git commit -m "fix(statistics): nil-safe timestamps, timezone validation, limit caps, required date params"
```

---

### Task 4: gRPC client — nil guards on statisticsClient + nullable timestamps

**Files:**
- Modify: `internal/webui/client/backend_client.go` — all statistics methods (~1558-2162), `IsConnected` (~70), QueryRecentlyResolved conversion (~1723-1724)

**Interfaces:**
- Consumes: `c.statisticsClient` field.
- Produces: no new exported symbols.

- [ ] **Step 1: Fix H3 — extend `IsConnected()` to also require statisticsClient**

At ~line 70, change:
```go
	return c.conn != nil && c.authClient != nil
```
to:
```go
	return c.conn != nil && c.authClient != nil && c.statisticsClient != nil
```

- [ ] **Step 2: Fix H3 — add a nil guard at the top of each statistics method**

For each method calling `c.statisticsClient.X` (QueryStatistics, GetStatisticsSummary, GetAlertHistory, GetAlertsByName, GetStatisticsViews, SaveStatisticsView, UpdateStatisticsView, DeleteStatisticsView, SetDefaultStatisticsView, QueryRecentlyResolved), add as the first statement (match each method's return signature):
```go
	if c.statisticsClient == nil {
		return nil, fmt.Errorf("statistics client not connected")
	}
```
Read each signature first; some return `(*T, error)`, return `nil, err`; if any returns only `error`, return just the error. (`fmt` is already imported.)

- [ ] **Step 3: Fix M9 — nil-safe `.AsTime()` in QueryRecentlyResolved conversion**

At ~lines 1723-1724, guard `FirstFiredAt`/`LastResolvedAt` before `.AsTime()`. If the file lacks a local helper, inline the check:
```go
		var firstFired, lastResolved time.Time
		if item.FirstFiredAt != nil {
			firstFired = item.FirstFiredAt.AsTime()
		}
		if item.LastResolvedAt != nil {
			lastResolved = item.LastResolvedAt.AsTime()
		}
```
and use `firstFired`/`lastResolved` in the result struct.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/client/backend_client.go
git commit -m "fix(statistics): guard nil statisticsClient and nullable timestamps in client"
```

---

### Task 5: Frontend templ/JS — missing method, weighted averages, absolute time, years unit, XSS, time-filter default

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (H5 ~2885, H6 ~2410-2432, M4 ~2241-2253 + ~1939, M5 ~1841-1910, L1 ~4315, L3 ~2094-2098)
- Modify: `internal/webui/templates/scripts/dashboard_filter_presets.templ` (L2 ~419-455)
- Regenerate: `*_templ.go` via `make webui-templates`

**Interfaces:**
- Consumes: existing Alpine methods `loadTimeSeriesData()`, `updateTimeSeriesChart()`, `escapeHtml`/`copyToClipboard` utilities if present (check `dashboard_utilities.templ`).
- Produces: no new cross-file contract.

- [ ] **Step 1: Fix H5 — replace the non-existent `loadTrendData()` call**

In `StatisticsDashboard.templ` ~line 2885, in `setChartPeriodType`, replace `this.loadTrendData()` with:
```js
this.loadTimeSeriesData().then(() => this.updateTimeSeriesChart());
```
First grep to confirm the real method names exist: `rg -n "loadTimeSeriesData|updateTimeSeriesChart" internal/webui/templates/pages/StatisticsDashboard.templ`. If named differently, use the actual names.

- [ ] **Step 2: Fix H6 — weighted averages in getOverallAvgMTTR/MTTA/FixTime**

For each of the three functions (~2410-2432), replace the mean-of-means with a count-weighted average. Pattern (adapt field names — confirm `total_mttr_seconds`/`avg_mttr_seconds`/`count` exist in `statsData.statistics` entries):
```js
getOverallAvgMTTR() {
    const stats = Object.values(this.statsData?.statistics || {});
    const totalAlerts = stats.reduce((s, g) => s + (g.count || 0), 0);
    if (totalAlerts === 0) return 0;
    const totalSecs = stats.reduce((s, g) => s + ((g.total_mttr_seconds != null) ? g.total_mttr_seconds : (g.avg_mttr_seconds || 0) * (g.count || 0)), 0);
    return totalSecs / totalAlerts;
}
```
Apply the same shape to MTTA (`total_mtta_seconds`/`avg_mtta_seconds`) and FixTime (`total_fix_time_seconds`/`avg_fix_time_seconds`). Check which `total_*` fields the backend actually returns (`rg total_mttr internal/backend`); if only `avg_*` is available, use the `avg * count` fallback shown above.

- [ ] **Step 3: Fix M4 — honor absolute end time in the query payload**

In `queryStatistics()` (~line 1939) where `end_date` is built as `... + 'T23:59:59Z'`, replace with logic that uses `absoluteUntilTime` when in absolute mode and "Now" is unchecked:
```js
end_date: this.filters.timeRangeMode === 'absolute' && !this.filters.relativeUntil.now
    ? this.filters.endDate + 'T' + (this.filters.absoluteUntilTime || '23:59') + ':00Z'
    : (this.filters.relativeUntil.now ? new Date().toISOString() : this.filters.endDate + 'T23:59:59Z'),
```
Read the surrounding payload construction first and adapt to the exact existing variable names (`absoluteUntilTime`, `relativeUntil.now`, `timeRangeMode`).

- [ ] **Step 4: Fix M5 — add `years` case to `applyTimeRangeFromRelative`**

In the inline `switch` of `applyTimeRangeFromRelative` (~line 1877), add alongside the other units:
```js
case 'years': fromDate.setFullYear(fromDate.getFullYear() - value); break;
```

- [ ] **Step 5: Fix L1 — escape filename in `showExportSuccess`**

At ~line 4315, do not interpolate `filename` into `innerHTML`. Either build the toast with `textContent` for the filename portion, or escape:
```js
const safe = String(filename).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#039;'}[c]));
```
and use `safe` in the template string.

- [ ] **Step 6: Fix L2 — escape user values in `getFilterSummary`**

In `dashboard_filter_presets.templ` (~419-455), wrap every interpolated user-controlled value (`searchQuery`, joined `teams`/`alertNames`/`alertmanagers`) with an escape helper before concatenating into the `x-html` string. Reuse the same `escapeHtml` regex as Step 5 (define a small `esc()` once if no shared helper exists).

- [ ] **Step 7: Fix L3 — correct `isTimeFilterActive` baseline**

At ~line 2094, the default `weekendMode` is `'full_weekends'` but the active-check compares against `'same_hours'`. Make the neutral state explicit: time filter is active only when `filterByTimeOfDay` is on OR the time-of-day window differs from default. Replace the body of `isTimeFilterActive()`:
```js
isTimeFilterActive() {
    return this.filters.filterByTimeOfDay === true;
}
```
Confirm `filterByTimeOfDay` is the binding that actually gates `filter_by_time_of_day` in the payload; if the dashboard intends time filtering to be opt-in, this also stops it being sent on every query. Read lines 2090-2110 before editing.

- [ ] **Step 8: Regenerate templates + build**

Run: `make webui-templates && go build ./...`
Expected: `templ generate` success, build success. (`*_templ.go` files will change — stage them.)

- [ ] **Step 9: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/scripts/dashboard_filter_presets.templ internal/webui/templates/pages/StatisticsDashboard_templ.go internal/webui/templates/scripts/dashboard_filter_presets_templ.go
git commit -m "fix(statistics): fix chart period reload, weighted KPIs, absolute end time, years range, XSS escaping, time-filter default"
```

---

## Self-Review

- **Coverage:** C1, C2, H1-H6, M1-M9, L1-L4 → mapped (C1/C2/H4/M6/M7/M8 → Task 3; H1/H2 → Task 1; M1/M2/M3 + date-range → Task 2; H3/M9 → Task 4; H5/H6/M4/M5/L1/L2/L3 → Task 5; L4 offset pagination → see note). 
- **L4 (GetAlertsByName offset):** folded into Task 3 only if the proto `GetAlertsByNameRequest` has an `offset` field — the implementer must check `rg -n offset internal/backend/proto/alert/alert.pb.go | rg -i alertsbyname` first; if absent, skip (no silent regression possible) and note it in the task commit body.
- **Excluded by design:** per-user scoping (single-tenant), the 2 agent-retracted findings.
- **Placeholders:** none — every step has concrete code or a "read-then-adapt" instruction tied to exact line ranges.
- **Type consistency:** `tsToTime` defined once (Task 3 Step 1) and reused; `MTTASeconds` matches model field; weighted-average field names flagged for verification against backend.

## Verification (manual, after all tasks)

- `go build ./...` and `go test ./internal/backend/...` pass.
- `make webui-templates` regenerates cleanly with no diff drift.
- Optional smoke: run the app, open `/statistics`, switch chart period type (H5), apply an absolute end time (M4), export CSV (L1).
