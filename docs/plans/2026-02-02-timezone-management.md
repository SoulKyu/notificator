# Timezone Management Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add user timezone preference with footer selector and convert time-of-day filters to UTC before API calls.

**Architecture:** Store timezone in User model (database), display/select via footer dropdown, convert local times to UTC in frontend using Day.js before sending to backend.

**Tech Stack:** Go, GORM, Gin, templ, Alpine.js, Day.js + timezone plugin

---

## Task 1: Add Timezone Field to User Model

**Files:**
- Modify: `internal/backend/models/models.go:13-31`

**Step 1: Add Timezone field to User struct**

Add after line 26 (after `EmailVerified`):

```go
// User preferences
Timezone *string `gorm:"size:100" json:"timezone,omitempty"` // IANA timezone (e.g., "Europe/Paris")
```

**Step 2: Verify GORM auto-migration**

The application uses GORM auto-migrate, so no manual migration needed. Restart will add the column.

---

## Task 2: Add Timezone to Proto User Message

**Files:**
- Modify: `proto/auth.proto:98-106`

**Step 1: Add timezone field to User message**

Add after line 106 (after `oauth_id`):

```protobuf
string timezone = 8;  // IANA timezone (e.g., "Europe/Paris")
```

**Step 2: Regenerate proto files**

Run:
```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/auth.proto
```

---

## Task 3: Add API Endpoint for Timezone Update

**Files:**
- Modify: `internal/webui/handlers/profile_handlers.go`
- Modify: `internal/webui/router.go`

**Step 1: Add UpdateTimezone handler to profile_handlers.go**

Add at end of file:

```go
// UpdateTimezone updates the user's timezone preference
func UpdateTimezone(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse("Not authenticated"))
		return
	}

	var req struct {
		Timezone string `json:"timezone" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Invalid request: timezone is required"))
		return
	}

	// Validate timezone is a valid IANA timezone
	_, err := time.LoadLocation(req.Timezone)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Invalid timezone: "+req.Timezone))
		return
	}

	// Update user timezone in database
	db := c.MustGet("db").(*gorm.DB)
	if err := db.Model(user).Update("timezone", req.Timezone).Error; err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to update timezone"))
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"timezone": req.Timezone,
	}))
}

// GetTimezone returns the user's timezone preference
func GetTimezone(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse("Not authenticated"))
		return
	}

	timezone := ""
	if user.Timezone != nil {
		timezone = *user.Timezone
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"timezone": timezone,
	}))
}
```

**Step 2: Add import for gorm**

Add to imports:
```go
"gorm.io/gorm"
```

**Step 3: Register routes in router.go**

Find the profile routes section and add:

```go
api.GET("/profile/timezone", handlers.GetTimezone)
api.PUT("/profile/timezone", handlers.UpdateTimezone)
```

---

## Task 4: Add Day.js to Base Layout

**Files:**
- Modify: `internal/webui/templates/layouts/Base.templ:19-21`

**Step 1: Add Day.js scripts after Alpine.js**

Add after line 21 (after alpinejs script):

```html
<!-- Day.js for timezone handling -->
<script src="https://unpkg.com/dayjs@1.11.10/dayjs.min.js" defer></script>
<script src="https://unpkg.com/dayjs@1.11.10/plugin/utc.js" defer></script>
<script src="https://unpkg.com/dayjs@1.11.10/plugin/timezone.js" defer></script>
```

---

## Task 5: Create TimezoneSelector Component

**Files:**
- Create: `internal/webui/templates/components/TimezoneSelector.templ`

**Step 1: Create the component file**

```templ
package components

