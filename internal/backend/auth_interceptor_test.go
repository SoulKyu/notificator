package backend

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"notificator/config"
	"notificator/internal/backend/database"
	alertpb "notificator/internal/backend/proto/alert"
	authpb "notificator/internal/backend/proto/auth"
	"notificator/internal/backend/services"
)

const testServiceToken = "test-service-token-32-characters-long!!"

func newTestServer(t *testing.T) *Server {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	gdb, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("NewGormDB: %v", err)
	}
	if err := gdb.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	adminConfig := &config.AdminConfig{}
	return &Server{
		db:                gdb,
		config:            &config.Config{},
		authService:       services.NewAuthServiceGorm(gdb, nil, adminConfig, true),
		alertService:      services.NewAlertServiceGorm(gdb, adminConfig),
		statisticsService: services.NewStatisticsServiceGorm(gdb, adminConfig),
		serviceToken:      testServiceToken,
	}
}

// dialTestServer starts the real gRPC services behind the real auth
// interceptors over an in-memory bufconn listener — the closest thing to a
// grpcurl-against-:50051 test without a real socket.
func dialTestServer(t *testing.T, s *Server) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(s.authUnaryInterceptor),
		grpc.StreamInterceptor(s.authStreamInterceptor),
	)
	authpb.RegisterAuthServiceServer(grpcServer, s.authService)
	alertpb.RegisterAlertServiceServer(grpcServer, s.alertService)
	alertpb.RegisterStatisticsServiceServer(grpcServer, s.statisticsService)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func withSession(ctx context.Context, sessionID string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, sessionMetadataKey, sessionID)
}

func withServiceToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, serviceTokenMetadataKey, token)
}

func requireUnauthenticated(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an Unauthenticated error, got nil")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}
}

func requireNotUnauthenticated(t *testing.T, err error) {
	t.Helper()
	if err != nil && status.Code(err) == codes.Unauthenticated {
		t.Fatalf("call was rejected by the auth interceptor: %v", err)
	}
}

// twelveRPCs mirrors the table in issue #160: RPCs that read or write
// collaboration/statistics data with no auth check of their own. Each call
// is built with just enough fields to reach the handler if auth passes —
// the assertions below only care about the auth outcome, not the handler's
// business-logic response.
func twelveRPCs(authClient authpb.AuthServiceClient, alertClient alertpb.AlertServiceClient, statsClient alertpb.StatisticsServiceClient) map[string]func(ctx context.Context) error {
	return map[string]func(ctx context.Context) error{
		"SearchUsers": func(ctx context.Context) error {
			_, err := authClient.SearchUsers(ctx, &authpb.SearchUsersRequest{Query: "a"})
			return err
		},
		"GetComments": func(ctx context.Context) error {
			_, err := alertClient.GetComments(ctx, &alertpb.GetCommentsRequest{AlertKey: "k"})
			return err
		},
		"GetCommentCountsBatch": func(ctx context.Context) error {
			_, err := alertClient.GetCommentCountsBatch(ctx, &alertpb.GetCommentCountsBatchRequest{AlertKeys: []string{"k"}})
			return err
		},
		"GetAcknowledgments": func(ctx context.Context) error {
			_, err := alertClient.GetAcknowledgments(ctx, &alertpb.GetAcknowledgmentsRequest{AlertKey: "k"})
			return err
		},
		"GetAllAcknowledgedAlerts": func(ctx context.Context) error {
			_, err := alertClient.GetAllAcknowledgedAlerts(ctx, &alertpb.GetAllAcknowledgedAlertsRequest{AlertKeys: []string{"k"}})
			return err
		},
		"CreateResolvedAlert": func(ctx context.Context) error {
			_, err := alertClient.CreateResolvedAlert(ctx, &alertpb.CreateResolvedAlertRequest{Fingerprint: "f", Source: "s"})
			return err
		},
		"GetResolvedAlerts": func(ctx context.Context) error {
			_, err := alertClient.GetResolvedAlerts(ctx, &alertpb.GetResolvedAlertsRequest{Limit: 10})
			return err
		},
		"GetResolvedAlert": func(ctx context.Context) error {
			_, err := alertClient.GetResolvedAlert(ctx, &alertpb.GetResolvedAlertRequest{Fingerprint: "f"})
			return err
		},
		"CaptureAlertFired": func(ctx context.Context) error {
			_, err := statsClient.CaptureAlertFired(ctx, &alertpb.CaptureAlertFiredRequest{Fingerprint: "f", AlertName: "n"})
			return err
		},
		"UpdateAlertResolved": func(ctx context.Context) error {
			_, err := statsClient.UpdateAlertResolved(ctx, &alertpb.UpdateAlertResolvedRequest{Fingerprint: "f"})
			return err
		},
		"UpdateAlertAcknowledged": func(ctx context.Context) error {
			_, err := statsClient.UpdateAlertAcknowledged(ctx, &alertpb.UpdateAlertAcknowledgedRequest{Fingerprint: "f"})
			return err
		},
		"GetAlertHistory": func(ctx context.Context) error {
			_, err := statsClient.GetAlertHistory(ctx, &alertpb.GetAlertHistoryRequest{Fingerprint: "f"})
			return err
		},
	}
}

