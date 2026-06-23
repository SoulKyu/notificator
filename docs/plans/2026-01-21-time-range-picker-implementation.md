# Time Range Picker Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace current date inputs with a modern Graylog-inspired time range picker with Relative, Absolute, and Time of Day tabs.

**Architecture:** Single-file frontend change in Alpine.js. Add new UI state properties to track picker mode and relative values. Convert relative times to absolute dates on Apply. No backend changes.

**Tech Stack:** Alpine.js, Tailwind CSS, templ (Go templating)

---

## Task 1: Extend Data Model

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ:1491-1504`

**Step 1: Add new state properties to filters object**

Find the `filters` object (line ~1491) and add the new properties:

```javascript
filters: {
    // Existing - keep unchanged
    startDate: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0],
    endDate: new Date().toISOString().split('T')[0],
    groupBy: 'severity',
    periodType: 'day',
    applyRules: false,
    limit: 20,
    filterByTimeOfDay: false,
    timeOfDayStart: '22:00',
    timeOfDayEnd: '06:00',
    includeWeekends: true,
    severities: [],
    teams: [],

    // New - time range picker UI state
    timeRangeMode: 'relative', // 'relative' | 'absolute'
    relativeFrom: { value: 7, unit: 'days', allTime: false },
    relativeUntil: { value: 0, unit: 'minutes', now: true },
    absoluteFromTime: '00:00',
    absoluteUntilTime: '23:59'
},
```

**Step 2: Add picker visibility state**

Add after the filters object (around line ~1508):

```javascript
// Time range picker state
timeRangePickerOpen: false,
timeRangePickerTab: 'relative', // 'relative' | 'absolute' | 'timeofday'
// Temporary state while editing (applied on "Apply")
tempFilters: null,
```

**Step 3: Run templ generate**

```bash
templ generate
```

**Step 4: Verify no errors**

```bash
go build ./...
```

**Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(stats): add time range picker data model"
```

---

## Task 2: Add Helper Functions

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (after `setTimePreset` function, ~line 1670)

**Step 1: Add time range display formatter**

```javascript
formatTimeRangeDisplay() {
    let fromText, untilText;

    if (this.filters.timeRangeMode === 'relative') {
        if (this.filters.relativeFrom.allTime) {
            fromText = 'All Time';
        } else {
            const v = this.filters.relativeFrom.value;
            const u = this.filters.relativeFrom.unit;
            fromText = `${v} ${u} ago`;
        }

        if (this.filters.relativeUntil.now) {
            untilText = 'Now';
        } else {
            const v = this.filters.relativeUntil.value;
            const u = this.filters.relativeUntil.unit;
            untilText = `${v} ${u} ago`;
        }
    } else {
        // Absolute mode
        fromText = this.formatDateForDisplay(this.filters.startDate, this.filters.absoluteFromTime);
        if (this.filters.relativeUntil.now) {
            untilText = 'Now';
        } else {
            untilText = this.formatDateForDisplay(this.filters.endDate, this.filters.absoluteUntilTime);
        }
    }

    let display = `From: ${fromText}    Until: ${untilText}`;

    if (this.filters.filterByTimeOfDay) {
        display += ` (${this.filters.timeOfDayStart}-${this.filters.timeOfDayEnd})`;
    }

    return display;
},

formatDateForDisplay(dateStr, timeStr) {
    const date = new Date(dateStr + 'T' + timeStr);
    const options = { month: 'short', day: 'numeric', year: 'numeric' };
    let result = date.toLocaleDateString('en-US', options);
    if (timeStr && timeStr !== '00:00') {
        result += ' ' + timeStr;
    }
    return result;
},
```

**Step 2: Add relative-to-absolute converter**