templ TimezoneSelector() {
	<div x-data="timezoneSelector()" x-init="init()" class="relative">
		<!-- Timezone button -->
		<button
			@click="open = !open"
			class="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
		>
			<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
			</svg>
			<span x-text="displayTimezone"></span>
			<svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
			</svg>
		</button>

		<!-- Dropdown -->
		<div
			x-show="open"
			x-cloak
			@click.away="open = false"
			class="absolute bottom-full mb-2 left-0 w-80 max-h-96 bg-white dark:bg-dark-bg-secondary rounded-lg shadow-lg border border-gray-200 dark:border-dark-border overflow-hidden z-50"
		>
			<!-- Search -->
			<div class="p-2 border-b border-gray-200 dark:border-dark-border">
				<input
					type="text"
					x-model="search"
					@input="filterTimezones()"
					placeholder="Search timezone..."
					class="w-full px-3 py-2 text-sm border border-gray-300 dark:border-dark-border rounded-md bg-white dark:bg-dark-bg-primary text-gray-900 dark:text-dark-text-primary focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
				/>
			</div>

			<!-- Timezone list -->
			<div class="max-h-72 overflow-y-auto">
				<template x-for="tz in filteredTimezones" :key="tz.name">
					<button
						@click="selectTimezone(tz.name)"
						class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-bg-tertiary flex justify-between items-center"
						:class="{ 'bg-blue-50 dark:bg-blue-900/20': tz.name === currentTimezone }"
					>
						<span x-text="tz.name" class="text-gray-900 dark:text-dark-text-primary"></span>
						<span x-text="tz.offset" class="text-gray-500 dark:text-gray-400 text-xs"></span>
					</button>
				</template>
			</div>
		</div>
	</div>

	<script>
		function timezoneSelector() {
			return {
				open: false,
				search: '',
				currentTimezone: '',
				allTimezones: [],
				filteredTimezones: [],
				saving: false,

				async init() {
					// Initialize Day.js plugins
					dayjs.extend(dayjs_plugin_utc);
					dayjs.extend(dayjs_plugin_timezone);

					// Build timezone list from Intl API
					this.allTimezones = this.buildTimezoneList();
					this.filteredTimezones = this.allTimezones;

					// Load user's timezone from API or fallback to browser
					await this.loadTimezone();
				},

				buildTimezoneList() {
					const timezones = Intl.supportedValuesOf('timeZone');
					return timezones.map(tz => {
						const offset = this.getOffset(tz);
						return { name: tz, offset: offset };
					}).sort((a, b) => a.name.localeCompare(b.name));
				},

				getOffset(timezone) {
					try {
						const now = dayjs().tz(timezone);
						const offsetMinutes = now.utcOffset();
						const hours = Math.floor(Math.abs(offsetMinutes) / 60);
						const mins = Math.abs(offsetMinutes) % 60;
						const sign = offsetMinutes >= 0 ? '+' : '-';
						return `UTC${sign}${hours}${mins > 0 ? ':' + String(mins).padStart(2, '0') : ''}`;
					} catch {
						return '';
					}
				},

				async loadTimezone() {
					try {
						const resp = await fetch('/api/profile/timezone');
						const data = await resp.json();
						if (data.success && data.data.timezone) {
							this.currentTimezone = data.data.timezone;
						} else {
							// Fallback to browser timezone
							this.currentTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
						}
					} catch {
						this.currentTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
					}

					// Store in window for other components
					window.__USER_TIMEZONE__ = this.currentTimezone;
					dayjs.tz.setDefault(this.currentTimezone);
				},

				get displayTimezone() {
					if (!this.currentTimezone) return 'Loading...';
					const offset = this.getOffset(this.currentTimezone);
					return `${this.currentTimezone} (${offset})`;
				},

				filterTimezones() {
					const searchLower = this.search.toLowerCase();
					this.filteredTimezones = this.allTimezones.filter(tz =>
						tz.name.toLowerCase().includes(searchLower)
					);
				},

				async selectTimezone(timezone) {
					this.currentTimezone = timezone;
					this.open = false;
					this.search = '';
					this.filteredTimezones = this.allTimezones;

					// Update window state immediately
					window.__USER_TIMEZONE__ = timezone;
					dayjs.tz.setDefault(timezone);

					// Save to backend (non-blocking)
					try {
						await fetch('/api/profile/timezone', {
							method: 'PUT',
							headers: { 'Content-Type': 'application/json' },
							body: JSON.stringify({ timezone: timezone })
						});
					} catch (e) {
						console.error('Failed to save timezone:', e);
					}

					// Dispatch event for other components to react
					window.dispatchEvent(new CustomEvent('timezoneChanged', { detail: timezone }));
				}
			};
		}
	</script>
}
```

**Step 2: Generate templ file**

Run:
```bash
templ generate
```

---

## Task 6: Add Footer with TimezoneSelector to Base Layout

**Files:**
- Modify: `internal/webui/templates/layouts/Base.templ`

**Step 1: Add import for TimezoneSelector**

Update the import at line 3:
```go
import "notificator/internal/webui/templates/components"
```

(Already imported, so just verify it's there)

**Step 2: Add footer before closing body tag**

Add before `</body>` (before line 302):

```templ
		<!-- Footer with timezone selector -->
		<footer class="fixed bottom-0 left-0 right-0 bg-white dark:bg-dark-bg-secondary border-t border-gray-200 dark:border-dark-border px-4 py-2 flex items-center justify-between z-40">
			<div class="flex items-center gap-4">
				@components.TimezoneSelector()
			</div>
			<div class="text-xs text-gray-400 dark:text-gray-500">
				Notificator
			</div>
		</footer>
		<!-- Spacer for fixed footer -->
		<div class="h-10"></div>
