# Spec: enforce registration/login gating in the backend and close the OAuth-only fail-open

- Issue: [SoulKyu/notificator#64](https://github.com/SoulKyu/notificator/issues/64)
- Date: 2026-07-30
- Status: planned

## Problem

Self-service account creation cannot be turned off, and the only check meant to
suppress it on OAuth-only deployments fails open.

`AuthServiceGorm.Register` (`internal/backend/services/services.go:44-93`) validates
only that `username`/`password` are present and the password is ≥ 4 characters
(`:52`), then creates the user unconditionally. `AuthServiceGorm.Login`
(`services.go:96`) has no gate at all. Neither method knows about
`OAuthPortalConfig.DisableClassicAuth`, even though the service already holds
`s.oauthService` and calls `s.oauthService.GetConfig()` from `GetOAuthConfig`
(`services.go:1519-1570`). There is no config flag anywhere that can disable
registration.

There are also two more places that render or gate registration and don't go
through `handlers.Register`/`handlers.Login` at all: `handlers.PlaygroundPage`
(`handlers.go:393-403`) and `handlers.IndexPage` (`handlers.go:388-391`), covered in
Goal 4 below.

The only request-time gate lives in the WebUI and fails open. `handlers.Register`
(`internal/webui/handlers/handlers.go:164-169`) and `handlers.Login`
(`handlers.go:90-95`) both do:

```go
oauthConfig := getOAuthConfig(c)
if oauthConfig != nil && oauthConfig.DisableClassicAuth {
    // reject
}
```

`getOAuthConfig` (`handlers.go:53-88`) returns `nil` whenever `backendClient == nil`,
`!backendClient.IsConnected()`, the `GetOAuthConfig` RPC errors, **or OAuth is simply
disabled** (`handlers.go:64-66`: `if !config["enabled"].(bool) { return nil }`), or
the RPC's own 10s-deadline context (`backend_client.go:934`) is exceeded — and `nil`
short-circuits the `&&`, so the request proceeds as if classic auth were allowed. This
last case is not a failure at all: on the repo's own default deployment
(`docker-compose.yml:72`, `OAUTH_ENABLED=false`), `getOAuthConfig` returns `nil` on
*every* request by design, which is exactly why a backend-side gate (Goal 1/2) is
required — the WebUI can never distinguish "OAuth off, nothing to check" from "OAuth
on, but I couldn't reach the backend" using this return value alone. This RPC
round-trips to the backend on every login/register/login-page/register-page/
playground-page request with no caching; a slow-but-alive backend (the RPC's own 10s
deadline elapses while `Register`'s independent 10s RPC still lands) or a WebUI cold
start makes the timeout path real, though a full backend outage does not — the same
outage that stalls `GetOAuthConfig` also stalls `Register`/`Login` themselves, so
`backendClient == nil || !IsConnected()` fails both paths together. `Login.templ`
(`internal/webui/templates/pages/Login.templ:38,189`) has the identical
`oauthConfig == nil || !oauthConfig.DisableClassicAuth` shape for showing the password
form and the "Create an account" link, and `Playground.templ:48,53` and
`Index.templ:24` have weaker or no gating at all — see Goal 4.

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
4. Every page that links to `/register` (`Login.templ`, `Playground.templ`,
   `Index.templ`) stops advertising it whenever registration is actually disabled —
   by OAuth-only mode, by `allow_registration=false`, or by an unknown backend state —
   consistent with whatever the server would actually do with the request.
5. The new flag is documented in `ENVIRONMENT_VARIABLES.md` and
   `charts/notificator-app/values.yaml`.

## Non-goals

- No invite/approval workflow for registration — this is an on/off switch, not a
  request queue.
- No change to session/login behavior for already-registered users when
  `allow_registration=false` — that flag only blocks *new* account creation.
- No change to the 4-character password minimum (`services.go:52`) — out of scope for
  this issue, which is about *whether* registration happens, not password strength.
- No general-purpose config cache framework — the WebUI-side cache is a single
  package-level TTL cache for this one RPC result, not a reusable abstraction.
- No admin-bootstrap/seeding mechanism for *password* accounts. `db.CreateUser` has
  exactly one production caller, `Register` (`services.go:79`); there is no CLI or admin
  path that creates one. This issue does not add one — see the deployment-order risk
  under Risks & trade-offs.
- **`allow_registration` does not gate OAuth just-in-time account provisioning.** The
  other way a brand-new user row appears today is `OAuthCallback` (`services.go:1664`)
  → `OAuthService.CreateOrUpdateOAuthUser` (`oauth_service.go:526`) →
  `db.CreateOAuthUser` (`internal/backend/database/oauth_db.go:16`), which creates an
  account on a first successful OAuth login (`oauth_service.go:544-549`). That path is
  deliberately left alone: it is the normal way users arrive on an SSO deployment, and
  gating it on `allow_registration` would break OAuth login for every new person rather
  than close a sign-up form. `allow_registration` means "username/password self-service
  sign-up", and the docs (step 6) must say so in those words. Restricting *who* may get
  an account on an OAuth deployment is the OAuth provider's/config's job, not this
  flag's.

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

Regenerate with `make proto` (`scripts/generate_proto.sh`). That script only writes
`internal/backend/proto/auth/auth.pb.go` and `internal/backend/proto/auth/auth_grpc.pb.go`
(and the equivalent `internal/backend/proto/alert/` pair) — `--go_opt=module=notificator`
combined with `option go_package = "notificator/internal/backend/proto/auth";`
(`proto/auth.proto:6`) means that's the only output location. The repo also has
`proto/auth.pb.go` / `proto/auth_grpc.pb.go` sitting in `proto/`; these are stale,
orphaned files — nothing imports `"notificator/proto"` (verified with
`grep -rl '"notificator/proto"' --include='*.go' .` returning no results) and `make
proto` does not touch them. **Leave them alone.** Never hand-edit
`internal/backend/proto/auth/auth*.pb.go` either — only regenerate via `make proto`.

`registration_allowed` is computed once, backend-side, from both gates (OAuth-only
*and* the new flag), so the WebUI never has to reimplement the combination logic and
can't drift from what `Register` will actually decide.

### 2. Config: `backend.allow_registration`

**Read this section before writing the code — the obvious recipe (struct tag +
`SetDefault` + `BindEnv`) produces a permanently dead flag in this repo.** Two earlier
revisions of this spec prescribed exactly that and were wrong; the mechanism is
described here so the implementer doesn't rediscover it in production.

Why: `LoadConfigWithViper` (`config/config.go:272`) populates the struct with
`viper.Unmarshal(cfg)` (`:278`), which decodes through mapstructure. Every struct in
`config/config.go` carries **`json` tags only — there is not a single `mapstructure`
tag in the package** (`grep -rn mapstructure --include='*.go' .` matches only
`config/oauth_config.go`). With no `mapstructure` tag, mapstructure falls back to
case-insensitive *field-name* matching, so the viper key `allow_registration` never
matches the field `AllowRegistration`: the underscore has no counterpart in the field
name. Single-word keys (`enabled`) match and work; every snake_case key silently does
not. Reproduced against this branch with the repo's own loader, on an existing field of
exactly the shape this section would have added (`resolved_alerts.retention_days`,
`SetDefault` at `:543`, `BindEnv` at `:650`, no post-`Unmarshal` read):

```
NOTIFICATOR_RESOLVED_ALERTS_RETENTION_DAYS=7
viper.GetInt("resolved_alerts.retention_days") = 7    <- viper has the env value
cfg.ResolvedAlerts.RetentionDays               = 90   <- struct kept the default
viper.GetBool("resolved_alerts.enabled")       = true <- control: single word
cfg.ResolvedAlerts.Enabled                     = true <- control: maps fine
```

`BindEnv` is necessary (this file uses no `viper.AutomaticEnv()`, so every env var
needs an explicit binding) but **not sufficient**: it only makes the value visible to
`viper.Get*`, never to the unmarshalled struct. The repo already works around this
everywhere it matters — `cmd/backend.go:52` reads `viper.GetString("backend.database.type")`
instead of trusting `cfg.Backend.Database.Type`, and the Sentry/Admin blocks are
assigned post-`Unmarshal` (`config/config.go:341-375`).

So `config/config.go` gets **five** changes, and the fifth is the load-bearing one:

- `BackendConfig` (line 58) gains `AllowRegistration bool \`json:"allow_registration"\``
  — `json` only, matching every other field in the file.
- Default `true` in `DefaultConfig()`'s `Backend: BackendConfig{...}` literal (line
  208), next to the existing `Enabled`/`GRPCListen`/etc. fields — `true` keeps every
  existing deployment working unchanged.
- `viper.SetDefault("backend.allow_registration", cfg.Backend.AllowRegistration)` in
  `setViperDefaults` (`:439`), next to the other `backend.*` defaults (lines 443-446).
- `viper.BindEnv("backend.allow_registration", "NOTIFICATOR_BACKEND_ALLOW_REGISTRATION")`
  next to the other explicit `backend.*` bindings (lines 633-639 — currently only the
  `backend.database.*` sub-fields are bound there).
- **Post-`Unmarshal` read, inside `LoadConfigWithViper`, after `viper.Unmarshal(cfg)`
  (`:278`) and alongside the existing post-`Unmarshal` overrides (`:341-375`), before
  `return cfg, nil` (`:436`):**
  ```go
  // viper.Unmarshal cannot map snake_case keys onto CamelCase fields here (no
  // mapstructure tags in this package), so read the bound value explicitly.
  cfg.Backend.AllowRegistration = viper.GetBool("backend.allow_registration")
  ```
  `viper.GetBool` resolves in the usual precedence order — env var (via `BindEnv`),
  config file, then `SetDefault` — so the `true` default still applies when nothing is
  set, and an operator can also set it from `config.json` under `backend`, not just
  from the environment.

Rejected alternative: adding `mapstructure:"allow_registration"` to the new field.
That should also make `Unmarshal` map it (not verified here, unlike the probe above),
but it would be the only mapstructure tag in `config/config.go`, leaving a file where
some snake_case fields load from config and the identical-looking ones next to them
don't. Fixing that for the whole package is a separate change, out of scope here.

Whichever way it is implemented, it must be verified by a test that goes through
`config.LoadConfigWithViper()` — see Validation. Unit tests that construct
`AuthServiceGorm` with `allowRegistration` passed directly cannot catch this class of
bug, because they never touch config loading.

### 3. Backend: `AuthServiceGorm` becomes the authoritative gate

`services.go`:
- `NewAuthServiceGorm(db *database.GormDB, oauthService *OAuthService, adminConfig
  *config.AdminConfig)` (line 36) is **already 3-param** — `adminConfig` arrived with
  the impersonation-allowlist work (#108/#148) and is unrelated to this issue. It gains
  a **fourth** parameter, `allowRegistration bool`, appended after `adminConfig`,
  stored as a new `allowRegistration bool` field on `AuthServiceGorm` (struct at lines
  29-34). Do not drop `adminConfig` — it's still required for impersonation checks
  elsewhere in the file (e.g. `resolveTargetUser` call sites). Update:
  - the one production call site (`internal/backend/server.go:130`):
    `services.NewAuthServiceGorm(s.db, s.oauthService, &s.config.Admin,
    s.config.Backend.AllowRegistration)`
  - **both** test helpers that construct `AuthServiceGorm` directly (there are two, not
    one):
    - `internal/backend/services/update_timezone_test.go:34`:
      `NewAuthServiceGorm(db, nil, nil, true)`
    - `internal/backend/services/auth_impersonation_test.go:44`:
      `NewAuthServiceGorm(db, nil, &config.AdminConfig{ImpersonationAllowedUsers:
      allowedUsers}, true)`
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
- `Register` (line 44): first check, before the existing username/password
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
- `Login` (line 96): first check, before the existing username/password validation:
  ```go
  if s.classicAuthDisabled() {
      return &authpb.LoginResponse{Success: false,
          Message: "Username/password authentication is disabled. Use OAuth."}, nil
  }
  ```
  (No `allowRegistration` check here — disabling *new* registration must not lock out
  existing accounts.)
- `GetOAuthConfig` (line 1519-1570): compute the new response field with a single
  expression reused in all three return sites — don't just compute it once "before the
  final return" and then also try to add it to the two early returns above that point
  in the function, since those early returns execute *before* any such
  once-computed value would exist. Instead, add one line to each of the three existing
  `return &authpb.GetOAuthConfigResponse{...}` literals:
  ```go
  RegistrationAllowed: s.allowRegistration && !s.classicAuthDisabled(),
  ```
  - `oauthService == nil` branch (lines 1521-1527): `classicAuthDisabled()` is
    trivially `false` here since it also checks `s.oauthService == nil`, so this
    reduces to `s.allowRegistration`.
  - `config == nil || !config.Enabled` branch (lines 1529-1535): same reduction.
  - the final populated response (lines 1566-1570): both terms can be non-trivial.

This makes `Register`/`Login` reject the request no matter which path reaches the RPC
— direct `AuthService/Register` gRPC call, WebUI with a healthy backend, or WebUI
mid-outage. The WebUI-level gate below becomes a fast-path UX optimization, not the
security boundary; a bug or regression there can no longer create an account.

### 4. WebUI: fail-closed gate with a short-lived cache

`internal/webui/handlers/handlers.go` — `getOAuthConfig` (currently lines 53-88) must
become a function that **never returns `nil`**, for any reason, including the case
where OAuth is simply turned off. Today it returns `nil` in four situations, and only
two of them are actual failures:

| Current early return | Meaning | New behavior |
|---|---|---|
| `backendClient == nil \|\| !backendClient.IsConnected()` (`:54`) | can't reach backend | fail closed |
| `err != nil` from the RPC (`:59-61`) | RPC failed | fail closed |
| `!config["enabled"].(bool)` (`:64-66`) | **OAuth is off — not a failure** | map it normally: `{Enabled: false, DisableClassicAuth: false, RegistrationAllowed: <from response>}` |
| (implicit: RPC succeeds) | normal path | map it normally |

The `!config["enabled"].(bool)` early return is the bug this spec must close (QA
finding): it is not part of the fail-open problem, but if left in place while the rest
of this step assumes `getOAuthConfig` "always returns non-nil", every login/register on
the repo's own default deployment (`OAUTH_ENABLED=false`) still nil-derefs on
`oauthConfig.RegistrationAllowed`. **Delete that early return.** The backend's
`GetOAuthConfig` RPC (step 3) already returns a fully-populated, correct response when
OAuth is off — `Enabled: false, DisableClassicAuth: false, RegistrationAllowed:
s.allowRegistration` — so the WebUI's job is just to map every field the RPC returns,
unconditionally, not to short-circuit on `enabled`.

Rework `getOAuthConfig` around a small package-level cache holding the last
successful `*pages.OAuthConfig` (now including `RegistrationAllowed`) plus a fetch
timestamp and a mutex, TTL ~15-30s — long enough that a slow or restarting backend
can't make every login page pay the RPC's own 10s deadline (`backend_client.go:934`),
short enough that flipping `allow_registration` or an OAuth setting shows up in the UI
within half a minute:
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
    cfg := parseOAuthConfig(raw) // field-mapping logic from today's getOAuthConfig,
                                  // MINUS the `!config["enabled"].(bool)` early return
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

Because `getOAuthConfig` now always returns non-nil, every caller that currently
branches on `oauthConfig != nil` or `oauthConfig == nil` must be updated — there are
four such call sites, not two:
- `Login` (`handlers.go:90-95`) and `Register` (`handlers.go:164-169`) keep their
  existing pre-check shape but drop the `oauthConfig != nil &&` guard:
  ```go
  oauthConfig := getOAuthConfig(c)
  if oauthConfig.DisableClassicAuth { // Login
  if !oauthConfig.RegistrationAllowed { // Register
  ```
  The backend call underneath still enforces the same rule independently (step 3), so
  this is defense-in-depth, not the only check.
- `LoginPage` (`handlers.go:405-415`) and `PlaygroundPage` (`handlers.go:393-403`)
  currently do
  ```go
  oauthConfig := getOAuthConfig(c)
  if oauthConfig != nil {
      pages.LoginWithOAuth(oauthConfig).Render(...) // or PlaygroundWithOAuth
  } else {
      pages.Login().Render(...) // or Playground() — internally calls *WithOAuth(nil)
  }
  ```
  The `else` branch is now unreachable (`oauthConfig` is never nil), so collapse both
  handlers to always call the `*WithOAuth(oauthConfig)` variant directly — no
  conditional. `pages.Login()`/`pages.Playground()` (the nil-argument wrappers) become
  dead code reachable only if some other, non-HTTP caller invokes them directly; leave
  the templ functions in place (harmless) but stop calling them from these two
  handlers.

### 5. WebUI: register link and login-page rendering

There are **three** places that link to `/register`, and one page (`RegisterPage`)
that redirects away from it — all four must key off `RegistrationAllowed`, not just
`Login.templ`:

- `pages.OAuthConfig` (`Login.templ:6-10`) gains `RegistrationAllowed bool`.
- `Login.templ:189` (`<!-- Register link -->`) and `handlers.RegisterPage`
  (`handlers.go:417-426`, redirect-to-`/login` guard at `:419`) switch from
  `oauthConfig == nil || !oauthConfig.DisableClassicAuth` /
  `oauthConfig != nil && oauthConfig.DisableClassicAuth` to
  `!oauthConfig.RegistrationAllowed` (guard now unconditional — `oauthConfig` is never
  nil, per step 4) — this one field already folds in both the OAuth-only gate and the
  `allow_registration` flag, computed backend-side (step 3), so the template doesn't
  need to know about two separate flags.
- `Playground.templ:53` currently gates its `/register` link on the same condition as
  the sign-in link, `oauthConfig == nil || !oauthConfig.DisableClassicAuth`
  (`:48`) — that's wrong for this feature: `DisableClassicAuth` is a *login* concern,
  so with `allow_registration=false` and OAuth off, the Playground would keep showing
  "Register" even though the backend now rejects it. Split the two: keep the sign-in
  link (`:50-52`) gated on `!oauthConfig.DisableClassicAuth`, and wrap the register
  link (`:53-55`) in its own `if oauthConfig.RegistrationAllowed { ... }`.
- `Index.templ:24` has **no gating at all** today — `IndexPage` (`handlers.go:388-391`)
  doesn't even call `getOAuthConfig`. In the shipped `docker-compose.yml` config,
  `cfg.WebUI.Playground` (`router.go:360`) picks `PlaygroundPage` for `/`, but
  `IndexPage` is the alternate landing page for deployments with that flag off, and it
  has the same bug. Fix: have `IndexPage` call `getOAuthConfig(c)` and pass
  `oauthConfig.RegistrationAllowed` through to the template (add a `registrationAllowed
  bool` parameter to `Index`/`IndexContent` in `Index.templ`, or reuse `*OAuthConfig`
  for consistency with `Login`/`Playground`); wrap the `<a href="/register">` block
  (`:24-26`) in `if registrationAllowed { ... }`.
- The classic-login-form visibility (`Login.templ:38`) and the OAuth-only message
  (`Login.templ:104`) keep using `DisableClassicAuth` as-is — that's specifically
  about *login*, which `allow_registration=false` must not affect.
- `Register.templ` has no copy of this condition today (verified) — no change needed
  there unless one is added in review.
- After editing `Login.templ`, `Playground.templ`, `Index.templ`, regenerate with
  `make webui-templates` — never hand-edit `*_templ.go`.

### 6. Docs

- `ENVIRONMENT_VARIABLES.md`: add `NOTIFICATOR_BACKEND_ALLOW_REGISTRATION` under
  "### Server Settings" (next to `NOTIFICATOR_BACKEND_ENABLED` at line 22), documented
  as "Allow username/password self-registration (default: `true`). Does not affect
  OAuth — a first OAuth login still provisions an account. On a deployment without
  OAuth, set this to `false` only after the accounts you need already exist: there is
  no other way to create a password account." (see the deployment-order risk below).
- `charts/notificator-app/values.yaml`: add a commented example under the backend
  `env:` block (lines 53-57, next to the `NOTIFICATOR_BACKEND_*` examples):
  `# NOTIFICATOR_BACKEND_ALLOW_REGISTRATION: "false"`.

### Files touched

- `proto/auth.proto`, regenerated `internal/backend/proto/auth/auth.pb.go` and
  `internal/backend/proto/auth/auth_grpc.pb.go` via `make proto`. **Not** the orphaned
  `proto/auth.pb.go` / `proto/auth_grpc.pb.go` — see step 1, they're unused and `make
  proto` doesn't write them.
- `config/config.go` — `BackendConfig.AllowRegistration`, default, `SetDefault`,
  `BindEnv`, **and the post-`Unmarshal` `viper.GetBool` read** (step 2 — without that
  last one the flag is dead).
- `config/config_test.go` (new) — env-var round-trip test through
  `LoadConfigWithViper`, below.
- `internal/backend/services/services.go` — `NewAuthServiceGorm`, `classicAuthDisabled`,
  `Register`, `Login`, `GetOAuthConfig`.
- `internal/backend/services/services_test.go` (existing) — unit tests below.
- `internal/backend/server.go` — updated `NewAuthServiceGorm` call.
- `internal/backend/services/update_timezone_test.go` and
  `internal/backend/services/auth_impersonation_test.go` — updated test helper calls
  (both construct `AuthServiceGorm` directly).
- `internal/webui/client/backend_client.go` — `GetOAuthConfig()` map gains
  `registration_allowed`.
- `internal/webui/handlers/handlers.go` — `getOAuthConfig` cache + fail-closed default
  (dropping the `!config["enabled"]` early return), `Login`, `Register`, `RegisterPage`,
  `LoginPage`, `PlaygroundPage`, `IndexPage`.
- `internal/webui/handlers/handlers_test.go` (new or existing) — unit tests below.
- `internal/webui/templates/pages/Login.templ` (+ regenerated `Login_templ.go`) —
  `OAuthConfig.RegistrationAllowed`, register-link condition.
- `internal/webui/templates/pages/Playground.templ` (+ regenerated
  `Playground_templ.go`) — split register-link condition from the sign-in-link
  condition, gate on `RegistrationAllowed`.
- `internal/webui/templates/pages/Index.templ` (+ regenerated `Index_templ.go`) —
  add a registration-state parameter, gate the register link.
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
- **Deployment-order lockout, on non-OAuth deployments only.** There are exactly two
  ways a user row is created today: `Register` → `db.CreateUser` (`services.go:79`),
  and `OAuthCallback` (`services.go:1664`) → `CreateOrUpdateOAuthUser`
  (`oauth_service.go:526`) → `db.CreateOAuthUser` (`oauth_db.go:16`) on first OAuth
  login. There is no admin-seeding or CLI bootstrap path. So a fresh deployment started
  with `NOTIFICATOR_BACKEND_ALLOW_REGISTRATION=false` **and OAuth off** can never create
  its first account: registration is closed and no user exists to log in as. With OAuth
  configured the lockout does not exist — the first OAuth login provisions the account
  (see Non-goals: the flag does not gate that path). This is not new risk introduced by
  this change (today nothing can close registration at all, so the failure mode didn't
  exist), but shipping the flag makes it reachable. Not fixing it here (no
  invite/approval workflow is in scope — see Non-goals); operators on a non-OAuth
  deployment must register at least one account (or flip the flag to `true`
  temporarily) before enabling `allow_registration=false`. Document this explicitly in
  `ENVIRONMENT_VARIABLES.md` next to the new variable.
- **`allow_registration=false` is not "no new accounts" on an OAuth deployment.** It
  closes the username/password sign-up form and the `Register` RPC; OAuth first-login
  provisioning keeps creating accounts. This is the intended scope (see Non-goals), and
  the trade-off is that an operator could read the flag name as broader than it is —
  which is why the doc string in step 6 names the OAuth exception rather than just
  saying "allow new user self-registration".
- **Cold-start fail-closed renders a dead-end `/login`.** With no cached config yet and
  the backend unreachable, `failClosedOAuthConfig()` yields `Enabled: false,
  DisableClassicAuth: true, RegistrationAllowed: false`, so `/login` shows no password
  form, no OAuth buttons, and `Login.templ:104`'s "use one of the OAuth providers above"
  message with none above. That is the intended meaning of fail-closed — during that
  window no login could succeed anyway — but the copy is misleading; if it matters,
  soften that message when `len(Providers) == 0`. Not required by this issue.

## Validation

- `go build ./...` and `make webui-templates && go build ./...` both pass after the
  proto regen and templ regen.
- **New Go test in `config/config_test.go` — the one that catches the dead-flag failure
  mode of step 2, and the only test here that exercises config loading at all:**
  - `t.Setenv("NOTIFICATOR_BACKEND_ALLOW_REGISTRATION", "false")`, then
    `cfg, err := LoadConfigWithViper()`; assert `cfg.Backend.AllowRegistration == false`.
    Asserting on `viper.GetBool("backend.allow_registration")` instead would pass even
    with the bug — the assertion must be on the **struct field**, since that is what
    `server.go:130` passes to `NewAuthServiceGorm`.
  - Same call with the env var unset: assert `cfg.Backend.AllowRegistration == true`
    (the `SetDefault` path).
  - Note `LoadConfigWithViper` uses the global viper instance, so these two cases must
    not run in parallel with each other or with other config tests.
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
  - `getOAuthConfig` returns a non-nil, correctly-mapped config (`Enabled: false,
    DisableClassicAuth: false, RegistrationAllowed` from the RPC) when the backend
    reports OAuth disabled — regression test for the `!config["enabled"].(bool)` early
    return removed in step 4; this is the repo's own default deployment shape
    (`docker-compose.yml:72`, `OAUTH_ENABLED=false`) and must not panic or 500.
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
     is rejected with a clear message, existing users can still log in. Confirm this on
     both the repo's default landing page (`/`, `PlaygroundPage` when
     `cfg.WebUI.Playground=true`) and with `WebUI.Playground=false` (`IndexPage`). This
     is the end-to-end check for the step-2 config plumbing — if `Register` still
     succeeds here while the env var is set, the post-`Unmarshal` read is missing and
     the flag never reached the service.
  5. `/login`, `/` (both `PlaygroundPage` and `IndexPage` variants), do not render
     "Register"/"Create an account" in any of the above disabled states, and do render
     it when both gates are open. This is the regression check for `Login.templ:189`,
     `Playground.templ:53`, and `Index.templ:24`.
  6. `OAUTH_ENABLED=false` with no other flags set (repo default): `/login`, `/`,
     `/register` all render without panicking and without a 500 — regression check for
     the `getOAuthConfig` nil-on-OAuth-disabled bug fixed in step 4.
  7. `ENVIRONMENT_VARIABLES.md` and `charts/notificator-app/values.yaml` document the
     new variable, including that it does not gate OAuth first-login provisioning.
  8. `OAUTH_ENABLED=true` with `NOTIFICATOR_BACKEND_ALLOW_REGISTRATION=false`: a
     first-time OAuth login still provisions its account (`✅ Created new OAuth user` in
     the backend log, `oauth_service.go:549`) — the flag must not break SSO onboarding.
     This is the scope decision from Non-goals, verified rather than assumed.
