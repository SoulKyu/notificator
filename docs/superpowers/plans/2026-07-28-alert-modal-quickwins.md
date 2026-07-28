# Alert Modal Quick Wins (Copy Link + Frequency Sparkline) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a copy-link button to the alert detail modal header and a 30-day frequency sparkline to its Overview tab.

**Architecture:** Frontend-only. Markup lives in templ templates (`modal_components.templ`), behavior in the Alpine.js mixin (`dashboard_modal.templ`), reactive state declarations in `dashboard_core.templ`. The sparkline reuses the existing alert-history endpoint; history is now fetched on modal open instead of lazily on History-tab click.

**Tech Stack:** Go templ templates, Alpine.js, Tailwind CSS. No new dependencies, no backend changes.

**Spec:** `docs/superpowers/specs/2026-07-28-alert-modal-quickwins-design.md`

## Global Constraints

- NEVER edit `*_templ.go` files — edit `.templ` sources, then run `make webui-templates` to regenerate.
- Both features MUST style dark mode using the existing `dark:` utility classes already used throughout the modal.
- Zero backend changes. The history endpoint caps at 50 entries; an alert firing >50×/30d displays `≥` prefix.
- Sparkline hidden entirely on history fetch failure or empty history — no error surfaced.
- Commits: Conventional Commits, English, no Co-Authored-By trailers.
- Scope is the DASHBOARD alert modal only (`modal_components.templ` AlertDetailsModal). Do not touch the shared components used by the Statistics readonly modal (`alert_modal_shared.templ`).

**Placement deviation from spec (approved rationale):** the spec says "sparkline inside the Timeline card". The Timeline card (`AlertModalTimelineCard`) is a shared component also used by the Statistics readonly modal, which has no `alertHistory` state. To honor the "dashboard-only, don't break statistics" constraint, the sparkline is a sibling full-width card directly below the Overview card grid instead.

---

### Task 1: Copy alert link button

**Files:**
- Modify: `internal/webui/templates/scripts/dashboard_core.templ:77` (state declaration)
- Modify: `internal/webui/templates/scripts/dashboard_modal.templ:122` (mixin method)
- Modify: `internal/webui/templates/components/modal_components.templ:975` (header button)

**Interfaces:**
- Consumes: existing `copyToClipboard(text)` from `dashboard_utilities.templ:57` (already has the non-secure-context `textarea` fallback — do NOT reimplement it).
- Produces: mixin method `copyAlertLink()` and reactive state `alertLinkCopied` (boolean), used only by the header button.

- [ ] **Step 1: Declare the `alertLinkCopied` state**

In `internal/webui/templates/scripts/dashboard_core.templ`, find:

```js
				alertHistory: null,
				historyLoading: false,
```

and add one line after it:

```js
				alertHistory: null,
				historyLoading: false,
				alertLinkCopied: false,
```

- [ ] **Step 2: Add the `copyAlertLink()` mixin method**

In `internal/webui/templates/scripts/dashboard_modal.templ`, the mixin object `window.dashboardModalMixin` contains `closeAlertModal(options) { ... },` (ends around line 122). Immediately after that closing `},` insert:

```js
			copyAlertLink() {
				const fingerprint = this.alertDetails?.alert?.fingerprint;
				if (!fingerprint) return;
				this.copyToClipboard(`${window.location.origin}/dashboard/alert/${fingerprint}`);
				this.alertLinkCopied = true;
				setTimeout(() => { this.alertLinkCopied = false; }, 2000);
			},
```

- [ ] **Step 3: Add the header button**

In `internal/webui/templates/components/modal_components.templ`, find the close button of the dashboard alert modal header (line ~975):

```html
							<!-- Close button - positioned absolutely for modern look -->
							<button @click="closeAlertModal()" 
```

Insert BEFORE that comment block (same indentation):