```javascript
applyRelativeToAbsolute() {
    const now = new Date();

    // Calculate start date
    if (this.filters.relativeFrom.allTime) {
        // Set to a far past date
        this.filters.startDate = '2000-01-01';
    } else {
        const startDate = this.subtractTime(now, this.filters.relativeFrom.value, this.filters.relativeFrom.unit);
        this.filters.startDate = startDate.toISOString().split('T')[0];
    }

    // Calculate end date
    if (this.filters.relativeUntil.now) {
        this.filters.endDate = now.toISOString().split('T')[0];
    } else {
        const endDate = this.subtractTime(now, this.filters.relativeUntil.value, this.filters.relativeUntil.unit);
        this.filters.endDate = endDate.toISOString().split('T')[0];
    }
},

subtractTime(date, value, unit) {
    const result = new Date(date);
    switch(unit) {
        case 'minutes':
            result.setMinutes(result.getMinutes() - value);
            break;
        case 'hours':
            result.setHours(result.getHours() - value);
            break;
        case 'days':
            result.setDate(result.getDate() - value);
            break;
        case 'weeks':
            result.setDate(result.getDate() - (value * 7));
            break;
        case 'months':
            result.setMonth(result.getMonth() - value);
            break;
        case 'years':
            result.setFullYear(result.getFullYear() - value);
            break;
    }
    return result;
},
```

**Step 3: Add picker open/close handlers**

```javascript
openTimeRangePicker() {
    // Save current state for cancel
    this.tempFilters = JSON.parse(JSON.stringify(this.filters));
    this.timeRangePickerOpen = true;
},

closeTimeRangePicker(apply = false) {
    if (apply) {
        // Apply relative to absolute conversion if in relative mode
        if (this.filters.timeRangeMode === 'relative') {
            this.applyRelativeToAbsolute();
        }
    } else {
        // Cancel - restore previous state
        if (this.tempFilters) {
            Object.assign(this.filters, this.tempFilters);
        }
    }
    this.tempFilters = null;
    this.timeRangePickerOpen = false;
},

incrementValue(field, delta) {
    const target = field === 'from' ? this.filters.relativeFrom : this.filters.relativeUntil;
    target.value = Math.max(0, target.value + delta);
},
```

**Step 4: Run templ generate and verify**

```bash
templ generate && go build ./...
```

**Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(stats): add time range picker helper functions"
```

---

## Task 3: Create Compact Trigger Button

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ:145-176` (replace date range selector)

**Step 1: Replace the date range selector div**

Find the "Date Range Selector" section (lines 145-176) and replace it with:

```html
<!-- Time Range Picker -->
<div class="xl:col-span-2 relative">
    <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
        Time Range
    </label>
    <!-- Compact Trigger Button -->
    <button
        @click="openTimeRangePicker()"
        type="button"
        class="w-full flex items-center px-4 py-2.5 border border-gray-300 dark:border-gray-600 rounded-lg shadow-sm bg-white dark:bg-dark-bg-tertiary hover:bg-gray-50 dark:hover:bg-gray-700 focus:outline-none focus:ring-2 focus:ring-blue-500 transition-colors"
    >
        <!-- Clock Icon -->
        <svg class="w-5 h-5 text-gray-500 dark:text-gray-400 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>
        </svg>
        <!-- Dropdown Arrow -->
        <svg class="w-4 h-4 text-gray-400 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
        </svg>
        <!-- Display Text -->
        <span class="flex-1 text-left text-sm text-gray-700 dark:text-gray-200" x-text="formatTimeRangeDisplay()"></span>
    </button>
</div>
```

**Step 2: Run templ generate and verify**

```bash
templ generate && go build ./...
```

**Step 3: Test in browser**

- Open the statistics page
- Verify the trigger button displays "From: 7 days ago    Until: Now"
- Click should call openTimeRangePicker() (nothing visible yet)

**Step 4: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(stats): add compact trigger button for time range picker"
```

---

## Task 4: Create Dropdown Panel Structure

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (after the trigger button, still inside the xl:col-span-2 div)

**Step 1: Add dropdown panel with tabs**

Insert after the trigger button `</button>`:

```html
<!-- Dropdown Panel -->
<div
    x-show="timeRangePickerOpen"
    x-transition:enter="transition ease-out duration-100"
    x-transition:enter-start="transform opacity-0 scale-95"
    x-transition:enter-end="transform opacity-100 scale-100"
    x-transition:leave="transition ease-in duration-75"
    x-transition:leave-start="transform opacity-100 scale-100"
    x-transition:leave-end="transform opacity-0 scale-95"
    @click.outside="closeTimeRangePicker(false)"
    @keydown.escape.window="closeTimeRangePicker(false)"
    class="absolute z-50 mt-2 w-full min-w-[400px] bg-white dark:bg-dark-bg-secondary rounded-lg shadow-xl border border-gray-200 dark:border-gray-700"
