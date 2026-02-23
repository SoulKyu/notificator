# Session Persistence & Disconnection Handling

## Problem

1. OAuth users get disconnected every day because backend sessions expire in 24 hours while frontend cookies last 7 days
2. After disconnection, alerts keep flowing because data endpoints use `OptionalAuth`
3. Users only discover they're disconnected when trying to take an action

## Solution

### 1. Extend Backend Session to 7 Days

**File:** `internal/backend/services/services.go`

Change session expiration from 24 hours to 7 days in two locations (~lines 125 and 1446):

```go
// Before
expiresAt := time.Now().Add(24 * time.Hour)

// After
expiresAt := time.Now().Add(7 * 24 * time.Hour)
```

This aligns backend session lifetime with the frontend cookie.

### 2. Require Authentication for Alert Endpoints

**File:** `internal/webui/router.go`

Change alert and dashboard data endpoints from `OptionalAuth` to `RequireAuth`:

```go
// Before
alertGroup.Use(am.OptionalAuth())

// After
alertGroup.Use(am.RequireAuth())
```

Endpoints to change:
- `/api/v1/alerts` and related alert endpoints
- `/api/v1/dashboard/*` data endpoints
- Any SSE/streaming endpoints

### 3. Consistent Frontend 401 Handling

**File:** `internal/webui/templates/scripts/dashboard_core.templ`

Wrap all fetch calls to catch 401 and redirect:

```javascript
async fetchWithAuth(url, options = {}) {
    const response = await fetch(url, {
        ...options,
        credentials: 'include'
    });

    if (response.status === 401) {
        this.stopAllPolling();
        window.location.href = '/login';
        return null;
    }

    return response;
}
```

All dashboard data fetches should use this wrapper.

## Behavior After Implementation

| Scenario | Before | After |
|----------|--------|-------|
| Session expires | Alerts keep flowing, user unaware | Immediate redirect to login |
| User returns after 3 days | Disconnected, confusing UX | Still logged in (within 7 days) |
| 401 on any endpoint | Inconsistent handling | Always redirect to /login |

## Edge Cases

- **Multiple tabs:** All tabs redirect on their next API call
- **Polling/auto-refresh:** Wrapper handles 401, stops polling, redirects
- **SSE connections:** Need auth validation, close on session expiry

## Post-Login Behavior

Users always land on dashboard home after re-login (no URL preservation).
