# Timezone Management Design

**Date:** 2026-02-02
**Status:** Approved
**Author:** Claude + User

## Problem Statement

The time-of-day filter operates on raw UTC timestamps. When a user filters for "09:00-18:00 business hours", they're actually filtering 09:00-18:00 UTC, not their local time. A Paris user and NYC user see different results for the same filter.

**Root cause:** The backend stores all timestamps in UTC (correct), but the time-of-day filtering interprets filter values as UTC hours instead of the user's local timezone.

## Solution Overview

1. **Add timezone preference** to user profile (persisted in database)
2. **Footer timezone selector** for easy visibility and switching
3. **Frontend conversion** of local times to UTC before API calls

## Architecture

### Timezone Storage & Retrieval

**Storage Strategy:**
1. **Primary:** User profile in database (persists across devices/sessions)
2. **Fallback:** Browser's `Intl.DateTimeFormat().resolvedOptions().timeZone`

**Flow:**
```
Page Load → Check user profile for timezone
  ├─ Has timezone → Use it
  └─ No timezone → Use browser timezone (Intl API)
```

### Libraries

| Library | Size | Purpose |
|---------|------|---------|
| Day.js | ~2KB | Date manipulation |
| Day.js timezone plugin | ~2KB | Timezone conversions, DST handling |

**Dropdown data:** Browser's built-in `Intl.supportedValuesOf('timeZone')` (0KB)

### Footer Timezone Selector UI

**Placement:** Bottom footer bar, visible on all pages

```
┌─────────────────────────────────────────────────────────────┐
│  [Main App Content]                                         │
│                                                             │
├─────────────────────────────────────────────────────────────┤
│  🌐 Europe/Paris (UTC+1) ▼                    © Notificator │
└─────────────────────────────────────────────────────────────┘
```

**Display format:** Full IANA name with UTC offset, e.g., `Europe/Paris (UTC+1)`

**Dropdown Behavior:**
- Click opens searchable timezone list (grouped by region parsed from IANA name)
- Shows current UTC offset next to each timezone (calculated via Day.js)
- Selection immediately:
  1. Updates `localStorage` for instant effect
  2. Saves to user profile via API (background, non-blocking)
  3. Refreshes any time-based displays on page

### Time-of-Day Filter Conversion

**Current Behavior (broken):**
```
User selects: 09:00 - 18:00
API receives: time_of_day_start=09:00, time_of_day_end=18:00
Backend filters: WHERE EXTRACT(HOUR FROM fired_at) BETWEEN 9 AND 18
→ Filters on UTC hours, not user's local hours
```

**New Behavior:**
```
User in Europe/Paris selects: 09:00 - 18:00
Frontend converts using Day.js:
  - 09:00 Paris → 08:00 UTC (winter) or 07:00 UTC (summer)
  - 18:00 Paris → 17:00 UTC (winter) or 16:00 UTC (summer)
API receives: time_of_day_start=08:00, time_of_day_end=17:00
Backend filters on UTC hours as before
```

**Key Point:** The backend doesn't change. Frontend does the conversion before sending.

## Data Flow

```
┌─────────────┐    ┌──────────────┐    ┌─────────────┐
│ Page Load   │───►│ Get timezone │───►│ Set Day.js  │
│             │    │ from profile │    │ default tz  │
│             │    │ or browser   │    │             │
└─────────────┘    └──────────────┘    └─────────────┘

┌─────────────┐    ┌──────────────┐    ┌─────────────┐
│ User picks  │───►│ Convert to   │───►│ Send to API │
│ 09:00-18:00 │    │ UTC via      │    │ as UTC      │
│ local time  │    │ Day.js       │    │ values      │
└─────────────┘    └──────────────┘    └─────────────┘

┌─────────────┐    ┌──────────────┐    ┌─────────────┐
│ Footer      │───►│ Save to      │───►│ Refresh     │
│ tz change   │    │ profile +    │    │ page data   │
│             │    │ localStorage │    │             │
└─────────────┘    └──────────────┘    └─────────────┘
```

## Files to Modify

| File | Action | Purpose |
|------|--------|---------|
| `internal/backend/models/user.go` | Modify | Add `Timezone` field to User model |
| `internal/backend/services/user_service.go` | Modify | Handle timezone in profile update |
| `proto/user.proto` | Modify | Add timezone to User proto message |
| `internal/webui/templates/layouts/Base.templ` | Modify | Add footer timezone selector |
| `internal/webui/templates/components/TimezoneSelector.templ` | Create | Dropdown component |
| `internal/webui/handlers/user_handlers.go` | Modify | API endpoint for timezone update |
| `internal/webui/templates/scripts/statistics_views.templ` | Modify | Convert times before API calls |

## Verification

After implementation:

1. **Timezone persistence test:**
   - Set timezone to Europe/Paris in footer
   - Refresh page → timezone should remain Europe/Paris
   - Login from different browser → timezone should be Europe/Paris

2. **Filter conversion test:**
   - User in Paris sets timezone to Europe/Paris
   - Filters by 09:00-18:00 business hours
   - Network tab shows API call with UTC-converted times (08:00-17:00 in winter)
   - Results match alerts fired during Paris business hours

3. **DST handling test:**
   - Set timezone to Europe/Paris
   - Filter for a date range that spans DST change
   - Verify correct hours are filtered on both sides of DST transition

## Design Decisions

1. **User profile over localStorage only:** Timezone persists across devices when logged in
2. **Browser fallback:** New users get sensible default without manual setup
3. **Frontend conversion:** Backend remains unchanged, simpler implementation
4. **Footer placement:** Always visible, easy to change without navigation
5. **Day.js:** Smallest footprint (4KB) with robust DST handling
6. **Intl API for dropdown:** 0KB, sufficient for listing timezones
