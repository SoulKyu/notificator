# Time of Day Filter Redesign - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Simplify the Time of Day filter by removing the enable checkbox, always showing time pickers with sensible defaults, and adding an On-Call quick preset.

**Architecture:** Frontend-first approach - modify UI behavior, then update backend to support `weekend_mode` string instead of `include_weekends` boolean.

**Tech Stack:** Alpine.js, templ, Go, Protocol Buffers, gRPC

---

## Task 1: Update Frontend Filter State

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ:1430-1440`

**Step 1: Update the filters object default values**

Find (around line 1432-1438):
```javascript
filterByTimeOfDay: false,
timeOfDayStart: '22:00',
timeOfDayEnd: '06:00',
includeWeekends: true,
```

Replace with:
```javascript
timeOfDayStart: '00:00',
timeOfDayEnd: '23:59',
weekendMode: 'same_hours', // 'exclude' | 'same_hours' | 'full_weekends'
activePreset: null, // null | 'oncall' | 'night' | 'business' | 'morning' | 'evening'
```

**Step 2: Verify change compiles**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: No errors

---

## Task 2: Update Time of Day Dropdown UI

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ:301-361`

**Step 1: Replace the entire Time of Day dropdown section**

Find the section starting at line 301 `<!-- Time of Day Filter -->` and ending at line 361.

Replace with:
```html
<!-- Time of Day Filter -->
<div class="relative" x-data="{ timeOfDayOpen: false }" @click.outside="timeOfDayOpen = false">
	<button @click="timeOfDayOpen = !timeOfDayOpen" type="button"
		class="inline-flex items-center gap-2 px-4 py-2.5 rounded-xl text-sm font-medium transition-colors border border-blue-200 dark:border-blue-700/50"
		:class="isTimeFilterActive() ? 'bg-blue-100 dark:bg-blue-800/40 text-blue-700 dark:text-blue-300 hover:bg-blue-200 dark:hover:bg-blue-800/60' : 'bg-white dark:bg-slate-800 text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-700'">
		<!-- Clock icon -->
		<svg class="w-4 h-4 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>
		</svg>
		<span x-text="getTimeOfDayButtonText()"></span>
		<svg class="w-3 h-3 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
		</svg>
	</button>
	<!-- Dropdown with time of day settings -->
	<div x-show="timeOfDayOpen" x-transition x-cloak class="absolute left-0 mt-2 w-80 bg-white dark:bg-dark-bg-secondary rounded-xl shadow-lg border border-slate-200 dark:border-slate-700 overflow-hidden z-50">
		<div class="p-4 space-y-4">
			<!-- Time inputs (always visible) -->
			<div class="grid grid-cols-2 gap-3">
				<div>
					<label class="block text-xs text-slate-500 dark:text-slate-400 mb-1">From</label>
					<input type="time" x-model="filters.timeOfDayStart" @change="filters.activePreset = null" class="w-full px-3 py-2 text-sm border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-dark-bg-tertiary text-slate-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"/>
				</div>
				<div>
					<label class="block text-xs text-slate-500 dark:text-slate-400 mb-1">To</label>
					<input type="time" x-model="filters.timeOfDayEnd" @change="filters.activePreset = null" class="w-full px-3 py-2 text-sm border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-dark-bg-tertiary text-slate-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"/>
				</div>
			</div>

			<!-- Quick presets -->
			<div>
				<span class="block text-xs text-slate-500 dark:text-slate-400 mb-2">Quick presets</span>
				<div class="flex flex-wrap gap-1.5">
					<button @click="setTimePreset('oncall')" type="button"
						class="px-2.5 py-1 text-xs font-medium rounded-lg transition-colors"
						:class="filters.activePreset === 'oncall' ? 'bg-purple-600 text-white' : 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 hover:bg-purple-200'">
						On-Call
					</button>
					<button @click="setTimePreset('night')" type="button"
						class="px-2.5 py-1 text-xs font-medium rounded-lg transition-colors"
						:class="filters.activePreset === 'night' ? 'bg-indigo-600 text-white' : 'bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 hover:bg-indigo-200'">
						Night
					</button>
					<button @click="setTimePreset('business')" type="button"
						class="px-2.5 py-1 text-xs font-medium rounded-lg transition-colors"
						:class="filters.activePreset === 'business' ? 'bg-emerald-600 text-white' : 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 hover:bg-emerald-200'">
						Business
					</button>
					<button @click="setTimePreset('morning')" type="button"
						class="px-2.5 py-1 text-xs font-medium rounded-lg transition-colors"
						:class="filters.activePreset === 'morning' ? 'bg-amber-600 text-white' : 'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 hover:bg-amber-200'">
						Morning
					</button>
					<button @click="setTimePreset('evening')" type="button"
						class="px-2.5 py-1 text-xs font-medium rounded-lg transition-colors"
						:class="filters.activePreset === 'evening' ? 'bg-orange-600 text-white' : 'bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300 hover:bg-orange-200'">
						Evening
					</button>
				</div>
			</div>

			<!-- Weekend mode dropdown -->
			<div>
				<label class="block text-xs text-slate-500 dark:text-slate-400 mb-1">Weekends</label>
				<div class="relative">
					<select x-model="filters.weekendMode"
						:disabled="filters.activePreset === 'oncall'"
						class="w-full px-3 py-2 text-sm border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-dark-bg-tertiary text-slate-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed appearance-none pr-8">
						<option value="exclude">Exclude weekends</option>
						<option value="same_hours">Same hours</option>
						<option value="full_weekends">Full weekends</option>
					</select>
					<svg class="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400 pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
					</svg>
				</div>
				<!-- Tooltip for on-call lock -->
				<p x-show="filters.activePreset === 'oncall'" class="text-xs text-purple-600 dark:text-purple-400 mt-1">
					On-call includes full weekends by design
				</p>
				<!-- Tooltips for each option -->
				<p x-show="filters.activePreset !== 'oncall'" class="text-xs text-slate-400 dark:text-slate-500 mt-1">
					<span x-show="filters.weekendMode === 'exclude'">Saturday and Sunday are excluded</span>
					<span x-show="filters.weekendMode === 'same_hours'">Same time filter applies to weekends</span>
					<span x-show="filters.weekendMode === 'full_weekends'">Include all 24 hours on Sat/Sun</span>
				</p>
			</div>
		</div>

		<!-- Reset button when not default -->
		<div x-show="isTimeFilterActive()" class="border-t border-slate-100 dark:border-slate-700 p-2">
			<button @click="resetTimeFilter()" class="w-full px-3 py-1.5 text-xs text-slate-500 dark:text-slate-300 hover:text-slate-700 dark:hover:text-slate-200 text-center">
				Reset to default
			</button>
		</div>
	</div>
</div>
```

