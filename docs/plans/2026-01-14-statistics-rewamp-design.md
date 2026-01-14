# Statistics View Revamp - Design Document

## Overview

This document outlines the design for revamping the Statistics view in Notificator with three main features:
1. **Saved Views** - Per-user saved filter configurations with sharing capability
2. **Improved Time Filtering** - On-Call Period quick filter with global configuration
3. **Layout Redesign** - Visual connection between group-by selection and related charts

## 1. Saved Views for Statistics

### Requirements
- Users can save their current filter state as a "View" (renamed from "Saved Filters")
- Views are per-user by default with option to share to everyone
- Separate implementation from dashboard FilterPresets (own code, own table)
- Same UX pattern as existing Saved Filters for alerts

### Data Model

**New DB Table: `statistics_views`**
```go
// internal/backend/models/statistics_view.go
type StatisticsView struct {
    ID          string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
    UserID      string    `gorm:"not null;size:32;index" json:"user_id"`
    Name        string    `gorm:"not null;size:255" json:"name"`
    Description string    `gorm:"type:text" json:"description,omitempty"`
    IsShared    bool      `gorm:"default:false;index" json:"is_shared"`
    IsDefault   bool      `gorm:"default:false" json:"is_default"`
    ViewData    JSONB     `gorm:"type:jsonb;not null" json:"view_data"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`

    User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}
```

**WebUI Model: `StatisticsViewData`**
```go
// internal/webui/models/statistics_view.go
type StatisticsViewData struct {
    // Date range
    StartDate string `json:"start_date,omitempty"` // Relative or absolute (e.g., "7d", "30d", "2024-01-01")
    EndDate   string `json:"end_date,omitempty"`   // Relative or absolute

    // Time filtering
    FilterByTimeOfDay bool   `json:"filter_by_time_of_day"`
    TimeOfDayStart    string `json:"time_of_day_start,omitempty"` // "HH:MM" format
    TimeOfDayEnd      string `json:"time_of_day_end,omitempty"`
    UseOnCallPeriod   bool   `json:"use_on_call_period"`          // Use global on-call config

    // Grouping
    GroupBy    string `json:"group_by,omitempty"`    // "severity", "team", "alert_name", "period"
    PeriodType string `json:"period_type,omitempty"` // "hour", "day", "week", "month"

    // Other filters
    ApplyRules bool `json:"apply_rules"`
    Limit      int  `json:"limit,omitempty"` // For top N alerts
}
```

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/statistics/views` | List user's views + shared views |
| POST | `/api/v1/statistics/views` | Create new view |
| PUT | `/api/v1/statistics/views/:id` | Update view |
| DELETE | `/api/v1/statistics/views/:id` | Delete view |
| POST | `/api/v1/statistics/views/:id/default` | Set as default view |

### Frontend Implementation

**New JS Mixin: `statisticsViewsMixin()`** in `internal/webui/templates/scripts/statistics_views.templ`

Following the pattern from `dashboard_filter_presets_templ.go`:
- `loadViews()` - Load all views (user's + shared)
- `saveView()` - Save current filter state
- `applyView(view)` - Apply view to filters
- `deleteView(id)` - Delete a view
- `setDefaultView(id)` - Set view as default

**UI Location**: Add dropdown next to filter panel header (same placement as dashboard Saved Filters)

---

## 2. On-Call Period Time Filter

### Requirements
- Add "On-Call" quick button to time presets
- On-Call period: weekdays 18:00-08:00 + full weekends (configurable globally)
- Add "Past Month" preset to quick date range buttons
- Global on-call schedule configurable in Settings modal (General section)

### On-Call Configuration Model

**Add to existing settings storage** (localStorage + optional backend sync):
```javascript
// Part of dashboardSettings in localStorage
{
    theme: 'light',
    resolvedAlertsLimit: 100,
    refreshInterval: 30,
    // NEW: On-Call Schedule Configuration
    onCallSchedule: {
        enabled: true,
        weekdayStart: '18:00',  // Start of on-call on weekdays
        weekdayEnd: '08:00',    // End of on-call on weekdays (next morning)
        includeWeekends: true,  // Full weekends are on-call
        weekendStart: 'friday-18:00', // When weekend on-call starts
        weekendEnd: 'monday-08:00'    // When weekend on-call ends
    }
}
```

### Settings Modal Changes

Add to General section in `dashboard_settings.templ`:

```html
<!-- On-Call Schedule Configuration -->
<div class="mt-6 border-t border-gray-200 dark:border-gray-700 pt-6">
    <h4 class="text-sm font-medium text-gray-900 dark:text-white mb-4">
        On-Call Schedule
    </h4>
    <p class="text-xs text-gray-500 dark:text-gray-400 mb-4">
        Configure your on-call hours for quick filtering in Statistics.
    </p>

    <!-- Weekday Hours -->
    <div class="flex items-center space-x-4 mb-4">
        <label class="text-sm text-gray-700 dark:text-gray-300 w-32">Weekday Hours</label>
        <input type="time" x-model="settings.onCallSchedule.weekdayStart" />
        <span>to</span>
        <input type="time" x-model="settings.onCallSchedule.weekdayEnd" />
    </div>

    <!-- Weekend Toggle -->
    <label class="flex items-center">
        <input type="checkbox" x-model="settings.onCallSchedule.includeWeekends" />
        <span class="ml-2">Include full weekends as on-call</span>
    </label>
