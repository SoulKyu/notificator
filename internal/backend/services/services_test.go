package services

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notificator/config"
	"notificator/internal/backend/database"

	alertpb "notificator/internal/backend/proto/alert"
)

// newActivityTestService mirrors setupImpersonationTest's harness style
// (internal/backend/services/impersonation_test.go): a file-backed sqlite DB
// migrated through the real AutoMigrate path, seeded with a user, a valid
// session, and a few comments for the activity feed.
func newActivityTestService(t *testing.T) (*AlertServiceGorm, string, string) {
	t.Helper()

	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	user, err := db.CreateUser("alice", "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	sessionID := "activity-session"
	if err := db.CreateSession(user.ID, sessionID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	seed := []struct {
		alertKey, content, kind string
	}{
		{"alert-1", "looks fine", "comment"},
		{"alert-1", "🔔 Alert acknowledged", "ack"},
		{"alert-2", "🔇 Alert silenced for 2h", "silence"},
	}
	for _, s := range seed {
		if _, err := db.CreateComment(s.alertKey, user.ID, s.content, s.kind); err != nil {
			t.Fatalf("failed to seed comment: %v", err)
		}
	}

	svc := NewAlertServiceGorm(db, &config.AdminConfig{})
	return svc, sessionID, user.ID
}

func TestGetRecentActivityValidatesAndClamps(t *testing.T) {
	svc, sessionID, _ := newActivityTestService(t) // seeds a user + session + a few comments

	// invalid session rejected
	_, err := svc.GetRecentActivity(context.Background(), &alertpb.GetRecentActivityRequest{
		SessionId: "", Since: timestamppb.New(time.Now().Add(-time.Hour)), Limit: 50,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("empty session: got %v, want Unauthenticated", err)
	}

	// limit above max is clamped (ask for 5000, assert the SQL limit used ≤ 200 by
	// seeding 3 comments and asserting no error + events returned)
	resp, err := svc.GetRecentActivity(context.Background(), &alertpb.GetRecentActivityRequest{
		SessionId: sessionID, Since: timestamppb.New(time.Now().Add(-24 * time.Hour)), Limit: 5000,
	})
	if err != nil {
		t.Fatalf("valid call: %v", err)
	}
	if len(resp.Events) == 0 {
		t.Fatal("want events, got none")
	}
	if resp.Events[0].Username == "" {
		t.Fatal("event missing username")
	}
}

func TestGetRecentActivityRejectsInvalidSession(t *testing.T) {
	svc, _, _ := newActivityTestService(t)

	_, err := svc.GetRecentActivity(context.Background(), &alertpb.GetRecentActivityRequest{
		SessionId: "does-not-exist", Since: timestamppb.New(time.Now().Add(-time.Hour)), Limit: 50,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("bogus session: got %v, want Unauthenticated", err)
	}
}

// TestGetRecentActivityClampsToMax proves the server-side ceiling is actually
// enforced, not just "no error": seed comfortably past activityMaxLimit and
// assert the response never exceeds it even when the caller asks for more.
func TestGetRecentActivityClampsToMax(t *testing.T) {
	svc, sessionID, userID := newActivityTestService(t)

	seedComments(t, svc.db, userID, activityMaxLimit+50, "clamp-key")

	resp, err := svc.GetRecentActivity(context.Background(), &alertpb.GetRecentActivityRequest{
		SessionId: sessionID, Since: timestamppb.New(time.Now().Add(-24 * time.Hour)), Limit: 5000,
	})
	if err != nil {
		t.Fatalf("valid call: %v", err)
	}
	if len(resp.Events) != activityMaxLimit {
		t.Fatalf("got %d events, want exactly the clamped max %d", len(resp.Events), activityMaxLimit)
	}
}

// TestGetRecentActivityAppliesDefaultLimit proves an unset/zero limit falls
// back to activityDefaultLimit rather than being treated as "no limit".
func TestGetRecentActivityAppliesDefaultLimit(t *testing.T) {
	svc, sessionID, userID := newActivityTestService(t)

	seedComments(t, svc.db, userID, activityDefaultLimit+20, "default-key")

	resp, err := svc.GetRecentActivity(context.Background(), &alertpb.GetRecentActivityRequest{
		SessionId: sessionID, Since: timestamppb.New(time.Now().Add(-24 * time.Hour)), Limit: 0,
	})
	if err != nil {
		t.Fatalf("valid call: %v", err)
	}
	if len(resp.Events) != activityDefaultLimit {
		t.Fatalf("got %d events, want exactly the default %d", len(resp.Events), activityDefaultLimit)
	}
}

// seedComments creates n plain comments for userID under distinct alert keys so
// GetRecentActivity has enough rows to make limit clamping observable.
func seedComments(t *testing.T, db *database.GormDB, userID string, n int, alertKeyPrefix string) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := db.CreateComment(alertKeyPrefix, userID, "note", "comment"); err != nil {
			t.Fatalf("seed comment %d: %v", i, err)
		}
	}
}

func TestDeriveActivityKind(t *testing.T) {
	tests := []struct {
		name, kind, content, want string
	}{
		{"stored kind wins", "ack", "🔔 anything", "ack"},
		{"legacy ack emoji", "", "🔔 Alert acknowledged", "ack"},
		{"legacy unack emoji", "", "🔕 Alert unacknowledged", "unack"},
		{"legacy silence emoji", "", "🔇 Alert silenced for 2h", "silence"},
		{"legacy resolve emoji", "", "✅ Alert resolved", "resolve"},
		{"legacy plain text", "", "just a note", "comment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveActivityKind(tt.kind, tt.content); got != tt.want {
				t.Errorf("deriveActivityKind(%q, %q) = %q, want %q", tt.kind, tt.content, got, tt.want)
			}
		})
	}
}
