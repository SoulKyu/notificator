# Time of Day Filter Redesign

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Simplify the Time of Day filter by removing the enable checkbox, always showing time pickers with sensible defaults, and adding an On-Call quick preset that uses saved settings.

**Architecture:** Modify the existing Time of Day dropdown in StatisticsDashboard.templ to always show time inputs, replace the weekends checkbox with a dropdown offering three modes, and add an On-Call preset that syncs with user settings.

**Tech Stack:** Alpine.js, templ, Go backend API

---

## Current State

The Time of Day filter currently has:
1. A checkbox to enable/disable filtering (`filterByTimeOfDay`)
2. From/To time pickers (only shown when enabled)
3. Quick presets: Night, Business, Morning, Evening
4. "Include weekends" checkbox
5. A "Clear filter" button

## New Design

### UI Structure

**Button Display:**
- Default: `00:00 - 23:59`
- Preset active: `{Preset} ({start} - {end})`, e.g., `On-Call (18:00 - 08:00)`
- Button highlighted when not default

**Dropdown Content:**
```
┌─────────────────────────────────┐
│ From: [00:00]    To: [23:59]    │
│                                 │
│ Quick presets                   │
│ [On-Call] [Night] [Business]    │
│ [Morning] [Evening]             │
│                                 │
│ Weekends: [Dropdown ▼]          │
│   - Exclude weekends            │
│   - Same hours                  │
│   - Full weekends               │
│                                 │
│ [Reset to default]              │
└─────────────────────────────────┘
```

### Data Model Changes

**Remove from `filters`:**
- `filterByTimeOfDay: false` - No longer needed, always active
- `includeWeekends: true` - Replaced by weekendMode

**Modify defaults:**
- `timeOfDayStart: '00:00'` - Changed from '22:00'
- `timeOfDayEnd: '23:59'` - Changed from '06:00'

**Add to `filters`:**
- `weekendMode: 'same_hours'` - Options: 'exclude', 'same_hours', 'full_weekends'
- `activePreset: null` - Options: null, 'oncall', 'night', 'business', 'morning', 'evening'

### Backend API Changes

**Request parameters:**
- Remove: `filter_by_time_of_day: bool`
- Keep: `time_of_day_start`, `time_of_day_end` (always sent)
- Replace: `include_weekends: bool` → `weekend_mode: string`

**Weekend mode values:**
- `exclude` - Don't include Saturday/Sunday data
- `same_hours` - Apply time filter to weekends
- `full_weekends` - Include all 24h on Saturday/Sunday

### Preset Behavior

**Standard presets (Night, Business, Morning, Evening):**
- Set time range
- Set `activePreset` to preset name
- Don't modify `weekendMode`

**On-Call preset (special):**
- Load times from `localStorage.dashboardSettings.onCallSchedule`
- Set `activePreset = 'oncall'`
- Force `weekendMode = 'full_weekends'`
- Disable weekend dropdown (user cannot change)
- Tooltip: "On-call includes full weekends by design"

### Button Display Logic

```javascript
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
}
```

### Active State Logic

```javascript
isTimeFilterActive() {
  return this.filters.timeOfDayStart !== '00:00' ||
         this.filters.timeOfDayEnd !== '23:59' ||
         this.filters.weekendMode !== 'same_hours';
}
```

### Tooltips

- **Exclude weekends:** "Don't include Saturday/Sunday data"
- **Same hours:** "Apply the same time filter to weekends"
- **Full weekends:** "Include all 24 hours on Saturday and Sunday"

## Files to Modify

1. `internal/webui/templates/pages/StatisticsDashboard.templ` - UI changes
2. Backend API handlers (if `weekend_mode` parameter needs to be added)
3. Backend statistics service (if weekend filtering logic needs update)
