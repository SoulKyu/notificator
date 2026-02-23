# Time Picker UX Redesign - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace native HTML time inputs with smart text inputs that accept flexible formats (902, 9:02, 0902) and display a visual 24h timeline bar.

**Architecture:** Frontend-only changes. Replace `<input type="time">` with styled text inputs using Alpine.js for parsing/validation. Add a read-only timeline visualization. Enhance presets to show times before clicking.

**Tech Stack:** Alpine.js, Tailwind CSS, Templ templates

---

## Task 1: Add CSS for Timeline Bar and Validation Animation

**Files:**
- Modify: `internal/webui/static/css/input.css` (add at end)

**Step 1: Add the shake animation keyframes**

Add this at the end of `input.css`:

```css
/* Time picker enhancements */
@keyframes shake {
  0%, 100% { transform: translateX(0); }
  20%, 60% { transform: translateX(-4px); }
  40%, 80% { transform: translateX(4px); }
}

.time-input-error {
  @apply border-red-500 dark:border-red-400;
  animation: shake 0.3s ease-in-out;
}

/* Timeline bar component */
.timeline-bar {
  @apply flex h-2 rounded-full overflow-hidden bg-slate-200 dark:bg-slate-700;
}

.timeline-segment {
  @apply h-full transition-colors duration-200;
}

.timeline-segment-active {
  @apply bg-blue-500 dark:bg-blue-400;
}

.timeline-segment-inactive {
  @apply bg-slate-200 dark:bg-slate-700;
}

/* Monospace time input */
.time-input {
  @apply font-mono text-center px-3 py-2 text-sm border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-dark-bg-tertiary text-slate-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 w-20;
}
```

**Step 2: Rebuild Tailwind CSS**

Run: `npx @tailwindcss/cli -i internal/webui/static/css/input.css -o internal/webui/static/css/output.css`
Expected: CSS file regenerated with new classes

**Step 3: Commit**

