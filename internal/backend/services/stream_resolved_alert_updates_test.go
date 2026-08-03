package services

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notificator/config"
	"notificator/internal/backend/database"
	alertpb "notificator/internal/backend/proto/alert"
)

// nopResolvedAlertUpdatesServer implements grpc.ServerStreamingServer[alertpb.ResolvedAlertUpdate]
// well enough to drive StreamResolvedAlertUpdates in a test, without a real connection.
type nopResolvedAlertUpdatesServer struct {
	grpc.ServerStream
}

func (nopResolvedAlertUpdatesServer) Send(*alertpb.ResolvedAlertUpdate) error { return nil }
func (nopResolvedAlertUpdatesServer) Context() context.Context               { return context.Background() }

// TestStreamResolvedAlertUpdates_Unimplemented guards against the RPC ever again
// registering a subscription: nothing in the codebase consumes this stream, and
// the old per-subscriber-goroutine broadcast could double-close a Done channel
// and crash the whole backend on a resolution burst after a client disconnected
// uncleanly (#183). Returning Unimplemented immediately means no subscription is
// created and no goroutine is left to leak or race.
func TestStreamResolvedAlertUpdates_Unimplemented(t *testing.T) {
	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	svc := NewAlertServiceGorm(db, &config.AdminConfig{})

	err = svc.StreamResolvedAlertUpdates(&alertpb.StreamResolvedAlertUpdatesRequest{SessionId: "irrelevant"}, nopResolvedAlertUpdatesServer{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected codes.Unimplemented, got: %v", err)
	}
}

// TestCreateResolvedAlert_BurstDoesNotCrash is a regression test for the failure
// scenario in #183: resolving several alerts back to back used to fire one
// broadcast goroutine per subscriber per call, racing on a dead stream's Done
// channel. With the subscription machinery removed, a burst of resolutions is
// just a sequence of DB writes and must succeed without panicking.
func TestCreateResolvedAlert_BurstDoesNotCrash(t *testing.T) {
	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	svc := NewAlertServiceGorm(db, &config.AdminConfig{})

	for i := range 3 {
		resp, err := svc.CreateResolvedAlert(context.Background(), &alertpb.CreateResolvedAlertRequest{
			Fingerprint: "fp",
			Source:      "test",
			AlertData:   []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !resp.Success {
			t.Fatalf("call %d: expected Success=true, got message: %s", i, resp.Message)
		}
	}
}
