# Dashboard Terminal-Inspired Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Apply Terminal-Inspired aesthetic to NewDashboard.templ with JetBrains Mono typography, deep slate palette, grid texture, and glowing status indicators.

**Architecture:** CSS-first approach - add custom classes to input.css, Google Fonts to Base.templ, then update class names in NewDashboard.templ. No backend changes.

**Tech Stack:** Tailwind CSS 4, Google Fonts, templ templates

---

## Task 1: Add Google Fonts to Base Layout

**Files:**
- Modify: `internal/webui/templates/layouts/Base.templ:6-17`

**Step 1: Add font preconnect and stylesheet**

In `Base.templ`, add after line 10 (after favicon link):

```html
<link rel="preconnect" href="https://fonts.googleapis.com"/>
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin/>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet"/>
```

**Step 2: Verify fonts load**

Run: `go run ./cmd/server` and open browser DevTools → Network → filter "fonts"
Expected: See IBM Plex Sans and JetBrains Mono loading

---

## Task 2: Add Terminal Theme CSS Classes

**Files:**
- Modify: `internal/webui/static/css/input.css`

**Step 1: Add ops theme colors to @theme block**

After line 71 (after shadows), add:

```css
  /* Ops Terminal Theme */
  --color-ops-bg-primary: #0c1222;
  --color-ops-bg-surface: #1e293b;
  --color-ops-border: #334155;
  --color-ops-border-hover: #475569;
  --color-ops-accent: #22d3ee;
  --color-ops-text: #f1f5f9;
  --color-ops-text-muted: #94a3b8;
```

**Step 2: Add font families to @layer base**

Replace the `html` rule in `@layer base` (lines 74-77) with:

```css
@layer base {
  html {
    font-family: 'IBM Plex Sans', system-ui, sans-serif;
  }

  .font-mono, .font-data {
    font-family: 'JetBrains Mono', monospace;
  }

  /* Hide x-cloak elements until Alpine.js initializes */
  [x-cloak] {
    display: none !important;
  }
}
```

**Step 3: Add ops-specific component classes**

After line 243 (after `.alert-row-info`), add:

```css
  /* Ops Terminal Theme Components */
  .ops-grid-bg {
    background-color: #0c1222;
    background-image:
      linear-gradient(rgba(51, 65, 85, 0.3) 1px, transparent 1px),
      linear-gradient(90deg, rgba(51, 65, 85, 0.3) 1px, transparent 1px);
    background-size: 24px 24px;
  }

  .ops-card {
    @apply bg-[#1e293b] border border-[#334155] rounded shadow-lg shadow-black/20;
  }

  .ops-card-hover {
    @apply transition-all duration-150 hover:border-[#475569] hover:translate-y-[-1px];
  }

  .ops-header {
    @apply bg-[#1e293b]/95 backdrop-blur-sm border-b border-[#334155];
  }

  .ops-btn-primary {
    @apply bg-cyan-500 hover:bg-cyan-400 text-slate-900 font-medium rounded transition-all;
    @apply hover:shadow-[0_0_12px_rgba(34,211,238,0.4)];
  }

  .ops-btn-secondary {
    @apply bg-[#334155] hover:bg-[#475569] text-slate-100 font-medium rounded transition-all;
  }

  /* Status dots with glow */
  .ops-dot-critical {
    @apply h-2 w-2 bg-red-500 rounded-full shadow-[0_0_8px_rgba(239,68,68,0.6)];
  }

  .ops-dot-warning {
    @apply h-2 w-2 bg-amber-500 rounded-full shadow-[0_0_8px_rgba(245,158,11,0.5)];
  }

  .ops-dot-info {
    @apply h-2 w-2 bg-cyan-400 rounded-full shadow-[0_0_8px_rgba(34,211,238,0.5)];
  }

  .ops-dot-resolved {
    @apply h-2 w-2 bg-emerald-500 rounded-full shadow-[0_0_8px_rgba(16,185,129,0.5)];
  }

  .ops-dot-acknowledged {
    @apply h-2 w-2 bg-violet-400 rounded-full shadow-[0_0_8px_rgba(167,139,250,0.5)];
  }

  /* Critical pulse animation */
  .ops-pulse-critical {
    animation: pulse-critical 2s ease-in-out infinite;
  }

  @keyframes pulse-critical {
    0%, 100% { box-shadow: 0 0 8px rgba(239, 68, 68, 0.6); }
    50% { box-shadow: 0 0 16px rgba(239, 68, 68, 0.8); }
  }

  /* Staggered fade-in animation */
  .ops-fade-in {
    animation: opsFadeIn 0.3s ease-out forwards;
  }

  @keyframes opsFadeIn {
    from { opacity: 0; transform: translateY(4px); }
    to { opacity: 1; transform: translateY(0); }
  }

  /* Table row hover */
  .ops-row-hover {
    @apply transition-colors duration-100 hover:bg-[#334155]/50;
  }

  /* Text colors for ops theme */
  .ops-text {
    @apply text-slate-100;
  }

  .ops-text-muted {
    @apply text-slate-400;
  }
```

