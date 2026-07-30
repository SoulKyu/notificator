# Spec: enforce registration/login gating in the backend and close the OAuth-only fail-open

- Issue: [SoulKyu/notificator#64](https://github.com/SoulKyu/notificator/issues/64)
- Date: 2026-07-30
- Status: planned

## Problem

Self-service account creation cannot be turned off, and the only check meant to
suppress it on OAuth-only deployments fails open.

`AuthServiceGorm.Register` (`internal/backend/services/services.go:42-87`) validates
only that `username`/`password` are present and the password is ≥ 4 characters, then
creates the user unconditionally. `AuthServiceGorm.Login` (`services.go:94-...`) has no
gate at all. Neither method knows about `OAuthPortalConfig.DisableClassicAuth`, even
though the service already holds `s.oauthService` and calls
`s.oauthService.GetConfig()` from `GetOAuthConfig` (`services.go:1517-1568`). There is
no config flag anywhere that can disable registration.

The only gate lives in the WebUI and fails open. `handlers.Register`
(`internal/webui/handlers/handlers.go:164-168`) and `handlers.Login` (`handlers.go:90-94`)
both do:

```go
oauthConfig := getOAuthConfig(c)
if oauthConfig != nil && oauthConfig.DisableClassicAuth {
    // reject
}
```

`getOAuthConfig` (`handlers.go:53-88`) returns `nil` whenever `backendClient == nil`,
`!backendClient.IsConnected()`, the `GetOAuthConfig` RPC errors, or the RPC's own
10s-deadline context (`backend_client.go:912`) is exceeded — and `nil` short-circuits
the `&&`, so the request proceeds as if classic auth were allowed. This RPC round-trips
to the backend on every login/register/login-page/register-page/playground-page
request with no caching, so a slow backend or rolling restart makes the timeout path
realistic, not theoretical. `Login.templ` (`internal/webui/templates/pages/Login.templ:38,189`)
has the identical `oauthConfig == nil || !oauthConfig.DisableClassicAuth` shape for
showing the password form and the "Create an account" link.

Net effect: on an OAuth-only deployment (`OAUTH_ENABLED=true`,
`OAUTH_DISABLE_CLASSIC_AUTH=true`, the default when OAuth is on —
`config/oauth_config.go:63`), a transient `GetOAuthConfig` failure lets an attacker
register and log in with a password account and reach every
`authMiddleware.RequireAuth()`-protected route. On a deployment **without** OAuth there
is no gate at all, by construction — nothing to fail open from.

## Goals

1. `AuthServiceGorm.Register` and `AuthServiceGorm.Login` reject username/password
   auth themselves whenever `OAuthPortalConfig.Enabled && DisableClassicAuth`, so the
   rule holds regardless of which caller reaches the RPC (direct gRPC client, WebUI
   with a healthy backend, WebUI mid-outage).
2. A new `backend.allow_registration` config flag (env
   `NOTIFICATOR_BACKEND_ALLOW_REGISTRATION`, default `true`) lets non-OAuth deployments
   close public sign-up without touching OAuth config. It gates `Register` only —
   existing accounts can still `Login`.
3. The WebUI's own gate fails **closed**: when it cannot positively determine that
   classic registration/login is allowed, it treats the request as disallowed instead
   of as allowed. A short-lived cache of the OAuth/registration state removes the
   per-request RPC that currently makes the timeout window realistic, so failing
   closed doesn't turn a single slow RPC into a login outage for legitimate users.
4. The login page stops advertising "Register" whenever registration is actually
   disabled — by OAuth-only mode, by `allow_registration=false`, or by an unknown
   backend state — consistent with whatever the server would actually do with the
   request.
5. The new flag is documented in `ENVIRONMENT_VARIABLES.md` and
   `charts/notificator-app/values.yaml`.

## Non-goals

- No invite/approval workflow for registration — this is an on/off switch, not a
  request queue.
- No change to session/login behavior for already-registered users when
  `allow_registration=false` — that flag only blocks *new* account creation.
- No change to the 4-character password minimum (`services.go:53`) — out of scope for
  this issue, which is about *whether* registration happens, not password strength.
- No general-purpose config cache framework — the WebUI-side cache is a single
  package-level TTL cache for this one RPC result, not a reusable abstraction.

## Approach

### 1. Proto: expose a single "is registration actually allowed" bit

`GetOAuthConfigResponse` (`proto/auth.proto:185-189`) currently returns `enabled`,
`disable_classic_auth`, `providers`. Add a fourth field:

```proto
message GetOAuthConfigResponse {
  bool enabled = 1;
  bool disable_classic_auth = 2;
  repeated OAuthProvider providers = 3;
  bool registration_allowed = 4;
}
```

Regenerate with `make proto` (`scripts/generate_proto.sh`, writes both `proto/*.pb.go`
and `internal/backend/proto/auth/*.pb.go` — never hand-edit either `.pb.go`).

`registration_allowed` is computed once, backend-side, from both gates (OAuth-only
*and* the new flag), so the WebUI never has to reimplement the combination logic and
can't drift from what `Register` will actually decide.

### 2. Config: `backend.allow_registration`

`config/config.go`:
- `BackendConfig` (line 58) gains `AllowRegistration bool \`json:"allow_registration"\``.
- Default `true` in `DefaultConfig()`'s `Backend: BackendConfig{...}` literal (line
  ~207), next to the existing `Enabled`/`GRPCListen`/etc. fields — `true` keeps every
  existing deployment working unchanged.