>
    <!-- Tab Headers -->
    <div class="flex border-b border-gray-200 dark:border-gray-700">
        <button
            @click="timeRangePickerTab = 'relative'"
            :class="timeRangePickerTab === 'relative' ? 'border-b-2 border-blue-500 text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'"
            class="flex-1 px-4 py-3 text-sm font-medium focus:outline-none"
            type="button"
        >
            Relative
        </button>
        <button
            @click="timeRangePickerTab = 'absolute'"
            :class="timeRangePickerTab === 'absolute' ? 'border-b-2 border-blue-500 text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'"
            class="flex-1 px-4 py-3 text-sm font-medium focus:outline-none"
            type="button"
        >
            Absolute
        </button>
        <button
            @click="timeRangePickerTab = 'timeofday'"
            :class="timeRangePickerTab === 'timeofday' ? 'border-b-2 border-blue-500 text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'"
            class="flex-1 px-4 py-3 text-sm font-medium focus:outline-none"
            type="button"
        >
            Time of Day
        </button>
    </div>

    <!-- Tab Content Area -->
    <div class="p-4">
        <!-- Relative Tab -->
        <div x-show="timeRangePickerTab === 'relative'">
            <p class="text-gray-500 text-sm">Relative tab content (Task 5)</p>
        </div>

        <!-- Absolute Tab -->
        <div x-show="timeRangePickerTab === 'absolute'">
            <p class="text-gray-500 text-sm">Absolute tab content (Task 6)</p>
        </div>

        <!-- Time of Day Tab -->
        <div x-show="timeRangePickerTab === 'timeofday'">
            <p class="text-gray-500 text-sm">Time of Day tab content (Task 7)</p>
        </div>
    </div>

    <!-- Footer with Cancel/Apply -->
    <div class="flex justify-end gap-2 px-4 py-3 bg-gray-50 dark:bg-dark-bg-tertiary border-t border-gray-200 dark:border-gray-700 rounded-b-lg">
        <button
            @click="closeTimeRangePicker(false)"
            type="button"
            class="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-white dark:bg-dark-bg-secondary border border-gray-300 dark:border-gray-600 rounded-md hover:bg-gray-50 dark:hover:bg-gray-700 focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
            Cancel
        </button>
        <button
            @click="closeTimeRangePicker(true)"
            type="button"
            class="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-md hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
            Apply
        </button>
    </div>
</div>
```

**Step 2: Run templ generate and verify**

```bash
templ generate && go build ./...
```

**Step 3: Test in browser**

- Click the trigger button
- Dropdown should appear with 3 tabs
- Clicking tabs should switch content
- Cancel closes dropdown
- Clicking outside closes dropdown
- Escape key closes dropdown

**Step 4: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(stats): add dropdown panel structure with tabs"
```

---

## Task 5: Implement Relative Tab

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (replace placeholder in Relative tab)

**Step 1: Replace the Relative tab placeholder**

Replace `<p class="text-gray-500 text-sm">Relative tab content (Task 5)</p>` with:

