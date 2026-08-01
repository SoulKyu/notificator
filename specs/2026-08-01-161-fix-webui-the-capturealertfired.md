# Spec: fix `CaptureAlertFired` reading a cache-resident alert pointer

- Issue: [SoulKyu/notificator#161](https://github.com/SoulKyu/notificator/issues/161)
- Date: 2026-08-01
- Status: planned

## Problem

`refreshAlerts` spawns a background goroutine per newly-seen alert to record
the fire event, and hands it the pointer it just stored in the cache map
instead of a snapshot
(`internal/webui/services/alert_cache.go:239-255`):

```go
if existingAlert, exists := ac.alerts[fingerprint]; !exists {
    ac.alerts[fingerprint] = dashAlert          // :240 — pointer goes into the cache
    ac.newAlerts = append(ac.newAlerts, fingerprint)
    alertCopy := *dashAlert                     // :245 — SSE payload IS snapshotted
    newAlertsForSSE = append(newAlertsForSSE, &alertCopy)

    ac.runBounded(func() {                      // :249
        if ac.backendClient != nil && ac.backendClient.IsConnected() {
            if err := ac.backendClient.CaptureAlertFired(dashAlert); err != nil {   // :251 — cache memory, no lock
```

`CaptureAlertFired` reads `alert.Annotations`
(`internal/webui/client/backend_client.go:1812-1813`) and `alert.Status`
(`:1836`, for `SilencedAtFire`) with no lock held. Both fields are rewritten
by the next poll cycle under `ac.mu` in `updateExistingAlert` —
`existing.Status = new.Status` (`alert_cache.go:414`) and
`existing.Annotations = new.Annotations` (`:419`) — on the same struct,
because once the alert is in the map the next cycle takes the `else` branch
and mutates it in place.

The window is wide enough to hit. `runBounded` caps concurrency at
`maxBackendWorkers = 8` (`alert_cache.go:55, 198-204`), each
`CaptureAlertFired` has a 3-second timeout (`backend_client.go:1804`), and
the default refresh interval is 10 seconds. A burst of new alerts — an
Alertmanager coming back after an outage, or the first refresh after a WebUI
restart — queues far more than 8 goroutines, spanning multiple refresh
cycles that are all rewriting `Status`/`Annotations` on those exact structs.

`Status` (`internal/webui/models`, `AlertStatus{State string; SilencedBy,
InhibitedBy []string}`) is a multi-word struct, so the assignment at `:414`
is not atomic. A concurrent reader can observe a `State` string header whose
pointer and length come from different writes; comparing it
(`alert.Status.State == "silenced"`) then reads out of bounds. The benign
outcome is a wrong `silenced_at_fire` flag permanently recorded in
`alert_statistics`; the bad one is a segfault in the WebUI process.

This is a leftover of the invariant #54 established. Commit `11ef6a9`
("stop alert cache from handing out live alert pointers") converted the
read accessors and the acknowledge statistics goroutine to snapshots but
did not touch this call site, and `b3d9002` later added exactly this copy
for the *neighbouring* goroutine twelve lines down —
`alert_cache.go:302-304`. The same reasoning was not carried over here.

## Goals

- `CaptureAlertFired` is invoked with a snapshot taken under `ac.mu`, never
  the pointer stored in `ac.alerts`.
- A `-race`-sensitive test reproduces the bug against current code and
  passes after the fix.
- The `UpdateAlertResolved` goroutine (`alert_cache.go:292-298`), which
  closes over a cache-resident pointer the same way, is checked for the same
  hazard and either fixed or documented as safe.

## Non-goals

- No change to `CaptureAlertFired`'s or `UpdateAlertResolved`'s signature,
  or to the backend RPC contract.
- No broader refactor of `refreshAlerts` or `runBounded` — this is a
  targeted snapshot fix, not a redesign of the refresh pipeline.
- No deep copy: per the existing convention at `alert_cache.go:695-698`,
  `Labels` is never rewritten after construction and `Annotations` is only
  ever replaced wholesale, never mutated in place, so a shallow copy is
  sufficient.

## Approach

### 1. Snapshot before spawning the fire-capture goroutine

Mirror what `alert_cache.go:302-304` already does for the resolved path, in
the `!exists` branch of `refreshAlerts` (`alert_cache.go:239-255`):

```go
// Snapshot, never the cache-resident pointer: Status/Annotations are
// rewritten under ac.mu by the next refresh cycle's updateExistingAlert
// while this goroutine reads them without a lock. Same reasoning as the
// resolved-alert copy below.
firedCopy := *dashAlert
ac.runBounded(func() {
    if ac.backendClient != nil && ac.backendClient.IsConnected() {
        if err := ac.backendClient.CaptureAlertFired(&firedCopy); err != nil {
            log.Printf("Failed to capture alert fired statistics for %s: %v", firedCopy.Fingerprint, err)
        }
    }
})
```

### 2. Document (not fix) `UpdateAlertResolved`

`alert_cache.go:292-298` closes over the cache-resident pointer too, but
`delete(ac.alerts, fingerprint)` happens a few lines below (`:306`), inside
the same `ac.mu` critical section that started at `:222` and doesn't unlock
until `:315`. Once deleted, no code path can reach that exact struct again:

- `MutateAlert`, `GetAlert`, `GetAllAlerts`, `GetLiveAlert` all look the
  alert up by map key under `ac.mu` — none resolve to this pointer once the
  key is gone.
- If the same labels fire again later, `refreshAlerts` takes the `!exists`
  branch and allocates a brand-new `dashAlert`, not this one.
- `storeResolvedAlertInBackend` already gets its own copy (`:303-304`), so
  it isn't a second reader of the original.

So the goroutine at `:292-298` is safe without a copy — add a comment
recording why, so the next person doesn't have to re-derive it:

```go
// Unlike the fire-capture goroutine above, this is not copied: the alert
// is deleted from ac.alerts a few lines below, inside this same lock, so
// no concurrent writer can reach this exact struct again afterwards
// (every lookup path keys off ac.alerts, which no longer holds it).
ac.runBounded(func() {
    if ac.backendClient != nil && ac.backendClient.IsConnected() {
        if err := ac.backendClient.UpdateAlertResolved(alert); err != nil {
            log.Printf("Failed to update alert resolved statistics for %s: %v", alert.Fingerprint, err)
        }
    }
})
```

### 3. Regression test

Add `TestAlertCache_ConcurrentFireCaptureAndRefresh` to
`internal/webui/services/alert_cache_test.go`, following the pattern
`TestAlertCache_ConcurrentRefreshAndReads` (added by `11ef6a9`) already
established in this file.

The tricky part: `ac.backendClient` is a concrete `*client.BackendClient`,
not an interface, so there's no seam to inject a fake for `CaptureAlertFired`.
Work with the real type instead of adding one:

- `CaptureAlertFired` reads `alert.Annotations`/`alert.Status` while
  building the gRPC request, *before* it sends anything over the wire — so
  the race is exercised regardless of whether the RPC itself succeeds.
- Point a real `*client.BackendClient` at a loopback address nothing is
  listening on (`net.Listen("tcp", "127.0.0.1:0")` then `Close()`
  immediately, reusing the freed port). `client.NewBackendClient(addr)` +
  `.Connect()` uses `grpc.NewClient`, which dials lazily — `Connect()`
  succeeds and `IsConnected()` reports true even though nothing answers.
  The eventual RPC then fails fast with "connection refused" instead of
  blocking for the 3-second timeout, and the error is just logged, matching
  production behavior when the backend is down.
- Set `cache.backendClient` to that client directly (test lives in package
  `services`, so the unexported field is assignable).
- Loop a handful of times (e.g. 8, matching `maxBackendWorkers`): each
  iteration first refreshes with a brand-new fingerprint (`!exists` branch —
  stores the pointer, spawns the fire-capture goroutine), then immediately
  refreshes again with the *same* fingerprint but a changed `Status`
  (e.g. `firing` → `silenced` with a `SilencedBy` entry) and `Annotations`
  (`exists` branch — `updateExistingAlert` writes those fields on the same
  cache-resident struct under `ac.mu`). No rendezvous between the two calls
  is needed or wanted: the absence of synchronization between them is
  exactly the bug, and `-race` flags conflicting accesses on the same
  memory regardless of how the goroutine scheduler happens to interleave
  them.
- Before the test returns, wait for every spawned fire-capture goroutine to
  actually finish its read — otherwise the process can exit before the race
  detector observes it. `runBounded` doesn't expose a `WaitGroup`, but its
  semaphore does: `cache.backendSem` (buffered channel, capacity
  `maxBackendWorkers`) holds one token for the lifetime of each in-flight
  call, so polling `len(cache.backendSem) == 0` on a short ticker is a
  reliable "all goroutines done" signal.
- Run: `go test -race -run TestAlertCache_ConcurrentFireCaptureAndRefresh ./internal/webui/services/...`.
  Expected: fails with `WARNING: DATA RACE` on `Status`/`Annotations` against
  today's code; passes cleanly once `refreshAlerts` snapshots before
  spawning.

### Files touched

- `internal/webui/services/alert_cache.go` — snapshot before the
  fire-capture `runBounded` call; add the safety comment on the
  resolved-statistics goroutine.
- `internal/webui/services/alert_cache_test.go` — new
  `TestAlertCache_ConcurrentFireCaptureAndRefresh`.

## Risks & trade-offs

- **Shallow copy only**: consistent with the existing convention
  (`alert_cache.go:695-698`, `:303-304`) — `Labels`/`Annotations` are
  replace-wholesale-only fields, so a deep copy would be dead weight here.
  If that invariant ever changes (in-place annotation mutation added
  elsewhere), this call site and the others following the same pattern
  would all need revisiting together.
- **Test uses a real `*client.BackendClient`** instead of a fake, since the
  field isn't an interface. This makes the test slightly more expensive
  (real TCP dial attempt, real JSON/proto marshalling) but exercises the
  exact production code path — a fake would risk not reproducing the read
  order that causes the race in the first place.
- **No fix to `UpdateAlertResolved`**: relies on the delete-under-the-same-lock
  argument holding. If a future change moves the `delete(ac.alerts, ...)`
  outside the lock, or adds another lookup path that doesn't key off
  `ac.alerts`, that reasoning breaks silently — the comment is the only
  guard against that regression.

## Validation

- `go build ./...`
- `go vet ./...`
- `go test -race ./internal/webui/...` (must include the new test passing,
  and confirm no other race is newly surfaced in the package)
