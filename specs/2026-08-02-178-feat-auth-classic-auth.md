# Spec: change password for classic-auth users

- Issue: [SoulKyu/notificator#178](https://github.com/SoulKyu/notificator/issues/178)
- Date: 2026-08-02
- Status: planned

## Problem

`/profile`'s "Change Password" button (`internal/webui/templates/pages/Profile.templ:210-217`,
gated on `data.User.OAuthProvider == nil`) calls `showChangePassword()`
(`Profile.templ:266-269`), which is a dead end:

```js
showChangePassword() {
    // TODO: Implement change password modal
    alert('Change password functionality coming soon!');
}
```

No RPC, handler, or route exists to back it. `proto/auth.proto`'s
`AuthService` has `password` only on `RegisterRequest` (field 2, line 45)
and `LoginRequest` (field 2, line 57) — nothing lets an existing user rotate
theirs. The quickstart (`openwiki/quickstart.md:35`) tells operators to log
in with `admin:admin` "(change it)"; that instruction cannot be followed
from the product today.

## Goals

1. A classic-auth user (one with a password hash, i.e. `models.User.HasPassword()`
   — `internal/backend/models/models.go:47-49`) can change their password
   from `/profile` by supplying their current password and a new one.
2. Wrong current password is rejected with a specific, visible error; no
   silent success.
3. OAuth-only accounts (no password hash) are refused server-side, not just
   hidden client-side (the button is already hidden for them).
4. On success, the user's other active sessions are invalidated; the
   session used to make the change stays valid.
5. The `admin:admin` bootstrap credential becomes rotatable end-to-end
   through the UI.

## Non-goals

- No email-based password reset / "forgot password" flow — this is
  in-session rotation only, requiring the current password.
- No change to `classicAuthDisabled()` gating (`internal/backend/services/services.go:50-56`).
  `ChangePassword` is not added to it: it only ever touches an account that
  already has a password hash, so it doesn't open a new classic-auth
  surface in OAuth-only deployments.
- No new password-strength policy beyond the existing 4-character minimum
  `Register` already enforces (`internal/backend/services/services.go:80-85`)
  — reused as-is, not redesigned.
- No changes to `internal/backend/auth_interceptor.go`'s `publicMethods`
  allowlist (`auth_interceptor.go:34-42`) — `ChangePassword` must **not** be
  added to it, since omission is exactly what makes it session/service-token
  gated by the existing deny-by-default `authenticate()` (`auth_interceptor.go:107-137`).

## Approach

### 1. Proto: `ChangePassword` RPC

`proto/auth.proto` — add to the `AuthService` block (after `rpc UpdateTimezone`,
line 17):

```protobuf
rpc ChangePassword(ChangePasswordRequest) returns (ChangePasswordResponse);
```

And messages (after `UpdateTimezoneResponse`, lines 105-108):

```protobuf
message ChangePasswordRequest {
  string session_id = 1;
  string old_password = 2;
  string new_password = 3;
}

message ChangePasswordResponse {
  bool success = 1;
  string error = 2;
}
```

### 1b. Regeneration (fix the target before using it)

`make proto` is a **silent no-op** at this SHA: `proto` is absent from the
`.PHONY` list (`Makefile:1`) and a `proto/` directory exists, so make
resolves the target against that directory and prints
`make: 'proto' is up to date.` with rc=0 — `scripts/generate_proto.sh`
never runs, and the `@echo "Generating proto files..."` line never prints.
Regenerating is therefore part of the change, not a build note:

1. Add `proto` to the `.PHONY` list at `Makefile:1`. One-word edit; the
   recipe itself (`Makefile:46-48`) is already correct.
2. Install the two protoc plugins the script shells out to — `protoc`
   alone is not enough and neither plugin is vendored:

   ```sh
   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
   ```

   Do this **first**: `scripts/generate_proto.sh` runs under `set -e`
   (`:1`) and `rm -rf internal/backend/proto/auth` (`:11-13`) *before*
   invoking `protoc` (`:19-24` auth, `:27-32` alert), so running it
   without the plugins deletes the generated auth package and leaves the
   tree unbuildable. Recovery is
   `git checkout -- internal/backend/proto`.
