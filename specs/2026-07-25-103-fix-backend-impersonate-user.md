# Spec: enforce the impersonation allowlist in the backend

- Issue: [SoulKyu/notificator#103](https://github.com/SoulKyu/notificator/issues/103)
- Date: 2026-07-25
- Status: planned

## Problem

Impersonation is authorized in exactly one place — the WebUI
(`canImpersonate` in `internal/webui/handlers/impersonation_handlers.go:23`).
Eleven gRPC handlers accept an `impersonate_user_id` field, validate the
`session_id`, and then trust the field outright:

```go
// internal/backend/services/services.go:912
targetUserID := user.ID
if req.ImpersonateUserId != "" {
    targetUserID = req.ImpersonateUserId   // no authorization check
}
```

The gRPC server is reachable outside the WebUI (`docker-compose.yml` publishes
`50051:50051`, the Helm chart exposes a ClusterIP Service) and `AuthService` is
registered on the same server, so any user with ordinary credentials can obtain
a `session_id` over gRPC and then read or overwrite another user's data.
`SaveUserColorPreferences` is the worst case: `GormDB.SaveUserColorPreferences`
(`internal/backend/database/gorm_db.go:586`) starts with an unscoped delete of
every row for the target user, so one call with an empty preference list wipes
the victim's colour configuration.

Affected call sites:

| RPC | Location | Effect |
|---|---|---|
| `GetUserColorPreferences` | `services.go:913` | read |
| `SaveUserColorPreferences` | `services.go:977` | destructive write |
| `DeleteUserColorPreference` | `services.go:1052` | write |
| `GetUserHiddenAlerts` | `services.go:1776` | read |
| `GetUserHiddenRules` | `services.go:1961` | read |
| `GetFilterPresets` | `services.go:2453` | read |
| `GetStatisticsViews` | `statistics_grpc_service.go:822` | read |
| `SaveStatisticsView` | `statistics_grpc_service.go:876` | write |
| `UpdateStatisticsView` | `statistics_grpc_service.go:945` | write |
| `DeleteStatisticsView` | `statistics_grpc_service.go:1026` | write |
| `SetDefaultStatisticsView` | `statistics_grpc_service.go:1067` | write |

## Goals

- The backend decides whether impersonation is allowed; `impersonate_user_id`
  stops being a trusted input.
- One shared helper used by all eleven call sites, so the twelfth RPC cannot
  quietly forget the check.
- Allowlisted admins keep working; the existing WebUI impersonation flow is
  unchanged end to end.
- `impersonate_user_id` equal to the session's own user ID stays valid.
- Denied attempts are logged with requesting user, target user and RPC name.

## Non-goals

- No gRPC auth interceptor (the broader gap noted in
  `openwiki/quickstart.md:93`) — that is a separate, larger change.
- No signed impersonation assertion from the WebUI (the issue's alternative);
  heavier, and unnecessary once the backend owns the decision.
- No new config source: `Admin.ImpersonationAllowedUsers` already lives in the
  shared `config.Config` (`config/config.go:30`), loaded by the backend
  process via `config.LoadConfigWithViper()` in `cmd/backend.go:43`.
- RPCs that already ignore `impersonate_user_id` server-side (`HideAlert`,
  `UnhideAlert`, `ClearAllHiddenAlerts`, `SaveHiddenRule`, `RemoveHiddenRule`,
  `SaveFilterPreset`, `UpdateFilterPreset`, …) are left alone. The WebUI client
  sends the field, the backend writes to the session user — a real behavioural
  inconsistency, but not a security hole, and out of scope here.

## Approach

### 1. Give the two services the admin allowlist

`config.AdminConfig` is already part of the config the backend loads — nothing
new to plumb from disk or env. Pass it into the constructors:

```go
// internal/backend/services/services.go
func NewAlertServiceGorm(db *database.GormDB, admin config.AdminConfig) *AlertServiceGorm

// internal/backend/services/statistics_grpc_service.go
func NewStatisticsServiceGorm(db *database.GormDB, admin config.AdminConfig) *StatisticsServiceGorm
```

Callers: `internal/backend/server.go:127-128` passes `s.config.Admin`; the one
existing test helper (`remove_resolved_alerts_test.go:28`) passes
`config.AdminConfig{}`. A constructor argument (not
a `SetAdminConfig` setter like `SetWorkerPool`) is deliberate — it makes the
allowlist non-optional at construction so a new service cannot be wired up
with impersonation silently unchecked.

### 2. One helper

New file `internal/backend/services/impersonation.go`:

```go
// resolveTargetUserID returns the user ID a request should operate on.
// Impersonating another user requires the session user to be on the
// admin allowlist; anything else is denied.
func resolveTargetUserID(admin config.AdminConfig, user *models.User, impersonateUserID, rpc string) (string, error) {
	if impersonateUserID == "" || impersonateUserID == user.ID {
		return user.ID, nil
	}
	if admin.CanImpersonate(user.Username) || admin.CanImpersonate(user.Email) {
		return impersonateUserID, nil
	}
	log.Printf("🚫 %s: user %s (%s) denied impersonation of user %s", rpc, user.ID, user.Username, impersonateUserID)
	return "", errImpersonationDenied
}
```

Fail-closed: an empty allowlist means nobody may impersonate, which matches
today's WebUI behaviour.

### 3. Rewrite the eleven call sites

Each existing three-line block becomes:

```go
targetUserID, err := resolveTargetUserID(s.admin, user, req.ImpersonateUserId, "SaveUserColorPreferences")
if err != nil {
	return &alertpb.SaveUserColorPreferencesResponse{
		Success: false,
		Message: "Impersonation not permitted",
	}, nil
}
```

Denials are reported in-band (`Success: false` + `Message`) rather than as a
gRPC `PermissionDenied` status, matching how every one of these handlers
already reports "Invalid session". That keeps the WebUI client and handler
error paths untouched — a status error would surface as a transport failure
and change how each of the eleven WebUI call sites behaves.

### Files touched

- `internal/backend/services/impersonation.go` — new, the helper + sentinel error.
- `internal/backend/services/services.go` — constructor arg, `admin` field, six call sites.
- `internal/backend/services/statistics_grpc_service.go` — constructor arg, `admin` field, five call sites.
- `internal/backend/server.go` — pass `s.config.Admin` to both constructors.
- `internal/backend/services/remove_resolved_alerts_test.go:28` — constructor signature (the only existing test caller).
- `internal/backend/services/impersonation_test.go` — new tests (below).
- `docker-compose.yml` — set `NOTIFICATOR_ADMIN_IMPERSONATION_ALLOWED_USERS` on
  the **backend** service (it is currently set on neither), so `make test` can
  exercise the admin path.
- `openwiki/configuration.md`, `openwiki/backend.md` — note that the allowlist
  is now enforced backend-side and **must be configured on the backend
  process**, not only the WebUI.

## Risks & trade-offs

- **Operational break**: an existing deployment that sets
  `NOTIFICATOR_ADMIN_IMPERSONATION_ALLOWED_USERS` only on the WebUI pod loses
  impersonation after upgrade — the backend allowlist is empty and denies
  everything. This is the correct fail-closed direction, but it needs to be
  called out in the PR description and the wiki. The Helm chart needs no
  template change (`backend.env` is a free-form map), only documentation.
- **Allowlist drift**: WebUI and backend each read their own copy of the
  config. If they disagree, the backend wins and the UI shows an impersonation
  banner over failing requests. Acceptable; a single enforcement point in the
  backend would be the follow-up, at the cost of removing the WebUI's early
  rejection.
- **Silent denial shape**: `Success: false` looks like any other failure to the
  WebUI. The server log line is the audit trail, per the acceptance criteria.
- **Constructor churn**: two signatures change, five call sites total. Cheaper
  than a setter that can be forgotten.

## Validation

- `go build ./... && go test ./internal/backend/...` passes.
- New `internal/backend/services/impersonation_test.go`, sqlite-backed like
  `remove_resolved_alerts_test.go`, covering one read and one destructive
  write, both directions:
  - `SaveUserColorPreferences` with a non-allowlisted session and another
    user's `impersonate_user_id` → `Success: false`, and the victim's
    preferences are still present in the DB afterwards (this is the
    unscoped-delete path, so the assertion has to be on the stored rows).
  - Same call from an allowlisted admin → `Success: true`, target's
    preferences replaced.
  - `GetUserColorPreferences` with a non-allowlisted session impersonating →
    `Success: false`, no preferences leaked in the response.
  - `impersonate_user_id` equal to the session user's own ID → succeeds for a
    non-admin.
- Manual check via `make test` (docker-compose stack) with
  `NOTIFICATOR_ADMIN_IMPERSONATION_ALLOWED_USERS` set on both services:
  - Admin impersonation from the WebUI still loads and saves the target's
    colours, hidden alerts, filter presets and statistics views.
  - `grpcurl` against the published `50051` with a non-admin session and a
    foreign `impersonate_user_id` is rejected on `SaveUserColorPreferences`,
    and the backend logs the denial with both user IDs and the RPC name.
