# Dashboard Bug Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 7 bugs discovered during browser testing: accessibility issues, undefined userID, redundant API calls, incorrect alert state display, search UX, and resolved view count.

**Architecture:** Each fix is isolated to specific template files. Fixes target Alpine.js logic in `.templ` files, with focus on caching, state management, and accessibility attributes.

**Tech Stack:** Go templ templates, Alpine.js, JavaScript, TailwindCSS

---

## Task 1: Fix Missing id/name Attributes on Checkboxes

**Files:**
- Modify: `internal/webui/templates/components/resolved_alerts_view.templ:97-102`

**Step 1: Add id and name attributes to Include Silenced checkbox**

Open `internal/webui/templates/components/resolved_alerts_view.templ` and locate lines 97-102:

```templ
<input
    type="checkbox"
    x-model="resolvedIncludeSilenced"
    @change="applyResolvedTimeRange()"
    class="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
/>
```

Replace with:

```templ
<input
    type="checkbox"
    id="resolved-include-silenced"
    name="resolved-include-silenced"
    x-model="resolvedIncludeSilenced"
    @change="applyResolvedTimeRange()"
    class="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
/>
```

**Step 2: Regenerate templ files**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: Files regenerated successfully

**Step 3: Verify the change**

Run: `grep -n "resolved-include-silenced" internal/webui/templates/components/resolved_alerts_view.templ`
Expected: Shows lines with id and name attributes

**Step 4: Commit**

```bash
git add internal/webui/templates/components/resolved_alerts_view.templ internal/webui/templates/components/resolved_alerts_view_templ.go
git commit -m "fix: add id/name attributes to resolved view checkbox for accessibility"
```

---

## Task 2: Fix userID Undefined in NotificationService

**Files:**
- Modify: `internal/webui/templates/scripts/notification_service.templ:363-377`

**Step 1: Add defensive check for undefined userID**

Open `internal/webui/templates/scripts/notification_service.templ` and locate the `processNewAlerts` function at line 363.

Find this block (lines 363-378):

```javascript
processNewAlerts(allAlerts, currentFilters, userID) {
    console.log('[NotificationService.processNewAlerts] Called with', allAlerts.length, 'total alerts');
    console.log('[NotificationService.processNewAlerts] currentFilters:', JSON.stringify(currentFilters));
    console.log('[NotificationService.processNewAlerts] userID:', userID);
    console.log('[NotificationService.processNewAlerts] seenAlertsInitialized:', this.seenAlertsInitialized);

    // Skip notification processing if seenAlerts hasn't been properly initialized
    // This prevents race conditions during page load where SSE updates arrive
    // before the dashboard has initialized the seen alerts set
    if (!this.seenAlertsInitialized) {
        console.log('[NotificationService.processNewAlerts] Skipping - seenAlerts not yet initialized from dashboard');
        // Still mark alerts as seen to prevent notifications after init
        const fingerprints = allAlerts.map(a => a.fingerprint);
        this.markAsSeen(fingerprints, userID);
        return;
    }
```

Replace with:

```javascript
processNewAlerts(allAlerts, currentFilters, userID) {
    console.log('[NotificationService.processNewAlerts] Called with', allAlerts.length, 'total alerts');
    console.log('[NotificationService.processNewAlerts] currentFilters:', JSON.stringify(currentFilters));
    console.log('[NotificationService.processNewAlerts] userID:', userID);
    console.log('[NotificationService.processNewAlerts] seenAlertsInitialized:', this.seenAlertsInitialized);

    // Skip if userID is not available (user not logged in or profile not loaded)
    if (!userID) {
        console.log('[NotificationService.processNewAlerts] Skipping - userID not available');
        return;
    }

    // Skip notification processing if seenAlerts hasn't been properly initialized
    // This prevents race conditions during page load where SSE updates arrive
    // before the dashboard has initialized the seen alerts set
    if (!this.seenAlertsInitialized) {
        console.log('[NotificationService.processNewAlerts] Skipping - seenAlerts not yet initialized from dashboard');
        // Still mark alerts as seen to prevent notifications after init
        const fingerprints = allAlerts.map(a => a.fingerprint);
        this.markAsSeen(fingerprints, userID);
        return;
    }
```

**Step 2: Regenerate templ files**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: Files regenerated successfully

**Step 3: Verify the change**

