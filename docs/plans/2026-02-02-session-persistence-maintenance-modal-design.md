# Session Persistence & Maintenance Modal Design

**Date:** 2026-02-02
**Status:** Approved
**Author:** Claude + User

## Problem Statement

When backend/frontend services restart, users are immediately redirected to the SSO login page, even though their session data persists in the PostgreSQL database. This creates unnecessary friction and confusion.

**Root causes identified:**
1. `NOTIFICATOR_SESSION_SECRET` not configured → cookie encryption key changes on restart → cookies become unreadable
2. When backend is temporarily unreachable, frontend middleware redirects to login instead of showing a friendly message

## Solution Overview

Two-part solution:

1. **Configure `NOTIFICATOR_SESSION_SECRET`** → Sessions persist across restarts
2. **Add maintenance modal** → Users see friendly overlay instead of SSO redirect when backend is temporarily down

## Architecture

### Current Flow (Problematic)
```
Backend restarts → Frontend middleware can't reach backend
→ RequireAuth() fails → Redirect to /login → SSO redirect
→ User loses context, must re-authenticate
```

### New Flow
```
Backend restarts → API call fails (timeout/connection error)
→ JavaScript detects backend unreachable
→ Show modal overlay with blur + "Maintenance in progress"
→ Auto-retry every 5 seconds in background
→ Backend returns → Modal auto-closes
→ User continues where they left off (no page reload needed)
```

## Components

### 1. MaintenanceModal Component

**File:** `internal/webui/templates/components/MaintenanceModal.templ`

**Visual Design:**
```
┌─────────────────────────────────────────┐
│          (blurred page behind)          │
│                                         │
│    ┌─────────────────────────────┐      │
│    │      🔧 Maintenance         │      │
│    │                             │      │
│    │   Maintenance in progress   │      │
│    │   Please wait...            │      │
│    │                             │      │
│    │   ○○○ (loading spinner)     │      │
│    │                             │      │
│    │   Reconnecting in 5s...     │      │
│    └─────────────────────────────┘      │
│                                         │
└─────────────────────────────────────────┘
```

**Behavior:**
- Fixed position overlay covering full viewport
- `backdrop-blur-sm` for blur effect
- Centered card with Tailwind styling matching existing UI
- Dark mode support
- Shows only when backend is unreachable
- Auto-hides when backend returns

### 2. Health Check Logic (Alpine.js)

**Endpoint:** `/health/backend` (already exists)

```javascript
function maintenanceMonitor() {
  return {
    backendDown: false,
    countdown: 5,
    intervalId: null,
    countdownId: null,

    init() {
      this.startMonitoring();
    },

    async checkBackend() {
      try {
        const response = await fetch('/health/backend', {
          timeout: 3000
        });
        if (response.ok) {
          this.onBackendRestored();
        } else {
          this.onBackendDown();
        }
      } catch (error) {
        this.onBackendDown();
      }
    },

    onBackendDown() {
      if (!this.backendDown) {
        this.backendDown = true;
        this.startCountdown();
      }
    },

    onBackendRestored() {
      if (this.backendDown) {
        this.backendDown = false;
        this.stopCountdown();
      }
    },

    startMonitoring() {
      this.intervalId = setInterval(() => this.checkBackend(), 5000);
    },

    startCountdown() {
      this.countdown = 5;
      this.countdownId = setInterval(() => {
        this.countdown--;
        if (this.countdown <= 0) this.countdown = 5;
      }, 1000);
    },

    stopCountdown() {
      if (this.countdownId) clearInterval(this.countdownId);
    }
  }
}
```

### 3. Base Layout Integration

**File:** `internal/webui/templates/layouts/Base.templ`

Import and include the MaintenanceModal component at the root level so it's available on all pages.

### 4. SESSION_SECRET Configuration

**Files:** `docker-compose.yml`, `.env`

```yaml
# docker-compose.yml
services:
  webui:
    environment:
      - NOTIFICATOR_SESSION_SECRET=${NOTIFICATOR_SESSION_SECRET}
```

```bash
# .env (generate with: openssl rand -hex 32)
NOTIFICATOR_SESSION_SECRET=<64-character-hex-secret>
```

## Files to Modify

| File | Action | Purpose |
|------|--------|---------|
| `internal/webui/templates/components/MaintenanceModal.templ` | Create | Modal with blur overlay |
| `internal/webui/templates/layouts/Base.templ` | Modify | Import and include modal |
| `docker-compose.yml` | Modify | Add `NOTIFICATOR_SESSION_SECRET` env var |
| `.env` or secrets | Modify | Add actual secret value |

## Verification

After implementation:

1. **Session persistence test:**
   - Login to the app
   - Restart webui service
   - Refresh page → should remain logged in
   - Logs should show: `✅ Using configured session secret from NOTIFICATOR_SESSION_SECRET`

2. **Maintenance modal test:**
   - Login to the app
   - Stop backend service
   - Within 5 seconds, modal should appear with blur effect
   - Start backend service
   - Modal should auto-close, user continues without action

## Design Decisions

1. **Modal over full-page:** User retains visual context of where they were
2. **5-second interval:** Quick enough for fast restarts, not too aggressive on server
3. **No page reload on recovery:** Seamless experience, preserves any unsaved state
4. **Blur effect:** Clearly indicates app is temporarily unavailable while maintaining context