```

**Step 3: Regenerate templ**

Run:
```bash
templ generate
```

---

## Task 7: Add Timezone Conversion to Statistics Filters

**Files:**
- Modify: `internal/webui/templates/scripts/statistics_views.templ`

**Step 1: Add time conversion helper function**

Add at the beginning of the script (after line 4, inside the script tag):

```javascript
// Convert local time (HH:MM) to UTC time using user's timezone
function convertTimeToUTC(localTime, timezone) {
	if (!localTime || !timezone) return localTime;

	// Create a date with the local time in the user's timezone
	// We use today's date but only care about the time portion
	const today = dayjs().format('YYYY-MM-DD');
	const localDateTime = dayjs.tz(`${today} ${localTime}`, timezone);

	// Convert to UTC and extract just the time
	return localDateTime.utc().format('HH:mm');
}

// Convert UTC time (HH:MM) to local time for display
function convertTimeFromUTC(utcTime, timezone) {
	if (!utcTime || !timezone) return utcTime;

	const today = dayjs().format('YYYY-MM-DD');
	const utcDateTime = dayjs.utc(`${today} ${utcTime}`);

	return utcDateTime.tz(timezone).format('HH:mm');
}
```

**Step 2: Modify getCurrentViewData to convert times**

Update the `getCurrentViewData()` method (around line 80-112) to convert time-of-day values:

Find:
```javascript
// Time of day filtering
filter_by_time_of_day: this.filters.filterByTimeOfDay,
time_of_day_start: this.filters.timeOfDayStart,
time_of_day_end: this.filters.timeOfDayEnd,
```

Replace with:
```javascript
// Time of day filtering (convert to UTC before sending)
filter_by_time_of_day: this.filters.filterByTimeOfDay,
time_of_day_start: this.filters.filterByTimeOfDay
	? convertTimeToUTC(this.filters.timeOfDayStart, window.__USER_TIMEZONE__)
	: this.filters.timeOfDayStart,
time_of_day_end: this.filters.filterByTimeOfDay
	? convertTimeToUTC(this.filters.timeOfDayEnd, window.__USER_TIMEZONE__)
	: this.filters.timeOfDayEnd,
```

---

## Task 8: Add Padding for Fixed Footer

**Files:**
- Modify: `internal/webui/templates/layouts/Base.templ`

**Step 1: Add bottom padding to main content**

Find line 47:
```html
<div class="min-h-full" x-data="darkModeHandler()">
```

Replace with:
```html
<div class="min-h-full pb-12" x-data="darkModeHandler()">
```

---

## Task 9: Rebuild and Test

**Step 1: Regenerate all templ files**

```bash
templ generate
```

**Step 2: Build the application**

```bash
go build -o notificator ./cmd/notificator
```

**Step 3: Run and verify**

1. Start the application
2. Check footer appears with timezone selector
3. Click timezone selector, search for a timezone
4. Select a different timezone
5. Verify it persists after page refresh
6. Apply a time-of-day filter and check Network tab for UTC-converted times

---

## Verification Checklist

- [ ] Footer displays current timezone with UTC offset
- [ ] Dropdown opens with searchable timezone list
- [ ] Selecting timezone saves to backend (check Network tab)
- [ ] Timezone persists after page refresh
- [ ] Time-of-day filters send UTC-converted times to API
- [ ] Browser timezone is used as fallback for new users
