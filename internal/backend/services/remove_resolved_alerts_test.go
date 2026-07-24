package services

import (
	"context"
	"testing"
	"time"

	"notificator/config"
	"notificator/internal/backend/database"
	alertpb "notificator/internal/backend/proto/alert"
)

func setupAlertServiceWithResolvedAlert(t *testing.T) (*AlertServiceGorm, *database.GormDB) {
	t.Helper()

	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	if _, err := db.CreateResolvedAlert("fp-1", "test", []byte(`{}`), []byte(`[]`), []byte(`[]`), 24); err != nil {
		t.Fatalf("failed to seed resolved alert: %v", err)
	}

	return NewAlertServiceGorm(db), db
}

func resolvedAlertCount(t *testing.T, db *database.GormDB) int64 {
	t.Helper()
	count, err := db.GetResolvedAlertsCount()
	if err != nil {
		t.Fatalf("failed to count resolved alerts: %v", err)
	}
	return count
}

func TestRemoveAllResolvedAlerts_RejectsMissingSession(t *testing.T) {
	svc, db := setupAlertServiceWithResolvedAlert(t)

	resp, err := svc.RemoveAllResolvedAlerts(context.Background(), &alertpb.RemoveAllResolvedAlertsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected Success=false for missing session")
	}
	if count := resolvedAlertCount(t, db); count != 1 {
		t.Errorf("expected resolved alerts untouched, got count %d", count)
	}
}

func TestRemoveAllResolvedAlerts_RejectsInvalidSession(t *testing.T) {
	svc, db := setupAlertServiceWithResolvedAlert(t)

	resp, err := svc.RemoveAllResolvedAlerts(context.Background(), &alertpb.RemoveAllResolvedAlertsRequest{SessionId: "bogus"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected Success=false for invalid session")
	}
	if count := resolvedAlertCount(t, db); count != 1 {
		t.Errorf("expected resolved alerts untouched, got count %d", count)
	}
}

func TestRemoveAllResolvedAlerts_ValidSessionDeletes(t *testing.T) {
	svc, db := setupAlertServiceWithResolvedAlert(t)

	user, err := db.CreateUser("tester", "tester@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := db.CreateSession(user.ID, "session-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	resp, err := svc.RemoveAllResolvedAlerts(context.Background(), &alertpb.RemoveAllResolvedAlertsRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected Success=true, got message: %s", resp.Message)
	}
	if resp.RemovedCount != 1 {
		t.Errorf("expected RemovedCount=1, got %d", resp.RemovedCount)
	}
	if count := resolvedAlertCount(t, db); count != 0 {
		t.Errorf("expected all resolved alerts removed, got count %d", count)
	}
}