3. Run `make proto`, then check the generated **content**, not the exit
   code:

   ```sh
   grep -q ChangePasswordRequest internal/backend/proto/auth/auth.pb.go
   grep -q ChangePassword internal/backend/proto/auth/auth_grpc.pb.go
   ```

   Both must exit 0. Skipping this check is what makes the failure surface
   later as `undefined: authpb.ChangePasswordRequest` at `go build` with
   nothing pointing back at the generation step.

Do not hand-edit `auth.pb.go` or `auth_grpc.pb.go`.

### 2. Database: two new `GormDB` methods

`internal/backend/database/gorm_db.go`:

- Next to `UpdateUserTimezone` (line 238-240), add:

  ```go
  func (gdb *GormDB) UpdateUserPasswordHash(userID, passwordHash string) error {
      return gdb.db.Model(&models.User{}).Where("id = ?", userID).Update("password_hash", passwordHash).Error
  }
  ```

- Next to `DeleteSession` (line 309-311), add:

  ```go
  // DeleteOtherSessions removes every session for userID except
  // keepSessionID, so a password change can invalidate stolen/stale
  // sessions without logging the requester out of the session they used.
  func (gdb *GormDB) DeleteOtherSessions(userID, keepSessionID string) error {
      return gdb.db.Where("user_id = ? AND id != ?", userID, keepSessionID).Delete(&models.Session{}).Error
  }
  ```

  `models.Session` (`internal/backend/models/models.go:65-72`) has `UserID`
  and `ID` columns, so this is a direct filter — no new model/migration.

### 3. Backend service: `AuthServiceGorm.ChangePassword`

`internal/backend/services/services.go`, added after `UpdateTimezone`
(line 278-321), following that method's exact shape (session lookup via
`s.db.GetUserBySession`, no RPC-level `classicAuthDisabled` check since this
never creates a new password, only rotates an existing one):

```go
// ChangePassword implements the ChangePassword RPC method
func (s *AuthServiceGorm) ChangePassword(ctx context.Context, req *authpb.ChangePasswordRequest) (*authpb.ChangePasswordResponse, error) {
    if req.SessionId == "" {
        return &authpb.ChangePasswordResponse{Success: false, Error: "Session ID is required"}, nil
    }

    user, err := s.db.GetUserBySession(req.SessionId)
    if err != nil {
        return &authpb.ChangePasswordResponse{Success: false, Error: "Invalid session"}, nil
    }

    if !user.HasPassword() {
        return &authpb.ChangePasswordResponse{Success: false, Error: "This account has no password to change"}, nil
    }

    if req.OldPassword == "" || req.NewPassword == "" {
        return &authpb.ChangePasswordResponse{Success: false, Error: "Current and new password are required"}, nil
    }

    if len(req.NewPassword) < 4 {
        return &authpb.ChangePasswordResponse{Success: false, Error: "Password must be at least 4 characters long"}, nil
    }

    if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
        return &authpb.ChangePasswordResponse{Success: false, Error: "Current password is incorrect"}, nil
    }

    newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
    if err != nil {
        log.Printf("Error hashing password: %v", err)
        return &authpb.ChangePasswordResponse{Success: false, Error: "Internal server error"}, nil
    }

    if err := s.db.UpdateUserPasswordHash(user.ID, string(newHash)); err != nil {
        log.Printf("Error updating password for user %s: %v", user.ID, err)
        return &authpb.ChangePasswordResponse{Success: false, Error: "Failed to update password"}, nil
    }

    if err := s.db.DeleteOtherSessions(user.ID, req.SessionId); err != nil {
        // Password is already changed; log but don't fail the request over
        // a session-cleanup error, matching UpdateLastLogin's precedent
        // (services.go:175-178) of not failing the primary action.
        log.Printf("Error invalidating other sessions for user %s: %v", user.ID, err)
    }

    return &authpb.ChangePasswordResponse{Success: true}, nil
}
```

`bcrypt`, `context`, `log` are already imported in `services.go` (lines
3-14); no new imports needed for this file.