**Step 4: Rebuild Tailwind CSS**

Run: `npx tailwindcss -i internal/webui/static/css/input.css -o internal/webui/static/css/output.css`
Expected: No errors, output.css updated

---

## Task 3: Update NewDashboard Main Container

**Files:**
- Modify: `internal/webui/templates/pages/NewDashboard.templ:14`

**Step 1: Replace main container classes**

Change line 14 from:
```html
<div class="min-h-screen bg-gray-50 dark:bg-dark-bg-primary" x-data="newDashboard()" x-init="init()">
```

To:
```html
<div class="min-h-screen ops-grid-bg" x-data="newDashboard()" x-init="init()">
```

---

## Task 4: Update Header Styling

**Files:**
- Modify: `internal/webui/templates/pages/NewDashboard.templ:16`

**Step 1: Replace header classes**

Change line 16 from:
```html
<header class="bg-white dark:bg-dark-bg-secondary shadow-sm border-b border-gray-200 dark:border-dark-border-subtle">
```

To:
```html
<header class="ops-header">
```

---

## Task 5: Update Logo and Title

**Files:**
- Modify: `internal/webui/templates/pages/NewDashboard.templ:22-31`

**Step 1: Update logo gradient to cyan**

Change line 22 from:
```html
<div class="h-8 w-8 bg-gradient-to-r from-blue-500 to-purple-600 rounded-lg flex items-center justify-center">
```

To:
```html
<div class="h-8 w-8 bg-gradient-to-r from-cyan-500 to-cyan-400 rounded flex items-center justify-center shadow-[0_0_12px_rgba(34,211,238,0.3)]">
```

**Step 2: Update title text colors**

Change line 29 from:
```html
<h1 class="text-xl font-semibold text-gray-900 dark:text-white">Alert Dashboard</h1>
```

To:
```html
<h1 class="text-xl font-semibold text-slate-100">Alert Dashboard</h1>
```

Change line 30 from:
```html
<p class="text-sm text-gray-500 dark:text-gray-400" x-text="getStatusText()"></p>
```

To:
```html
<p class="text-sm text-slate-400 font-mono" x-text="getStatusText()"></p>
```

---

## Task 6: Update Display Mode Selector

**Files:**
- Modify: `internal/webui/templates/pages/NewDashboard.templ:37-58`

**Step 1: Update selector container**

Change line 37 from:
```html
<div class="hidden md:flex items-center space-x-1 bg-gray-100 dark:bg-dark-bg-tertiary rounded-lg p-1">
```

To:
```html
<div class="hidden md:flex items-center space-x-1 bg-[#334155] rounded p-1">
```

**Step 2: Update button classes (lines 38-57)**

For each button, update the `:class` binding. Example for "Classic" button (lines 38-42):

Change from:
```html
<button @click="setDisplayMode('classic')"
        :class="displayMode === 'classic' ? 'bg-white dark:bg-dark-bg-secondary shadow text-gray-900 dark:text-white' : 'text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white'"
        class="px-3 py-1 text-sm font-medium rounded-md transition-colors">
```

To:
```html
<button @click="setDisplayMode('classic')"
        :class="displayMode === 'classic' ? 'bg-cyan-500 text-slate-900 shadow-[0_0_8px_rgba(34,211,238,0.3)]' : 'text-slate-300 hover:text-slate-100'"
        class="px-3 py-1 text-sm font-medium rounded transition-colors">
```

Apply same pattern to Resolved, Acknowledged, and Hidden buttons.

---

## Task 7: Update Stats Bar Cards

**Files:**
- Modify: `internal/webui/templates/pages/NewDashboard.templ:231-357`

**Step 1: Update stat card containers**

For each stat card, change from:
```html
<div class="bg-white dark:bg-dark-bg-secondary overflow-hidden shadow rounded-lg">
```

To:
```html
<div class="ops-card ops-card-hover ops-fade-in" style="animation-delay: 0ms">
```

Increment animation-delay by 50ms for each card (0ms, 50ms, 100ms, 150ms, 200ms, 250ms, 300ms).

**Step 2: Update stat numbers to use mono font**

