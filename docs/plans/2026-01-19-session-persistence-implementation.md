# Session Persistence & Disconnection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend session lifetime to 7 days and ensure full disconnection when session expires.

**Architecture:** Change backend session expiry from 24h to 7 days, require auth for all data endpoints, add global fetch interceptor to catch 401s and redirect.

**Tech Stack:** Go (Gin, gRPC), JavaScript (Alpine.js), Templ

---

## Task 1: Extend Backend Session to 7 Days

**Files:**
- Modify: `internal/backend/services/services.go:125`
- Modify: `internal/backend/services/services.go:1446`

**Step 1: Change regular login session expiry**

In `services.go` at line 125, change:

```go
// Before
expiresAt := time.Now().Add(24 * time.Hour)

// After
expiresAt := time.Now().Add(7 * 24 * time.Hour)
```

**Step 2: Change OAuth login session expiry**

In `services.go` at line 1446, change:

```go
// Before
expiresAt := time.Now().Add(24 * time.Hour)

// After
expiresAt := time.Now().Add(7 * 24 * time.Hour)
```

**Step 3: Build to verify no syntax errors**

Run: `go build ./...`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add internal/backend/services/services.go
git commit -m "feat: extend session lifetime from 24h to 7 days"
```

---

## Task 2: Require Authentication for Alert Endpoints

**Files:**
- Modify: `internal/webui/router.go:218`
- Modify: `internal/webui/router.go:226`

**Step 1: Change alerts group from OptionalAuth to RequireAuth**

In `router.go` at line 218, change:

```go
// Before
alerts.Use(authMiddleware.OptionalAuth()) // Optional auth for now

// After
alerts.Use(authMiddleware.RequireAuth())
```

**Step 2: Change dashboard group from OptionalAuth to RequireAuth**

In `router.go` at line 226, change:

```go
// Before
dashboard.Use(authMiddleware.OptionalAuth()) // Optional auth for now

// After
dashboard.Use(authMiddleware.RequireAuth())
```

**Step 3: Build to verify no syntax errors**

Run: `go build ./...`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add internal/webui/router.go
git commit -m "feat: require authentication for alert and dashboard endpoints"
```

---

## Task 3: Add Global Fetch Interceptor for 401 Handling

**Files:**
- Modify: `internal/webui/templates/scripts/dashboard_core.templ`

**Step 1: Add global fetch interceptor in init()**

In `dashboard_core.templ`, add this at the beginning of the `init()` function (after line 188):

```javascript
async init() {
    // Install global fetch interceptor for auth errors
    this.installFetchInterceptor();

    Object.assign(this, window.dashboardDataMixin || {});
    // ... rest of init
```

**Step 2: Add the installFetchInterceptor method**

Add this new method to the dashboard object (after the `handleAuthError` method around line 165):

```javascript
// Install global fetch interceptor to handle auth errors consistently
installFetchInterceptor() {
    const originalFetch = window.fetch;
    const dashboard = this;

    window.fetch = async function(...args) {
        try {
            const response = await originalFetch.apply(this, args);

            // Check for auth errors on any API call
            if (response.status === 401) {
                console.log('Session expired, redirecting to login');
                dashboard.stopAutoRefresh();
                window.location.href = '/login';
                // Return a never-resolving promise to prevent further processing
                return new Promise(() => {});
            }

            return response;
        } catch (error) {
            // Network errors - let them propagate
            throw error;
        }
    };
},
```

**Step 3: Generate templ files**

Run: `templ generate`
Expected: Generates Go files successfully

**Step 4: Build to verify**

Run: `go build ./...`
Expected: Build succeeds

**Step 5: Commit**

```bash
git add internal/webui/templates/scripts/dashboard_core.templ
git add internal/webui/templates/scripts/dashboard_core_templ.go
git commit -m "feat: add global fetch interceptor for consistent 401 handling"
```

---

## Task 4: Manual Testing

**Step 1: Start the application**

Run backend and webui services.

**Step 2: Test login persistence**

1. Log in with OAuth
2. Verify session works
3. Check browser dev tools - cookie should have 7-day expiry

**Step 3: Test disconnection handling**

1. Log in normally
2. In a separate tab, call the logout endpoint or manually delete the session from DB
3. Return to dashboard tab
4. Verify: No alerts shown, immediate redirect to login

**Step 4: Test unauthenticated access**

1. Open incognito window
2. Go to `/dashboard` directly
3. Verify: Redirected to login (no alert data visible)

---

## Summary of Changes

| File | Line(s) | Change |
|------|---------|--------|
| `services.go` | 125 | `24 * time.Hour` → `7 * 24 * time.Hour` |
| `services.go` | 1446 | `24 * time.Hour` → `7 * 24 * time.Hour` |
| `router.go` | 218 | `OptionalAuth()` → `RequireAuth()` |
| `router.go` | 226 | `OptionalAuth()` → `RequireAuth()` |
| `dashboard_core.templ` | init() | Add `installFetchInterceptor()` call |
| `dashboard_core.templ` | new method | Add `installFetchInterceptor()` method |