**Step 2: Verify change compiles**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: No errors

---

## Task 3: Add Helper Functions

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ:1709-1728`

**Step 1: Replace setTimePreset function and add helper functions**

Find the `setTimePreset` function (around line 1709-1728) and replace with:

```javascript
setTimePreset(preset) {
	this.filters.activePreset = preset;

	switch(preset) {
		case 'oncall':
			const schedule = this.getOnCallSchedule();
			this.filters.timeOfDayStart = schedule.weekdayStart;
			this.filters.timeOfDayEnd = schedule.weekdayEnd;
			this.filters.weekendMode = 'full_weekends'; // Forced for on-call
			break;
		case 'night':
			this.filters.timeOfDayStart = '22:00';
			this.filters.timeOfDayEnd = '06:00';
			break;
		case 'business':
			this.filters.timeOfDayStart = '09:00';
			this.filters.timeOfDayEnd = '18:00';
			break;
		case 'morning':
			this.filters.timeOfDayStart = '06:00';
			this.filters.timeOfDayEnd = '12:00';
			break;
		case 'evening':
			this.filters.timeOfDayStart = '18:00';
			this.filters.timeOfDayEnd = '22:00';
			break;
	}
},

getTimeOfDayButtonText() {
	const start = this.filters.timeOfDayStart;
	const end = this.filters.timeOfDayEnd;

	if (this.filters.activePreset) {
		const presetNames = {
			oncall: 'On-Call',
			night: 'Night',
			business: 'Business',
			morning: 'Morning',
			evening: 'Evening'
		};
		return `${presetNames[this.filters.activePreset]} (${start} - ${end})`;
	}
	return `${start} - ${end}`;
},

isTimeFilterActive() {
	return this.filters.timeOfDayStart !== '00:00' ||
		   this.filters.timeOfDayEnd !== '23:59' ||
		   this.filters.weekendMode !== 'same_hours';
},

