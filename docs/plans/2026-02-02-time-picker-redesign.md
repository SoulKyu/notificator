# Time Picker UX Redesign

**Date**: 2026-02-02
**Status**: Design Complete
**Author**: Claude + User collaboration

## Problem Statement

The current time-of-day filter on the statistics page uses native HTML `<input type="time">` elements with poor UX:

1. **Browser inconsistency** - Native picker shows AM/PM format with tiny scrolling lists
2. **Format mismatch** - Button shows 24h format ("00:00 - 23:59") but picker shows 12h ("12:00 AM")
3. **Blind presets** - Users don't know what "Business" or "Night" means until clicking
4. **Slow input** - Scrolling through 60 minutes is tedious when users know the exact time

## User Requirements

- Users need **minute-level precision** (e.g., 09:02)
- Users typically **know the exact time** they want (not exploring)
- Fast, direct entry is more important than visual picking

## Solution: Smart Text Input

Replace native time pickers with styled text inputs that accept flexible formats and normalize to 24h.

### Input Format & Parsing

| User types | Parsed as | Display |
|------------|-----------|---------|
| `9:02` | 09:02 | 09:02 |
| `902` | 09:02 | 09:02 |
| `0902` | 09:02 | 09:02 |
| `14:30` | 14:30 | 14:30 |
| `1430` | 14:30 | 14:30 |
| `9` | 09:00 | 09:00 |
| `14` | 14:00 | 14:00 |

### Validation Rules

- Hours: 0-23
- Minutes: 0-59
- Invalid input: Red border + shake animation, revert to last valid value on blur
- Empty input: Not allowed, defaults to `00:00` (start) or `23:59` (end)

### Parsing Logic

```javascript
function parseTimeInput(input) {
  const cleaned = input.replace(/[^0-9]/g, ''); // Strip non-digits

  let hours, minutes;

  if (cleaned.length <= 2) {
    // "9" → 09:00, "14" → 14:00
    hours = parseInt(cleaned, 10);
    minutes = 0;
  } else if (cleaned.length === 3) {
    // "902" → 09:02
    hours = parseInt(cleaned[0], 10);
    minutes = parseInt(cleaned.slice(1), 10);
  } else {
    // "0902" or "1430" → HH:MM
    hours = parseInt(cleaned.slice(0, 2), 10);
    minutes = parseInt(cleaned.slice(2, 4), 10);
  }

  // Validation
  if (isNaN(hours) || hours < 0 || hours > 23) return null;
  if (isNaN(minutes) || minutes < 0 || minutes > 59) return null;

  return `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}`;
}
```

## Visual Design

### Aesthetic Direction: Refined Utility

- **Monospace font** for time values (perfect digit alignment)
- **24h timeline bar** showing selected range visually
- **Preset chips with times** - users see exactly what they'll get

### Timeline Bar

```
[░░░░░░░░░████████████░░░░░░░░░░]
0    6    9         18    24
         ▲ From      ▲ To
```

- Gray = excluded hours
- Colored segment = selected range
- Overnight ranges (22:00-06:00) wrap visually as two segments
- Read-only visualization (not interactive)

### Dropdown Layout

```
┌─────────────────────────────────────────────┐
│  From          To                           │
│  ┌────────┐    ┌────────┐                   │
│  │ 09:00  │    │ 18:00  │                   │
│  └────────┘    └────────┘                   │
│                                             │
│  [Timeline bar visualization]               │
│                                             │
│  Quick presets                              │
│  ┌──────────┐ ┌───────┐ ┌──────────┐       │
│  │ On-Call  │ │ Night │ │ Business │ ...   │
│  │ 18→09+WE │ │ 22→06 │ │ 09→18    │       │
│  └──────────┘ └───────┘ └──────────┘       │
│                                             │
│  Weekends                                   │
│  [Dropdown: Same hours ▼]                   │
│                                             │
│  [Reset to default]                         │
└─────────────────────────────────────────────┘
```

### Preset Data

| Preset | Start | End | Notes |
|--------|-------|-----|-------|
| On-Call | 18:00 | 09:00 | Forces full weekends |
| Night | 22:00 | 06:00 | |
| Business | 09:00 | 18:00 | |
| Morning | 06:00 | 12:00 | |
| Evening | 18:00 | 22:00 | |

## Implementation

### Files to Modify

| File | Changes |
|------|---------|
| `internal/webui/templates/pages/StatisticsDashboard.templ` | Replace `<input type="time">` with custom text inputs, add timeline bar |
| `internal/webui/static/css/output.css` | Timeline bar styles, shake animation |

### Alpine.js Changes

```javascript
// Add to filters object
timeOfDayStartRaw: '00:00',  // Raw input value
timeOfDayEndRaw: '23:59',    // Raw input value
timeInputError: null,         // 'start' | 'end' | null

// New methods
parseTimeInput(input) { ... },
validateAndSetTime(field, value) { ... },
getTimelineSegments() { ... }  // Returns segments for visual bar
```

### Keyboard Navigation

- `Tab` / `Enter`: Move to next input
- `Shift+Tab`: Move to previous input
- `Escape`: Revert to last valid value, close dropdown

### CSS Requirements

- Monospace font for inputs: `font-family: ui-monospace, monospace`
- Timeline bar: 24 flex segments, CSS variables for colors
- Validation shake: `@keyframes shake` with transform
- Dark mode support via Tailwind `dark:` variants

## Benefits

1. **Faster input** - Type "902" instead of scroll-scroll-scroll-click
2. **Consistent 24h format** - No AM/PM confusion anywhere
3. **Transparent presets** - See times before clicking
4. **Visual feedback** - Timeline bar shows range at a glance
5. **Keyboard friendly** - Power users can tab through quickly
