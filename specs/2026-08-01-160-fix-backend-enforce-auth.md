# Spec: enforce auth at the gRPC layer with a deny-by-default interceptor

- Issue: [SoulKyu/notificator#160](https://github.com/SoulKyu/notificator/issues/160)
- Date: 2026-08-01
- Status: planned

## Problem

`startGRPCServer` (`internal/backend/server.go:145-178`) builds the gRPC
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

Client side, `internal/webui/client/backend_client.go:57-70` dials with
`insecure.NewCredentials()` and never attaches gRPC metadata anywhere
(`grep -c metadata\. backend_client.go` → 0). Four of the twelve RPCs
(`CreateResolvedAlert`, `CaptureAlertFired`, `UpdateAlertResolved`,
`UpdateAlertAcknowledged`) plus two dual-use ones (`GetComments`,
`GetAcknowledgments`) are called from paths that never have an HTTP
session in scope:

- `CaptureAlertFired`, `UpdateAlertResolved` — called from
  `AlertCache.refreshAlerts()` (`internal/webui/services/alert_cache.go:251,294`),
  the ticker-driven background poller (`backgroundRefresh`, alert_cache.go:181).
- `UpdateAlertAcknowledged` — called from a detached `go func(...)`
  spawned by the acknowledge HTTP handler (`internal/webui/handlers/dashboard_handlers.go:980-984`)
  specifically so it doesn't block the response; by the time it runs, the
  request/session context is gone.
- `CreateResolvedAlert`, and the poller's own calls to `GetComments` /
  `GetAcknowledgments` — all inside `storeResolvedAlertInBackend`
  (alert_cache.go:1096, calls at :1112, :1125, :1139), same background poller.
- `GetCommentCountsBatch`, `GetAllAcknowledgedAlerts`, `GetResolvedAlerts`,
  `GetResolvedAlert` — all called from the cache-refresh path
  (`loadCommentCountsEfficiently` alert_cache.go:653, `loadAcknowledgmentsEfficiently`
  :557, `GetResolvedAlertsWithPagination` :782/:821), same background poller,
  **not** dual-use despite being user-visible data.

`GetComments` and `GetAcknowledgments` are genuinely dual-use: besides the
poller, they're also called on-demand with a live session from
`dashboard_handlers.go:1363,1380,1577` and `statistics_handlers.go:668,689`.
`GetAlertHistory` already threads a `sessionID` parameter
(`GetAlertHistory(sessionID, fingerprint string, limit int32)`,
backend_client.go:2091) obtained via `middleware.GetSessionID(c)`
(dashboard_handlers.go:2478, statistics_handlers.go) — that's the exact
pattern the two dual-use methods need to adopt.

`SearchUsers` has no caller anywhere in `internal/webui` today
(`grep -rl SearchUsers internal/webui` → no matches) — it's reachable only
from generated proto code and `gorm_db.go`. It still needs to be denied by
default (it returns emails/last-login for the whole user directory), but no
webui client change is needed for it.

`internal/backend/database/sentry_db.go:19-51` already has the fail-closed
precedent this needs: `EncryptionKeyEnvVar = "NOTIFICATOR_ENCRYPTION_KEY"`,
validated once by `ValidateEncryptionKey()` before `Start()` does anything
else (`internal/backend/server.go:47-50`), returning an error that aborts
startup — no fallback to "open".

## Goals

1. Every RPC on the three registered gRPC services is denied with
   `codes.Unauthenticated` by default. A method is reachable only if it's on
   an explicit allowlist or the caller presents a valid session or service
   credential.
2. User-driven calls authenticate via a session ID carried in gRPC metadata
   (not a per-message field — most of the 12 messages don't have one, and
   the pattern shouldn't require touching every proto message going
   forward).
3. Machine calls from the WebUI's background poller authenticate via a
   shared service token compared with `subtle.ConstantTimeCompare`.
4. `reflection.Register` only runs when explicitly enabled by config
   (dev-only).
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

### 2. `internal/backend/server.go`

In `Start()` (line 47), validate the service token in the same place and
the same way as the encryption key, right next to it:

```go
if err := database.ValidateEncryptionKey(); err != nil {
    return fmt.Errorf("invalid encryption key configuration: %w", err)
}
if err := ValidateServiceToken(); err != nil {
    return fmt.Errorf("invalid service token configuration: %w", err)
}
```

In `startGRPCServer()` (lines 145-178), chain the new interceptors onto the
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
existing `backend.*` viper wiring right next to it (line 443-446):

```go
viper.SetDefault("backend.grpc_reflection", cfg.Backend.GRPCReflection)
```
```go
viper.BindEnv("backend.grpc_reflection", "NOTIFICATOR_GRPC_REFLECTION")
```

### 4. `internal/webui/client/backend_client.go`

Add two small helpers next to `Connect()`:

```go
func withSessionMetadata(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return withServiceTokenMetadata(ctx)
	}
	return metadata.AppendToOutgoingContext(ctx, "x-notificator-session", sessionID)
}

func withServiceTokenMetadata(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "x-notificator-service-token", serviceToken)
}
```

`serviceToken` is read once, in `Connect()` (line 59), reusing the existing
fail-closed pattern already there for a missing backend
(`internal/webui/router.go:50-54` already does `log.Fatalf` on
`Connect()` error — no new startup-failure plumbing needed):

```go
func (c *BackendClient) Connect() error {
	c.serviceToken = os.Getenv("NOTIFICATOR_SERVICE_TOKEN")
	if len(c.serviceToken) < 32 {
		return fmt.Errorf("NOTIFICATOR_SERVICE_TOKEN must be set to a secret of at least 32 characters; generate one with `openssl rand -hex 32`")
	}
	conn, err := grpc.NewClient(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	...
```

Every `ctx := context.Background()` passed to the twelve RPCs' underlying
`c.alertClient.*`/`c.authClient.*` calls gets wrapped:

- **Service-token-only** (always poller/detached-goroutine callers, no
  signature change): `GetCommentCountsBatch`, `GetAllAcknowledgedAlerts`,
  `GetResolvedAlerts` (and `GetResolvedAlertsCount`, which reuses the same
  RPC), `GetResolvedAlert`, `CreateResolvedAlert`, `CaptureAlertFired`,
  `UpdateAlertResolved`, `UpdateAlertAcknowledged` — call
  `ctx := withServiceTokenMetadata(context.Background())` at the top of each
  method.
- **Dual-use, gains a `sessionID` param** (mirrors `GetAlertHistory`'s
  existing shape): `GetComments(sessionID, alertKey string)`,
  `GetAcknowledgments(sessionID, alertKey string)` — call
  `ctx := withSessionMetadata(context.Background(), sessionID)`.
- **`GetAlertHistory`** already takes `sessionID` — switch its `ctx` from
  bare `context.Background()` to `withSessionMetadata(context.Background(), sessionID)`.
- **`SearchUsers`**: no webui caller exists, so no client change is
  required for this change to compile; the server-side interceptor already
  protects it.

Update the four call sites that now need a `sessionID` argument:
- `internal/webui/services/alert_cache.go:1112,1125` (poller,
  `storeResolvedAlertInBackend`) → pass `""` (routes to service token).
- `internal/webui/handlers/dashboard_handlers.go:1363,1380,1577` and
  `internal/webui/handlers/statistics_handlers.go:668,689` → pass
  `middleware.GetSessionID(c)`, the same helper `GetAlertHistory`'s callers
  already use (dashboard_handlers.go:2478).

### 5. Docs and deploy config

`openwiki/quickstart.md:91` — replace:

```
- The backend has **no auth interceptor** — every gRPC handler validates `session_id` by hand.
  A new RPC that forgets the check is wide open. See [backend](backend.md#auth).
```

with a note that the interceptor is deny-by-default now, and that
`NOTIFICATOR_SERVICE_TOKEN` must be set identically on both `backend` and
`webui` — a mismatch silently breaks the background poller (`Unauthenticated`
on every write, logged but not user-visible).

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

- `internal/backend/auth_interceptor.go` — new: allowlist, `authorize`,
  unary/stream interceptors, `ValidateServiceToken`.
- `internal/backend/server.go` — `Start()`, `startGRPCServer()`.
- `internal/backend/auth_interceptor_test.go` — new (below).
- `config/config.go` — `BackendConfig.GRPCReflection` + defaults/viper.
- `internal/webui/client/backend_client.go` — `serviceToken` field,
  `withSessionMetadata`/`withServiceTokenMetadata`, per-call `ctx` wiring,
  `GetComments`/`GetAcknowledgments` signature change.
- `internal/webui/services/alert_cache.go` — two call sites pass `""`.
- `internal/webui/handlers/dashboard_handlers.go` — three call sites pass
  `middleware.GetSessionID(c)`.
- `internal/webui/handlers/statistics_handlers.go` — two call sites pass
  `middleware.GetSessionID(c)`.
- `openwiki/quickstart.md` — correct the stale hazard note.
- `docker-compose.yml` — `NOTIFICATOR_SERVICE_TOKEN` (both services),
  `NOTIFICATOR_GRPC_REFLECTION` (backend).
- `charts/notificator-app/values.yaml` — documented env var examples.

## Risks & trade-offs

- **Token/session mismatch fails closed, not loud to the end user.** If
  `NOTIFICATOR_SERVICE_TOKEN` differs between `backend` and `webui` (e.g. one
  redeployed without the other picking up a rotated secret), every poller
  write silently starts failing `Unauthenticated` — logged
  (`"Failed to capture alert acknowledged statistics..."` etc., the existing
  `fmt.Printf` error paths already there) but not surfaced anywhere a human
  is likely to look first. Mitigated by requiring identical values in
  `docker-compose.yml`/Helm docs, not by new monitoring — out of scope here.
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
  intended trade (prod-shaped default, opt-in for local debugging).
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
- Manual, against the `make test` docker-compose stack (per the issue's
  acceptance criteria):
  1. `grpcurl -plaintext localhost:50051 list` — works (reflection is on in
     compose), confirming the dev-only escape hatch.
  2. `grpcurl -plaintext localhost:50051 notificator.auth.AuthService.SearchUsers`
     with no metadata → `Unauthenticated`. Repeat for the other 11 RPCs in
     the issue's table.
  3. Log in through the webui, exercise the dashboard, silences,
     statistics, and activity pages end to end — confirms session metadata
     reaches the RPCs that need it and nothing regresses.
  4. Acknowledge and resolve an alert through the UI, then check
     `alert_statistics` picked up the write — confirms the service-token
     path works for the detached-goroutine and poller call sites.
  5. Stop the backend, edit `docker-compose.yml` to blank out
     `NOTIFICATOR_SERVICE_TOKEN` on the backend only, restart it — confirm
     it refuses to start with a message naming the env var (not a generic
     panic).