resetTimeFilter() {
	this.filters.timeOfDayStart = '00:00';
	this.filters.timeOfDayEnd = '23:59';
	this.filters.weekendMode = 'same_hours';
	this.filters.activePreset = null;
},
```

**Step 2: Verify change compiles**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: No errors

---

## Task 4: Update API Request Payloads

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (multiple locations)

**Step 1: Find all API request objects and update them**

Search for `filter_by_time_of_day` and `include_weekends` in the file. Update each API request object.

The old pattern:
```javascript
filter_by_time_of_day: this.filters.filterByTimeOfDay,
time_of_day_start: this.filters.timeOfDayStart,
time_of_day_end: this.filters.timeOfDayEnd,
include_weekends: this.filters.includeWeekends,
```

Replace with new pattern:
```javascript
filter_by_time_of_day: this.isTimeFilterActive(),
time_of_day_start: this.filters.timeOfDayStart,
time_of_day_end: this.filters.timeOfDayEnd,
weekend_mode: this.filters.weekendMode,
```

**Locations to update (approximate line numbers):**
- Line ~1613-1620 (loadStatistics)
- Line ~2182-2189 (loadTrendData)
- Line ~2214-2221 (loadOverviewData)
- Line ~2249-2256 (loadAlertData)
- Line ~3060-3067 (comparison query)
- Line ~3153-3160 (alert details)

**Step 2: Verify change compiles**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: No errors

---

## Task 5: Update formatTimeRangeDisplay Function

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ:1760-1766`

**Step 1: Update the display function**

Find the time filter display section in `formatTimeRangeDisplay` (around line 1762-1764):

```javascript
if (this.filters.filterByTimeOfDay) {
	display += ` (${this.filters.timeOfDayStart}-${this.filters.timeOfDayEnd})`;
}
```

Replace with:
```javascript
if (this.isTimeFilterActive()) {
	display += ` (${this.filters.timeOfDayStart}-${this.filters.timeOfDayEnd})`;
	if (this.filters.weekendMode === 'exclude') {
		display += ' [weekdays only]';
	} else if (this.filters.weekendMode === 'full_weekends') {
		display += ' [full weekends]';
	}
}
```

**Step 2: Verify change compiles**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: No errors

---

## Task 6: Update Proto Definition

**Files:**
- Modify: `proto/alert.proto`

**Step 1: Add weekend_mode field to QueryStatisticsRequest**

Find `QueryStatisticsRequest` message (around line 653) and add after `include_weekends`:

```protobuf
string weekend_mode = 16;         // "exclude", "same_hours", "full_weekends"
```

**Step 2: Add weekend_mode field to GetAlertsByNameRequest**

Find `GetAlertsByNameRequest` message and add:

```protobuf
string weekend_mode = 11;         // "exclude", "same_hours", "full_weekends"
```

**Step 3: Add weekend_mode field to StatisticsViewData**

Find `StatisticsViewData` message and add:

```protobuf
string weekend_mode = 12;         // "exclude", "same_hours", "full_weekends"
```

**Step 4: Regenerate proto files**

Run: `cd /home/gule/Workspace/soulkyu/notificator && make proto`
Expected: Proto files regenerated successfully

---

## Task 7: Update Backend QueryRequest Struct

**Files:**
- Modify: `internal/backend/services/statistics_query.go:35-44`

**Step 1: Update QueryRequest struct**

Find the struct and update:

```go
type QueryRequest struct {
	StartDate          time.Time
	EndDate            time.Time
	GroupBy            string
	SecondaryGroupBy   string
	PeriodType         string
	Limit              int
	Offset             int
	FilterByTimeOfDay  bool
	TimeOfDayStart     string
	TimeOfDayEnd       string
	WeekendMode        string   // "exclude", "same_hours", "full_weekends"
	Severities         []string
	Teams              []string
}
```

**Step 2: Update query logic**

Find the filtering logic (around line 81-88) and replace:

```go
// Apply time-of-day filter if enabled
if req.FilterByTimeOfDay && req.TimeOfDayStart != "" && req.TimeOfDayEnd != "" {
	baseQuery = sqs.applyTimeOfDayFilter(baseQuery, req.TimeOfDayStart, req.TimeOfDayEnd)
	// Apply weekend filter if not including weekends
	if !req.IncludeWeekends {
		baseQuery = sqs.applyWeekendFilter(baseQuery)
	}
}
```

With:

```go
// Apply time-of-day filter if enabled
if req.FilterByTimeOfDay && req.TimeOfDayStart != "" && req.TimeOfDayEnd != "" {
	// Apply weekend mode logic
	switch req.WeekendMode {
	case "exclude":
		// Apply time filter to weekdays only, exclude weekends entirely
		baseQuery = sqs.applyTimeOfDayFilter(baseQuery, req.TimeOfDayStart, req.TimeOfDayEnd)
		baseQuery = sqs.applyWeekendFilter(baseQuery)
	case "full_weekends":
		// Apply time filter only to weekdays, include all of weekends
		baseQuery = sqs.applyTimeOfDayFilterWeekdaysOnly(baseQuery, req.TimeOfDayStart, req.TimeOfDayEnd)
	default: // "same_hours"
		// Apply same time filter to all days
		baseQuery = sqs.applyTimeOfDayFilter(baseQuery, req.TimeOfDayStart, req.TimeOfDayEnd)
	}
}
```

