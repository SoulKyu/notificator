# Restrict `RemoveAllResolvedAlerts` to admins

- Issue: [SoulKyu/notificator#68](https://github.com/SoulKyu/notificator/issues/68)
- Follow-up to: #62 (session layer), #65 (session validation shipped)
- Base branch: `main`

## Problem

`AlertServiceGorm.RemoveAllResolvedAlerts` (`internal/backend/services/services.go:1197-1232`)
validates the session and then authorizes **any** authenticated user to hard-delete every
resolved alert for the whole team:

```go
user, err := s.db.GetUserBySession(req.SessionId)   // services.go:1205
// ...no authorization check...
removedCount, err := s.db.RemoveAllResolvedAlerts() // services.go:1217
```

The only admin gate today lives in the WebUI handler
(`internal/webui/handlers/dashboard_handlers.go:1878` → `canImpersonate(c)`), which is UX
only: the gRPC method is reachable directly by anyone holding a valid session.

Impact: one compromised or careless non-admin account irreversibly destroys the resolved-alert
history (audit trail, MTTR data, incident records).

## Goals

- Deny `RemoveAllResolvedAlerts` for a valid **non-admin** session, with an explicit message
  and zero deletion.
- Allow it for a valid **admin** session, unchanged behaviour.
- Reuse a config-driven admin notion; no DB schema change, no new `User.IsAdmin` column.
- Preserve the session-validation behaviour shipped in #65.
- Document the config/env surface.

### Non-goals

- Generalised RBAC / permission framework (YAGNI — one destructive endpoint needs gating).
- Persisting OAuth roles on `User` or wiring the OAuth group mapping into authorization.
- Changing the WebUI gate semantics beyond keeping it consistent with the backend.
- Gating other RPCs (out of issue scope).

## Admin authority — decision

Two candidate mechanisms exist. Neither is used as-is.

| Candidate | State in code | Verdict |
|---|---|---|
| `config.AdminConfig.CanImpersonate` (`config/config.go:30-41`), fed by `NOTIFICATOR_ADMIN_IMPERSONATION_ALLOWED_USERS` | Live, consumed by `internal/webui/handlers/impersonation_handlers.go:34` | Rejected as *the* authority — impersonation is a distinct capability; overloading it silently is exactly what the issue forbids. Kept as a compatibility fallback (below). |
| OAuth group → `administrator` role (`config/oauth_config.go:222+`) | **Dead**: the `administrator` string only appears in default `GroupMapping` tables; no Go code reads it, no role is persisted on `User`, and it is unavailable for local (non-OAuth) accounts | Rejected — making it authoritative means building role persistence + propagation, far beyond this fix. |

**Chosen:** a dedicated, explicit admin list on the existing `config.AdminConfig`.

```go
type AdminConfig struct {
    Users                     []string `json:"users"`
    ImpersonationAllowedUsers []string `json:"impersonation_allowed_users"`
}

// IsAdmin reports whether a username or email is an administrator.
// Falls back to the impersonation list when no explicit admin list is configured,
// to preserve behaviour for deployments that already use it as their admin list.
func (a *AdminConfig) IsAdmin(usernameOrEmail string) bool
```

Rationale: matches the existing config pattern, works for both local and OAuth accounts,
zero schema change, one env var to operate.

**Fallback, explicit not silent:** if `admin.users` is empty, `IsAdmin` consults
`ImpersonationAllowedUsers`. Today the WebUI already gates this exact button on
`canImpersonate`, so the fallback keeps current deployments working. If both lists are empty,
nobody is admin and the endpoint is **fail-closed** — deliberate for a destructive, irreversible
team-wide action.

## Approach

1. **`config/config.go`**
   - Add `Users []string` (`json:"users"`) to `AdminConfig`.
   - Add `IsAdmin(usernameOrEmail string) bool` with the documented fallback (case-insensitive,
     `strings.EqualFold`, mirroring `CanImpersonate`).
   - Parse `NOTIFICATOR_ADMIN_USERS` (comma-separated, trimmed) next to the existing
     impersonation parsing (`config.go:326-336`).
   - Add `viper.SetDefault("admin.users", []string{})` and
     `viper.BindEnv("admin.users", "NOTIFICATOR_ADMIN_USERS")` (`config.go:566-568`).

2. **`internal/backend/services/services.go`**
   - Add an `adminConfig config.AdminConfig` field to `AlertServiceGorm` (`services.go:430`)
     and a parameter to `NewAlertServiceGorm` (`services.go:437`). Pass the value, not
     `*config.Config`, to keep the service decoupled.
   - In `RemoveAllResolvedAlerts`, right after `GetUserBySession` succeeds and before
     `s.db.RemoveAllResolvedAlerts()`:

     ```go
     if !s.adminConfig.IsAdmin(user.Username) && !s.adminConfig.IsAdmin(user.Email) {
         log.Printf("RemoveAllResolvedAlerts: denied for non-admin user %s (ID: %s)", user.Username, user.ID)
         return &alertpb.RemoveAllResolvedAlertsResponse{
             Success: false,
             Message: "Admin rights required",
         }, nil
     }
     ```

   - Session error paths (`Session ID is required`, `Invalid session`) untouched.

3. **`internal/backend/server.go:127`** — pass `s.config.Admin` to `NewAlertServiceGorm`.

4. **WebUI** — keep `dashboard_handlers.go:1878` as the UX gate but switch it from
   `canImpersonate(c)` to an admin check based on `AdminConfig.IsAdmin`, so the button is
   hidden/refused for the same population the backend denies. Backend remains the trust
   boundary; the WebUI check only avoids a confusing 500-ish round trip.

5. **Documentation**
   - `ENVIRONMENT_VARIABLES.md`: add `NOTIFICATOR_ADMIN_USERS` (and the pre-existing
     `NOTIFICATOR_ADMIN_IMPERSONATION_ALLOWED_USERS`, currently undocumented) with the
     fallback and fail-closed semantics.
   - `.env.example`: commented example entry.
   - `charts/notificator-app/values.yaml` + `README.md`: expose the var in the backend and
     webui env blocks alongside the existing OAuth vars.

## Risks

| Risk | Mitigation |
|---|---|
| Deployments with neither list set lose the "remove all resolved alerts" feature | Intended fail-closed default; called out in the PR description and env docs, one env var restores it |
| Fallback on the impersonation list conflates two capabilities | Explicit, documented, and only active when `admin.users` is unset; operators migrate by setting `NOTIFICATOR_ADMIN_USERS` |
| Username/email collision between an OAuth identity and a local account could grant admin | Same exposure as the existing impersonation gate; no regression. Operators should list emails when OAuth is enabled |
| `NewAlertServiceGorm` signature change breaks callers | Only 2 call sites (`server.go:127`, `remove_resolved_alerts_test.go:28`); `go build ./...` catches any miss |

## Validation

- `go build ./...`
- `go test ./config/... ./internal/backend/services/...`
- New/updated tests in `internal/backend/services/remove_resolved_alerts_test.go`:
  - existing `RejectsMissingSession` / `RejectsInvalidSession` unchanged (proves #65 preserved);
  - `TestRemoveAllResolvedAlerts_ValidSessionDeletes` updated to configure the user as admin;
  - **new** `TestRemoveAllResolvedAlerts_RejectsNonAdmin`: valid session, empty admin config →
    `Success=false`, `Message="Admin rights required"`, resolved-alert count unchanged;
  - **new** `TestRemoveAllResolvedAlerts_AllowsAdminByEmail`: admin listed by email → deletion succeeds.
- Config unit test for `AdminConfig.IsAdmin`: explicit list, case-insensitive match, fallback to
  the impersonation list, and deny-all when both lists are empty.
- Manual smoke via `make test` (docker-compose): non-admin session gets the denial, admin session
  deletes as before.

## Acceptance criteria mapping

| Criterion | Covered by |
|---|---|
| Non-admin session denied, no deletion, explicit message | Step 2 + `RejectsNonAdmin` test |
| Admin session succeeds and deletes | Step 1-3 + `ValidSessionDeletes` / `AllowsAdminByEmail` |
| Admin determination reuses a config-driven mechanism, documented | "Admin authority — decision" section, restated in the PR body |
| Env/config surface documented | Step 5 |
| #65 session validation preserved | Untouched error paths + existing tests kept green |
