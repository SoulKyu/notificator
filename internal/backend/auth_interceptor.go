package backend

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"notificator/internal/backend/models"
)

// ServiceTokenEnvVar is the environment variable holding the shared secret
// the WebUI's background poller uses to authenticate gRPC calls that carry
// no user session (e.g. capturing statistics for an alert nobody is
// currently looking at). Its value must match what the WebUI is configured
// with.
const ServiceTokenEnvVar = "NOTIFICATOR_SERVICE_TOKEN"

const (
	sessionMetadataKey       = "x-notificator-session"
	authorizationMetadataKey = "authorization"
	serviceTokenMetadataKey  = "x-notificator-service-token"
)

// publicMethods is the explicit allowlist of gRPC methods reachable without
// a session or service token. Everything else is denied by default — see
// authenticate.
var publicMethods = map[string]bool{
	"/notificator.auth.AuthService/Login":             true,
	"/notificator.auth.AuthService/Register":          true,
	"/notificator.auth.AuthService/ValidateSession":   true,
	"/notificator.auth.AuthService/GetOAuthConfig":    true,
	"/notificator.auth.AuthService/GetOAuthProviders": true,
	"/notificator.auth.AuthService/GetOAuthAuthURL":   true,
	"/notificator.auth.AuthService/OAuthCallback":     true,
}

type authContextKey struct{}

// authenticatedCaller is stashed in the request context once a call clears
// the interceptor, so handlers can read it instead of re-validating a
// session id themselves. Existing per-handler `req.SessionId` checks keep
// working unchanged alongside it.
type authenticatedCaller struct {
	user      *models.User
	isService bool
}

// AuthenticatedUser returns the user resolved from the caller's session, for
// calls authenticated by session rather than by service token.
func AuthenticatedUser(ctx context.Context) (*models.User, bool) {
	caller, ok := ctx.Value(authContextKey{}).(*authenticatedCaller)
	if !ok || caller.user == nil {
		return nil, false
	}
	return caller.user, true
}

// ValidateServiceToken reads and validates NOTIFICATOR_SERVICE_TOKEN. It
// follows the same fail-closed posture as database.ValidateEncryptionKey:
// the backend refuses to start rather than silently leaving the gRPC layer
// open to any caller holding no credential at all.
func ValidateServiceToken() (string, error) {
	token := os.Getenv(ServiceTokenEnvVar)
	if len(token) < 32 {
		return "", fmt.Errorf("%s must be set to a random secret of at least 32 characters; generate one with `openssl rand -hex 32`", ServiceTokenEnvVar)
	}
	return token, nil
}

func (s *Server) authUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	authedCtx, err := s.authenticate(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	return handler(authedCtx, req)
}

func (s *Server) authStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	authedCtx, err := s.authenticate(ss.Context(), info.FullMethod)
	if err != nil {
		return err
	}
	return handler(srv, &authenticatedServerStream{ServerStream: ss, ctx: authedCtx})
}

// authenticatedServerStream overrides Context() so stream handlers observe
// the context populated by authenticate (mirrors the unary path).
type authenticatedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (a *authenticatedServerStream) Context() context.Context {
	return a.ctx
}

// authenticate enforces deny-by-default: fullMethod must either be in the
// public allowlist, or the call must carry a valid service token or a valid
// session id in gRPC metadata.
func (s *Server) authenticate(ctx context.Context, fullMethod string) (context.Context, error) {
	if publicMethods[fullMethod] {
		return ctx, nil
	}

	md, _ := metadata.FromIncomingContext(ctx)

	if token := firstMetadataValue(md, serviceTokenMetadataKey); token != "" {
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.serviceToken)) == 1 {
			return context.WithValue(ctx, authContextKey{}, &authenticatedCaller{isService: true}), nil
		}
		return ctx, status.Error(codes.Unauthenticated, "invalid service token")
	}

	sessionID := firstMetadataValue(md, sessionMetadataKey)
	if sessionID == "" {
		if bearer := firstMetadataValue(md, authorizationMetadataKey); strings.HasPrefix(bearer, "Bearer ") {
			sessionID = strings.TrimPrefix(bearer, "Bearer ")
		}
	}
	if sessionID == "" {
		return ctx, status.Errorf(codes.Unauthenticated, "%s requires a session or service token", fullMethod)
	}

	user, err := s.db.GetUserBySession(sessionID)
	if err != nil {
		return ctx, status.Error(codes.Unauthenticated, "invalid or expired session")
	}

	return context.WithValue(ctx, authContextKey{}, &authenticatedCaller{user: user}), nil
}

func firstMetadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
