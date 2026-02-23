# Dashboard Terminal-Inspired Design

## Overview

Redesign NewDashboard.templ with a Terminal-Inspired, Technical/Operations aesthetic. High-density, utilitarian, data-focused - similar to Grafana or Prometheus.

## Typography

**Fonts:**
- **JetBrains Mono** - Data, numbers, alert names, metrics, timestamps
- **IBM Plex Sans** - UI labels, buttons, navigation, headers

**Font Loading (add to Base.templ `<head>`):**
```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
```

**Application:**
| Element | Font |
|---------|------|
| Alert names, severity labels | JetBrains Mono |
| Stats bar numbers, counts | JetBrains Mono |
| Timestamps, durations | JetBrains Mono |
| Table data cells | JetBrains Mono |
| Headers, navigation | IBM Plex Sans |
| Buttons, labels | IBM Plex Sans |
| Filter dropdowns | IBM Plex Sans |

---

## Color Palette

**Dark-only theme** (commit to ops aesthetic).

**Base Palette:**
| Role | Hex | Tailwind |
|------|-----|----------|
| Background | `#0c1222` | `bg-[#0c1222]` |
| Surface/Panel | `#1e293b` | `bg-[#1e293b]` |
| Border | `#334155` | `border-[#334155]` |
| Border hover | `#475569` | `border-[#475569]` |
| Primary accent | `#22d3ee` | `cyan-400` |
| Text primary | `#f1f5f9` | `text-slate-100` |
| Text muted | `#94a3b8` | `text-slate-400` |

**Status Colors:**
| Status | Hex | Class |
|--------|-----|-------|
| Critical | `#ef4444` | `bg-red-500` |
| Warning | `#f59e0b` | `bg-amber-500` |
| Info | `#22d3ee` | `bg-cyan-400` |
| Resolved | `#10b981` | `bg-emerald-500` |
| Acknowledged | `#a78bfa` | `bg-violet-400` |

---

## Background & Texture

**Grid pattern for technical feel:**
```css
.ops-grid-bg {
  background-color: #0c1222;
  background-image:
    linear-gradient(rgba(51, 65, 85, 0.3) 1px, transparent 1px),
    linear-gradient(90deg, rgba(51, 65, 85, 0.3) 1px, transparent 1px);
  background-size: 24px 24px;
}
```

**Optional gradient fade at top:**
```css
.ops-grid-bg::before {
  content: '';
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 200px;
  background: linear-gradient(to bottom, #0c1222, transparent);
  pointer-events: none;
  z-index: 1;
}
```

---

## Component Refinements

### Corners
Replace `rounded-lg` with `rounded` or `rounded-sm` for precision.

### Cards/Panels
```html
<!-- Before -->
<div class="bg-white dark:bg-dark-bg-secondary overflow-hidden shadow rounded-lg">

<!-- After -->
<div class="bg-[#1e293b] border border-[#334155] rounded shadow-lg shadow-black/20">
```

### Status Dots with Glow
```html
<!-- Critical -->
<div class="h-2 w-2 bg-red-500 rounded-full shadow-[0_0_8px_rgba(239,68,68,0.6)]"></div>

<!-- Warning -->
<div class="h-2 w-2 bg-amber-500 rounded-full shadow-[0_0_8px_rgba(245,158,11,0.5)]"></div>

<!-- Info -->
<div class="h-2 w-2 bg-cyan-400 rounded-full shadow-[0_0_8px_rgba(34,211,238,0.5)]"></div>

<!-- Resolved -->
<div class="h-2 w-2 bg-emerald-500 rounded-full shadow-[0_0_8px_rgba(16,185,129,0.5)]"></div>
```

### Primary Buttons
```html
<!-- Before -->
<button class="bg-blue-600 hover:bg-blue-700 text-white rounded-lg">

<!-- After -->
<button class="bg-cyan-500 hover:bg-cyan-400 text-slate-900 font-medium rounded transition-all hover:shadow-[0_0_12px_rgba(34,211,238,0.4)]">
```

### Stats Bar Numbers
```html
<dd class="text-lg font-semibold font-mono text-slate-100" x-text="...">
```

---

## Micro-interactions & Animations

### Card Hover
```html
<div class="... transition-all duration-150 hover:border-[#475569] hover:translate-y-[-1px]">
```

### Critical Alert Pulse
```css
@keyframes pulse-critical {
  0%, 100% { box-shadow: 0 0 8px rgba(239, 68, 68, 0.6); }
  50% { box-shadow: 0 0 16px rgba(239, 68, 68, 0.8); }
}

.alert-critical {
  animation: pulse-critical 2s ease-in-out infinite;
}
```

### Stats Bar Staggered Fade-in
```html
<div class="... opacity-0 animate-[fadeIn_0.3s_ease-out_forwards]" style="animation-delay: 0ms">
<div class="... opacity-0 animate-[fadeIn_0.3s_ease-out_forwards]" style="animation-delay: 50ms">
<div class="... opacity-0 animate-[fadeIn_0.3s_ease-out_forwards]" style="animation-delay: 100ms">
```

```css
@keyframes fadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}
```

### Table Row Hover
```html
<tr class="transition-colors duration-100 hover:bg-[#334155]/50">
```

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/webui/templates/layouts/Base.templ` | Add Google Fonts link, add CSS for grid background and animations |
| `internal/webui/templates/pages/NewDashboard.templ` | Apply new classes throughout |
| `internal/webui/static/css/input.css` | Add custom CSS classes if using Tailwind |
| `tailwind.config.js` | Extend with font families (optional) |

---

## Summary of Key Changes

1. **Typography**: JetBrains Mono for data, IBM Plex Sans for UI
2. **Colors**: Deep slate background (#0c1222), cyan accents, dark-only
3. **Texture**: Subtle 24px grid pattern
4. **Components**: Sharper corners, glowing status dots, defined borders
5. **Motion**: Fast transitions (150ms), critical pulse, staggered fade-in