**Step 3: Add new helper function**

Add after `applyWeekendFilter`:

```go
// applyTimeOfDayFilterWeekdaysOnly applies time filter only to weekdays, includes full weekends
func (sqs *StatisticsQueryService) applyTimeOfDayFilterWeekdaysOnly(query *gorm.DB, startTime, endTime string) *gorm.DB {
	startMinutes := parseTimeToMinutes(startTime)
	endMinutes := parseTimeToMinutes(endTime)

	if startMinutes < 0 || endMinutes < 0 {
		return query
	}

	isPostgres := sqs.db.IsPostgreSQL()

	if startMinutes <= endMinutes {
		// Same-day range on weekdays OR any time on weekends
		if isPostgres {
			return query.Where(
				"(EXTRACT(DOW FROM fired_at) IN (0, 6)) OR ((EXTRACT(HOUR FROM fired_at) * 60 + EXTRACT(MINUTE FROM fired_at)) BETWEEN ? AND ?)",
				startMinutes, endMinutes,
			)
		}
		return query.Where(
			"(CAST(strftime('%w', fired_at) AS INTEGER) IN (0, 6)) OR ((CAST(strftime('%H', fired_at) AS INTEGER) * 60 + CAST(strftime('%M', fired_at) AS INTEGER)) BETWEEN ? AND ?)",
			startMinutes, endMinutes,
		)
	}

	// Cross-midnight range on weekdays OR any time on weekends
	if isPostgres {
		return query.Where(
			"(EXTRACT(DOW FROM fired_at) IN (0, 6)) OR ((EXTRACT(HOUR FROM fired_at) * 60 + EXTRACT(MINUTE FROM fired_at)) >= ? OR (EXTRACT(HOUR FROM fired_at) * 60 + EXTRACT(MINUTE FROM fired_at)) <= ?)",
			startMinutes, endMinutes,
		)
	}
	return query.Where(
		"(CAST(strftime('%w', fired_at) AS INTEGER) IN (0, 6)) OR ((CAST(strftime('%H', fired_at) AS INTEGER) * 60 + CAST(strftime('%M', fired_at) AS INTEGER)) >= ? OR (CAST(strftime('%H', fired_at) AS INTEGER) * 60 + CAST(strftime('%M', fired_at) AS INTEGER)) <= ?)",
		startMinutes, endMinutes,
	)
}
```

**Step 4: Verify changes compile**

Run: `cd /home/gule/Workspace/soulkyu/notificator && go build ./...`
Expected: No errors

---

## Task 8: Update Backend Models and Handlers

**Files:**
- Modify: `internal/backend/models/statistics_view.go`
- Modify: `internal/webui/models/statistics_view.go`
- Modify: `internal/webui/handlers/statistics_handlers.go`

**Step 1: Update models to add WeekendMode field**

In both model files, find the struct with `IncludeWeekends` and add:

```go
WeekendMode       string `json:"weekend_mode"`        // "exclude", "same_hours", "full_weekends"
```

**Step 2: Update handler request parsing**

In `statistics_handlers.go`, find the request struct and handler that uses `IncludeWeekends` and add `WeekendMode` mapping.

**Step 3: Verify changes compile**

Run: `cd /home/gule/Workspace/soulkyu/notificator && go build ./...`
Expected: No errors

---

## Task 9: Update gRPC Service Mappings

**Files:**
- Modify: `internal/backend/services/statistics_grpc_service.go`
- Modify: `internal/webui/client/backend_client.go`

**Step 1: Update statistics_grpc_service.go**

Find all places where `IncludeWeekends` is mapped and add `WeekendMode` mapping alongside it.

**Step 2: Update backend_client.go**

Find all places where `IncludeWeekends` is mapped and add `WeekendMode` mapping.

**Step 3: Verify changes compile**

Run: `cd /home/gule/Workspace/soulkyu/notificator && go build ./...`
Expected: No errors

---

## Task 10: Final Verification

**Step 1: Run templ generate**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: Success

**Step 2: Run full build**

Run: `cd /home/gule/Workspace/soulkyu/notificator && make build`
Expected: Success

**Step 3: Run tests**

Run: `cd /home/gule/Workspace/soulkyu/notificator && make test`
Expected: All tests pass

---

## Summary

This implementation:
1. Removes the "Enable time of day filter" checkbox
2. Always shows time pickers with defaults (00:00-23:59)
3. Adds On-Call preset that uses saved settings and forces full weekends
4. Replaces weekend checkbox with dropdown (exclude/same_hours/full_weekends)
5. Shows preset name in button when active
6. Updates backend to support new weekend_mode logic