func TestTwelveRPCs_RejectedWithoutCredentials(t *testing.T) {
	s := newTestServer(t)
	conn := dialTestServer(t, s)
	rpcs := twelveRPCs(authpb.NewAuthServiceClient(conn), alertpb.NewAlertServiceClient(conn), alertpb.NewStatisticsServiceClient(conn))

	for name, call := range rpcs {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			requireUnauthenticated(t, call(ctx))
		})
	}
}

func TestTwelveRPCs_SucceedWithServiceToken(t *testing.T) {
	s := newTestServer(t)
	conn := dialTestServer(t, s)
	rpcs := twelveRPCs(authpb.NewAuthServiceClient(conn), alertpb.NewAlertServiceClient(conn), alertpb.NewStatisticsServiceClient(conn))

	for name, call := range rpcs {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			requireNotUnauthenticated(t, call(withServiceToken(ctx, testServiceToken)))
		})
	}
}

func TestTwelveRPCs_SucceedWithValidSession(t *testing.T) {
	s := newTestServer(t)
	user, err := s.db.CreateUser("alice", "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sessionID := "sess-1"
	if err := s.db.CreateSession(user.ID, sessionID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	conn := dialTestServer(t, s)
	rpcs := twelveRPCs(authpb.NewAuthServiceClient(conn), alertpb.NewAlertServiceClient(conn), alertpb.NewStatisticsServiceClient(conn))

	for name, call := range rpcs {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			requireNotUnauthenticated(t, call(withSession(ctx, sessionID)))
		})
	}
}

func TestInvalidServiceTokenRejected(t *testing.T) {
	s := newTestServer(t)
	conn := dialTestServer(t, s)
	client := authpb.NewAuthServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.SearchUsers(withServiceToken(ctx, "wrong-token-that-does-not-match-configured"), &authpb.SearchUsersRequest{})
	requireUnauthenticated(t, err)
}

func TestInvalidSessionRejected(t *testing.T) {
	s := newTestServer(t)
	conn := dialTestServer(t, s)
	client := authpb.NewAuthServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.SearchUsers(withSession(ctx, "no-such-session"), &authpb.SearchUsersRequest{})
	requireUnauthenticated(t, err)
}

// TestNonAllowlistedMethodRejectedWithoutCredential is the acceptance test
// called out explicitly in issue #160: a method absent from the public
// allowlist must be denied by default, even one that isn't part of the
// twelve originally-reported RPCs.
func TestNonAllowlistedMethodRejectedWithoutCredential(t *testing.T) {
	s := newTestServer(t)
	conn := dialTestServer(t, s)
	client := authpb.NewAuthServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetProfile(ctx, &authpb.GetProfileRequest{SessionId: "irrelevant"})
	requireUnauthenticated(t, err)
}

func TestAllowlistedMethodReachesHandlerWithoutCredential(t *testing.T) {
	s := newTestServer(t)
	conn := dialTestServer(t, s)
	client := authpb.NewAuthServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Login is public: it must reach the handler (and fail on bad
	// credentials, not on missing auth).
	_, err := client.Login(ctx, &authpb.LoginRequest{Username: "nobody", Password: "wrong"})
	requireNotUnauthenticated(t, err)
}