- `viper.SetDefault("backend.allow_registration", cfg.Backend.AllowRegistration)` next
  to the other `backend.*` defaults (line 441-444).
- `viper.BindEnv("backend.allow_registration", "NOTIFICATOR_BACKEND_ALLOW_REGISTRATION")`
  next to the other explicit `backend.*` bindings (line 630-636) — this file binds
  every env var explicitly (no `viper.AutomaticEnv()`), so a new field needs its own
  `BindEnv` line or it silently won't read from the environment.

### 3. Backend: `AuthServiceGorm` becomes the authoritative gate

`services.go`:
- `NewAuthServiceGorm(db *database.GormDB, oauthService *OAuthService)` (line 35) gains
  a third parameter, `allowRegistration bool`, stored as a new `allowRegistration bool`
  field on `AuthServiceGorm`. Update the one production call site
  (`internal/backend/server.go:130`, `services.NewAuthServiceGorm(s.db, s.oauthService,
  s.config.Backend.AllowRegistration)`) and the one test helper
  (`internal/backend/services/update_timezone_test.go:34`, pass `true`).
- Add an unexported helper reused by both `Register` and `Login`:
  ```go
  func (s *AuthServiceGorm) classicAuthDisabled() bool {
      if s.oauthService == nil {
          return false
      }
      cfg := s.oauthService.GetConfig()
      return cfg != nil && cfg.Enabled && cfg.DisableClassicAuth
  }
  ```
- `Register` (line 42): first check, before the existing username/password
  validation:
  ```go
  if s.classicAuthDisabled() {
      return &authpb.RegisterResponse{Success: false,
          Message: "Username/password registration is disabled. Use OAuth."}, nil
  }
  if !s.allowRegistration {
      return &authpb.RegisterResponse{Success: false,
          Message: "Registration is currently disabled."}, nil
  }
  ```
- `Login` (line 94): first check, before the existing username/password validation:
  ```go
  if s.classicAuthDisabled() {
      return &authpb.LoginResponse{Success: false,
          Message: "Username/password authentication is disabled. Use OAuth."}, nil
  }
  ```
  (No `allowRegistration` check here — disabling *new* registration must not lock out
  existing accounts.)
- `GetOAuthConfig` (line 1517): compute the new response field once, right before the
  final `return`:
  ```go
  registrationAllowed := s.allowRegistration && !s.classicAuthDisabled()
  ```
  and add `RegistrationAllowed: registrationAllowed` to both early-return branches
  (`oauthService == nil` and `!config.Enabled`, where `classicAuthDisabled()` is
  trivially `false`) and the final populated response.

This makes `Register`/`Login` reject the request no matter which path reaches the RPC
— direct `AuthService/Register` gRPC call, WebUI with a healthy backend, or WebUI
mid-outage. The WebUI-level gate below becomes a fast-path UX optimization, not the
security boundary; a bug or regression there can no longer create an account.

### 4. WebUI: fail-closed gate with a short-lived cache