```html
							<!-- Copy alert link button -->
							<button @click="copyAlertLink()"
									title="Copy alert link"
									class="absolute top-4 right-14 p-2 rounded-full hover:bg-white/80 dark:hover:bg-black/20 transition-colors duration-200 group">
								<svg x-show="!alertLinkCopied" class="w-5 h-5 text-gray-400 group-hover:text-gray-600 dark:group-hover:text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.828 10.172a4 4 0 010 5.656l-4 4a4 4 0 01-5.656-5.656l1.102-1.101m9.554-1.899l1.102-1.101a4 4 0 10-5.656-5.656l-4 4a4 4 0 001.885 6.727"/>
								</svg>
								<svg x-show="alertLinkCopied" style="display: none;" class="w-5 h-5 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/>
								</svg>
							</button>
```

Note: `right-14` places it left of the close button (`right-4` + button width). The green check swaps in for 2s via `alertLinkCopied`.

- [ ] **Step 4: Regenerate templates and build**

Run: `make webui-templates && go build ./...`
Expected: both succeed, no diff outside `*_templ.go` regeneration.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/templates/scripts/dashboard_core.templ internal/webui/templates/scripts/dashboard_core_templ.go internal/webui/templates/scripts/dashboard_modal.templ internal/webui/templates/scripts/dashboard_modal_templ.go internal/webui/templates/components/modal_components.templ internal/webui/templates/components/modal_components_templ.go
git commit -m "feat(webui): copy alert deep link button in alert modal header"
```

---

### Task 2: Fetch history on modal open + sparkline data computation

**Files:**
- Modify: `internal/webui/templates/scripts/dashboard_core.templ:77` (state declaration)
- Modify: `internal/webui/templates/scripts/dashboard_modal.templ:52-87` (`showAlertDetails`), `:574-606` (`loadAlertHistory`), new method `computeSparkline`
- Modify: `internal/webui/templates/components/modal_components.templ:1120` (History tab button, avoid double fetch)

**Interfaces:**
- Consumes: existing `loadAlertHistory()` (`dashboard_modal.templ:574`) which fills `this.alertHistory` from `GET /api/v1/dashboard/alert/:fingerprint/history`. Response shape: `{ fingerprint, total_occurrences, history: [{ id, fired_at (RFC3339), resolved_at?, ... }] }` (see `HandleGetAlertHistory`, `internal/webui/handlers/dashboard_handlers.go:2432-2492`, capped at 50 entries).
- Produces: reactive state `sparkline` — either `null` or `{ bins: [{count, pct, label}] (length 30, oldest first), total: number, capped: boolean, last7: number, prev7: number, trendUp: boolean }`. Task 3's markup binds exactly these names.

- [ ] **Step 1: Declare the `sparkline` state**

In `internal/webui/templates/scripts/dashboard_core.templ`, extend the block from Task 1 Step 1:

```js
				alertHistory: null,
				historyLoading: false,
				alertLinkCopied: false,
				sparkline: null,
```

- [ ] **Step 2: Reset stale history and fetch on modal open**

In `internal/webui/templates/scripts/dashboard_modal.templ`, `showAlertDetails` currently starts:

```js
			async showAlertDetails(fingerprint, options) {
				this.alertDetailsLoading = true;
				this.showAlertModal = true;
				this.currentAlertTab = 'overview';
				this.alertDetails = null;
```

Add two reset lines after `this.alertDetails = null;`:

```js
				this.alertDetails = null;
				this.alertHistory = null;
				this.sparkline = null;
```

Then in the same function, after the success assignment:

```js
					if (result.success) {
						this.alertDetails = result.data;
```

add a fire-and-forget history load (no `await` — the modal must not block on it):

```js
					if (result.success) {
						this.alertDetails = result.data;
						this.loadAlertHistory();
```

- [ ] **Step 3: Compute sparkline data whenever history lands**

Still in `dashboard_modal.templ`, in `window.dashboardModalMixin.loadAlertHistory` (line ~574), the `finally` block currently reads:

```js
				} finally {
					this.historyLoading = false;
				}
```

Change it to:

```js
				} finally {
					this.historyLoading = false;
					this.computeSparkline();
				}
```

Then add the new mixin method immediately after `loadAlertHistory`'s closing `};` (same `window.dashboardModalMixin.X = function` style as its neighbors):

```js
		window.dashboardModalMixin.computeSparkline = function() {
			const entries = this.alertHistory?.history || [];
			const DAYS = 30;
			const dayMs = 24 * 60 * 60 * 1000;
			const todayStart = new Date();
			todayStart.setHours(0, 0, 0, 0);
			const counts = new Array(DAYS).fill(0);
			for (const e of entries) {
				const t = Date.parse(e.fired_at);
				if (isNaN(t)) continue;
				const d = new Date(t);
				d.setHours(0, 0, 0, 0);
				// Math.round absorbs DST hour shifts in the day delta
				const daysAgo = Math.round((todayStart.getTime() - d.getTime()) / dayMs);
				if (daysAgo < 0 || daysAgo >= DAYS) continue;
				counts[DAYS - 1 - daysAgo]++;
			}
			const total = counts.reduce((a, b) => a + b, 0);
			if (total === 0) {
				this.sparkline = null;
				return;
			}
			const max = Math.max(...counts);
			const bins = counts.map((count, i) => {
				const daysAgo = DAYS - 1 - i;
				return {
					count: count,
					pct: count === 0 ? 4 : Math.max(10, Math.round((count / max) * 100)),
					label: (daysAgo === 0 ? 'Today' : daysAgo + 'd ago') + ' — ' + count + ' occurrence' + (count === 1 ? '' : 's')
				};
			});
			const last7 = counts.slice(-7).reduce((a, b) => a + b, 0);
			const prev7 = counts.slice(-14, -7).reduce((a, b) => a + b, 0);
			this.sparkline = {
				bins: bins,
				total: total,
				// ponytail: history endpoint caps at 50 entries; a flapper shows "≥N". Add a limit param server-side if this ever matters.
				capped: entries.length >= 50,
				last7: last7,
				prev7: prev7,
				trendUp: last7 > prev7
			};
		};