```html
<!-- From Section -->
<div class="mb-6">
    <div class="flex items-center mb-3">
        <span class="text-sm font-medium text-gray-700 dark:text-gray-300 w-16">From:</span>
        <label class="flex items-center cursor-pointer">
            <input
                type="checkbox"
                x-model="filters.relativeFrom.allTime"
                class="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
            />
            <span class="ml-2 text-sm text-gray-600 dark:text-gray-400">All Time</span>
        </label>
    </div>
    <div class="flex items-center gap-3" x-show="!filters.relativeFrom.allTime">
        <!-- Spinner -->
        <div class="flex flex-col items-center">
            <button
                @click="incrementValue('from', 1)"
                type="button"
                class="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded"
            >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7"/>
                </svg>
            </button>
            <input
                type="number"
                x-model.number="filters.relativeFrom.value"
                min="0"
                class="w-16 text-center py-1 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
            <button
                @click="incrementValue('from', -1)"
                type="button"
                class="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded"
            >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
                </svg>
            </button>
        </div>
        <!-- Unit Dropdown -->
        <select
            x-model="filters.relativeFrom.unit"
            class="px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
            <option value="minutes">Minutes</option>
            <option value="hours">Hours</option>
            <option value="days">Days</option>
            <option value="weeks">Weeks</option>
            <option value="months">Months</option>
            <option value="years">Years</option>
        </select>
        <span class="text-sm text-gray-600 dark:text-gray-400">ago</span>
    </div>
</div>

<!-- Until Section -->
<div>
    <div class="flex items-center mb-3">
        <span class="text-sm font-medium text-gray-700 dark:text-gray-300 w-16">Until:</span>
        <label class="flex items-center cursor-pointer">
            <input
                type="checkbox"
                x-model="filters.relativeUntil.now"
                class="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
            />
            <span class="ml-2 text-sm text-gray-600 dark:text-gray-400">Now</span>
        </label>
    </div>
    <div class="flex items-center gap-3" x-show="!filters.relativeUntil.now">
        <!-- Spinner -->
        <div class="flex flex-col items-center">
            <button
                @click="incrementValue('until', 1)"
                type="button"
                class="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded"
            >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7"/>
                </svg>
            </button>
            <input
                type="number"
                x-model.number="filters.relativeUntil.value"
                min="0"
                class="w-16 text-center py-1 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
            <button
                @click="incrementValue('until', -1)"
                type="button"
                class="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded"
            >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
                </svg>
            </button>
        </div>
        <!-- Unit Dropdown -->
        <select
            x-model="filters.relativeUntil.unit"
            class="px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
            <option value="minutes">Minutes</option>
            <option value="hours">Hours</option>
            <option value="days">Days</option>
            <option value="weeks">Weeks</option>
            <option value="months">Months</option>
            <option value="years">Years</option>
        </select>
        <span class="text-sm text-gray-600 dark:text-gray-400">ago</span>
    </div>
</div>
```

**Step 2: Update data model to set mode on tab switch**

Add to the Relative tab button's @click:

```html
@click="timeRangePickerTab = 'relative'; filters.timeRangeMode = 'relative'"
```

**Step 3: Run templ generate and verify**

```bash
templ generate && go build ./...
```

**Step 4: Test in browser**

- Open picker, go to Relative tab
- Click ▲/▼ arrows to change value
- Type directly in number input
- Change unit dropdown
- Check/uncheck "All Time" and "Now"
- Verify display updates on Apply

**Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(stats): implement relative tab with spinner inputs"
```

---

## Task 6: Implement Absolute Tab

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (replace placeholder in Absolute tab)

**Step 1: Replace the Absolute tab placeholder**

Replace `<p class="text-gray-500 text-sm">Absolute tab content (Task 6)</p>` with:

```html
<!-- From Section -->
<div class="mb-6">
    <span class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">From:</span>
    <div class="flex gap-2">
        <input
            type="date"
            x-model="filters.startDate"
            class="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <input
            type="time"
            x-model="filters.absoluteFromTime"
            class="w-28 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
    </div>
</div>

<!-- Until Section -->
<div>
    <div class="flex items-center mb-2">
        <span class="text-sm font-medium text-gray-700 dark:text-gray-300 mr-3">Until:</span>
        <label class="flex items-center cursor-pointer">
            <input
                type="checkbox"
                x-model="filters.relativeUntil.now"
                class="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
            />
            <span class="ml-2 text-sm text-gray-600 dark:text-gray-400">Now</span>
        </label>
    </div>
    <div class="flex gap-2" x-show="!filters.relativeUntil.now">
        <input
            type="date"
            x-model="filters.endDate"
            class="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <input
            type="time"
            x-model="filters.absoluteUntilTime"
            class="w-28 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
    </div>
    <div x-show="filters.relativeUntil.now" class="text-sm text-gray-500 dark:text-gray-400 italic">
        End date will be set to current time
    </div>