Run: `grep -A5 "processNewAlerts(allAlerts" internal/webui/templates/scripts/notification_service.templ | head -15`
Expected: Shows the new userID check

**Step 4: Commit**

```bash
git add internal/webui/templates/scripts/notification_service.templ internal/webui/templates/scripts/notification_service_templ.go
git commit -m "fix: add defensive check for undefined userID in NotificationService"
```

---

## Task 3: Fix Redundant alert-colors API Calls

**Files:**
- Modify: `internal/webui/templates/scripts/dashboard_data.templ:170-245` and `343-346`

**Step 1: Add request deduplication to loadAlertColors**

Open `internal/webui/templates/scripts/dashboard_data.templ` and locate the `loadAlertColors` function at line 170.

Find this block (lines 170-175):

```javascript
async loadAlertColors(force = false) {
    // Skip loading if colors are already loaded and not forcing refresh
    if (!force && Object.keys(this.alertColors).length > 0) {
        return;
    }

    try {
```

Replace with:

```javascript
async loadAlertColors(force = false) {
    // Skip loading if colors are already loaded and not forcing refresh
    if (!force && Object.keys(this.alertColors).length > 0) {
        return;
    }

    // Prevent concurrent requests - if already loading, skip
    if (this._loadingAlertColors) {
        console.log('Skipping alert colors load - request already in progress');
        return;
    }
    this._loadingAlertColors = true;

    try {
```

**Step 2: Add cleanup in finally block**

Find the end of the `loadAlertColors` function (around line 236-239):

```javascript
            } catch (error) {
                console.error('Error loading alert colors:', error);
            }
        },
```

Replace with:

```javascript
            } catch (error) {
                console.error('Error loading alert colors:', error);
            } finally {
                this._loadingAlertColors = false;
            }
        },
```

**Step 3: Add debounce to SSE-triggered color loading**

Find the SSE color loading block (lines 343-346):

```javascript
            } else if (this.sseConnection && (update.newAlerts?.length > 0 || update.updatedAlerts?.length > 0)) {
                // SSE doesn't include colors (they're user-specific), so fetch them
                this.loadAlertColors(true);
            }
```

Replace with:

```javascript
            } else if (this.sseConnection && (update.newAlerts?.length > 0 || update.updatedAlerts?.length > 0)) {
                // SSE doesn't include colors (they're user-specific), so fetch them
                // Debounce to prevent multiple rapid calls
                if (this._colorLoadTimeout) {
                    clearTimeout(this._colorLoadTimeout);
                }
                this._colorLoadTimeout = setTimeout(() => {
                    this.loadAlertColors(true);
                }, 500);
            }
```

**Step 4: Regenerate templ files**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: Files regenerated successfully

**Step 5: Verify the changes**

Run: `grep -n "_loadingAlertColors\|_colorLoadTimeout" internal/webui/templates/scripts/dashboard_data.templ`
Expected: Shows the new deduplication variables

**Step 6: Commit**

```bash
git add internal/webui/templates/scripts/dashboard_data.templ internal/webui/templates/scripts/dashboard_data_templ.go
git commit -m "fix: add request deduplication and debounce for alert-colors API calls"
```

---

## Task 4: Fix Alert "Ended" Time Showing for Active Alerts

**Files:**
- Modify: `internal/webui/templates/components/alert_modal_shared.templ:229-241`

**Step 1: Add conditional display for Ended field when useDateHelper is true**

Open `internal/webui/templates/components/alert_modal_shared.templ` and locate lines 229-241:

```templ
		<div class="flex flex-col space-y-1">
			<div class="flex items-center justify-between">
				<span class="text-sm font-medium text-gray-500 dark:text-gray-400">Ended:</span>
				if useDateHelper {
					<span class="text-xs font-mono bg-gray-100 dark:bg-gray-700 px-2 py-1 rounded text-gray-900 dark:text-white"
						  x-text={ "formatAlertDate(" + dataVar + "?.endsAt)" }></span>
				} else {
					<span x-show={ dataVar + "?.endsAt" } class="text-xs font-mono bg-gray-100 dark:bg-gray-700 px-2 py-1 rounded text-gray-900 dark:text-white"
						  x-text={ dataVar + "?.endsAt ? new Date(" + dataVar + ".endsAt).toLocaleString() : '—'" }></span>
					<span x-show={ "!" + dataVar + "?.endsAt" } class="text-xs font-mono bg-gray-100 dark:bg-gray-700 px-2 py-1 rounded text-gray-900 dark:text-white">—</span>
				}
			</div>
		</div>
```