For each stat `<dd>` element, add `font-mono` class. Example:

Change from:
```html
<dd class="text-lg font-semibold text-gray-900 dark:text-white" x-text="metadata.counters.critical">0</dd>
```

To:
```html
<dd class="text-lg font-semibold font-mono text-slate-100" x-text="metadata.counters.critical">0</dd>
```

**Step 3: Update stat labels**

Change from:
```html
<dt class="text-xs font-medium text-gray-500 dark:text-gray-400 truncate">Critical</dt>
```

To:
```html
<dt class="text-xs font-medium text-slate-400 truncate">Critical</dt>
```

**Step 4: Update status dots to ops-dot classes**

Change from:
```html
<div class="h-2 w-2 bg-red-500 rounded-full"></div>
```

To:
```html
<div class="ops-dot-critical ops-pulse-critical"></div>
```

Apply corresponding ops-dot class for each severity:
- Critical: `ops-dot-critical ops-pulse-critical`
- Warning: `ops-dot-warning`
- Info: `ops-dot-info`
- Resolved: `ops-dot-resolved`
- Acknowledged: `ops-dot-acknowledged`
- With Comments: `ops-dot-info` (use cyan)
- Total: Use `bg-slate-500 rounded-full h-2 w-2`

---

## Task 8: Update Filter Section

**Files:**
- Modify: `internal/webui/templates/pages/NewDashboard.templ:360-784`

**Step 1: Update filter container card**

Change from:
```html
<div class="bg-white dark:bg-dark-bg-secondary shadow rounded-lg mb-6">
```

To:
```html
<div class="ops-card mb-6">
```

**Step 2: Update search input**

Change the input field styling from:
```html
class="block w-full pl-10 pr-3 py-2 border border-gray-300 dark:border-dark-border-DEFAULT rounded-md leading-5 bg-white dark:bg-dark-bg-tertiary text-gray-900 dark:text-white placeholder-gray-500 dark:placeholder-gray-400 focus:outline-none focus:placeholder-gray-400 focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
```

To:
```html
class="block w-full pl-10 pr-3 py-2 border border-[#334155] rounded leading-5 bg-[#0c1222] text-slate-100 placeholder-slate-500 focus:outline-none focus:ring-1 focus:ring-cyan-500 focus:border-cyan-500 font-mono"
```

**Step 3: Update filter dropdown buttons**

Change button styling from:
```html
class="inline-flex items-center px-4 py-2 border border-gray-300 dark:border-dark-border-DEFAULT rounded-md shadow-sm bg-white dark:bg-dark-bg-tertiary text-sm font-medium text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-dark-bg-secondary focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 relative"
```

To:
```html
class="inline-flex items-center px-4 py-2 border border-[#334155] rounded shadow-sm bg-[#1e293b] text-sm font-medium text-slate-200 hover:bg-[#334155] hover:border-[#475569] focus:outline-none focus:ring-2 focus:ring-cyan-500 transition-all relative"
```

---

## Task 9: Update Alerts Content Card

**Files:**
- Modify: `internal/webui/templates/pages/NewDashboard.templ:787`

**Step 1: Update main alerts container**

Change from:
```html
<div x-show="displayMode !== 'resolved'" class="bg-white dark:bg-dark-bg-secondary shadow overflow-hidden sm:rounded-lg">
```

To:
```html
<div x-show="displayMode !== 'resolved'" class="ops-card overflow-hidden">
```

---

## Task 10: Rebuild and Verify

**Step 1: Rebuild Tailwind CSS**

Run: `npx tailwindcss -i internal/webui/static/css/input.css -o internal/webui/static/css/output.css`

**Step 2: Regenerate templ files**

Run: `templ generate`

**Step 3: Start server and verify visually**

Run: `go run ./cmd/server`

Open browser to dashboard and verify:
- [ ] Deep slate background with grid pattern visible
- [ ] JetBrains Mono on numbers/data
- [ ] IBM Plex Sans on labels/buttons
- [ ] Cyan accent color on active elements
- [ ] Glowing status dots (especially critical pulse)
- [ ] Staggered fade-in on stats cards
- [ ] Sharper corners throughout

---

## Summary

| Task | Description |
|------|-------------|
| 1 | Add Google Fonts to Base.templ |
| 2 | Add ops theme CSS classes to input.css |
| 3 | Update main container to ops-grid-bg |
| 4 | Update header styling |
| 5 | Update logo and title |
| 6 | Update display mode selector |
| 7 | Update stats bar cards |
| 8 | Update filter section |
| 9 | Update alerts content card |
| 10 | Rebuild and verify |
