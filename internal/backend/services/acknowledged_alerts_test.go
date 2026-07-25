package services

import (
	"context"
	"testing"

	"notificator/config"
	"notificator/internal/backend/database"
	alertpb "notificator/internal/backend/proto/alert"
)

// TestGetAllAcknowledgedAlerts_SurfacesDatabaseError guards the webui cache:
// applyAcknowledgments clears every fingerprint missing from the response, so an
// empty map returned with a nil error would un-acknowledge the whole dashboard
// on a statement timeout or a failover. The failure has to reach the caller.
func TestGetAllAcknowledgedAlerts_SurfacesDatabaseError(t *testing.T) {
	// No AutoMigrate: the acknowledgments table is missing, so the query fails.
	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{SQLitePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	svc := NewAlertServiceGorm(db)

	resp, err := svc.GetAllAcknowledgedAlerts(context.Background(), &alertpb.GetAllAcknowledgedAlertsRequest{
		AlertKeys: []string{"fp-1"},
	})
	if err == nil {
		t.Fatalf("expected a database failure to surface as an error, got response %+v", resp)
	}
	if resp != nil {
		t.Errorf("expected no response alongside the error, got %+v", resp)
	}
}