</div>
```

### Statistics Dashboard Changes

**Add to Time Presets** (line ~102-107 in StatisticsDashboard.templ):
```html
<!-- Existing presets -->
<button @click="setDateRange('today')">Today</button>
<button @click="setDateRange('week')">Last 7 Days</button>
<button @click="setDateRange('month')">Last 30 Days</button>
<button @click="setDateRange('quarter')">Last 90 Days</button>
<!-- NEW: Past Month preset -->
<button @click="setDateRange('past_month')">Past Month</button>
```

**Add On-Call Quick Button** (new section below time presets):
```html
<!-- On-Call Quick Filter -->
<div class="flex items-center mt-2">
    <button
        @click="applyOnCallFilter()"
        class="text-xs px-2 py-1 bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 rounded hover:bg-purple-200">
        <svg class="w-3 h-3 inline mr-1"><!-- moon icon --></svg>
        On-Call Hours
    </button>
    <span class="text-xs text-gray-500 ml-2" x-text="getOnCallDescription()"></span>
</div>
```

**New JS Methods**:
```javascript
applyOnCallFilter() {
    const schedule = this.getOnCallSchedule();
    this.filters.filterByTimeOfDay = true;
    this.filters.timeOfDayStart = schedule.weekdayStart;
    this.filters.timeOfDayEnd = schedule.weekdayEnd;
    // Note: Weekend handling requires server-side logic
    // (filter by day-of-week OR time-of-day)
},

getOnCallSchedule() {
    const saved = localStorage.getItem('dashboardSettings');
    if (saved) {
        const settings = JSON.parse(saved);
        if (settings.onCallSchedule) {
            return settings.onCallSchedule;
        }
    }
    // Default on-call schedule
    return {
        weekdayStart: '18:00',
        weekdayEnd: '08:00',
        includeWeekends: true
    };
},

getOnCallDescription() {
    const schedule = this.getOnCallSchedule();
    let desc = `${schedule.weekdayStart} - ${schedule.weekdayEnd} weekdays`;
    if (schedule.includeWeekends) {
        desc += ' + weekends';
    }
    return desc;
}
```

### Backend Changes for On-Call Filtering

The current `filter_by_time_of_day` filter only handles time ranges within a day. For on-call (which includes full weekends), we need to extend the query logic:

**Extend Statistics Query Payload**:
```go
type StatisticsQueryRequest struct {
    // ... existing fields ...
    FilterByTimeOfDay bool   `json:"filter_by_time_of_day"`
    TimeOfDayStart    string `json:"time_of_day_start"`
    TimeOfDayEnd      string `json:"time_of_day_end"`
    // NEW: Full on-call filtering
    UseOnCallFilter   bool   `json:"use_on_call_filter"`
    IncludeWeekends   bool   `json:"include_weekends"`
}
```

**Query Logic** (pseudo-code):
```sql
-- If use_on_call_filter is true:
WHERE (
    -- Weekday nights (Mon-Thu 18:00 - next day 08:00)
    (EXTRACT(DOW FROM fired_at) BETWEEN 1 AND 4
     AND (EXTRACT(HOUR FROM fired_at) >= 18 OR EXTRACT(HOUR FROM fired_at) < 8))
    OR
    -- Friday night to Monday morning (if include_weekends)
    (EXTRACT(DOW FROM fired_at) = 5 AND EXTRACT(HOUR FROM fired_at) >= 18)
    OR
    (EXTRACT(DOW FROM fired_at) IN (0, 6)) -- Saturday, Sunday
    OR
    (EXTRACT(DOW FROM fired_at) = 1 AND EXTRACT(HOUR FROM fired_at) < 8) -- Monday morning
)
```

---

## 3. Layout Redesign - Group-By Visual Connection

### Requirements
From the French requirements doc:
> "J'aimerai mettre en avant (par une couleur ?) le group by, couleur qu'on retrouverait dans l'encadré des graphes / tableaux liés au group by."

Translation: Highlight the group-by selection with a color that appears in the frame of related graphs/tables.

### Design Approach

**Color Coding System**:
- Each group-by option has an associated accent color
- When a group-by is selected, its color appears as a border/accent on related charts

**Group-By Colors**:
```javascript
const groupByColors = {
    'severity': {
        border: 'border-red-500',
        bg: 'bg-red-50 dark:bg-red-900/10',
        accent: '#ef4444' // red-500
    },
    'team': {
        border: 'border-blue-500',
        bg: 'bg-blue-50 dark:bg-blue-900/10',
        accent: '#3b82f6' // blue-500
    },
    'alert_name': {
        border: 'border-green-500',
        bg: 'bg-green-50 dark:bg-green-900/10',
        accent: '#22c55e' // green-500
    },
    'period': {
        border: 'border-purple-500',
        bg: 'bg-purple-50 dark:bg-purple-900/10',
        accent: '#a855f7' // purple-500
    },
    '': { // Overall (no grouping)
        border: 'border-gray-300',
        bg: 'bg-gray-50 dark:bg-gray-900/10',
        accent: '#6b7280' // gray-500
    }
};
```

### UI Changes

**Group-By Selector Enhancement**:
```html
<div :class="getGroupByContainerClass()">
    <label class="block text-sm font-medium mb-2" :class="getGroupByLabelClass()">
        Group By
    </label>
    <select x-model="filters.groupBy" :class="getGroupBySelectClass()">
        <option value="">Overall</option>
        <option value="severity">Severity</option>
        <option value="team">Team</option>
        <option value="alert_name">Alert Name</option>
        <option value="period">Time Period</option>
    </select>