Replace with:

```templ
		<div class="flex flex-col space-y-1">
			<div class="flex items-center justify-between">
				<span class="text-sm font-medium text-gray-500 dark:text-gray-400">Ended:</span>
				if useDateHelper {
					<span x-show={ dataVar + "?.endsAt && " + dataVar + "?.state !== 'active'" } class="text-xs font-mono bg-gray-100 dark:bg-gray-700 px-2 py-1 rounded text-gray-900 dark:text-white"
						  x-text={ "formatAlertDate(" + dataVar + "?.endsAt)" }></span>
					<span x-show={ "!" + dataVar + "?.endsAt || " + dataVar + "?.state === 'active'" } class="text-xs font-mono bg-gray-100 dark:bg-gray-700 px-2 py-1 rounded text-gray-900 dark:text-white">—</span>
				} else {
					<span x-show={ dataVar + "?.endsAt && " + dataVar + "?.state !== 'active'" } class="text-xs font-mono bg-gray-100 dark:bg-gray-700 px-2 py-1 rounded text-gray-900 dark:text-white"
						  x-text={ dataVar + "?.endsAt ? new Date(" + dataVar + ".endsAt).toLocaleString() : '—'" }></span>
					<span x-show={ "!" + dataVar + "?.endsAt || " + dataVar + "?.state === 'active'" } class="text-xs font-mono bg-gray-100 dark:bg-gray-700 px-2 py-1 rounded text-gray-900 dark:text-white">—</span>
				}
			</div>
		</div>
```

**Step 2: Regenerate templ files**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: Files regenerated successfully

**Step 3: Verify the change**

Run: `grep -n "state !== 'active'" internal/webui/templates/components/alert_modal_shared.templ`
Expected: Shows the new conditional checks

**Step 4: Commit**

```bash
git add internal/webui/templates/components/alert_modal_shared.templ internal/webui/templates/components/alert_modal_shared_templ.go
git commit -m "fix: hide Ended time for active alerts in detail panel"
```

---

## Task 5: Improve Search Filter UX with Loading Indicator

**Files:**
- Modify: `internal/webui/templates/pages/NewDashboard.templ:298-308`

**Step 1: Add search loading state and visual feedback**

Open `internal/webui/templates/pages/NewDashboard.templ` and locate lines 298-308:

```templ
						<div class="relative">
							<div class="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
								<svg class="h-5 w-5 text-gray-400" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" fill="none">
									<path stroke-linecap="round" stroke-linejoin="round" d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z" />
								</svg>
							</div>
							<input x-model="searchQuery" @input.debounce.300ms="applyFilters()"
								   id="dashboard-search" name="dashboard-search"
								   type="text" placeholder="Search alerts, instances, summaries..."
								   class="block w-full pl-10 pr-3 py-2 border border-gray-300 dark:border-dark-border-DEFAULT rounded-md leading-5 bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white placeholder-gray-500 dark:placeholder-gray-400 focus:outline-none focus:placeholder-gray-400 focus:ring-1 focus:ring-blue-500 focus:border-blue-500">
						</div>
```

Replace with:

```templ
						<div class="relative">
							<div class="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
								<!-- Search icon (shown when not loading) -->
								<svg x-show="!isSearching" class="h-5 w-5 text-gray-400" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" fill="none">
									<path stroke-linecap="round" stroke-linejoin="round" d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z" />
								</svg>
								<!-- Loading spinner (shown when searching) -->
								<svg x-show="isSearching" class="h-5 w-5 text-blue-500 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
								</svg>
							</div>
							<input x-model="searchQuery"
								   @input.debounce.300ms="isSearching = true; applyFilters().finally(() => isSearching = false)"
								   @keydown.enter="isSearching = true; applyFilters().finally(() => isSearching = false)"
								   id="dashboard-search" name="dashboard-search"
								   type="text" placeholder="Search alerts, instances, summaries..."
								   class="block w-full pl-10 pr-3 py-2 border border-gray-300 dark:border-dark-border-DEFAULT rounded-md leading-5 bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white placeholder-gray-500 dark:placeholder-gray-400 focus:outline-none focus:placeholder-gray-400 focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
								   :class="{ 'ring-2 ring-blue-500': isSearching }">
						</div>
```

