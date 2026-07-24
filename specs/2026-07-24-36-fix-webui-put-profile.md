# Spec: fix(webui) — PUT /profile/timezone panics, timezone preference never persisted

- Issue: [SoulKyu/notificator#36](https://github.com/SoulKyu/notificator/issues/36)
- Date: 2026-07-24
- Status: planned

## Problem

`PUT /api/v1/profile/timezone` panics on every call, and even without the panic
there is nowhere to write the value.

`UpdateTimezone` (`internal/webui/handlers/profile_handlers.go:126`) does
`c.MustGet("db").(*gorm.DB)`, but no middleware ever sets `"db"` on the gin
context — the WebUI process is an all-gRPC client
(`internal/webui/client/backend_client.go`) with no database handle.
`MustGet` panics; `gin.Recovery()` (`internal/webui/router.go:133`) turns it
into a 500 and dumps a stack trace in the log. Even with a magically injected
`*gorm.DB`, the next line runs `db.Model(user).Update(...)` on a
`*client.User` — a plain DTO with no table or GORM tags — so the statement
could never work. `gorm.io/gorm` (`profile_handlers.go:10`) is the only GORM
import in the whole `internal/webui/` tree.

The route is live (`internal/webui/router.go:201`) and the timezone picker
calls it fire-and-forget on every selection
(`internal/webui/templates/components/TimezoneSelector.templ:151`), so the
failure is invisible in the UI.

Nothing persists the value on the backend either: `proto/auth.proto` has
`GetProfile` but no update RPC, and although `string timezone = 8` exists in
the profile message (`proto/auth.proto:106`) and the column exists
(`internal/backend/models/models.go:29`), neither `ValidateSession` nor
`GetProfile` (`internal/backend/services/services.go:177,206`) populates it,
and `client.User` mapping never sets `Timezone`. So `GetTimezone`
(`profile_handlers.go:138-153`) always returns `""` and the picker falls back
to the browser timezone on every reload.

Impact: the timezone preference resets per device/reload, silently shifting
time-of-day and weekend bucketing on the statistics dashboard
(`window.__USER_TIMEZONE__` feeds eight query payloads in
`StatisticsDashboard.templ`), and every selection emits a panic stack trace —
log noise and false alarms during incident triage.

## Goals

- `PUT /api/v1/profile/timezone` persists the timezone to `users.timezone`
  via a new backend RPC — no panic, no GORM in the WebUI.
- The stored timezone survives reloads and follows the user across
  browsers/machines; `GET /api/v1/profile/timezone` returns it.
- Invalid timezones are rejected at both layers: 400 in the WebUI handler,
  error from the backend RPC (which is independently reachable).
- The RPC enforces session validation itself (there is no auth interceptor).

## Non-goals

- No generic `UpdateProfile` RPC with field-mask semantics — `timezone` is
  the only writable preference on this message today.
- No `localStorage` fallback / 501 stub (considered and rejected in the
  issue: deletes a shipped preference, keeps stats device-dependent).
- No changes to the statistics engine's timezone handling — it already
  consumes `window.__USER_TIMEZONE__` correctly.

## Approach

Complete the feature end to end: proto → backend → read path → WebUI handler.

### 1. Proto

Add to `AuthService` in `proto/auth.proto`:

```proto
rpc UpdateTimezone(UpdateTimezoneRequest) returns (UpdateTimezoneResponse);

message UpdateTimezoneRequest {
  string session_id = 1;
  string timezone = 2;   // IANA timezone, e.g. "Europe/Paris"
}

message UpdateTimezoneResponse {
  bool success = 1;
  string error = 2;
}
```

Regenerate with `make proto`; never hand-edit `*.pb.go`. (LSP may show stale
"unknown field" errors after regen — trust `go build`.)

### 2. Backend service

Implement `UpdateTimezone` on `AuthServiceGorm`
(`internal/backend/services/services.go`), following the hand-rolled session
pattern of its neighbours (`Logout`/`ValidateSession`: empty `session_id` →
error response, then `s.db.GetUserBySession(req.SessionId)`). There is **no**
auth interceptor, so the check is mandatory here.

Validate the timezone in the backend with `time.LoadLocation` and return
`success=false` with an error for unknown zones — the RPC is reachable
without the WebUI, so the WebUI's check is not a trust boundary. Reject
`timezone == ""` and `timezone == "Local"` explicitly **before** calling
`time.LoadLocation`: both pass `LoadLocation` without error, but empty is
not a clear-preference API (it would silently reset the stored value to the
browser-fallback path), and `Local` is not a portable IANA name — its
meaning depends on whichever binary later interprets it.

Persist via a new `GormDB` method in
`internal/backend/database/gorm_db.go`, modelled on `UpdateLastLogin`
(`gorm_db.go:230`):

```go
func (gdb *GormDB) UpdateUserTimezone(userID, timezone string) error {
    return gdb.db.Model(&models.User{}).Where("id = ?", userID).
        Update("timezone", timezone).Error
}
```

### 3. Read path

- Populate `Timezone` in the `authpb.User` built by `GetProfile` **and**
  `ValidateSession` (`services.go:196-202,221-227`) — `ValidateSession`
  feeds `middleware.GetCurrentUserFromContext`, which is what `GetTimezone`
  reads.
- Map the proto field into `client.User.Timezone` in
  `internal/webui/client/backend_client.go` (`ValidateSession` at :209,
  `GetProfile` at :246). The `Timezone *string` field on `client.User`
  already exists; keep `nil` for empty proto strings so `GetTimezone`'s
  existing nil check keeps returning `""` for users who never set one.

### 4. WebUI handler

Rewrite `UpdateTimezone` (`profile_handlers.go:103-135`):

- Keep the auth check, JSON binding, and the `time.LoadLocation` 400 guard
  (`:119-123`) — a cheap reject before the gRPC round-trip. Extend the
  guard to also 400 on `"Local"` (empty is already caught by
  `binding:"required"`), mirroring the backend's explicit rejects.
- Replace the GORM block (`:125-130`) with a call to a new
  `BackendClient.UpdateTimezone(sessionID, timezone string) error` method
  (session ID via `middleware.GetSessionID(c)`, same as `ProfilePage`).
  Backend `success=false` → always 500 with the backend error message.
  `UpdateTimezoneResponse` carries no machine-readable error code, and the
  invalid-timezone case is effectively unreachable from the WebUI because
  the handler runs the same guard first — so a 400 mapping would require
  string-matching backend error text; don't.
- Remove the `gorm.io/gorm` import (`:10`). After this, `internal/webui/`
  is GORM-free again.

`GetTimezone` needs no code change — it starts working once the read path
populates `user.Timezone`.

### Files touched

- `proto/auth.proto` — new RPC + messages; regen via `make proto`.
- `internal/backend/services/services.go` — `UpdateTimezone` impl; populate
  `Timezone` in `ValidateSession` and `GetProfile` responses.
- `internal/backend/database/gorm_db.go` — `UpdateUserTimezone`.
- `internal/webui/client/backend_client.go` — `UpdateTimezone` client
  method; map `Timezone` in `ValidateSession`/`GetProfile`.
- `internal/webui/handlers/profile_handlers.go` — rewrite `UpdateTimezone`,
  drop GORM import.

No `.templ` changes: the picker already PUTs on select and loads via
`GET /api/v1/profile/timezone`.

## Risks & trade-offs

- **Proto regen churn**: `make proto` regenerates `*.pb.go`; commit the
  generated files as-is, never hand-edit.
- **Backward compatibility**: adding an RPC and populating an existing proto
  field are both wire-compatible; old WebUI against new backend keeps
  working.
- **Timezone validation drift**: `time.LoadLocation` depends on the tzdata
  available to each binary. Both checks use the same Go stdlib, so drift is
  only possible across differing base images — acceptable; the backend check
  is authoritative.
- **Empty timezone semantics**: proto3 `string` cannot distinguish
  unset/empty, so empty proto string maps to `nil` in `client.User` and the
  browser-timezone fallback in `loadTimezone()` remains the behaviour for
  users who never chose one — which is the intended default.

## Validation

- `make proto && go build ./...` passes.
- `rg 'MustGet\("db"\)' internal/webui/` and `rg 'gorm' internal/webui/`
  both return nothing.
- Manual check via `make test` (docker-compose stack):
  - Selecting `UTC` in the picker → 200, no stack trace in the WebUI log.
  - Reload → picker still shows `UTC`; `GET /api/v1/profile/timezone`
    returns `{"timezone":"UTC"}`.
  - Second browser session (same user) shows the stored timezone.
  - `PUT` with `Mars/Olympus` → 400, value not persisted.
- Direct RPC check (e.g. `grpcurl`): `UpdateTimezone` with missing/invalid
  `session_id` → error; valid session with `Mars/Olympus` → error, row
  unchanged; valid session with `timezone: ""` → error, row unchanged
  (empty is not a clear-preference API); valid session with
  `timezone: "Local"` → error, row unchanged.