</div>
```

**Chart Cards with Group-By Accent**:
```html
<!-- Distribution Chart (shows when groupBy is set) -->
<div x-show="filters.groupBy && filters.groupBy !== 'period'"
     class="rounded-lg shadow-sm p-6 border-l-4"
     :class="getGroupByCardClass()">
    <h3 class="text-lg font-medium mb-4">
        Distribution by <span x-text="getGroupByLabel()" class="font-bold"></span>
    </h3>
    <!-- Chart content -->
</div>
```

**Helper Methods**:
```javascript
getGroupByColors() {
    return {
        'severity': { border: 'border-l-red-500', bg: 'bg-red-50/50 dark:bg-red-900/10' },
        'team': { border: 'border-l-blue-500', bg: 'bg-blue-50/50 dark:bg-blue-900/10' },
        'alert_name': { border: 'border-l-green-500', bg: 'bg-green-50/50 dark:bg-green-900/10' },
        'period': { border: 'border-l-purple-500', bg: 'bg-purple-50/50 dark:bg-purple-900/10' },
        '': { border: 'border-l-gray-300', bg: 'bg-white dark:bg-dark-bg-secondary' }
    };
},

getGroupByCardClass() {
    const colors = this.getGroupByColors();
    const selected = colors[this.filters.groupBy] || colors[''];
    return `${selected.border} ${selected.bg} border border-gray-200 dark:border-dark-border-subtle`;
},

getGroupByLabel() {
    const labels = {
        'severity': 'Severity',
        'team': 'Team',
        'alert_name': 'Alert Name',
        'period': 'Time Period'
    };
    return labels[this.filters.groupBy] || 'Overall';
}
```

---

## Implementation Plan

### Phase 1: On-Call Time Filter
1. Add on-call schedule configuration to Settings modal (General section)
2. Add "Past Month" date preset button
3. Add "On-Call Hours" quick filter button
4. Update statistics query to support on-call filtering

### Phase 2: Saved Views
1. Create `statistics_views` DB table and model
2. Create `StatisticsView` WebUI model
3. Implement API handlers (CRUD + default)
4. Implement gRPC methods in backend client
5. Create `statisticsViewsMixin()` JavaScript
6. Add Saved Views dropdown to statistics page

### Phase 3: Layout Redesign
1. Add color system for group-by options
2. Update Group-By selector with color accent
3. Update chart cards with group-by color borders
4. Add visual indicator showing active group-by

---

## Files to Create/Modify

### New Files
- `internal/backend/models/statistics_view.go` - DB model
- `internal/webui/models/statistics_view.go` - WebUI model
- `internal/webui/handlers/statistics_view_handlers.go` - API handlers
- `internal/webui/templates/scripts/statistics_views.templ` - JS mixin

### Modified Files
- `internal/backend/models/models.go` - Add StatisticsView to auto-migrate
- `internal/webui/client/backend_client.go` - Add gRPC methods
- `internal/webui/routes/routes.go` - Add new API routes
- `internal/webui/templates/scripts/dashboard_settings.templ` - Add on-call config
- `internal/webui/templates/pages/StatisticsDashboard.templ` - All UI changes

---

## Notes

- The Scorecard tab mentioned in requirements is explicitly marked for "time 2" (later phase) and is not included in this design
- Views use relative date ranges (e.g., "7d", "30d") rather than absolute dates to remain useful over time
- On-call schedule uses a simple model initially; can be extended later to support per-team schedules