`internal/webui/handlers/handlers.go`:
- Replace `getOAuthConfig`'s "return `nil` on any failure" shape with a small
  package-level cache holding the last successful `*pages.OAuthConfig` (now including
  `RegistrationAllowed`) plus a fetch timestamp and a mutex, TTL ~15-30s (shorter than
  the RPC's own 10s deadline, long enough to absorb one restart/blip):
  ```go
  var (
      oauthConfigMu    sync.RWMutex
      oauthConfigCache *pages.OAuthConfig
      oauthConfigAt    time.Time
  )

  func getOAuthConfig(c *gin.Context) *pages.OAuthConfig {
      if cfg, ok := cachedOAuthConfig(); ok {
          return cfg
      }
      if backendClient == nil || !backendClient.IsConnected() {
          return failClosedOAuthConfig()
      }
      raw, err := backendClient.GetOAuthConfig()
      if err != nil {
          fmt.Printf("Failed to get OAuth config: %v\n", err)
          return failClosedOAuthConfig()
      }
      cfg := parseOAuthConfig(raw) // existing field-mapping logic from today's getOAuthConfig
      storeOAuthConfigCache(cfg)
      return cfg
  }
  ```
  `failClosedOAuthConfig()` returns `&pages.OAuthConfig{DisableClassicAuth: true,
  RegistrationAllowed: false}` — the safe default when the state truly can't be
  determined *and* there's no usable cached value yet (e.g. cold start). Once a real
  config has been fetched once, transient RPC blips are absorbed by the cache instead
  of blocking real logins, which is what makes failing closed acceptable here instead
  of a self-inflicted outage.
- `Login` (line 90) and `Register` (line 164) keep their existing pre-check shape but
  drop the `oauthConfig != nil &&` guard, since `getOAuthConfig` now always returns a
  non-nil, meaningfully-defaulted config:
  ```go
  oauthConfig := getOAuthConfig(c)
  if oauthConfig.DisableClassicAuth { // Login
  if !oauthConfig.RegistrationAllowed { // Register
  ```
  The backend call underneath still enforces the same rule independently (step 3), so
  this is defense-in-depth, not the only check.

### 5. WebUI: register link and login-page rendering

- `pages.OAuthConfig` (`Login.templ:6-10`) gains `RegistrationAllowed bool`.
- `Login.templ:189` (`<!-- Register link -->`) and `handlers.RegisterPage`
  (`handlers.go:416-...`, redirect-to-`/login` guard) switch from
  `oauthConfig == nil || !oauthConfig.DisableClassicAuth` to
  `oauthConfig.RegistrationAllowed` — this one field already folds in both the
  OAuth-only gate and the `allow_registration` flag, computed backend-side (step 3),
  so the template doesn't need to know about two separate flags.
- The classic-login-form visibility (`Login.templ:38`) and the OAuth-only message
  (`Login.templ:104`) keep using `DisableClassicAuth` as-is — that's specifically
  about *login*, which `allow_registration=false` must not affect.
- After editing `Login.templ` (and `Register.templ` if it duplicates the same
  condition), regenerate with `make webui-templates` — never hand-edit `*_templ.go`.

### 6. Docs

- `ENVIRONMENT_VARIABLES.md`: add `NOTIFICATOR_BACKEND_ALLOW_REGISTRATION` under
  "### Server Settings" (next to `NOTIFICATOR_BACKEND_ENABLED` at line 22), documented
  as "Allow new user self-registration via username/password (default: true)".
- `charts/notificator-app/values.yaml`: add a commented example under the backend
  `env:` block (line 52-57, next to the `NOTIFICATOR_BACKEND_*` examples):
  `# NOTIFICATOR_BACKEND_ALLOW_REGISTRATION: "false"`.

### Files touched

- `proto/auth.proto`, regenerated `proto/auth.pb.go`, `proto/auth_grpc.pb.go`,
  `internal/backend/proto/auth/auth.pb.go`, `internal/backend/proto/auth/auth_grpc.pb.go`.
- `config/config.go` — `BackendConfig.AllowRegistration`, default, `BindEnv`.
- `internal/backend/services/services.go` — `NewAuthServiceGorm`, `classicAuthDisabled`,
  `Register`, `Login`, `GetOAuthConfig`.
- `internal/backend/services/services_test.go` (new or existing) — unit tests below.
- `internal/backend/server.go` — updated `NewAuthServiceGorm` call.
- `internal/backend/services/update_timezone_test.go` — updated test helper call.
- `internal/webui/client/backend_client.go` — `GetOAuthConfig()` map gains
  `registration_allowed`.
- `internal/webui/handlers/handlers.go` — `getOAuthConfig` cache + fail-closed default,
  `Login`, `Register`, `RegisterPage`.
- `internal/webui/handlers/handlers_test.go` (new or existing) — unit tests below.
- `internal/webui/templates/pages/Login.templ` (+ regenerated `Login_templ.go`) —
  `OAuthConfig.RegistrationAllowed`, register-link condition.
- `internal/webui/templates/pages/Register.templ` (+ regenerated `Register_templ.go`)
  if it has its own copy of the same condition.
- `ENVIRONMENT_VARIABLES.md`, `charts/notificator-app/values.yaml`.

## Risks & trade-offs

- **Two gates checked in two places (backend authoritative, WebUI fast-path) can drift
  in message text.** Mitigated by keeping the WebUI check as a UX-only optimization
  that never has to be *correct* for security — the backend rejects the RPC either
  way, so drift produces a wrong error message, not a security hole.
- **The WebUI cache means a config change (flipping `allow_registration` or OAuth
  settings) takes up to the TTL to take effect on the WebUI's own pre-check and on
  page rendering.** The backend enforcement (step 3) is not cached and applies
  immediately: no cache window during which registration is actually possible, only a
  window during which the WebUI might show a stale "Register" link that then 400s on
  submit. Acceptable given the security boundary moved to the backend.
- **Proto change is additive (new field, number 4) so existing clients that don't read
  it are unaffected** — no wire-format break, no need to bump anything else in
  `auth.proto`.
- **`classicAuthDisabled()` reads `s.oauthService.GetConfig()` on every `Register`/
  `Login` call.** This mirrors what `GetOAuthConfig` already does per-RPC today; no new
  I/O is introduced (`OAuthService.GetConfig()` returns an in-memory struct, it doesn't
  hit the DB or network).
- **`make proto` requires `protoc` + the Go plugins to be installed locally**, same
  precondition as any other proto change in this repo — not new to this issue.

## Validation

- `go build ./...` and `make webui-templates && go build ./...` both pass after the
  proto regen and templ regen.
- New/updated Go tests in `internal/backend/services/services_test.go`:
  - `Register` rejects when `oauthService` config has `Enabled: true,
    DisableClassicAuth: true` — no user row created (`db.GetUserByUsername` still
    errors afterward).
  - `Register` rejects when `allowRegistration: false` and OAuth is off entirely — no
    user row created.
  - `Register` succeeds when both gates are open (regression check — existing
    passing-case behavior unchanged).
  - `Login` rejects for an existing user when `DisableClassicAuth: true`, regardless of
    `allowRegistration`.
  - `Login` succeeds for an existing user when `allowRegistration: false` (registration
    being closed must not lock out current accounts).
  - `GetOAuthConfig` returns `RegistrationAllowed: false` in exactly the two blocking
    cases above, `true` otherwise.
- New/updated Go tests in `internal/webui/handlers/handlers_test.go` (or equivalent):
  - `getOAuthConfig` returns a fail-closed config (`DisableClassicAuth: true,
    RegistrationAllowed: false`) when `backendClient.GetOAuthConfig()` errors and the
    cache is empty.
  - `getOAuthConfig` serves the last cached config (not fail-closed) when a fresh fetch
    errors but a not-yet-expired cached value exists.
- Manual check via `make test` (docker-compose stack), covering the issue's acceptance
  criteria directly:
  1. `OAUTH_ENABLED=true`, `OAUTH_DISABLE_CLASSIC_AUTH=true`: direct `grpcurl` call to
     `AuthService/Register` is rejected; no row appears in the `users` table.
  2. Same config: force `GetOAuthConfig` to error (e.g. stop the backend gRPC listener
     briefly while the WebUI cache is cold) and confirm `POST /api/v1/auth/register`
     still 4xxs with no user created.
  3. Same for `POST /api/v1/auth/login` against an unknown OAuth-config state — no
     password login succeeds.
  4. `OAUTH_ENABLED=false`, `NOTIFICATOR_BACKEND_ALLOW_REGISTRATION=false`: registration
     is rejected with a clear message, existing users can still log in.
  5. Login page (`/login`) does not render "Create an account" in any of the above
     disabled states, and does render it when both gates are open.
  6. `ENVIRONMENT_VARIABLES.md` and `charts/notificator-app/values.yaml` document the
     new variable.