</div>
```

**Step 2: Update data model to set mode on tab switch**

Update the Absolute tab button's @click:

```html
@click="timeRangePickerTab = 'absolute'; filters.timeRangeMode = 'absolute'"
```

**Step 3: Update closeTimeRangePicker to handle absolute mode**

In `closeTimeRangePicker` function, update the apply logic:

```javascript
closeTimeRangePicker(apply = false) {
    if (apply) {
        if (this.filters.timeRangeMode === 'relative') {
            this.applyRelativeToAbsolute();
        } else {
            // Absolute mode - update endDate if "Now" is checked
            if (this.filters.relativeUntil.now) {
                this.filters.endDate = new Date().toISOString().split('T')[0];
            }
        }
    } else {
        if (this.tempFilters) {
            Object.assign(this.filters, this.tempFilters);
        }
    }
    this.tempFilters = null;
    this.timeRangePickerOpen = false;
},
```

**Step 4: Run templ generate and verify**

```bash
templ generate && go build ./...
```

**Step 5: Test in browser**

- Open picker, go to Absolute tab
- Select dates using date pickers
- Select times using time inputs
- Check/uncheck "Now" for until
- Verify query uses correct dates on Apply

**Step 6: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(stats): implement absolute tab with date/time pickers"
```

---

## Task 7: Implement Time of Day Tab

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (replace placeholder in Time of Day tab)

**Step 1: Replace the Time of Day tab placeholder**

Replace `<p class="text-gray-500 text-sm">Time of Day tab content (Task 7)</p>` with:

```html
<!-- Enable Checkbox -->
<label class="flex items-center cursor-pointer mb-4">
    <input
        type="checkbox"
        x-model="filters.filterByTimeOfDay"
        class="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
    />
    <span class="ml-2 text-sm font-medium text-gray-700 dark:text-gray-300">Filter by time of day</span>
</label>

<div x-show="filters.filterByTimeOfDay" x-collapse>
    <!-- Time Inputs -->
    <div class="flex items-center gap-4 mb-4">
        <div>
            <label class="block text-xs text-gray-500 dark:text-gray-400 mb-1">From</label>
            <input
                type="time"
                x-model="filters.timeOfDayStart"
                class="px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
        </div>
        <div>
            <label class="block text-xs text-gray-500 dark:text-gray-400 mb-1">To</label>
            <input
                type="time"
                x-model="filters.timeOfDayEnd"
                class="px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
        </div>
    </div>

    <!-- Quick Presets -->
    <div class="mb-4">
        <span class="block text-xs text-gray-500 dark:text-gray-400 mb-2">Quick presets:</span>
        <div class="flex flex-wrap gap-2">
            <button @click="setTimePreset('night')" type="button" class="inline-flex items-center px-3 py-1.5 text-xs font-medium bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 rounded-md hover:bg-indigo-200 dark:hover:bg-indigo-800/40">
                <span class="mr-1.5">🌙</span> Night (22h-06h)
            </button>
            <button @click="setTimePreset('business')" type="button" class="inline-flex items-center px-3 py-1.5 text-xs font-medium bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded-md hover:bg-green-200 dark:hover:bg-green-800/40">
                <span class="mr-1.5">💼</span> Business (09h-18h)
            </button>
            <button @click="setTimePreset('morning')" type="button" class="inline-flex items-center px-3 py-1.5 text-xs font-medium bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 rounded-md hover:bg-yellow-200 dark:hover:bg-yellow-800/40">
                <span class="mr-1.5">🌅</span> Morning (06h-12h)
            </button>
            <button @click="setTimePreset('evening')" type="button" class="inline-flex items-center px-3 py-1.5 text-xs font-medium bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300 rounded-md hover:bg-orange-200 dark:hover:bg-orange-800/40">
                <span class="mr-1.5">🌆</span> Evening (18h-22h)
            </button>
            <button @click="applyOnCallFilter()" type="button" class="inline-flex items-center px-3 py-1.5 text-xs font-medium bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 rounded-md hover:bg-purple-200 dark:hover:bg-purple-800/40">
                <span class="mr-1.5">🌑</span> On-Call
            </button>
        </div>
    </div>

    <!-- Weekend Checkbox -->
    <label class="flex items-center cursor-pointer">
        <input
            type="checkbox"
            x-model="filters.includeWeekends"
            class="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
        />
        <span class="ml-2 text-sm text-gray-600 dark:text-gray-400">Include weekends (Sat/Sun)</span>
    </label>
</div>
```