This RPC is reachable only through `authUnaryInterceptor` →
`authenticate()`. Since `/notificator.auth.AuthService/ChangePassword` is
absent from `publicMethods`, `authenticate()` requires a valid service
token or session before the handler above ever runs (deny-by-default,
already in place from #172) — no interceptor change needed to satisfy
"the RPC is unreachable without a valid session."

### 4. WebUI gRPC client wrapper

`internal/webui/client/backend_client.go`, after `UpdateTimezone`
(line 376-397). Unlike `UpdateTimezone` (which collapses success/failure
into a single `error` the handler can't distinguish from a transport
failure), this must preserve the business-level message so the handler can
show "Current password is incorrect" to the user — same shape as
`Login`'s `(*AuthResult, error)` split between transport error and
business failure (`backend_client.go:187-222`):

```go
// ChangePassword rotates the caller's password, verifying oldPassword
// server-side. Returns (success, message) for business outcomes (wrong
// password, too short, ...) and a non-nil error only for transport/connection
// failures.
func (c *BackendClient) ChangePassword(sessionID, oldPassword, newPassword string) (bool, string, error) {
    if c.authClient == nil {
        return false, "", fmt.Errorf("not connected to backend")
    }

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    resp, err := c.authClient.ChangePassword(ctx, &authpb.ChangePasswordRequest{
        SessionId:   sessionID,
        OldPassword: oldPassword,
        NewPassword: newPassword,
    })
    if err != nil {
        return false, "", err
    }

    return resp.Success, resp.Error, nil
}
```

### 5. WebUI handler + route

`internal/webui/handlers/profile_handlers.go`, after `UpdateTimezone`
(line 101-137), same guard order (auth → bind → backend-availability →
call):

```go
// ChangePassword rotates the current user's password
func ChangePassword(c *gin.Context) {
    user := middleware.GetCurrentUserFromContext(c)
    if user == nil {
        c.JSON(http.StatusUnauthorized, models.ErrorResponse("Not authenticated"))
        return
    }

    var req struct {
        OldPassword string `json:"old_password" binding:"required"`
        NewPassword string `json:"new_password" binding:"required"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, models.ErrorResponse("Current and new password are required"))
        return
    }

    if backendClient == nil || !backendClient.IsConnected() {
        c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend not available"))
        return
    }

    success, errMsg, err := backendClient.ChangePassword(middleware.GetSessionID(c), req.OldPassword, req.NewPassword)
    if err != nil {
        c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Failed to reach backend"))
        return
    }
    if !success {
        c.JSON(http.StatusBadRequest, models.ErrorResponse(errMsg))
        return
    }

    c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
        "message": "Password changed successfully",
    }))
}
```

Route in `internal/webui/router.go`, inside the existing `authProtected`
group (already `authMiddleware.RequireAuth()`-gated, line 200-207),
alongside `/logout` and `/me`:

```go
authProtected.POST("/change-password", handlers.ChangePassword)
```

This resolves to `POST /api/v1/auth/change-password` (group prefix
`/api/v1` at `router.go:183`, `/auth` sub-group at `router.go:201`) —
exactly the path the issue proposes.

### 6. Frontend: modal replacing the `alert()`

`internal/webui/templates/pages/Profile.templ`:

- Replace the dead `showChangePassword()` body (lines 266-269) with state
  toggling (`showChangePasswordModal`, form fields, `saving`, `error`) on
  the existing `profilePage()` Alpine component (`Profile.templ:239-271`)
  — no new mounting site needed, it's the same component already
  initialized via `x-data="profilePage()"` on the page root (`Profile.templ:44`).
- Add a modal block as a sibling of the existing content, following the
  overlay/card structure of `MaintenanceModal.templ:5-17` (`fixed inset-0`
  backdrop, centered card, `x-show`/`x-cloak`/`x-transition`) for visual
  consistency: three password `<input type="password">` fields (current,
  new, confirm), inline error text bound to `x-text="passwordError"`,
  Cancel and Submit buttons.
- Submit handler posts JSON and surfaces the server error verbatim:

  ```js
  async submitPasswordChange() {
      if (this.newPassword !== this.confirmPassword) {
          this.passwordError = 'New passwords do not match';
          return;
      }
      this.savingPassword = true;
      this.passwordError = '';
      try {
          const resp = await fetch('/api/v1/auth/change-password', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                  old_password: this.oldPassword,
                  new_password: this.newPassword
              })
          });
          const data = await resp.json();
          if (!resp.ok || !data.success) {
              this.passwordError = data.error || 'Failed to change password';
              return;
          }
          this.showChangePasswordModal = false;
          this.oldPassword = this.newPassword = this.confirmPassword = '';
      } catch (e) {
          this.passwordError = 'Network error, please try again';
      } finally {
          this.savingPassword = false;
      }
  }
  ```

  This mirrors `TimezoneSelector.templ:150-158`'s `fetch(... method: 'PUT' ...)`
  pattern for calling a profile endpoint from Alpine — same-origin
  session-cookie auth, no CSRF token wiring needed (nothing else on this
  page does either).
- Do not interpolate any server-rendered Go value (username, ID, etc.)
  into new `<script>` string literals — the existing
  `userId: '{ data.User.ID }'` at `Profile.templ:242` is the html-entity-escaping
  trap noted for this file (values ending up HTML-entity-escaped inside a
  JS string); the new modal's state is 100% client-typed input plus a
  fetch response, so this doesn't apply here, but don't add a new
  server-value interpolation to work around it.
- Regenerate with `make webui-templates` (never hand-edit `Profile_templ.go`).

## Risks & trade-offs

- **Session invalidation scope**: `DeleteOtherSessions` is a hard
  cross-device logout on every password change. This is the behavior the
  issue's proposed approach asks for ("keep the current one") and matches
  common practice; no opt-out is offered, since offering one would
  reintroduce the exact "leaked password, old session still valid"
  exposure a password-rotation feature exists to close.
- **No rate limiting on `ChangePassword`**: an authenticated attacker with a
  stolen session can brute-force the current password via repeated calls.
  Out of scope here — no other auth RPC (`Login` included) in this codebase
  has rate limiting today, so adding it only to this one RPC would be
  inconsistent scope creep; call out as a follow-up if it needs solving
  properly, backend-wide.
- **Mixed-auth accounts**: `HasPassword()` gates the RPC, but `Profile.templ:210`
  gates the button on `OAuthProvider == nil`, which is computed with extra
  session-derived logic in `profile_handlers.go:25-36`. If an account is
  ever both password- and OAuth-linked and its display badge shows OAuth,
  the button would stay hidden even though the RPC would accept a change.
  Not addressed here — same display-vs-capability gap already exists today
  independent of this feature, and mixed accounts aren't currently
  produced by any code path in this repo.

## Validation

- Regeneration verified by generated content, not by exit code (see §1b —
  `make proto` returns 0 while doing nothing until the `.PHONY` fix lands):

  ```sh
  make proto
  grep -q ChangePasswordRequest internal/backend/proto/auth/auth.pb.go
  make webui-templates
  go build ./...
  ```

  The `grep` is the gate; `make proto`'s exit code proves nothing on its own.
- New Go test file `internal/backend/services/change_password_test.go`,
  modeled on `internal/backend/services/update_timezone_test.go`. That
  file's `setupAuthServiceWithSession` helper creates users with
  `db.CreateUser("alice", "alice@example.com", "hash")` — a literal
  placeholder string, not a real bcrypt hash, which is fine for
  `UpdateTimezone` but means it **cannot** be reused as-is here: a real
  test needs a user created with `bcrypt.GenerateFromPassword([]byte("initial-pw"), bcrypt.DefaultCost)`
  as the stored hash so `bcrypt.CompareHashAndPassword` has something
  genuine to check against. Add a small local helper (or inline the
  bcrypt hash + `db.CreateUser`) in the new test file rather than changing
  the shared helper's signature. Cases:
  - missing session → `Success == false`.
  - invalid session → `Success == false`.
  - wrong `old_password` against the known hash → `Success == false`,
    `Error == "Current password is incorrect"`.
  - `new_password` under 4 chars → `Success == false`.
  - correct `old_password` + valid `new_password` → `Success == true`;
    follow-up `Login` with the new password succeeds; `Login` with the old
    password fails.
  - OAuth-only user (create via `db` with empty `PasswordHash`, `OAuthProvider`/`OAuthID` set) → `Success == false`, `Error == "This account has no password to change"`.
  - a second session for the same user is deleted after a successful
    change (assert via `s.db.GetUserBySession` returning an error for it),
    while the session used for the `ChangePassword` call itself still
    resolves.
- Manual check via `make test` (docker-compose stack), covering the
  issue's acceptance criteria directly:
  1. Log in as a classic-auth user (or `admin`/`admin`), open `/profile`,
     click **Change Password**, submit wrong current password → inline
     error shown, no page reload, modal stays open.
  2. Submit correct current password + valid new password → modal closes;
     log out; log in with the new password (succeeds) then the old one
     (fails).
  3. Confirm any other open session for that user (e.g. a second browser
     logged in earlier) is now logged out, while the tab used to change
     the password stays logged in.
  4. Confirm an OAuth-only test account never shows the **Change Password**
     button on `/profile`.
  5. Rotate the bootstrap `admin`/`admin` credential end-to-end through
     this flow and confirm the new password logs in.

## Verification ledger

- `Profile.templ:210-217` button gating on `OAuthProvider == nil` and
  `:266-269` dead `alert()`: read directly, matches issue's cited lines.
- `proto/auth.proto` password fields only on `RegisterRequest`/`LoginRequest`:
  read full file, confirmed no `ChangePassword`-shaped RPC/message exists
  anywhere in it.
- `auth_interceptor.go:34-42` `publicMethods` allowlist and `:107-137`
  `authenticate()` deny-by-default logic: read in full; confirmed
  `ChangePassword` needs no entry to be session/service-token gated.
- `services.go:124-191` (`Login`) and `:278-321` (`UpdateTimezone`): read in
  full to confirm the session-lookup-then-mutate pattern and that
  `classicAuthDisabled()` is only checked by `Register`/`Login`, not by
  every mutating RPC.
- `models.go:13-63` (`User` struct, `HasPassword`/`IsOAuthUser`) and `:65-72`
  (`Session` struct): read in full; `HasPassword()` chosen over
  `IsOAuthUser()` for the reject check since it's the precise "is there a
  hash to compare against" condition.
- `gorm_db.go:206-321`: read in full; confirmed no existing password-update
  or partial-session-delete method, and that `CreateUser`/`CreateSession`/
  `GetUserBySession`/`DeleteSession` have the exact signatures used above.
- `backend_client.go:187-222` (`Login`) vs `:376-397` (`UpdateTimezone`):
  read both in full; confirmed `UpdateTimezone`'s single-`error` return
  swallows the business message, which is why `ChangePassword`'s client
  wrapper is modeled on `Login`'s split return instead.
- `profile_handlers.go` (full file, 155 lines): read in full; confirmed
  `UpdateTimezone`/`GetTimezone` handler pattern and that `backendClient`/
  `middleware.GetCurrentUserFromContext`/`middleware.GetSessionID` are the
  established building blocks, no new plumbing required.
- `router.go:200-215`: read to confirm the `authProtected` group already
  sits behind `authMiddleware.RequireAuth()` and that `/api/v1` +
  `/auth` prefixes compose to `/api/v1/auth/change-password`; confirmed
  group prefix at `router.go:183` via targeted grep.
- `MaintenanceModal.templ` (full file) and `TimezoneSelector.templ` (full
  file): read in full as the modal-markup and Alpine-`fetch` style
  references respectively; confirmed no CSRF token is wired into any
  existing same-page fetch call, so the new modal doesn't need one either.
- `update_timezone_test.go` (full file): read in full; confirmed
  `setupAuthServiceWithSession`'s `"hash"` placeholder is not bcrypt-valid,
  driving the "needs a local bcrypt-real helper" note in Validation.
- `auth_interceptor_test.go:107-163` (`twelveRPCs` table): read in full;
  confirmed it's a fixed historical list from issue #160, not a
  "every non-public RPC" enumeration — no update needed there for the new
  RPC.
- Makefile `proto`/`webui-templates` targets (lines 41-48) and the `.PHONY`
  list (line 1): both targets were *run*, not just read. `make
  webui-templates` really invokes `templ generate`; `make proto` prints
  `make: 'proto' is up to date.` and exits 0 without running the script,
  because `proto` is missing from `.PHONY` while a `proto/` directory
  exists — hence §1b.
- `scripts/generate_proto.sh` (full file): read in full; confirmed `set -e`
  (`:1`), the `rm -rf` of the generated packages at `:11-13` ahead of the
  `protoc` calls at `:19-25`, and that the script depends on
  `protoc-gen-go`/`protoc-gen-go-grpc` being on `PATH` (`which` finds only
  `protoc` itself in a bare checkout environment).
- `openwiki/quickstart.md:35`: read to confirm the exact `admin:admin`
  wording the issue references.
