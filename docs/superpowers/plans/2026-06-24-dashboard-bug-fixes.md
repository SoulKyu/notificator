# Dashboard Bug Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`).

**Goal:** Fix the 15 CONFIRMED bugs on the /dashboard page (verified against code; 3 agent findings were rejected as false positives and are excluded).

**Architecture:** Go backend (Gin handlers + in-memory `AlertCache`/`HiddenAlertsService`/`ColorService` with `sync.RWMutex`) + templ/Alpine.js frontend. Bugs cluster by file, so tasks are file-grouped (edits to one file cannot be parallelized). Concurrency fixes mirror an existing correct pattern in the same file rather than re-architecting the lock model.

**Tech Stack:** Go, Gin, gRPC client, templ, Alpine.js.

## Global Constraints

- `go build ./...` must pass after every backend task; `go test -race ./internal/webui/services/` must still pass (it's a regression guard — the existing tests do NOT reproduce these races, so passing is necessary, not sufficient; the real gate is review of the locking logic).
- `.templ` edits: run `make webui-templates` then `go build ./...`. Never hand-edit `*_templ.go`. Stage source + generated. NOTE: `make webui-templates` also regenerates `internal/webui/static/css/output.css` — do NOT stage `output.css` in fix commits (generated artifact, already dirty).
- Commits: Conventional Commits (`fix:`), NO `Co-Authored-By` trailer. One atomic commit per task.
- EXCLUDED as false positives (do NOT touch): C6 (session assertion — `RequireAuth` always sets `session_id`), H2 (`.AsTime()` is nil-safe via proto getters), H5 (`applyFilters` — no `.finally` caller exists).
- Do not re-architect the cache locking model. Apply the targeted, low-risk fix described per bug.

---

### Task 1: Backend services — AlertCache + ColorService concurrency/correctness

**Files:**
- Modify: `internal/webui/services/alert_cache.go` (C1-targeted, C2, H3)
- Modify: `internal/webui/services/color_service.go` (M3)

**Interfaces:** no public signature changes; internal locking/correctness only.

- [ ] **Step 1: Fix C2 — move the gRPC call OUT of the write-lock in `loadCommentCountsEfficiently`**

Around `alert_cache.go:427-446`, the function acquires `ac.mu.Lock()` then calls `ac.backendClient.GetCommentCountsBatch(fingerprints)` while holding it. READ `loadAcknowledgmentsEfficiently` (the correct sibling, ~lines 397-424) and mirror its structure exactly: (1) acquire `RLock`, collect `fingerprints`, release; (2) call `GetCommentCountsBatch` with NO lock held; (3) acquire `Lock` only to write the counts back onto the alerts. Keep behavior identical otherwise.

- [ ] **Step 2: Fix H3 — protect `refreshTicker` access**

`SetRefreshInterval` (~112-121) writes `ac.refreshTicker` under `ac.mu.Lock()`; `backgroundRefresh` (~123-131) reads `ac.refreshTicker.C` with no lock → data race on the pointer. Minimal fix: in `backgroundRefresh`, read the ticker channel under a short `ac.mu.RLock()`/`RUnlock()` into a local before the `select`, OR guard `refreshTicker` with its own dedicated `sync.Mutex`. Do NOT hold `ac.mu` across the `select`/refresh. Choose the smallest change that removes the race; explain the choice in the report.

- [ ] **Step 3: Fix C1 (targeted leg) — copy the alert struct before handing it to the background goroutine**

In `refreshAlerts` (~192-217), a `*DashboardAlert` is mutated under the lock then passed to `go ac.storeResolvedAlertInBackend(alert)` (and/or an inline goroutine) which reads/`json.Marshal`s it after the lock is released, racing future writers. Fix: before spawning the goroutine, take a value copy under the lock and pass its address:
```go
alertCopy := *alert
ac.mu.Unlock()            // keep existing unlock ordering
go ac.storeResolvedAlertInBackend(&alertCopy)
```
Adjust to the real code's unlock ordering — the key invariant: the goroutine must operate on a COPY, never the cache-resident pointer. Do NOT change `GetAllAlerts` to deep-copy (callers like `FilterHiddenAlerts` rely on mutating the live pointer to persist `IsHidden`; changing that would break hidden-alert behavior — leave it, note it as a known broader issue in the report).

- [ ] **Step 4: Fix M3 — deterministic key ordering in `buildLookupKey`**

`color_service.go:200-212` does `json.Marshal(map[string]string)` whose key order is non-deterministic, so the exact-match color cache rarely hits. Fix: build the key from SORTED keys. Collect keys, `sort.Strings`, then build a canonical string (e.g. join `k=v` pairs with a separator) instead of `json.Marshal`. Apply the SAME canonicalization everywhere the lookup key is built (both the `buildLookupMap` insert side ~183 and the `findColorMatch` query side ~215) so they match. Add `sort` to imports if needed.

- [ ] **Step 5: Build + race test**

Run: `go build ./... && go test -race ./internal/webui/services/`
Expected: build success, tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/webui/services/alert_cache.go internal/webui/services/color_service.go
git commit -m "fix(dashboard): move comment-count gRPC out of cache lock, guard ticker race, copy alert for bg goroutine, deterministic color key"
```

---

### Task 2: HiddenAlertsService concurrency

**Files:**
- Modify: `internal/webui/services/hidden_alerts_service.go` (C3, C4)

- [ ] **Step 1: Fix C3 — synchronize the map read in `IsAlertHidden`**

At ~107-115, `s.userHiddenAlerts[sessionID]` is read BEFORE `s.mu.RLock()`. Move the not-loaded check inside a lock:
```go
s.mu.RLock()
loaded := s.userHiddenAlerts[sessionID] != nil
s.mu.RUnlock()
if !loaded {
    _ = s.LoadUserData(sessionID)
}
s.mu.RLock()
defer s.mu.RUnlock()
// ... existing hidden-check logic ...
```
Ensure no map access remains outside a lock.

- [ ] **Step 2: Fix C4 — move gRPC calls OUT of the write-lock in `LoadUserData`**

At ~52-99, `s.mu.Lock()` (deferred) is held across `GetUserHiddenAlerts` and `GetUserHiddenRules` gRPC calls. Restructure: make the two backend calls FIRST with no lock held, then acquire `s.mu.Lock()` only to write the results into `s.userHiddenAlerts`/the rules map. Preserve existing error handling and return values. Watch for re-entrancy: `LoadUserData` must not call another method that takes `s.mu` while holding it.

- [ ] **Step 3: Build + race test**

Run: `go build ./... && go test -race ./internal/webui/services/`
Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/webui/services/hidden_alerts_service.go
git commit -m "fix(dashboard): synchronize hidden-alerts map read and move user-data gRPC out of lock"
```

---

### Task 3: Dashboard handlers — safety & correctness

**Files:**
- Modify: `internal/webui/handlers/dashboard_handlers.go` (C5, C7, H1, M1, H11)

- [ ] **Step 1: Fix C5 — guard the package-level `userSettings` map with a mutex**

`userSettings` (~line 28) is read (~308) and written (~326, ~971) with no lock. Add a package-level `var userSettingsMu sync.RWMutex` next to it. Wrap every read with `RLock`/`RUnlock` and every write with `Lock`/`Unlock`. Confirm `sync` is imported. Keep critical sections small (don't hold the lock across I/O).

- [ ] **Step 2: Fix C7 — nil-check `alertCache` in the 3 unguarded handlers**

In `GetDashboardData` (~135), `GetAlertColors` (~1683), `GetAvailableFields` (~2369), add at the top (mirror the existing guard in `GetDashboardIncremental` ~1071):
```go
if alertCache == nil {
    c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Dashboard cache not ready"))
    return
}
```

- [ ] **Step 3: Fix H1 — validate `RefreshInterval` before `SetRefreshInterval`**

In `SaveDashboardSettings` (~968), before the call, clamp:
```go
if settings.RefreshInterval < 1 {
    settings.RefreshInterval = 1 // seconds; avoid time.NewTicker(0) panic
}
```
(Pick a sane minimum; 1s is fine. Also consider an upper bound only if trivial — otherwise just the lower bound.)

- [ ] **Step 4: Fix M1 — `processGroupAction` should continue on error, not stop at the first**

At ~944-951, the loop returns on the first `processAlertAction` error, partially applying the action. Change it to accumulate failures and continue (mirror the per-fingerprint loop at ~793-800 which already collects errors). Return an aggregate so the caller's `FailedCount` reflects the true number of failures.

- [ ] **Step 5: Fix H11 — ownership check in `DeleteAlertComment` (best-effort)**

At ~1426-1474, before calling `backendClient.DeleteComment`, fetch the alert's comments (the handler already has the fingerprint; use the same `GetComments`/details path used by `GetAlertDetails`), find the comment by `commentID`, and verify its `UserId`/owner matches the current user (`getCurrentUserID(c)` or the established helper). If it doesn't match, return `403 Forbidden`. IF the comment list does not expose an owner/userID field (check the proto `Comment` message first), this cannot be enforced at the handler — in that case do NOT fake it: leave the code as-is and report H11 as "requires a backend authz change (Comment has no owner field exposed)" with the evidence. Do not invent an owner field.

- [ ] **Step 6: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 7: Commit**

```bash
git add internal/webui/handlers/dashboard_handlers.go
git commit -m "fix(dashboard): guard userSettings map, nil-check alertCache, validate refresh interval, continue bulk on error, comment delete authz"
```

---

### Task 4: Frontend dashboard bug fixes

**Files:**
- Modify: `internal/webui/templates/scripts/dashboard_modal.templ` (C8)
- Modify: `internal/webui/templates/scripts/dashboard_settings.templ` (C9 — implement missing methods)
- Modify: `internal/webui/templates/scripts/dashboard_data.templ` (H6)
- Modify: `internal/webui/templates/scripts/dashboard_utilities.templ` (H7, H10, M7)
- Modify: `internal/webui/templates/scripts/dashboard_filter_presets.templ` (H10)
- Modify: `internal/webui/templates/scripts/dashboard_actions.templ` (M8)
- Regenerate: the corresponding `*_templ.go`

**Interfaces:** confirm real Alpine field/method names by grepping before each edit; line numbers may drift.

- [ ] **Step 1: Fix C8 — correct the unsilence JSON field**

In `dashboard_modal.templ` (~99), `processUnsilenceAction` POSTs `{ fingerprints: [...] }` but the backend `BulkActionRequest` expects `alertFingerprints` (json tag, `models/dashboard.go:194`). Change the key to `alertFingerprints` (match the other callers, e.g. `dashboard_actions.templ`).

- [ ] **Step 2: Fix C9 — implement the 3 missing Alpine methods**

`modal_components.templ` calls `removeLabelCondition(preference, key)`, `clearAllHiddenAlerts()`, `unhideSpecificAlert(fingerprint)` which are not defined in `dashboard_settings.templ` (only `unhideAlert` exists). FIRST grep to confirm (`rg -n "removeLabelCondition|clearAllHiddenAlerts|unhideSpecificAlert|unhideAlert\b" internal/webui/templates`). Then in `dashboard_settings.templ`'s `settingsModalData()` add:
- `unhideSpecificAlert(fingerprint)` — alias to / same body as existing `unhideAlert(fingerprint)`.
- `removeLabelCondition(preference, key)` — `delete preference.labelConditions[key]` (match the real field name used in the color-preference object; grep `labelConditions`/`label_conditions`) then persist if the existing edit flow persists (mirror how other condition edits save).
- `clearAllHiddenAlerts()` — iterate the hidden alerts and call the existing unhide path for each (reuse `unhideAlert`), or call a bulk endpoint if one exists (grep routes for a clear-all; if none, loop). Confirm the data source field name for the hidden list first.
If any of these already exists under a different name reachable in this Alpine scope, alias instead of duplicating.

- [ ] **Step 3: Fix H6 — null-guard sort on `status`/`instance`**

In `dashboard_data.templ` (~393-398), `sortAlerts` does `a.status.toLowerCase()` / `a.instance.toLowerCase()`. `status` may be an object `{state,...}` and `instance` may be null. Replace with:
```js
case 'status':
    aVal = ((typeof a.status === 'object' ? a.status?.state : a.status) || '').toLowerCase();
    bVal = ((typeof b.status === 'object' ? b.status?.state : b.status) || '').toLowerCase();
    break;
case 'instance':
    aVal = (a.instance || '').toLowerCase();
    bVal = (b.instance || '').toLowerCase();
    break;
```
(Adapt to the real switch/case structure.)

- [ ] **Step 4: Fix H7 — stop interpolating `fingerprint` into onclick strings**

In `dashboard_utilities.templ` (~849, 866, 876), the `x-html` row renderer builds `onclick="...('${alert.fingerprint}')"`. Defensive fix matching the project's XSS-hardening: escape the fingerprint before interpolation using the existing `escapeHtml` helper (grep for it in `dashboard_utilities.templ`), OR switch to `data-fingerprint="..."` + a delegated listener. Minimal acceptable fix: wrap each interpolated `alert.fingerprint` in the existing escape helper. Apply to all three sites.

- [ ] **Step 5: Fix H10 — use `null` (not `'all'`) as the no-filter sentinel**

`dashboard_filter_presets.templ` (~175) sets `acknowledgmentFilter = data.acknowledged || 'all'` and `commentsFilter = data.comments || 'all'`. The rest of the code treats `null`/falsy as "no filter" but `updateURL` (`dashboard_utilities.templ` ~19) emits any truthy value and `hasActiveFilters` (~117-125) tests `!== null`. Make `'all'` the no-filter sentinel consistently OR normalize to `null`. Simplest: in the preset, use `data.acknowledged || null` / `data.comments || null`; and where the value is read from URL/preset, map the literal `'all'` to `null`. Verify `updateURL`/`hasActiveFilters`/`clearAllFilters` all agree on the sentinel afterward.

- [ ] **Step 6: Fix M7 — `initializeColumns` reads the wrong field name**

In `dashboard_utilities.templ` (~617), `this.filterPresets` is undefined (the mixin defines `this.presets`). Change `this.filterPresets` → `this.presets` (grep to confirm the real field is `presets`).

- [ ] **Step 7: Fix M8 — guard `currentAckAlert`/`currentSilenceAlert` undefined**

In `dashboard_actions.templ` (~206-211 ack, ~621-631 silence), after `const alert = this.alerts.find(...)`, add `if (!alert) { return; }` before assigning/opening the modal. (Optional: also guard the submit at ~31 — but the early return prevents the modal from opening with no alert.)

- [ ] **Step 8: Regenerate + build**

Run: `make webui-templates && go build ./...`
Expected: success. Stage the edited `.templ` files and their regenerated `*_templ.go` (NOT `output.css`).

- [ ] **Step 9: Commit**

```bash
git add internal/webui/templates/scripts/dashboard_modal.templ internal/webui/templates/scripts/dashboard_settings.templ internal/webui/templates/scripts/dashboard_data.templ internal/webui/templates/scripts/dashboard_utilities.templ internal/webui/templates/scripts/dashboard_filter_presets.templ internal/webui/templates/scripts/dashboard_actions.templ internal/webui/templates/scripts/*_templ.go internal/webui/templates/components/*_templ.go
git commit -m "fix(dashboard): correct unsilence field, add missing settings methods, null-safe sort, escape fingerprint, fix filter sentinel, column preset field, guard modal alert"
```

---

## Self-Review

- **Coverage:** C1(targeted)/C2/H3/M3 → Task 1; C3/C4 → Task 2; C5/C7/H1/M1/H11 → Task 3; C8/C9/H6/H7/H10/M7/M8 → Task 4.
- **Excluded (false positives):** C6, H2, H5. **Deferred/flagged, not blindly fixed:** C1's broader "GetAllAlerts returns live pointers mutated by callers" architectural issue (note in Task 1 report); H11 if `Comment` has no owner field (note in Task 3 report).
- **Sequencing:** Tasks 1-3 touch disjoint backend files (could parallelize, but run sequentially per the skill). Task 4 touches multiple templ files + one templ regen — single task, runs last.
- **Verification:** every backend task runs `go build` + `go test -race ./internal/webui/services/`; frontend runs `make webui-templates` + `go build`.

## Verification (manual, after all tasks)

- `go build ./...`, `go test -race ./internal/webui/services/` pass.
- Optional smoke: run the app, open /dashboard, exercise unsilence from the modal (C8), the settings hidden-alerts buttons (C9), sort by status (H6).