```

- [ ] **Step 4: Self-check the binning logic**

The function is plain JS embedded in a templ script — no test framework covers it. Run this one-off check (duplicated logic on purpose, fixture-driven):

```bash
node -e '
const dayMs = 86400000, DAYS = 30;
const todayStart = new Date(); todayStart.setHours(0,0,0,0);
const iso = (daysAgo) => new Date(todayStart.getTime() - daysAgo*dayMs + 3600000).toISOString();
const entries = [ {fired_at: iso(0)}, {fired_at: iso(0)}, {fired_at: iso(3)}, {fired_at: iso(29)}, {fired_at: iso(31)}, {fired_at: "garbage"} ];
const counts = new Array(DAYS).fill(0);
for (const e of entries) {
  const t = Date.parse(e.fired_at); if (isNaN(t)) continue;
  const d = new Date(t); d.setHours(0,0,0,0);
  const daysAgo = Math.round((todayStart.getTime() - d.getTime()) / dayMs);
  if (daysAgo < 0 || daysAgo >= DAYS) continue;
  counts[DAYS - 1 - daysAgo]++;
}
const assert = require("assert");
assert.equal(counts[29], 2, "today bucket");
assert.equal(counts[26], 1, "3d-ago bucket");
assert.equal(counts[0], 1, "29d-ago bucket");
assert.equal(counts.reduce((a,b)=>a+b,0), 4, "31d-ago and garbage excluded");
console.log("binning OK");
'
```

Expected output: `binning OK`

- [ ] **Step 5: Avoid double fetch from the History tab**

In `internal/webui/templates/components/modal_components.templ` line ~1120, the History tab button currently reads:

```html
								<button @click="currentAlertTab = 'history'; loadAlertHistory()"
```

History is now loaded on modal open, so only refetch if the open-time fetch failed:

```html
								<button @click="currentAlertTab = 'history'; if (!alertHistory) loadAlertHistory()"
```

- [ ] **Step 6: Regenerate templates and build**

Run: `make webui-templates && go build ./...`
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add internal/webui/templates/scripts/dashboard_core.templ internal/webui/templates/scripts/dashboard_core_templ.go internal/webui/templates/scripts/dashboard_modal.templ internal/webui/templates/scripts/dashboard_modal_templ.go internal/webui/templates/components/modal_components.templ internal/webui/templates/components/modal_components_templ.go
git commit -m "feat(webui): load alert history on modal open and compute 30-day sparkline data"
```

---

### Task 3: Sparkline markup in the Overview tab

**Files:**
- Modify: `internal/webui/templates/components/modal_components.templ:1159` (Overview tab, below the card grid)