```bash
git add internal/webui/static/css/input.css
git commit -m "$(cat <<'EOF'
style: add time picker CSS for timeline bar and validation

- Add shake animation for invalid input feedback
- Add timeline-bar component styles
- Add monospace time-input class

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add Time Parsing Helper Function

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (in the Alpine.js data section, around line 1500)

**Step 1: Add the parseTimeInput function**

Find the Alpine.js component initialization (around line 1475 `x-data="{ ... }"`). Inside the data object, add this function after the existing helper functions (after `getOnCallSchedule()` around line 1770):

```javascript
// Parse flexible time input formats to HH:MM
parseTimeInput(input) {
    if (!input || typeof input !== 'string') return null;

    const cleaned = input.replace(/[^0-9]/g, '');
    if (cleaned.length === 0) return null;

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
},
```

**Step 2: Add validateAndSetTime function**

Add this function right after `parseTimeInput`:

```javascript
// Validate input and update filter, show error if invalid
validateAndSetTime(field, rawValue, inputId) {
    const parsed = this.parseTimeInput(rawValue);
    const inputEl = document.getElementById(inputId);

    if (parsed) {
        // Valid: update the filter and clear error styling
        if (field === 'start') {
            this.filters.timeOfDayStart = parsed;
            this.timeOfDayStartRaw = parsed;
        } else {
            this.filters.timeOfDayEnd = parsed;
            this.timeOfDayEndRaw = parsed;
        }
        this.filters.activePreset = null;
        if (inputEl) inputEl.classList.remove('time-input-error');
    } else {
        // Invalid: show error animation, revert to last valid
        if (inputEl) {
            inputEl.classList.remove('time-input-error');
            // Force reflow to restart animation
            void inputEl.offsetWidth;
            inputEl.classList.add('time-input-error');
        }
        // Revert to last valid value after animation
        setTimeout(() => {
            if (field === 'start') {
                this.timeOfDayStartRaw = this.filters.timeOfDayStart;
            } else {
                this.timeOfDayEndRaw = this.filters.timeOfDayEnd;
            }
            if (inputEl) inputEl.classList.remove('time-input-error');
        }, 300);
    }
},
```

**Step 3: Add getTimelineSegments function**

Add this function to compute the timeline bar segments:

```javascript
// Get timeline segments for visualization (24 segments, one per hour)
getTimelineSegments() {
    const startTime = this.filters.timeOfDayStart || '00:00';
    const endTime = this.filters.timeOfDayEnd || '23:59';

    const startHour = parseInt(startTime.split(':')[0], 10);
    const endHour = parseInt(endTime.split(':')[0], 10);
    const endMinute = parseInt(endTime.split(':')[1], 10);

    // Effective end hour (if minutes > 0, that hour is active)
    const effectiveEndHour = endMinute > 0 ? endHour : endHour - 1;

    const segments = [];
    for (let h = 0; h < 24; h++) {
        let active = false;

        if (startHour <= effectiveEndHour) {
            // Normal range (e.g., 09:00 - 18:00)
            active = h >= startHour && h <= effectiveEndHour;
        } else {
            // Overnight range (e.g., 22:00 - 06:00)
            active = h >= startHour || h <= effectiveEndHour;
        }

        segments.push({ hour: h, active });
    }
    return segments;
},
```

**Step 4: Add raw input tracking properties to filters**

Find the `filters: {` object initialization (around line 1500) and add these two properties:

```javascript
// After timeOfDayEnd: '23:59',
timeOfDayStartRaw: '00:00',
timeOfDayEndRaw: '23:59',
```

**Step 5: Verify the file compiles**

Run: `templ generate`
Expected: No errors

**Step 6: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ
git commit -m "$(cat <<'EOF'
feat(time-picker): add parsing and validation functions

- parseTimeInput: accepts 9, 902, 09:02, 0902 formats
- validateAndSetTime: validates input, shows error animation
- getTimelineSegments: computes 24h timeline bar data

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Replace Native Time Inputs with Text Inputs

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (lines 319-328)

**Step 1: Replace the time input section**

Find this block (around lines 319-328):

```html
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
```

Replace it with:

```html
<!-- Time inputs (smart text inputs) -->
<div class="grid grid-cols-2 gap-3">
    <div>
        <label class="block text-xs text-slate-500 dark:text-slate-400 mb-1">From</label>
        <input
            type="text"
            id="time-input-start"
            x-model="timeOfDayStartRaw"
            @focus="$el.select()"
            @blur="validateAndSetTime('start', timeOfDayStartRaw, 'time-input-start')"
            @keydown.enter="validateAndSetTime('start', timeOfDayStartRaw, 'time-input-start'); $el.blur()"
            placeholder="HH:MM"
            class="time-input w-full"
            x-init="timeOfDayStartRaw = filters.timeOfDayStart"
        />
    </div>
    <div>
        <label class="block text-xs text-slate-500 dark:text-slate-400 mb-1">To</label>
        <input
            type="text"
            id="time-input-end"
            x-model="timeOfDayEndRaw"
            @focus="$el.select()"
            @blur="validateAndSetTime('end', timeOfDayEndRaw, 'time-input-end')"
            @keydown.enter="validateAndSetTime('end', timeOfDayEndRaw, 'time-input-end'); $el.blur()"
            placeholder="HH:MM"
            class="time-input w-full"
            x-init="timeOfDayEndRaw = filters.timeOfDayEnd"
        />
    </div>
</div>

<!-- Timeline bar visualization -->
<div class="mt-3">
    <div class="timeline-bar">
        <template x-for="seg in getTimelineSegments()" :key="seg.hour">
            <div
                class="timeline-segment flex-1"
                :class="seg.active ? 'timeline-segment-active' : 'timeline-segment-inactive'"
                :title="seg.hour + ':00'"
            ></div>
        </template>
    </div>
    <div class="flex justify-between text-[10px] text-slate-400 dark:text-slate-500 mt-1 px-0.5">
        <span>0</span>
        <span>6</span>
        <span>12</span>
        <span>18</span>
        <span>24</span>
    </div>
</div>
```

**Step 2: Verify the file compiles**

Run: `templ generate`
Expected: No errors

**Step 3: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ
git commit -m "$(cat <<'EOF'
feat(time-picker): replace native inputs with smart text inputs

- Text inputs accept flexible formats (9, 902, 9:02, 0902)
- Add 24h timeline bar visualization
- Select all on focus for easy replacement

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Enhance Presets to Show Times

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ` (lines 333-360)

**Step 1: Add preset data constant**

Find the Alpine.js data initialization and add this constant (around line 1500, in the filters section):

```javascript
// Preset definitions with times
timePresets: {
    oncall: { start: '18:00', end: '09:00', label: 'On-Call', color: 'purple', weekends: 'full' },
    night: { start: '22:00', end: '06:00', label: 'Night', color: 'indigo' },
    business: { start: '09:00', end: '18:00', label: 'Business', color: 'emerald' },
    morning: { start: '06:00', end: '12:00', label: 'Morning', color: 'amber' },
    evening: { start: '18:00', end: '22:00', label: 'Evening', color: 'orange' }
},
```

**Step 2: Replace the preset buttons section**

Find the Quick presets section (around lines 330-360):

```html
<!-- Quick presets -->
<div>
    <span class="block text-xs text-slate-500 dark:text-slate-400 mb-2">Quick presets</span>
    <div class="flex flex-wrap gap-1.5">
        <button @click="setTimePreset('oncall')" type="button"
            class="px-2.5 py-1 text-xs font-medium rounded-lg transition-colors"
            :class="filters.activePreset === 'oncall' ? 'bg-purple-600 text-white' : 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 hover:bg-purple-200'">
            On-Call
        </button>
        <!-- ... other buttons ... -->
    </div>
</div>
```

Replace with:

```html
<!-- Quick presets with time hints -->
<div>
    <span class="block text-xs text-slate-500 dark:text-slate-400 mb-2">Quick presets</span>
    <div class="flex flex-wrap gap-1.5">
        <template x-for="(preset, key) in timePresets" :key="key">
            <button @click="setTimePreset(key)" type="button"
                class="flex flex-col items-center px-2.5 py-1.5 text-xs font-medium rounded-lg transition-colors"
                :class="filters.activePreset === key
                    ? `bg-${preset.color}-600 text-white`
                    : `bg-${preset.color}-100 dark:bg-${preset.color}-900/30 text-${preset.color}-700 dark:text-${preset.color}-300 hover:bg-${preset.color}-200 dark:hover:bg-${preset.color}-900/50`">
                <span x-text="preset.label"></span>
                <span class="text-[10px] opacity-75 font-mono" x-text="preset.start + '→' + preset.end"></span>
            </button>
        </template>
    </div>
</div>
```

**Step 3: Update setTimePreset to use the preset data**

Find the `setTimePreset` function (around line 1778) and replace it with:

```javascript
setTimePreset(preset) {
    this.filters.activePreset = preset;
    const p = this.timePresets[preset];

    if (preset === 'oncall') {
        // On-call uses dynamic schedule
        const schedule = this.getOnCallSchedule();
        this.filters.timeOfDayStart = schedule.weekdayStart;
        this.filters.timeOfDayEnd = schedule.weekdayEnd;
        this.filters.weekendMode = 'full_weekends';
    } else if (p) {
        this.filters.timeOfDayStart = p.start;
        this.filters.timeOfDayEnd = p.end;
    }

    // Sync raw values
    this.timeOfDayStartRaw = this.filters.timeOfDayStart;
    this.timeOfDayEndRaw = this.filters.timeOfDayEnd;
},
```

**Step 4: Verify the file compiles**

Run: `templ generate`
Expected: No errors

**Step 5: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ
git commit -m "$(cat <<'EOF'
feat(time-picker): show times on preset buttons

- Presets now show their time range (e.g., "09:00→18:00")
- Users see exactly what clicking will do before clicking
- Refactored preset data into reusable constant

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Sync Raw Values on Preset Selection and Reset

**Files:**
- Modify: `internal/webui/templates/pages/StatisticsDashboard.templ`

**Step 1: Update resetTimeFilter to sync raw values**

Find the `resetTimeFilter` function (around line 1830) and update it:

```javascript
resetTimeFilter() {
    this.filters.timeOfDayStart = '00:00';
    this.filters.timeOfDayEnd = '23:59';
    this.filters.weekendMode = 'same_hours';
    this.filters.activePreset = null;
    // Sync raw values
    this.timeOfDayStartRaw = '00:00';
    this.timeOfDayEndRaw = '23:59';
},
```

**Step 2: Update applyView to sync raw values**

Find the `applyView` function in `statistics_views.templ` or in the StatisticsDashboard (wherever views are applied). After setting `timeOfDayStart` and `timeOfDayEnd`, add:

```javascript
// After: if (data.time_of_day_start) this.filters.timeOfDayStart = data.time_of_day_start;
// After: if (data.time_of_day_end) this.filters.timeOfDayEnd = data.time_of_day_end;
// Add:
this.timeOfDayStartRaw = this.filters.timeOfDayStart;
this.timeOfDayEndRaw = this.filters.timeOfDayEnd;
```

**Step 3: Verify the file compiles**

Run: `templ generate`
Expected: No errors

**Step 4: Commit**

```bash
git add internal/webui/templates/pages/StatisticsDashboard.templ internal/webui/templates/scripts/statistics_views.templ
git commit -m "$(cat <<'EOF'
fix(time-picker): sync raw input values on reset and view apply

Ensures text inputs stay in sync when:
- Reset to default is clicked
- A saved view is applied
- A preset is selected

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Test End-to-End and Polish

**Step 1: Rebuild the CSS**

Run: `npx @tailwindcss/cli -i internal/webui/static/css/input.css -o internal/webui/static/css/output.css`

**Step 2: Regenerate templ files**

Run: `templ generate`

**Step 3: Start the application**

Run: `go run cmd/webui/main.go` (or however the app is started)

**Step 4: Manual testing checklist**

Test in browser at `/statistics`:
- [ ] Type "9" → becomes "09:00"
- [ ] Type "902" → becomes "09:02"
- [ ] Type "14:30" → stays "14:30"
- [ ] Type "1430" → becomes "14:30"
- [ ] Type "25:00" → shows shake, reverts
- [ ] Type "abc" → shows shake, reverts
- [ ] Timeline bar shows correct range
- [ ] Overnight range (22:00-06:00) shows wrapped segments
- [ ] Presets show times (e.g., "Business 09:00→18:00")
- [ ] Clicking preset updates inputs AND timeline
- [ ] Reset clears to 00:00-23:59
- [ ] Dark mode styling works

**Step 5: Final commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
feat(time-picker): complete UX redesign implementation

Smart text inputs with flexible format parsing:
- Accepts: 9, 902, 09:02, 0902 → normalizes to HH:MM
- Validation with shake animation on invalid input
- 24h timeline bar visualization
- Presets show times before clicking
- Full dark mode support

Closes: time-picker-ux-improvement

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Add CSS styles | `input.css` |
| 2 | Add parsing functions | `StatisticsDashboard.templ` |
| 3 | Replace time inputs | `StatisticsDashboard.templ` |
| 4 | Enhance presets | `StatisticsDashboard.templ` |
| 5 | Sync raw values | `StatisticsDashboard.templ`, `statistics_views.templ` |
| 6 | Test and polish | All files |