**Step 2: Run templ generate and verify**

```bash
templ generate && go build ./...
```

**Step 3: Test in browser**

- Open picker, go to Time of Day tab
- Enable "Filter by time of day"
- Click preset buttons - times should update
- Verify trigger displays time filter in parentheses

**Step 4: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(stats): implement time of day tab with presets"
```

---

## Task 8: Remove Old Time of Day Section

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ:178-244` (remove old Time of Day filter section)

**Step 1: Remove the old "Time of Day Filter" section**

Find and delete the entire section from line ~178 (after the new picker) to ~244 that contains:
- "Filter by Time of Day" checkbox
- Time inputs (timeOfDayStart, timeOfDayEnd)
- Time preset buttons (Night, Business, Morning, Evening, On-Call)
- Weekend checkbox

This is the `xl:col-span-2` div that starts with `<!-- Time of Day Filter -->`.

**Step 2: Adjust grid layout**

The filter panel should now have more space. Update the remaining filters (Group By, Severity, Teams) to fill the space appropriately if needed.

**Step 3: Run templ generate and verify**

```bash
templ generate && go build ./...
```

**Step 4: Test in browser**

- Verify old time of day section is gone
- Time of Day is only accessible via the picker dropdown
- All functionality still works

**Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "refactor(stats): remove old time of day section (moved to picker)"
```

---

## Task 9: Final Polish and Testing

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ`

**Step 1: Add keyboard support to spinners**

Update the number inputs in Relative tab to handle arrow keys:

```html
<input
    type="number"
    x-model.number="filters.relativeFrom.value"
    @keydown.up.prevent="incrementValue('from', 1)"
    @keydown.down.prevent="incrementValue('from', -1)"
    min="0"
    ...
/>
```

**Step 2: Add Enter key to apply**

Add to the dropdown panel:

```html
@keydown.enter.prevent="closeTimeRangePicker(true)"
```

**Step 3: Run templ generate**

```bash
templ generate && go build ./...
```

**Step 4: Full integration test**

Test all scenarios:
- [ ] Relative: Last 7 days → Now (default)
- [ ] Relative: Last 30 minutes → Now
- [ ] Relative: All Time → Now
- [ ] Relative: Last 1 year → 6 months ago
- [ ] Absolute: Specific dates with times
- [ ] Absolute: Date → Now
- [ ] Time of Day: Business hours filter
- [ ] Time of Day: Night + exclude weekends
- [ ] Cancel reverts changes
- [ ] Keyboard: arrows, enter, escape
- [ ] Dark mode styling

**Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/pages/StatisticsDashboard_templ.go
git commit -m "feat(stats): finalize time range picker with keyboard support"
```

---

## Summary

| Task | Description | Estimated Steps |
|------|-------------|-----------------|
| 1 | Extend data model | 5 |
| 2 | Add helper functions | 5 |
| 3 | Create compact trigger | 4 |
| 4 | Create dropdown structure | 4 |
| 5 | Implement Relative tab | 5 |
| 6 | Implement Absolute tab | 6 |
| 7 | Implement Time of Day tab | 4 |
| 8 | Remove old section | 5 |
| 9 | Final polish | 5 |

**Total: 9 tasks, ~43 steps**
