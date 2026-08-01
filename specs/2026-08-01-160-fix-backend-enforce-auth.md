# Spec: enforce auth at the gRPC layer with a deny-by-default interceptor

- Issue: [SoulKyu/notificator#160](https://github.com/SoulKyu/notificator/issues/160)
- Date: 2026-08-01
- Status: planned

## Problem

`startGRPCServer` (`internal/backend/server.go:146-178`) builds the gRPC
server with a single interceptor slot:

```go
opts := []grpc.ServerOption{
    grpc.UnaryInterceptor(s.loggingUnaryInterceptor),
}
s.grpcServer = grpc.NewServer(opts...)
authpb.RegisterAuthServiceServer(s.grpcServer, s.authService)
alertpb.RegisterAlertServiceServer(s.grpcServer, s.alertService)
alertpb.RegisterStatisticsServiceServer(s.grpcServer, s.statisticsService)
reflection.Register(s.grpcServer)
```

`loggingUnaryInterceptor` (`internal/backend/server.go:282-296`) only logs;
it never rejects a call. `reflection.Register` runs unconditionally, so the
full method/message schema is free to anyone who can reach the port.
Authorization is entirely opt-in, hand-written per RPC as
`s.db.GetUserBySession(req.SessionId)` — that call appears 45 times in
`internal/backend/services/services.go`. Twelve RPCs never do it:

`SearchUsers` (services.go:289), `GetComments` (:574),
`GetCommentCountsBatch` (:683), `GetAcknowledgments` (:813),
`GetAllAcknowledgedAlerts` (:855), `CreateResolvedAlert` (:1213),
`GetResolvedAlerts` (:1288), `GetResolvedAlert` (:1389),
`CaptureAlertFired` (statistics_grpc_service.go:357),
`UpdateAlertResolved` (:418), `UpdateAlertAcknowledged` (:471),
`GetAlertHistory` (:606).

Confirmed against `proto/alert.proto` and `proto/auth.proto`: 11 of these 12
request messages carry **no** `session_id` field at all (`SearchUsersRequest`,
`GetCommentsRequest`, `GetCommentCountsBatchRequest`,
`GetAcknowledgmentsRequest`, `GetAllAcknowledgedAlertsRequest`,
`CreateResolvedAlertRequest`, `GetResolvedAlertsRequest`,
`GetResolvedAlertRequest`, `CaptureAlertFiredRequest`,
`UpdateAlertResolvedRequest`, `UpdateAlertAcknowledgedRequest`) — so a
"thread `session_id` through the proto" fix (the #148 shape) isn't even
mechanically available for most of them without a proto change.
`GetAlertHistoryRequest` is the one exception: it has `session_id` (proto/alert.proto:1000),
but the handler never reads it. This is why the fix has to live at the
transport layer (gRPC metadata), not inside twelve more message structs.

Two of `AlertService`'s methods are server-streaming
(`SubscribeToAlertUpdates`, `StreamResolvedAlertUpdates` —
services.go:930, :1426) and already hand-validate `req.SessionId`
internally. They're not in the 12-RPC list, but they're served by the same
`grpc.Server` and the issue's acceptance criteria explicitly ask for a
stream interceptor too, so this spec adds one and lets it cover them for
defense in depth.

`reflection.Register` (server.go:167) also puts two more RPCs on this same
`grpc.Server` once §3's flag turns it on:
`grpc.reflection.v1.ServerReflection/ServerReflectionInfo` and its legacy
`grpc.reflection.v1alpha` twin — one `reflection.Register` call wires up both
(`google.golang.org/grpc@v1.73.0/reflection/serverreflection.go:60-66`, the
version pinned in `go.mod:18`). Both are server-streaming, so they run
through the same `authStreamInterceptor` as every other RPC. Neither is on
the issue's 7-method `AuthService` allowlist, so as originally drafted,
turning `GRPCReflection` on would still get `grpcurl` an `Unauthenticated`
from the interceptor — silently breaking the exact
`grpcurl -plaintext localhost:50051 list` step this spec's own Validation
section relies on to prove the flag works. §1 fixes this by allowlisting the
two reflection methods, but only when `s.config.Backend.GRPCReflection` is
`true` — the same flag that gates whether `reflection.Register` runs at all,
so reflection can't become reachable through the interceptor while the
config says it's off.

Client side, `internal/webui/client/backend_client.go:59-71` dials with
`insecure.NewCredentials()` and never attaches gRPC metadata anywhere
(`grep -c metadata\. backend_client.go` → 0). This isn't a twelve-RPC
problem: the client makes 74 gRPC calls across ~71 distinct RPCs
(`grep -cE 'c\.(authClient|alertClient|statisticsClient)\.[A-Za-z]+\(ctx' backend_client.go`
→ 74), and deny-by-default means every one of them needs a credential path,
not just the twelve the issue happened to enumerate. Wiring credentials into
twelve hand-picked call sites and leaving the rest on bare
`context.Background()` denies the other ~59 the moment this ships —
including everyday user-driven writes like `AddComment` and `Logout` that
already thread a `sessionID` Go parameter today (used only to populate
`req.SessionId` on the proto message; the interceptor never looks at proto
fields).

Most of these ~71 calls don't need to be attributed to a specific logged-in
user at the transport layer — they're fine authenticating as "the webui
backend" via the shared service token, exactly like the four
poller/detached-goroutine RPCs the issue calls out:

- `CaptureAlertFired`, `UpdateAlertResolved` — called from
  `AlertCache.refreshAlerts()` (`internal/webui/services/alert_cache.go:251,294`),
  the ticker-driven background poller (`backgroundRefresh`, alert_cache.go:181).
- `UpdateAlertAcknowledged` — called from a detached `go func(...)`
  spawned by the acknowledge HTTP handler (`internal/webui/handlers/dashboard_handlers.go:980-984`)
  specifically so it doesn't block the response; by the time it runs, the
  request/session context is gone.
- `CreateResolvedAlert`, `GetCommentCountsBatch`, `GetAllAcknowledgedAlerts`,
  `GetResolvedAlerts`/`GetResolvedAlertsCount`, `GetResolvedAlert` — all
  called from the same background poller (`storeResolvedAlertInBackend`,
  `loadCommentCountsEfficiently`, `loadAcknowledgmentsEfficiently`,
  `GetResolvedAlertsWithPagination`, alert_cache.go:557-1139).

User-level authorization for all of these is unchanged and stays exactly
where it already is — the 45 handler-side `GetUserBySession(req.SessionId)`
checks (Non-goals). None of them need any client code change under this
spec's fix direction (§4): they get the service token for free from a
single dial-level interceptor.

`GetComments` and `GetAcknowledgments` are genuinely dual-use: besides the
poller, they're also called on-demand with a live session from
`dashboard_handlers.go:1363,1380,1577` and `statistics_handlers.go:668,689`.
`GetAlertHistory` already threads a `sessionID` parameter
(`GetAlertHistory(sessionID, fingerprint string, limit int32)`,
backend_client.go:2091) obtained via `middleware.GetSessionID(c)`
(dashboard_handlers.go:2478, statistics_handlers.go). These three are the
only call sites in the entire client that need the RPC attributed to the
real user rather than "the webui backend" — everything else is covered
wholesale by §4's dial-level interceptor, not by enumerating call sites.

`SearchUsers` has no caller anywhere in `internal/webui` today
(`grep -rl SearchUsers internal/webui` → no matches) — it's reachable only
from generated proto code and `gorm_db.go`. It still needs to be denied by
default (it returns emails/last-login for the whole user directory), but no
webui client change is needed for it.

`internal/backend/database/sentry_db.go:19-51` already has the fail-closed
precedent this needs: `EncryptionKeyEnvVar = "NOTIFICATOR_ENCRYPTION_KEY"`,
validated once by `ValidateEncryptionKey()` before `Start()` does anything
else (`internal/backend/server.go:48-51`), returning an error that aborts
startup — no fallback to "open".

## Goals

1. Every RPC on the three registered gRPC services is denied with
   `codes.Unauthenticated` by default. A method is reachable only if it's on
   an explicit allowlist or the caller presents a valid session or service
   credential.
2. The handful of user-driven calls that must be attributed to the actual
   logged-in user — not just "the webui backend" — authenticate via a
   session ID carried in gRPC metadata (not a per-message field — most of
   the ~71 messages don't have one, and the pattern shouldn't require
   touching every proto message going forward).
3. Every other RPC the WebUI backend makes over its one `BackendClient`
   connection — all ~71 distinct RPCs the client invokes today, and any
   added later — authenticates by default via a shared service token
   compared with `subtle.ConstantTimeCompare`, attached once at the gRPC
   dial level so no per-call-site wiring is required and nothing new can
   ship uncovered.
4. `reflection.Register` only runs when explicitly enabled by config
   (dev-only), and the reflection RPCs themselves are only reachable
   through the auth interceptor when that same flag is on — the flag
   can't be enabled without reflection actually working, and can't leave
   reflection reachable when it's off.
5. Both the backend and the webui refuse to start if their required auth
   secret (`NOTIFICATOR_SERVICE_TOKEN`) is missing — fail closed, matching
   `ValidateEncryptionKey`'s existing posture, not fall back to open.
6. `openwiki/quickstart.md`'s hazard note about the missing interceptor is
   corrected once it's no longer true.

## Non-goals

- No refactor of the 45 existing `s.db.GetUserBySession(req.SessionId)`
  call sites inside handlers — they keep working unchanged. Simplifying them
  to read the interceptor-resolved identity from context is explicitly a
  follow-up (per the issue).
- No TLS between webui and backend — separate, already flagged in the issue
  as out of scope unless it falls out for free (it doesn't here).
- No change to `SubscribeToAlertUpdates` / `StreamResolvedAlertUpdates`
  handler bodies — the new stream interceptor covers them at the transport
  layer; their existing internal `req.SessionId` check is redundant but
  harmless and is left alone.
- No Helm `Deployment` template changes — `env` is already a free-form
  passthrough map in `values.yaml` (`backend.env` at line 53, `webui.env` at
  line 152), so wiring a new var is a values.yaml documentation addition,
  not a template change.

## Approach

### 1. New file: `internal/backend/auth_interceptor.go`

```go
package backend

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	sessionMetadataKey      = "x-notificator-session"
	serviceTokenMetadataKey = "x-notificator-service-token"

	// ServiceTokenEnvVar is the shared secret the WebUI's background poller
	// presents in place of a user session. Required — see ValidateServiceToken.
	ServiceTokenEnvVar = "NOTIFICATOR_SERVICE_TOKEN"
)

// publicMethods lists the only RPCs reachable without a session or service
// token. Every other method on any registered service is denied by default.
var publicMethods = map[string]bool{
	"/notificator.auth.AuthService/Login":            true,
	"/notificator.auth.AuthService/Register":         true,
	"/notificator.auth.AuthService/ValidateSession":  true,
	"/notificator.auth.AuthService/GetOAuthConfig":    true,
	"/notificator.auth.AuthService/GetOAuthProviders": true,
	"/notificator.auth.AuthService/GetOAuthAuthURL":   true,
	"/notificator.auth.AuthService/OAuthCallback":     true,
}

// reflectionMethods lists the gRPC server reflection RPCs that
// reflection.Register puts on the server (both the current and legacy wire
// versions). They're allowed only when s.config.Backend.GRPCReflection is
// true — the same flag that gates whether reflection.Register runs at all
// in startGRPCServer — so this is never a static bypass independent of
// that config.
var reflectionMethods = map[string]bool{
	"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":      true,
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": true,
}

// ValidateServiceToken mirrors database.ValidateEncryptionKey's fail-closed
// posture: no default, no fallback to open, name the env var in the error.
func ValidateServiceToken() error {
	if len(os.Getenv(ServiceTokenEnvVar)) < 32 {
		return fmt.Errorf("%s must be set to a secret of at least 32 characters; generate one with `openssl rand -hex 32`", ServiceTokenEnvVar)
	}
	return nil
}

type identityContextKey struct{}

// authorize is shared by the unary and stream interceptors: same allowlist,
// same two credential kinds, same rejection codes.
func (s *Server) authorize(ctx context.Context, fullMethod string) (context.Context, error) {
	if publicMethods[fullMethod] {
		return ctx, nil
	}
	if s.config.Backend.GRPCReflection && reflectionMethods[fullMethod] {
		return ctx, nil
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing credentials")
	}
	if tokens := md.Get(serviceTokenMetadataKey); len(tokens) == 1 && tokens[0] != "" {
		want := os.Getenv(ServiceTokenEnvVar)
		if subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(want)) == 1 {
			return ctx, nil
		}
		return nil, status.Error(codes.Unauthenticated, "invalid service token")
	}
	if sessions := md.Get(sessionMetadataKey); len(sessions) == 1 && sessions[0] != "" {
		user, err := s.db.GetUserBySession(sessions[0])
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid session")
		}
		return context.WithValue(ctx, identityContextKey{}, user), nil
	}
	return nil, status.Error(codes.Unauthenticated, "missing credentials")
}

func (s *Server) authUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx, err := s.authorize(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func (s *Server) authStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := s.authorize(ss.Context(), info.FullMethod)
	if err != nil {
		return err
	}
	return handler(srv, &authenticatedStream{ServerStream: ss, ctx: ctx})
}

// authenticatedStream overrides Context() to hand the resolved identity to
// the handler — grpc.ServerStream has no other way to carry it.
type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedStream) Context() context.Context { return s.ctx }
```

`publicMethods` is keyed on `info.FullMethod`, which is
`/<package>.<Service>/<Method>` — confirmed against `proto/auth.proto:4,11`
(`package notificator.auth; service AuthService`). All 7 methods listed in
the issue live on `AuthService`; nothing on `AlertService` or
`StatisticsService` is public.

`reflectionMethods`' two full-method strings are confirmed against the
`grpc.reflection.v1`/`grpc.reflection.v1alpha` package and
`ServerReflection` service names declared in
`google.golang.org/grpc@v1.73.0/reflection/grpc_reflection_v1/reflection_grpc.pb.go`
and the `v1alpha` sibling — both registered by the single
`reflection.Register(s.grpcServer)` call already in `startGRPCServer` (§2).
Gating the allow-check on `s.config.Backend.GRPCReflection` — rather than
allowlisting them statically like `publicMethods` — keeps the two in
lockstep with whether the reflection service is even registered, so
reflection can never be reachable through the interceptor while the config
says it's off.

### 2. `internal/backend/server.go`

In `Start()` (line 48), validate the service token in the same place and
the same way as the encryption key, right next to it:

```go
if err := database.ValidateEncryptionKey(); err != nil {
    return fmt.Errorf("invalid encryption key configuration: %w", err)
}
if err := ValidateServiceToken(); err != nil {
    return fmt.Errorf("invalid service token configuration: %w", err)
}
```

In `startGRPCServer()` (lines 146-178), chain the new interceptors onto the
existing logging one (order: logging outermost so denied calls are still
logged, auth innermost so it runs right before the handler) and gate
reflection:

```go
opts := []grpc.ServerOption{
    grpc.ChainUnaryInterceptor(s.loggingUnaryInterceptor, s.authUnaryInterceptor),
    grpc.ChainStreamInterceptor(s.authStreamInterceptor),
}
s.grpcServer = grpc.NewServer(opts...)
authpb.RegisterAuthServiceServer(s.grpcServer, s.authService)
alertpb.RegisterAlertServiceServer(s.grpcServer, s.alertService)
alertpb.RegisterStatisticsServiceServer(s.grpcServer, s.statisticsService)
if s.config.Backend.GRPCReflection {
    reflection.Register(s.grpcServer)
}
```

`grpc.UnaryInterceptor` (singular) and `grpc.ChainUnaryInterceptor` can't
both appear in `opts`; the singular form is dropped since it only ever held
one interceptor.

### 3. `config/config.go`

Add a field to `BackendConfig` (line 58-64):

```go
type BackendConfig struct {
	Enabled        bool           `json:"enabled"`
	GRPCListen     string         `json:"grpc_listen"`
	GRPCClient     string         `json:"grpc_client"`
	GRPCReflection bool           `json:"grpc_reflection"` // gRPC reflection — dev only, off by default
	HTTPListen     string         `json:"http_listen"`
	Database       DatabaseConfig `json:"database"`
}
```

Default `false` in the defaults block (line 208-212), and follow the
existing `backend.*` `SetDefault` pattern right next to it (line 443-446;
the `BindEnv` line has no direct non-nested `backend.*` sibling to copy —
the closest precedent is the `backend.database.*` block at line 632-639):

```go
viper.SetDefault("backend.grpc_reflection", cfg.Backend.GRPCReflection)
```
```go
viper.BindEnv("backend.grpc_reflection", "NOTIFICATOR_GRPC_REFLECTION")
```

**This wiring alone does not bind the value.** `config.go` has zero
`mapstructure` tags anywhere (`grep -c mapstructure config/config.go` → 0),
and `LoadConfigWithViper` (`config.go:272-280`, used by `cmd/backend.go:43`)
does `cfg := DefaultConfig()` → `setViperDefaults(cfg)` →
`viper.Unmarshal(cfg)` with no decoder hooks and no post-`Unmarshal` patch.
mapstructure's default field matcher compares the struct field name to the
map key case-insensitively but does **not** split on underscores, so an
underscored key like `grpc_reflection` never resolves to a field named
`GRPCReflection` — the field silently keeps whatever `DefaultConfig()` set
(`false`), no matter what `SetDefault`, `BindEnv`, or the env var itself
say.

This isn't hypothetical: the identically-shaped sibling
`statistics.retention_days` → `Statistics.RetentionDays` (same
`SetDefault`+`BindEnv`-only pattern, `config.go:547-548,653`) has this exact
bug live today, confirmed on the running `make test` stack —
`NOTIFICATOR_STATISTICS_RETENTION_DAYS=7` is set in the backend container's
environment, but the cleanup job logs `retention: 90 days`,
`DefaultConfig()`'s value, not the env var's. Copying the surrounding
`backend.*` pattern "as-is" would reproduce the same bug for
`GRPCReflection`.

Fix it locally for this one field: immediately after `viper.Unmarshal(cfg)`
(`config.go:278-280`), add an explicit read-back —

```go
if err := viper.Unmarshal(cfg); err != nil {
    return nil, fmt.Errorf("failed to unmarshal config: %w", err)
}
cfg.Backend.GRPCReflection = viper.GetBool("backend.grpc_reflection")
```

— so `viper.GetBool` (which does resolve `SetDefault`/`BindEnv`/env-var
precedence correctly; only the struct-unmarshal step is broken) is the
source of truth for this field. Scoped intentionally to `GRPCReflection`
only: it does not fix the same latent binding gap on `GRPCListen`,
`GRPCClient`, or `Statistics.RetentionDays` — those are pre-existing bugs
outside this change's blast radius (see Risks).

### 4. `internal/webui/client/backend_client.go`

Wrapping the twelve issue-listed RPCs' contexts one call site at a time is
exactly the design that left the other ~59 of the client's ~71 distinct
RPCs credential-less (see Problem). Instead, attach credentials once, at
the gRPC dial, so the fix covers every call the client conn ever makes —
including RPCs added after this ships, with no future call site left to
remember to wire:

```go
import (
	...
	"os"

	"google.golang.org/grpc/metadata"
)

const (
	sessionMetadataKey      = "x-notificator-session"
	serviceTokenMetadataKey = "x-notificator-service-token"
)

type sessionIDContextKey struct{}

// WithSessionID tags ctx so the dial-level interceptor sends the caller's
// live session instead of the service token. Used only by the handful of
// calls that must be attributed to the specific logged-in user rather than
// "the webui backend" generically — every other call needs no per-call
// change and gets the service token automatically.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey{}, sessionID)
}

func (c *BackendClient) attachCredentials(ctx context.Context) context.Context {
	if sessionID, ok := ctx.Value(sessionIDContextKey{}).(string); ok && sessionID != "" {
		return metadata.AppendToOutgoingContext(ctx, sessionMetadataKey, sessionID)
	}
	return metadata.AppendToOutgoingContext(ctx, serviceTokenMetadataKey, c.serviceToken)
}

func (c *BackendClient) authUnaryClientInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(c.attachCredentials(ctx), method, req, reply, cc, opts...)
}

func (c *BackendClient) authStreamClientInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return streamer(c.attachCredentials(ctx), desc, cc, method, opts...)
}
```

Add a `serviceToken string` field to the `BackendClient` struct next to
`address`. `serviceToken` is read once, in `Connect()` (line 59-71), reusing
the existing fail-closed pattern already there for a missing backend
(`internal/webui/router.go:50-54` already does `log.Fatalf` on `Connect()`
error — no new startup-failure plumbing needed), and both interceptors are
installed as dial options so they apply uniformly to the `authClient`,
`alertClient`, and `statisticsClient` stubs built from the same `conn`:

```go
func (c *BackendClient) Connect() error {
	c.serviceToken = os.Getenv("NOTIFICATOR_SERVICE_TOKEN")
	if len(c.serviceToken) < 32 {
		return fmt.Errorf("NOTIFICATOR_SERVICE_TOKEN must be set to a secret of at least 32 characters; generate one with `openssl rand -hex 32`")
	}
	conn, err := grpc.NewClient(c.address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(c.authUnaryClientInterceptor),
		grpc.WithChainStreamInterceptor(c.authStreamClientInterceptor),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to backend: %w", err)
	}
	...
```

With this in place, all 74 call sites across ~71 distinct RPCs get the
service token by default — no code change needed at
`GetCommentCountsBatch`, `GetAllAcknowledgedAlerts`,
`GetResolvedAlerts`/`GetResolvedAlertsCount`, `GetResolvedAlert`,
`CreateResolvedAlert`, `CaptureAlertFired`, `UpdateAlertResolved`,
`UpdateAlertAcknowledged`, `AddComment`, `Logout`, or any of the other RPCs
this spec's earlier draft left uncovered. Only the three call sites that
must be attributed to the real logged-in user change:

- **`GetComments(alertKey string)` → `GetComments(sessionID, alertKey string)`**
  and **`GetAcknowledgments(alertKey string)` →
  `GetAcknowledgments(sessionID, alertKey string)`**
  (backend_client.go:406,447) — wrap the existing
  `ctx, cancel := context.WithTimeout(context.Background(), ...)` with
  `ctx = WithSessionID(ctx, sessionID)` before the RPC call; the poller's
  own calls pass `""`, which `attachCredentials` treats as "no session tag"
  and falls through to the service-token default.
- **`GetAlertHistory`** (backend_client.go:2091) already takes `sessionID`
  — after building its `ctx`, also call
  `ctx = WithSessionID(ctx, sessionID)`.
- **`SearchUsers`**: no webui caller exists, so no client change is
  required for this change to compile; the server-side interceptor already
  protects it.

Update the same seven call sites that need the `sessionID` argument for
`GetComments`/`GetAcknowledgments` (shape unchanged from the original plan
— only the mechanism they route through changed):
- `internal/webui/services/alert_cache.go:1112,1125` (poller,
  `storeResolvedAlertInBackend`) → pass `""` (routes to service token).
- `internal/webui/handlers/dashboard_handlers.go:1363,1380,1577` and
  `internal/webui/handlers/statistics_handlers.go:668,689` → pass
  `middleware.GetSessionID(c)`, the same helper `GetAlertHistory`'s callers
  already use (dashboard_handlers.go:2478).

### 5. Docs and deploy config

`openwiki/quickstart.md:94-95` (not line 91, which is the unrelated "Never
edit `*_templ.go`" bullet two items above it) — replace:

```
- The backend has **no auth interceptor** — every gRPC handler validates `session_id` by hand.
  A new RPC that forgets the check is wide open. See [backend](backend.md#auth).
```

with a note that the interceptor is deny-by-default now, and that
`NOTIFICATOR_SERVICE_TOKEN` must be set identically on both `backend` and
`webui` — a mismatch silently breaks every RPC the client interceptor
attaches the service token to (§4), i.e. effectively the whole app, not just
the background poller (`Unauthenticated` on every call, logged but not
user-visible).

`docker-compose.yml` — in the `backend` service `environment:` block, next
to `NOTIFICATOR_ENCRYPTION_KEY` (same `${VAR:-dev-default}` convention):

```yaml
- NOTIFICATOR_SERVICE_TOKEN=${NOTIFICATOR_SERVICE_TOKEN:-devdevdevdevdevdevdevdevdevdevdevdevdevdev}
- NOTIFICATOR_GRPC_REFLECTION=true
```

and in the `webui` service `environment:` block, the identical
`NOTIFICATOR_SERVICE_TOKEN` line (same literal default — it's compared
byte-for-byte against the backend's, so the two blocks must match).
`NOTIFICATOR_GRPC_REFLECTION` stays backend-only; it's dev/debug tooling for
`grpcurl`, not needed by the webui client, which talks typed proto.

`charts/notificator-app/values.yaml` — `backend.env` (line 53) and
`webui.env` (line 152) are already free-form passthrough maps (see
`NOTIFICATOR_ENCRYPTION_KEY`'s commented example at line 59-61); add a
matching commented example for `NOTIFICATOR_SERVICE_TOKEN` in both, no
template changes needed since `env` already renders whatever key/value
pairs are set.

### Files touched

- `internal/backend/auth_interceptor.go` — new: allowlist, `reflectionMethods`,
  `authorize`, unary/stream interceptors, `ValidateServiceToken`.
- `internal/backend/server.go` — `Start()`, `startGRPCServer()`.
- `internal/backend/auth_interceptor_test.go` — new (below).
- `config/config.go` — `BackendConfig.GRPCReflection`, defaults/viper
  wiring, and the post-`Unmarshal` read-back in `LoadConfigWithViper`.
- `internal/webui/client/backend_client.go` — `serviceToken` field,
  dial-level `authUnaryClientInterceptor`/`authStreamClientInterceptor`,
  `attachCredentials`, `WithSessionID`, `GetComments`/`GetAcknowledgments`
  signature change, `GetAlertHistory` ctx wiring.
- `internal/webui/client/backend_client_test.go` — unit tests for
  `attachCredentials` (below).
- `internal/webui/services/alert_cache.go` — two call sites pass `""`.
- `internal/webui/handlers/dashboard_handlers.go` — three call sites pass
  `middleware.GetSessionID(c)`.
- `internal/webui/handlers/statistics_handlers.go` — two call sites pass
  `middleware.GetSessionID(c)`.
- `openwiki/quickstart.md` — correct the stale hazard note (line 94-95).
- `docker-compose.yml` — `NOTIFICATOR_SERVICE_TOKEN` (both services),
  `NOTIFICATOR_GRPC_REFLECTION` (backend).
- `charts/notificator-app/values.yaml` — documented env var examples.

## Risks & trade-offs

- **Token/session mismatch fails closed, not loud to the end user.** If
  `NOTIFICATOR_SERVICE_TOKEN` differs between `backend` and `webui` (e.g. one
  redeployed without the other picking up a rotated secret), *every* RPC the
  dial-level interceptor attaches the service token to starts failing
  `Unauthenticated` — not just poller writes, since §4's fix covers all ~71
  RPCs by default. Logged (existing `fmt.Printf`/handler error paths) but
  not surfaced anywhere a human is likely to look first. Mitigated by
  requiring identical values in `docker-compose.yml`/Helm docs, not by new
  monitoring — out of scope here.
- **The dial-level client interceptor is a wider blast radius than the
  original per-call design, on purpose.** A bug in `attachCredentials`
  breaks all ~71 RPCs at once instead of just the twelve originally listed
  — that's the intended trade for "nothing new can ship uncovered." Mitigated
  by `backend_client_test.go` unit coverage on `attachCredentials` itself
  (below) plus `go build`/`go test` catching any signature mismatch
  immediately, and the manual webui walkthrough (Validation) exercises the
  shared code path that every RPC now goes through.
- **Streaming interceptor is new territory for this codebase** — no
  existing test exercises `grpc.ServerStream`/`grpc.StreamServerInfo`, and
  `SubscribeToAlertUpdates`/`StreamResolvedAlertUpdates` aren't in the
  issue's 12-RPC list. Getting `authenticatedStream.Context()` wrong (e.g.
  forgetting to override it) would silently make the stream interceptor a
  no-op for context-based metadata lookups inside the handler — the unit
  tests below assert on this directly rather than trusting the wiring.
- **`reflection.Register` behind a flag defaulting to `false` changes local
  dev workflow** — anyone who currently points `grpcurl` at a plain `go run`
  backend loses reflection unless they set
  `NOTIFICATOR_GRPC_REFLECTION=true`. `docker-compose.yml` sets it so `make
  test` is unaffected; a bare `go run ./cmd/backend` is not, and that's the
  intended trade (prod-shaped default, opt-in for local debugging). This is
  now genuinely true end-to-end (flag → config → registration → interceptor
  allow-check all agree) — see §1/§3 for the two places the original draft
  of this spec got this wrong.
- **`config.go`'s viper-to-struct binding has a pre-existing latent bug on
  every underscored `backend.*`/nested-config field that isn't
  `GRPCReflection`** — `GRPCListen`, `GRPCClient`, and
  `Statistics.RetentionDays` at minimum share the same "no mapstructure tag,
  no post-Unmarshal read-back" shape (§3), meaning their env-var overrides
  may silently not apply either. §3 fixes only `GRPCReflection`, since
  that's the field this change adds and depends on; fixing the others is a
  pre-existing bug outside this change's scope, called out here so it isn't
  mistaken for "already handled."
- **`GetComments`/`GetAcknowledgments` signature change touches 7 call
  sites across 3 files** — mechanical (thread a string through), but it's
  the one part of this change that isn't purely additive. Compile failures
  here are the fast-fail signal that a call site was missed.
- **No context propagation refactor for the 45 existing handler-side
  `GetUserBySession` calls** — they keep re-validating the session that the
  interceptor already validated. Redundant, not incorrect; the issue
  explicitly defers this to a follow-up rather than bundling a 45-call-site
  refactor into an auth-enforcement fix.

## Validation

- `go build ./... && go vet ./... && go test ./...` passes.
- `internal/backend/auth_interceptor_test.go`, following the existing
  in-process test convention (`internal/backend/services/services_test.go`:
  file-backed sqlite via `t.TempDir()`, real `AutoMigrate()`, seeded
  user/session — no `bufconn`, no real listener):
  - Table of the 12 issue-listed `FullMethod` strings, built from the real
    package/service names (`proto/auth.proto:4,11` → `notificator.auth.AuthService`;
    `proto/alert.proto:4,11,638` → `notificator.alert.AlertService` and
    `notificator.alert.StatisticsService`):
    `/notificator.auth.AuthService/SearchUsers`,
    `/notificator.alert.AlertService/GetComments`,
    `/notificator.alert.AlertService/GetCommentCountsBatch`,
    `/notificator.alert.AlertService/GetAcknowledgments`,
    `/notificator.alert.AlertService/GetAllAcknowledgedAlerts`,
    `/notificator.alert.AlertService/CreateResolvedAlert`,
    `/notificator.alert.AlertService/GetResolvedAlerts`,
    `/notificator.alert.AlertService/GetResolvedAlert`,
    `/notificator.alert.StatisticsService/CaptureAlertFired`,
    `/notificator.alert.StatisticsService/UpdateAlertResolved`,
    `/notificator.alert.StatisticsService/UpdateAlertAcknowledged`,
    `/notificator.alert.StatisticsService/GetAlertHistory`. Each rejected
    with `codes.Unauthenticated` when `authUnaryInterceptor` runs against a
    `context.Background()` with no metadata — handler closure must never be
    invoked (assert via a flag set inside it).
  - Same call with `x-notificator-session` metadata set to a session seeded
    via `db.CreateSession(...)` succeeds; handler is invoked; the resolved
    user is retrievable from the context the handler receives.
  - Same call with `x-notificator-session` set to a garbage string is
    rejected `Unauthenticated`, handler never invoked.
  - Same call with `x-notificator-service-token` set to
    `os.Getenv(ServiceTokenEnvVar)`'s value (test sets the env var) succeeds.
  - Same call with a wrong service token is rejected.
  - One of the 7 allowlisted methods (e.g. `Login`) succeeds with no
    metadata at all.
  - `authStreamInterceptor`: a fake `grpc.ServerStream` wrapped in
    `authenticatedStream`; assert `handler`'s `ss.Context()` carries the
    resolved identity, and that a call with no credentials is rejected
    before `handler` runs.
  - `ValidateServiceToken`: empty env var and a 31-character value both
    error; a 32+-character value passes.
  - `authorize` with `fullMethod` = `/grpc.reflection.v1.ServerReflection/ServerReflectionInfo`
    and `s.config.Backend.GRPCReflection == false` (the default) is rejected
    `Unauthenticated`, handler never invoked — proves reflection is not a
    static bypass. The same call with `GRPCReflection == true` and no other
    credentials succeeds. Repeat both for the `v1alpha` full-method string.
- `internal/webui/client/backend_client_test.go`, extending the existing
  `fakeAlertClient`-based convention in that file:
  - `attachCredentials` with a bare `context.Background()` (no
    `WithSessionID`) appends `x-notificator-service-token` set to
    `c.serviceToken` and no `x-notificator-session` key.
  - `attachCredentials` with `WithSessionID(ctx, "sess-1")` appends
    `x-notificator-session` set to `"sess-1"` and no service-token key.
  - `attachCredentials` with `WithSessionID(ctx, "")` (the poller's
    call-site convention) falls back to the service-token path, matching
    the "" pass-through documented in §4.
- Manual, against the `make test` docker-compose stack (per the issue's
  acceptance criteria):
  1. With `NOTIFICATOR_GRPC_REFLECTION` unset on a fresh backend (default
     `false`), `grpcurl -plaintext localhost:50051 list` fails (reflection
     not registered) — confirms the flag is off by default end to end, not
     just in the struct.
  2. `grpcurl -plaintext localhost:50051 list` — works against the
     `make test` compose stack (reflection is on there), confirming the
     dev-only escape hatch actually works once §1/§3 are both applied.
  3. `grpcurl -plaintext localhost:50051 notificator.auth.AuthService.SearchUsers`
     with no metadata → `Unauthenticated`. Repeat for the other 11 RPCs in
     the issue's table.
  4. Log in through the webui, exercise the dashboard, silences,
     statistics, and activity pages end to end — since every RPC now goes
     through the same dial-level interceptor (§4), this exercises the fix
     for all ~71 RPCs at once, not just the twelve originally enumerated;
     confirms nothing regresses and session-attributed calls
     (`GetComments`/`GetAcknowledgments`/`GetAlertHistory`) still work.
  5. Acknowledge and resolve an alert through the UI, then check
     `alert_statistics` picked up the write — confirms the service-token
     path works for the detached-goroutine and poller call sites.
  6. Stop the backend, edit `docker-compose.yml` to blank out
     `NOTIFICATOR_SERVICE_TOKEN` on the backend only, restart it — confirm
     it refuses to start with a message naming the env var (not a generic
     panic).