**Interfaces:**
- Consumes: reactive state `sparkline` produced by Task 2 — `{ bins: [{count, pct, label}], total, capped, last7, prev7, trendUp }`, or `null` when hidden.
- Produces: nothing consumed downstream.

- [ ] **Step 1: Add the Frequency card**

In `internal/webui/templates/components/modal_components.templ`, the Overview tab contains (line ~1155):

```html
										<!-- Quick Info Cards - using shared components -->
										<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 mb-8">
											@AlertModalStatusCard("alertDetails?.alert")
											@AlertModalTimelineCard("alertDetails?.alert", false)
											@AlertModalMetadataCard("alertDetails?.alert")
										</div>
```

Insert AFTER that grid's closing `</div>` (and before the `<!-- Summary and Description Cards -->` block), same indentation:

```html
										<!-- Frequency sparkline (30-day occurrence histogram, hidden when no history) -->
										<div x-show="sparkline" class="bg-gradient-to-br from-white to-gray-50 dark:from-dark-bg-tertiary dark:to-gray-800 rounded-xl p-6 shadow-sm border border-gray-200/50 dark:border-dark-border-subtle/50 mb-8">
											<h4 class="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center">
												<svg class="w-5 h-5 mr-2 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
													<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 8v8m-4-5v5m-4-2v2m-2 4h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"/>
												</svg>
												Frequency
											</h4>
											<div class="flex flex-wrap items-baseline justify-between gap-2 mb-3">
												<span class="text-sm font-semibold text-gray-900 dark:text-white"
													  x-text="sparkline ? (sparkline.capped ? '≥' + sparkline.total : sparkline.total) + ' occurrences in the last 30 days' : ''"></span>
												<span class="text-xs font-semibold"
													  :class="sparkline?.trendUp ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'"
													  x-text="sparkline ? (sparkline.trendUp ? '▲ ' : '▼ ') + sparkline.last7 + ' this week (vs ' + sparkline.prev7 + ' last week)' : ''"></span>
											</div>
											<div class="flex items-end gap-0.5 h-12">
												<template x-for="(bin, i) in sparkline?.bins || []" :key="i">
													<div class="flex-1 rounded-sm transition-colors"
														 :class="i === 29 ? 'bg-red-400 dark:bg-red-500' : (bin.count > 0 ? 'bg-blue-300 dark:bg-blue-600 hover:bg-blue-500 dark:hover:bg-blue-400' : 'bg-gray-200 dark:bg-gray-700')"
														 :style="'height: ' + bin.pct + '%'"
														 :title="bin.label"></div>
												</template>
											</div>
											<div class="flex justify-between text-[10px] text-gray-400 dark:text-gray-500 mt-1">
												<span>30 days ago</span>
												<span>today</span>
											</div>
										</div>
```

Bars use plain flex divs, not SVG — Alpine `<template x-for>` cannot live inside an `<svg>` element, and divs need no viewBox math. Zero-count days show a 4% stub; today's bar is red; hover shows the native `title` tooltip.

- [ ] **Step 2: Regenerate templates and build**

Run: `make webui-templates && go build ./...`
Expected: both succeed.

- [ ] **Step 3: Manual verification (user's standard workflow)**

Run: `make test` (docker-compose full rebuild), open `http://127.0.0.1:8081/dashboard`, then check:

1. Open any alert → header shows the link icon left of ✕; click it → icon flips to a green check for 2s; pasted URL is `http://127.0.0.1:8081/dashboard/alert/<fingerprint>` and opening it in a new tab deep-links to the same modal.
2. Overview tab of a recurring alert → Frequency card with bars, total line, and week-over-week trend; hover a bar → "Nd ago — X occurrences".
3. Alert with no history (fresh alert) → no Frequency card, no console error.
4. History tab still renders its table (loaded once on open, no second network call in DevTools unless the first failed).
5. Toggle dark mode → both the button hover state and the Frequency card follow the theme.

- [ ] **Step 4: Commit**

```bash
git add internal/webui/templates/components/modal_components.templ internal/webui/templates/components/modal_components_templ.go
git commit -m "feat(webui): 30-day frequency sparkline in alert modal overview"
```
