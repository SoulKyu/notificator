package database

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"notificator/internal/backend/models"
)

func newTestDB(t *testing.T) *GormDB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Acknowledgment{}, &models.ResolvedAlert{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &GormDB{db: db, dbType: "sqlite"}
}

func TestGetAllAcknowledgedAlerts(t *testing.T) {
	gdb := newTestDB(t)

	alice := models.User{ID: "u1", Username: "alice", Email: "alice@example.com"}
	bob := models.User{ID: "u2", Username: "bob", Email: "bob@example.com"}
	if err := gdb.db.Create([]*models.User{&alice, &bob}).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Second)
	acks := []models.Acknowledgment{
		{ID: "a1", AlertKey: "key-a", UserID: alice.ID, Reason: "old", CreatedAt: base.Add(-time.Hour)},
		{ID: "a2", AlertKey: "key-a", UserID: bob.ID, Reason: "latest", CreatedAt: base},
		{ID: "b1", AlertKey: "key-b", UserID: alice.ID, Reason: "only", CreatedAt: base},
		{ID: "c1", AlertKey: "key-not-cached", UserID: alice.ID, Reason: "stale", CreatedAt: base},
	}
	for i := range acks {
		if err := gdb.db.Create(&acks[i]).Error; err != nil {
			t.Fatalf("create ack: %v", err)
		}
	}

	result, err := gdb.GetAllAcknowledgedAlerts([]string{"key-a", "key-b", "key-missing"})
	if err != nil {
		t.Fatalf("GetAllAcknowledgedAlerts: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 acks, got %d: %v", len(result), result)
	}
	if got := result["key-a"]; got.Reason != "latest" || got.Username != "bob" {
		t.Errorf("key-a: expected latest ack by bob, got reason=%q user=%q", got.Reason, got.Username)
	}
	if got := result["key-b"]; got.Reason != "only" || got.Username != "alice" {
		t.Errorf("key-b: expected ack by alice, got reason=%q user=%q", got.Reason, got.Username)
	}
	if _, ok := result["key-not-cached"]; ok {
		t.Errorf("key-not-cached must not be returned when not requested")
	}

	empty, err := gdb.GetAllAcknowledgedAlerts(nil)
	if err != nil {
		t.Fatalf("empty keys: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no acks for empty key list, got %v", empty)
	}
}

func TestAcknowledgmentCompositeIndexExists(t *testing.T) {
	gdb := newTestDB(t)
	if !gdb.db.Migrator().HasIndex(&models.Acknowledgment{}, "idx_acknowledgments_alert_key_created_at") {
		t.Fatal("composite index idx_acknowledgments_alert_key_created_at missing after migration")
	}
}

// TestCreateResolvedAlertClearsAcknowledgments pins the ack lifecycle to a
// firing: once the alert resolves, the live rows go away so the next firing of
// the same labels — same fingerprint — is not acknowledged on someone's behalf.
func TestCreateResolvedAlertClearsAcknowledgments(t *testing.T) {
	gdb := newTestDB(t)

	alice := models.User{ID: "u1", Username: "alice", Email: "alice@example.com"}
	if err := gdb.db.Create(&alice).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	acks := []models.Acknowledgment{
		{ID: "a1", AlertKey: "resolved-key", UserID: alice.ID, Reason: "on it"},
		{ID: "a2", AlertKey: "other-key", UserID: alice.ID, Reason: "unrelated"},
	}
	for i := range acks {
		if err := gdb.db.Create(&acks[i]).Error; err != nil {
			t.Fatalf("create ack: %v", err)
		}
	}

	snapshot := []byte(`[{"username":"alice","reason":"on it"}]`)
	resolved, err := gdb.CreateResolvedAlert("resolved-key", "prod", []byte(`{}`), nil, snapshot, 24)
	if err != nil {
		t.Fatalf("CreateResolvedAlert: %v", err)
	}

	// History survives on the resolved snapshot.
	if string(resolved.Acknowledgments) != string(snapshot) {
		t.Errorf("resolved alert should keep the acknowledgment snapshot, got %q", resolved.Acknowledgments)
	}

	live, err := gdb.GetAcknowledgments("resolved-key")
	if err != nil {
		t.Fatalf("GetAcknowledgments: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("live acknowledgments should be cleared on resolution, got %d", len(live))
	}

	if others, _ := gdb.GetAcknowledgments("other-key"); len(others) != 1 {
		t.Errorf("acknowledgments of other alerts must be untouched, got %d", len(others))
	}
}

// TestCleanupResolvedAcknowledgments covers the one-off migration that drops
// acknowledgment rows orphaned by resolutions that happened before the fix.
func TestCleanupResolvedAcknowledgments(t *testing.T) {
	gdb := newTestDB(t)

	alice := models.User{ID: "u1", Username: "alice", Email: "alice@example.com"}
	if err := gdb.db.Create(&alice).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	acks := []models.Acknowledgment{
		{ID: "a1", AlertKey: "orphan-key", UserID: alice.ID, Reason: "stale"},
		{ID: "a2", AlertKey: "firing-key", UserID: alice.ID, Reason: "current"},
	}
	for i := range acks {
		if err := gdb.db.Create(&acks[i]).Error; err != nil {
			t.Fatalf("create ack: %v", err)
		}
	}

	now := time.Now()
	if err := gdb.db.Create(&models.ResolvedAlert{
		Fingerprint: "orphan-key",
		AlertData:   models.JSONB(`{}`),
		ResolvedAt:  now.Add(-time.Hour),
		ExpiresAt:   now.Add(23 * time.Hour),
		Source:      "prod",
	}).Error; err != nil {
		t.Fatalf("create resolved alert: %v", err)
	}

	if err := gdb.cleanupResolvedAcknowledgments(); err != nil {
		t.Fatalf("cleanupResolvedAcknowledgments: %v", err)
	}

	if orphans, _ := gdb.GetAcknowledgments("orphan-key"); len(orphans) != 0 {
		t.Errorf("orphaned acknowledgments should be deleted, got %d", len(orphans))
	}
	if firing, _ := gdb.GetAcknowledgments("firing-key"); len(firing) != 1 {
		t.Errorf("acknowledgment of a still-firing alert must survive, got %d", len(firing))
	}
}