**Step 2: Add isSearching state to Alpine data**

Find the Alpine.js data initialization in the dashboard (search for `x-data` with the main dashboard object). Add `isSearching: false` to the data object.

This is typically in `internal/webui/templates/scripts/dashboard_core.templ`. Search for the data initialization and add:

```javascript
isSearching: false,
```

**Step 3: Regenerate templ files**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: Files regenerated successfully

**Step 4: Verify the changes**

Run: `grep -n "isSearching" internal/webui/templates/pages/NewDashboard.templ`
Expected: Shows the new loading state references

**Step 5: Commit**

```bash
git add internal/webui/templates/pages/NewDashboard.templ internal/webui/templates/pages/NewDashboard_templ.go internal/webui/templates/scripts/dashboard_core.templ internal/webui/templates/scripts/dashboard_core_templ.go
git commit -m "fix: add visual loading feedback during search filter application"
```

---

## Task 6: Fix Resolved View Count Display

**Files:**
- Modify: `internal/webui/templates/components/resolved_alerts_view.templ:111-116`

**Step 1: Add loading state and improve count display logic**

Open `internal/webui/templates/components/resolved_alerts_view.templ` and locate lines 110-116:

```templ
			<div class="mt-3 flex items-center justify-between text-sm">
				<div class="text-gray-600 dark:text-gray-400">
					<span x-text="`Showing ${resolvedAlerts.length} of ${resolvedTotalCount} resolved alerts`"></span>
					<span class="ml-2 text-xs text-gray-500 dark:text-gray-500">
						(Active filters: <span x-text="getActiveFiltersCount()"></span>)
					</span>
				</div>
```

Replace with:

```templ
			<div class="mt-3 flex items-center justify-between text-sm">
				<div class="text-gray-600 dark:text-gray-400">
					<span x-show="resolvedLoading" class="flex items-center">
						<svg class="animate-spin h-4 w-4 mr-2 text-blue-500" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
						</svg>
						Loading resolved alerts...
					</span>
					<span x-show="!resolvedLoading" x-text="`Showing ${resolvedAlerts.length} of ${resolvedTotalCount} resolved alerts`"></span>
					<span x-show="!resolvedLoading" class="ml-2 text-xs text-gray-500 dark:text-gray-500">
						(Active filters: <span x-text="getActiveFiltersCount()"></span>)
					</span>
				</div>
```

**Step 2: Regenerate templ files**

Run: `cd /home/gule/Workspace/soulkyu/notificator && templ generate`
Expected: Files regenerated successfully

**Step 3: Verify the change**

Run: `grep -n "resolvedLoading" internal/webui/templates/components/resolved_alerts_view.templ`
Expected: Shows the new loading state checks

**Step 4: Commit**

```bash
git add internal/webui/templates/components/resolved_alerts_view.templ internal/webui/templates/components/resolved_alerts_view_templ.go
git commit -m "fix: add loading state to resolved alerts count display"
```

---

## Task 7: Test All Fixes in Browser

**Step 1: Build and run the application**

Run: `cd /home/gule/Workspace/soulkyu/notificator && go build -o notificator ./cmd/notificator && ./notificator`
Expected: Application starts successfully

**Step 2: Open browser and test each fix**

1. Open https://notificator.numberly.dev/dashboard
2. Check console for errors - should not see "userID: undefined"
3. Click Resolved view - should show loading spinner, then proper count
4. Click on an active alert - should show "—" for Ended time, not a date
5. Type in search box - should see loading spinner while searching
6. Monitor Network tab - alert-colors should not be called multiple times rapidly
7. Inspect checkbox elements - should have id/name attributes

**Step 3: Final commit if all tests pass**

```bash
git add -A
git commit -m "test: verify all dashboard bug fixes work correctly"
```

---

## Summary

| Task | Bug | File | Status |
|------|-----|------|--------|
| 1 | Missing checkbox id/name | resolved_alerts_view.templ | Pending |
| 2 | userID undefined | notification_service.templ | Pending |
| 3 | Redundant API calls | dashboard_data.templ | Pending |
| 4 | Active alert shows Ended | alert_modal_shared.templ | Pending |
| 5 | Search UX feedback | NewDashboard.templ | Pending |
| 6 | Resolved count display | resolved_alerts_view.templ | Pending |
| 7 | Browser verification | N/A | Pending |
