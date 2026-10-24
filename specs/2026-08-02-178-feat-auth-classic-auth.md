# Spec: change password for classic-auth users

- Issue: [SoulKyu/notificator#178](https://github.com/SoulKyu/notificator/issues/178)
- Date: 2026-08-02
- Status: planned

## How this spec addresses code

**No line numbers.** Every reference in this document is a *symbol anchor* —
a file plus an identifier or an exact source string you can `grep -F` for.
Line numbers were used in the first two revisions of this spec and were wrong
on arrival (copied from the issue rather than read from the tree), which is
the failure mode a reader has no way to detect: a wrong number still looks
authoritative, and "insert after line N" silently points into the wrong
function. Symbol anchors either match or they don't, and §Anchor check turns
that into a script anyone can run before starting work.

If an anchor in this document does not resolve, **stop and re-read the file**
— the spec is stale, not the tree.

## Problem

`/profile` renders a "Change Password" button, gated on
`data.User.OAuthProvider == nil`, in
`internal/webui/templates/pages/Profile.templ`. It is wired to
`@click="showChangePassword"`, whose implementation in the page's inline
`<script>` is a dead end:

```js
showChangePassword() {
    // TODO: Implement change password modal
    alert('Change password functionality coming soon!');
}
```

No RPC, handler, or route exists to back it. In `proto/auth.proto`,
`AuthService` carries a `password` field only on `RegisterRequest` and
`LoginRequest` — nothing lets an existing user rotate theirs.

## Goals

1. A classic-auth user (one with a password hash, i.e. `models.User.HasPassword()`
   in `internal/backend/models/models.go`) can change their password from
   `/profile` by supplying their current password and a new one.
2. Wrong current password is rejected with a specific, visible error; no
   silent success.
3. OAuth-only accounts (no password hash) are refused server-side, not just
   hidden client-side (the button is already hidden for them).
4. On success, the user's other active sessions are invalidated; the
   session used to make the change stays valid.

## Non-goals

- No email-based password reset / "forgot password" flow — this is
  in-session rotation only, requiring the current password.
- No change to `classicAuthDisabled()` (`internal/backend/services/services.go`).
  `ChangePassword` does not call it: it only ever touches an account that
  already has a password hash, so it doesn't open a new classic-auth
  surface in OAuth-only deployments.
- No new password-strength policy beyond the existing 4-character minimum
  that `AuthServiceGorm.Register` enforces (`"Password must be at least 4
  characters long"` in `services.go`) — reused as-is, not redesigned.
- No changes to the `publicMethods` allowlist in
  `internal/backend/auth_interceptor.go` — `ChangePassword` must **not** be
  added to it, since omission is what routes it through the deny-by-default
  tail of `(*Server).authenticate`.
- **Not fixing the `admin:admin` documentation bug.** `README.md` and
  `openwiki/quickstart.md` both tell operators to log in with `admin:admin`
  and change it. No such account exists: the only non-test caller of
  `GormDB.CreateUser` is `AuthServiceGorm.Register`, there is no seeding
  path anywhere in the repo, and a freshly-booted stack has zero rows in
  `users`. This feature does not create one and cannot make that
  instruction true — the docs are simply wrong and need their own issue.
  Do not treat "rotate the bootstrap admin password" as an acceptance
  criterion for this work; there is nothing to rotate.

## Approach

### 1. Proto: `ChangePassword` RPC

`proto/auth.proto` — add to the `service AuthService` block, next to
`rpc UpdateTimezone`:

```protobuf
rpc ChangePassword(ChangePasswordRequest) returns (ChangePasswordResponse);
```

And messages, next to `message UpdateTimezoneResponse`:

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
`.PHONY` list (the first line of the `Makefile`) and a `proto/` directory
exists, so make resolves the target against that directory and prints
`make: 'proto' is up to date.` with rc=0 — `scripts/generate_proto.sh`
never runs, and the `@echo "Generating proto files..."` line never prints.
Confirm the precondition before touching anything:

```sh
head -1 Makefile | grep -qw proto && echo "already fixed" || echo "no-op confirmed"
```

Regenerating is therefore part of the change, not a build note:

1. Add `proto` to the `.PHONY` list on the Makefile's first line. One-word
   edit; the `proto:` recipe itself is already correct. After the edit the
   command above must print `already fixed`.
2. Install the two protoc plugins the script shells out to — `protoc`
   alone is not enough and neither plugin is vendored:

   ```sh
   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
   ```

   Do this **first**: `scripts/generate_proto.sh` runs under `set -e` (its
   first line) and does `rm -rf internal/backend/proto/auth` /
   `rm -rf internal/backend/proto/alert` *before* invoking `protoc`, so
   running it without the plugins deletes the generated auth package and
   leaves the tree unbuildable. Recovery is
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

- Next to `func (gdb *GormDB) UpdateUserTimezone`, add:

  ```go
  func (gdb *GormDB) UpdateUserPasswordHash(userID, passwordHash string) error {
      return gdb.db.Model(&models.User{}).Where("id = ?", userID).Update("password_hash", passwordHash).Error
  }
  ```

- Next to `func (gdb *GormDB) DeleteSession`, add:

  ```go
  // DeleteOtherSessions removes every session for userID except
  // keepSessionID, so a password change can invalidate stolen/stale
  // sessions without logging the requester out of the session they used.
  func (gdb *GormDB) DeleteOtherSessions(userID, keepSessionID string) error {
      return gdb.db.Where("user_id = ? AND id != ?", userID, keepSessionID).Delete(&models.Session{}).Error
  }
  ```

  `models.Session` (`internal/backend/models/models.go`) has `UserID`
  and `ID` columns, so this is a direct filter — no new model/migration.

  The raw column strings above are deliberate: GORM's default naming would
  turn some struct fields into surprising column names (`OAuthProvider` →
  `o_auth_provider`, for instance). `password_hash`, `user_id` and `id` are
  the real column names as migrated — verify with `\d users` / `\d sessions`
  rather than reading them off the struct.

`AuthServiceGorm.db` is a concrete `*database.GormDB`, not an interface, so
adding methods to `GormDB` is sufficient — there is no interface to widen.

### 3. Backend service: `AuthServiceGorm.ChangePassword`

`internal/backend/services/services.go`, next to
`func (s *AuthServiceGorm) UpdateTimezone`, following that method's exact
shape (session lookup via `s.db.GetUserBySession`, no `classicAuthDisabled`
check since this never creates a new password, only rotates an existing one):

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
        // a session-cleanup error, matching the precedent in Login, where a
        // failing s.db.UpdateLastLogin is logged and swallowed rather than
        // failing the primary action.
        log.Printf("Error invalidating other sessions for user %s: %v", user.ID, err)
    }

    return &authpb.ChangePasswordResponse{Success: true}, nil
}
```

`bcrypt`, `context` and `log` are already in `services.go`'s import block;
no new imports needed. `AuthServiceGorm` embeds
`authpb.UnimplementedAuthServiceServer`, so the new method registers with
the existing server wiring — no registration change.

**What actually gates this RPC.** Two distinct checks, and it matters which
does what:

- `(*Server).authenticate` rejects the call unless it carries either a valid
  session or the shared `NOTIFICATOR_SERVICE_TOKEN`
  (`x-notificator-service-token` metadata). The WebUI's gRPC client presents
  the *service token*, so the interceptor is satisfied for every WebUI-origin
  call regardless of who is logged in. Adding `ChangePassword` to
  `publicMethods` would remove even that — hence the non-goal above.
- The user scoping — "this call may only change *this* user's password" —
  comes entirely from `s.db.GetUserBySession(req.SessionId)` in the handler
  above, plus `authMiddleware.RequireAuth()` on the HTTP route (§5). Do not
  weaken either on the assumption that the interceptor covers it.

### 4. WebUI gRPC client wrapper

`internal/webui/client/backend_client.go`, next to
`func (c *BackendClient) UpdateTimezone`. Unlike `UpdateTimezone` (which
collapses success/failure into a single `error` the handler can't distinguish
from a transport failure), this must preserve the business-level message so
the handler can show "Current password is incorrect" to the user — same shape
as `func (c *BackendClient) Login`'s `(*AuthResult, error)` split between
transport error and business failure:

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

`internal/webui/handlers/profile_handlers.go`, next to
`func UpdateTimezone(c *gin.Context)`, same guard order (auth → bind →
backend-availability → call):

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

Route in `internal/webui/router.go`, inside the existing block opened by
`authProtected := api.Group("/auth")` (already
`authProtected.Use(authMiddleware.RequireAuth())`-gated), alongside the
`/logout` and `/me` registrations:

```go
authProtected.POST("/change-password", handlers.ChangePassword)
```

That group hangs off `api := r.Group("/api/v1")`, so this resolves to
`POST /api/v1/auth/change-password` — exactly the path the issue proposes,
and currently a 404 on a running stack.

### 6. Frontend: modal replacing the `alert()`

All of this lives in `internal/webui/templates/pages/Profile.templ`, in the
`ProfileContent(data ProfileData)` template and the `profilePage()` Alpine
component defined in the `<script>` block at the bottom of the same file.

- Replace the body of `showChangePassword()` — the `alert('Change password
  functionality coming soon!')` quoted in §Problem — with state toggling on
  the *same* `profilePage()` component (`showChangePasswordModal`,
  `oldPassword`, `newPassword`, `confirmPassword`, `savingPassword`,
  `passwordError`, added next to the existing `idCopied` / `userId` state).
  No new mounting site is needed: the page root already carries
  `x-data="profilePage()"`, so the modal markup below is inside its scope.
- Add the modal block as a sibling of the existing content, inside that same
  `x-data` root, following the overlay/card structure of
  `internal/webui/templates/components/MaintenanceModal.templ` (its
  `x-show`-driven `fixed inset-0 ... backdrop-blur-sm` overlay wrapping a
  centered card, with `x-cloak` and `x-transition`) for visual consistency:
  three `<input type="password">` fields (current, new, confirm), inline
  error text bound to `x-text="passwordError"`, Cancel and Submit buttons.
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

  `models.SuccessResponse` / `models.ErrorResponse` serialize to
  `{"success":true,"data":…}` / `{"success":false,"error":…}`, which is what
  the `data.success` / `data.error` reads above expect.

  This mirrors how `internal/webui/templates/components/TimezoneSelector.templ`
  calls `fetch('/api/v1/profile/timezone', { method: 'PUT', ... })` from
  Alpine — same-origin session-cookie auth, no CSRF token wiring (nothing
  else on this page does either).
- **Pass server-rendered Go values through `data-*` attributes, never into a
  JS string literal.** This file already does it correctly: the root element
  carries `data-user-id={ data.User.ID }` and `profilePage()` reads it in
  `init()` via `this.userId = this.$el.dataset.userId`, with `userId: ''` as
  the declared default. That shape exists *because* interpolating a Go value
  directly into a `<script>` string literal gets HTML-entity-escaped by templ;
  it was fixed in `7ec1e5a` and must not be reintroduced. The new modal needs
  no server values at all — its state is client-typed input plus a fetch
  response — so follow the existing pattern if that ever changes.
- Regenerate with `make webui-templates` (never hand-edit `Profile_templ.go`).

## Risks & trade-offs

- **Session invalidation scope**: `DeleteOtherSessions` is a hard
  cross-device logout on every password change. This is the behavior the
  issue's proposed approach asks for ("keep the current one") and matches
  common practice; no opt-out is offered, since offering one would
  reintroduce the exact "leaked password, old session still valid"
  exposure a password-rotation feature exists to close. The mechanism works
  because `authMiddleware.RequireAuth()` revalidates against the backend on
  every request, so deleting the row logs the other browser out on its next
  navigation — it does not wait for the cookie to expire.
- **No rate limiting on `ChangePassword`**: an authenticated attacker with a
  stolen session can brute-force the current password via repeated calls.
  Out of scope here — no other auth RPC (`Login` included) in this codebase
  has rate limiting today, so adding it only to this one RPC would be
  inconsistent scope creep; call out as a follow-up if it needs solving
  properly, backend-wide.
- **Mixed-auth accounts**: `HasPassword()` gates the RPC, but the button in
  `Profile.templ` is gated on `data.User.OAuthProvider == nil`, and that
  field is computed in `ProfilePage` (`profile_handlers.go`) from the
  session's `auth_method` value, not straight off the user row. If an
  account is ever both password- and OAuth-linked and its display badge
  shows OAuth, the button would stay hidden even though the RPC would
  accept a change. Not addressed here — the same display-vs-capability gap
  already exists today independent of this feature, and mixed accounts
  aren't currently produced by any code path in this repo.

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
  file's `setupAuthServiceWithSession` helper creates its user with
  `db.CreateUser("alice", "alice@example.com", "hash")` — a literal
  placeholder string, not a real bcrypt hash, which is fine for
  `UpdateTimezone` but means it **cannot** be reused as-is here: a real
  test needs a user whose stored hash comes from
  `bcrypt.GenerateFromPassword([]byte("initial-pw"), bcrypt.DefaultCost)`
  so `bcrypt.CompareHashAndPassword` has something genuine to check
  against. Add a small local helper (or inline the bcrypt hash +
  `db.CreateUser`) in the new test file rather than changing the shared
  helper's signature — `setupAuthServiceWithSession` has other callers.
  Cases:
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

  No change needed to `auth_interceptor_test.go`: its `twelveRPCs` table is
  a fixed historical list from issue #160, not an exhaustive enumeration of
  non-public RPCs.
- Manual check via `make test` (docker-compose stack), covering the
  issue's acceptance criteria directly. **Register a fresh user through
  `/register` first** — a freshly-booted stack has an empty `users` table
  and no `admin` account (see §Non-goals):
  1. Log in as that classic-auth user, open `/profile`, click **Change
     Password**, submit a wrong current password → inline error shown, no
     page reload, modal stays open.
  2. Submit correct current password + valid new password → modal closes;
     log out; log in with the new password (succeeds) then the old one
     (fails).
  3. Confirm any other open session for that user (e.g. a second browser
     logged in earlier) is now logged out on its next request, while the
     tab used to change the password stays logged in.
  4. Confirm an OAuth-only test account never shows the **Change Password**
     button on `/profile`.

## Anchor check

Every anchor this spec relies on, as a runnable script. Run it before
starting work; all commands must exit 0 (the script exits non-zero and names
the first anchor that moved). This replaces the prose "read in full /
matches" ledger of earlier revisions, which asserted correctness the reader
could not check — and was wrong.

```sh
#!/usr/bin/env bash
set -u
fail=0
a() { # a <label> <file> <literal string>
  grep -qF -- "$3" "$2" || { echo "MISSING ANCHOR: $1 ($2)"; fail=1; }
}

a "rpc UpdateTimezone"        proto/auth.proto                                        "rpc UpdateTimezone(UpdateTimezoneRequest)"
a "UpdateTimezoneResponse"    proto/auth.proto                                        "message UpdateTimezoneResponse"
a "proto recipe"              Makefile                                                "./scripts/generate_proto.sh"
a "generate_proto set -e"     scripts/generate_proto.sh                               "set -e"
a "generate_proto rm -rf"     scripts/generate_proto.sh                               "rm -rf internal/backend/proto/auth"
a "UpdateUserTimezone"        internal/backend/database/gorm_db.go                    "func (gdb *GormDB) UpdateUserTimezone("
a "DeleteSession"             internal/backend/database/gorm_db.go                    "func (gdb *GormDB) DeleteSession("
a "CreateUser"                internal/backend/database/gorm_db.go                    "func (gdb *GormDB) CreateUser("
a "GetUserBySession"          internal/backend/database/gorm_db.go                    "func (gdb *GormDB) GetUserBySession("
a "HasPassword"               internal/backend/models/models.go                       "func (u *User) HasPassword()"
a "Session struct"            internal/backend/models/models.go                       "type Session struct"
a "classicAuthDisabled"       internal/backend/services/services.go                   "func (s *AuthServiceGorm) classicAuthDisabled()"
a "svc Login"                 internal/backend/services/services.go                   "func (s *AuthServiceGorm) Login("
a "svc UpdateTimezone"        internal/backend/services/services.go                   "func (s *AuthServiceGorm) UpdateTimezone("
a "4-char minimum"            internal/backend/services/services.go                   "Password must be at least 4 characters long"
a "UpdateLastLogin precedent" internal/backend/services/services.go                   "s.db.UpdateLastLogin(user.ID)"
a "publicMethods"             internal/backend/auth_interceptor.go                    "var publicMethods = map[string]bool{"
a "authenticate"              internal/backend/auth_interceptor.go                    "func (s *Server) authenticate("
a "service token metadata"    internal/backend/auth_interceptor.go                    "serviceTokenMetadataKey"
a "client Login"              internal/webui/client/backend_client.go                 "func (c *BackendClient) Login("
a "client UpdateTimezone"     internal/webui/client/backend_client.go                 "func (c *BackendClient) UpdateTimezone("
a "handler UpdateTimezone"    internal/webui/handlers/profile_handlers.go             "func UpdateTimezone(c *gin.Context)"
a "api group"                 internal/webui/router.go                                'api := r.Group("/api/v1")'
a "authProtected group"       internal/webui/router.go                                'authProtected := api.Group("/auth")'
a "RequireAuth on group"      internal/webui/router.go                                "authProtected.Use(authMiddleware.RequireAuth())"
a "profile x-data root"       internal/webui/templates/pages/Profile.templ            'x-data="profilePage()" data-user-id={ data.User.ID }'
a "OAuth-gated button"        internal/webui/templates/pages/Profile.templ            "if data.User.OAuthProvider == nil {"
a "profilePage component"     internal/webui/templates/pages/Profile.templ            "function profilePage() {"
a "dead alert"                internal/webui/templates/pages/Profile.templ            "alert('Change password functionality coming soon!');"
a "dataset.userId pattern"    internal/webui/templates/pages/Profile.templ            "this.userId = this.\$el.dataset.userId;"
a "modal overlay reference"   internal/webui/templates/components/MaintenanceModal.templ "fixed inset-0"
a "alpine fetch reference"    internal/webui/templates/components/TimezoneSelector.templ "await fetch('/api/v1/profile/timezone', {"
a "non-bcrypt test helper"    internal/backend/services/update_timezone_test.go       'db.CreateUser("alice", "alice@example.com", "hash")'
a "twelveRPCs table"          internal/backend/auth_interceptor_test.go               "func twelveRPCs("

# Absence checks — these must NOT exist yet.
! grep -rqF "ChangePassword" proto/auth.proto || { echo "UNEXPECTED: ChangePassword already in auth.proto"; fail=1; }
! grep -rqF "change-password" internal/webui/router.go || { echo "UNEXPECTED: route already registered"; fail=1; }
# No admin bootstrap account is seeded anywhere (see Non-goals).
! grep -rIqE 'CreateUser\("admin"|seedAdmin|bootstrapAdmin' --include='*.go' . || { echo "UNEXPECTED: an admin seeding path exists"; fail=1; }

exit $fail
```
