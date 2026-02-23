# Time Range Picker Redesign

## Overview

Replace the current date inputs with a modern, user-friendly time range picker inspired by Graylog. Target users are non-technical, so the interface prioritizes clarity and ease of use.

## Current State

- Two native HTML date pickers (start/end)
- Quick preset buttons below (Today, Last 7 Days, etc.)
- Separate "Time of Day" filter section with checkboxes

## New Design

### Component Structure

**Compact Trigger Button**
```
┌──────────────────────────────────────────────────────────┐
│ 🕐 ▼ │ From: 7 days ago          Until: Now              │
└──────────────────────────────────────────────────────────┘
```

With Time of Day filter:
```
┌──────────────────────────────────────────────────────────┐
│ 🕐 ▼ │ From: 7 days ago    Until: Now  (Business 09-18h) │
└──────────────────────────────────────────────────────────┘
```

Absolute mode:
```
┌──────────────────────────────────────────────────────────┐
│ 🕐 ▼ │ From: Jan 15, 2026 14:30    Until: Now            │
└──────────────────────────────────────────────────────────┘
```

**Dropdown Panel** - Opens on click with 3 tabs:

```
┌─────────────────────────────────────────────────┐
│  [Relative]  [Absolute]  [Time of Day]          │
├─────────────────────────────────────────────────┤
│  ...tab content...                              │
├─────────────────────────────────────────────────┤
│              [Cancel]  [Apply]                  │
└─────────────────────────────────────────────────┘
```

---

### Tab 1: Relative

Select time ranges relative to now using spinner inputs.

```
┌─────────────────────────────────────────────────┐
│  From: ☐ All Time                               │
│       ┌─────┐                                   │
│       │ [▲] │                                   │
│       │  7  │  [Days    ▼]  ago                 │
│       │ [▼] │                                   │
│       └─────┘                                   │
│                                                 │
│  Until: ☑ Now                                   │
│       ┌─────┐                                   │
│       │ [▲] │                                   │
│       │  0  │  [Minutes ▼]  ago  (disabled)     │
│       │ [▼] │                                   │
│       └─────┘                                   │
└─────────────────────────────────────────────────┘
```

**Number Input**
- Vertical spinner with ▲/▼ arrows
- Direct editing: click on number to type with keyboard
- Arrow keys supported

**Unit Dropdown Options**
- Minutes
- Hours
- Days
- Weeks
- Months
- Years

**Checkboxes**
- "All Time" (From): Disables spinner, means "from the beginning"
- "Now" (Until): Checked by default, disables spinner, means current moment

**Default State**: From = 7 Days ago, Until = Now

---

### Tab 2: Absolute

Select specific dates and times using calendar pickers.

```
┌─────────────────────────────────────────────────┐
│  From:                                          │
│  ┌────────────────────────┐ ┌────────────┐      │
│  │ 📅 Jan 15, 2026        │ │ 14:30      │      │
│  └────────────────────────┘ └────────────┘      │
│                                                 │
│  Until: ☑ Now                                   │
│  ┌────────────────────────┐ ┌────────────┐      │
│  │ 📅 Jan 21, 2026        │ │ 23:59      │      │
│  └────────────────────────┘ └────────────┘      │
└─────────────────────────────────────────────────┘
```

**Date Picker**
- Calendar popup on click
- User-friendly format: "Jan 15, 2026"
- "Today" shortcut at bottom of calendar

**Time Picker**
- Optional time input next to date
- 24h format (HH:MM)

**Checkbox**
- "Now" (Until): Same behavior as Relative tab

---

### Tab 3: Time of Day

Additional filter applied on top of Relative/Absolute selection.

```
┌─────────────────────────────────────────────────┐
│  ☐ Filter by time of day                        │
│                                                 │
│  From: [09] : [00]    To: [18] : [00]          │
│                                                 │
│  Quick presets:                                 │
│  ┌──────────┐ ┌───────────┐ ┌─────────┐        │
│  │🌙 Night  │ │💼 Business│ │🌅 Morning│        │
│  │22h-06h   │ │09h-18h    │ │06h-12h   │        │
│  └──────────┘ └───────────┘ └─────────┘        │
│  ┌──────────┐ ┌───────────┐                    │
│  │🌆 Evening│ │🌑 On-Call │                    │
│  │18h-22h   │ │nights+WE  │                    │
│  └──────────┘ └───────────┘                    │
│                                                 │
│  ☐ Include weekends (Sat/Sun)                  │
└─────────────────────────────────────────────────┘
```

**Behavior**
- Independent from Relative/Absolute tabs
- When enabled, filters results to only show alerts during specified hours
- Presets auto-fill the From/To time fields

---

## Interactions

| Action | Result |
|--------|--------|
| Click trigger | Opens dropdown |
| Click Cancel | Closes dropdown, reverts changes |
| Click Apply | Closes dropdown, updates filters, triggers query |
| Click outside | Closes dropdown, reverts changes |
| Press Enter | Apply |
| Press Escape | Cancel |
| Arrow keys in spinner | Increment/decrement value |
| Tab | Navigate between fields |

---

## Data Model

```javascript
filters: {
  // Existing - keep unchanged for API compatibility
  startDate: '2026-01-14',
  endDate: '2026-01-21',
  filterByTimeOfDay: false,
  timeOfDayStart: '09:00',
  timeOfDayEnd: '18:00',
  includeWeekends: true,

  // New - for UI state
  timeRangeMode: 'relative', // 'relative' | 'absolute'
  relativeFrom: { value: 7, unit: 'days', allTime: false },
  relativeUntil: { value: 0, unit: 'minutes', now: true }
}
```

When Apply is clicked:
- Relative mode: Convert relative values to actual dates, update `startDate`/`endDate`
- Absolute mode: Use selected dates directly

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/webui/templates/pages/StatisticsDashboard.templ` | Replace date inputs with new picker component, move Time of Day section into dropdown |

---

## No Backend Changes

The API continues to receive `start_date` and `end_date` as ISO strings. All relative-to-absolute conversion happens in JavaScript on the frontend.

---

## Future Enhancements (Not in Scope)

- Save/Load custom presets (localStorage)
- Keyboard shortcut to open picker
- URL parameter sync for shareable links
