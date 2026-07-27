package database

import (
	"encoding/json"
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
	if err := db.AutoMigrate(&models.User{}, &models.Acknowledgment{}, &models.ResolvedAlert{}, &models.Comment{}); err != nil {
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

func TestCreateCommentStoresKind(t *testing.T) {
	gdb := newTestDB(t)
	u := models.User{ID: "u1", Username: "alice", Email: "a@example.com"}
	if err := gdb.db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	got, err := gdb.CreateComment("key-a", u.ID, "🔇 Alert silenced for 2h: x", "silence")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if got.Kind != "silence" {
		t.Fatalf("Kind = %q, want silence", got.Kind)
	}

	plain, err := gdb.CreateComment("key-a", u.ID, "looks fine", "comment")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if plain.Kind != "comment" {
		t.Fatalf("Kind = %q, want comment", plain.Kind)
	}
}

// TestLegacyCommentKindStaysEmpty guards against a regression where the
// `kind` column carried a `NOT NULL DEFAULT 'comment'` gorm tag. On an
// existing database, AutoMigrate's ALTER TABLE ADD COLUMN would then
// backfill every pre-existing row (including legacy 🔇/🔔/✅ comments
// written before `kind` existed) to "comment", permanently defeating the
// emoji-prefix fallback in deriveActivityKind/deriveCommentKind. The column
// must have no default so legacy rows read back with Kind == "".
func TestLegacyCommentKindStaysEmpty(t *testing.T) {
	gdb := newTestDB(t)
	u := models.User{ID: "u1", Username: "alice", Email: "a@example.com"}
	if err := gdb.db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Raw insert bypassing CreateComment's "" -> "comment" coercion, simulating
	// a legacy row written before the kind column existed.
	now := time.Now().UTC().Truncate(time.Second)
	if err := gdb.db.Exec(
		"INSERT INTO comments (id, alert_key, user_id, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"legacy-1", "key-a", u.ID, "🔇 Alert silenced for 2h: x", now, now,
	).Error; err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	got, err := gdb.GetRecentActivity(now.Add(-time.Minute), 100)
	if err != nil {
		t.Fatalf("GetRecentActivity: %v", err)
	}
	if len(got) != 1 || got[0].ID != "legacy-1" {
		t.Fatalf("got %v, want [legacy-1]", ids(got))
	}
	if got[0].Kind != "" {
		t.Fatalf("Kind = %q, want empty (legacy row must not be defaulted to 'comment')", got[0].Kind)
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

// TestCreateResolvedAlertSnapshotsAcknowledgmentsWhenCallerHasNone pins the
// other half of that lifecycle: the caller builds its snapshot with a separate,
// best-effort RPC, so when that fetch fails the rows must still be recorded
// before the same transaction deletes them.
func TestCreateResolvedAlertSnapshotsAcknowledgmentsWhenCallerHasNone(t *testing.T) {
	for _, payload := range [][]byte{nil, []byte(`[]`), []byte(`null`)} {
		gdb := newTestDB(t)

		alice := models.User{ID: "u1", Username: "alice", Email: "alice@example.com"}
		if err := gdb.db.Create(&alice).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
		ack := models.Acknowledgment{ID: "a1", AlertKey: "resolved-key", UserID: alice.ID, Reason: "on it"}
		if err := gdb.db.Create(&ack).Error; err != nil {
			t.Fatalf("create ack: %v", err)
		}

		resolved, err := gdb.CreateResolvedAlert("resolved-key", "prod", []byte(`{}`), nil, payload, 24)
		if err != nil {
			t.Fatalf("CreateResolvedAlert(%q): %v", payload, err)
		}

		var snapshot []models.AcknowledgmentWithUser
		if err := json.Unmarshal([]byte(resolved.Acknowledgments), &snapshot); err != nil {
			t.Fatalf("snapshot for payload %q is not readable acknowledgment history: %v (%q)", payload, err, resolved.Acknowledgments)
		}
		if len(snapshot) != 1 || snapshot[0].Username != "alice" || snapshot[0].Reason != "on it" {
			t.Errorf("payload %q: acknowledgment history lost, got %+v", payload, snapshot)
		}

		if live, _ := gdb.GetAcknowledgments("resolved-key"); len(live) != 0 {
			t.Errorf("payload %q: live acknowledgments should still be cleared, got %d", payload, len(live))
		}
	}
}

// TestCleanupResolvedAcknowledgments covers the startup migration that drops
// acknowledgment rows orphaned by resolutions that happened before the fix. It
// re-runs on every boot, so it must keep the acknowledgment of an alert that
// resolved and then fired again inside the resolved-alert retention window.
func TestCleanupResolvedAcknowledgments(t *testing.T) {
	gdb := newTestDB(t)

	alice := models.User{ID: "u1", Username: "alice", Email: "alice@example.com"}
	if err := gdb.db.Create(&alice).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now()
	acks := []models.Acknowledgment{
		// Acknowledged before the resolution: belongs to the firing that ended.
		{ID: "a1", AlertKey: "orphan-key", UserID: alice.ID, Reason: "stale", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "a2", AlertKey: "firing-key", UserID: alice.ID, Reason: "current", CreatedAt: now.Add(-2 * time.Hour)},
		// Acknowledged after the resolution: belongs to the firing that is live now.
		{ID: "a3", AlertKey: "refired-key", UserID: alice.ID, Reason: "on the new one", CreatedAt: now.Add(-5 * time.Minute)},
	}
	for i := range acks {
		if err := gdb.db.Create(&acks[i]).Error; err != nil {
			t.Fatalf("create ack: %v", err)
		}
	}

	resolved := []models.ResolvedAlert{
		{Fingerprint: "orphan-key", AlertData: models.JSONB(`{}`), ResolvedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour), Source: "prod"},
		{Fingerprint: "refired-key", AlertData: models.JSONB(`{}`), ResolvedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour), Source: "prod"},
	}
	for i := range resolved {
		if err := gdb.db.Create(&resolved[i]).Error; err != nil {
			t.Fatalf("create resolved alert: %v", err)
		}
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
	if refired, _ := gdb.GetAcknowledgments("refired-key"); len(refired) != 1 {
		t.Errorf("acknowledgment created after the resolution belongs to the live firing, got %d", len(refired))
	}
}

func TestGetRecentActivity(t *testing.T) {
	gdb := newTestDB(t)
	u := models.User{ID: "u1", Username: "alice", Email: "a@example.com"}
	if err := gdb.db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	// three comments at -3h, -1h, -10m
	seed := []models.Comment{
		{ID: "c1", AlertKey: "k1", UserID: u.ID, Content: "old", Kind: "comment", CreatedAt: base.Add(-3 * time.Hour)},
		{ID: "c2", AlertKey: "k1", UserID: u.ID, Content: "🔇 silenced", Kind: "silence", CreatedAt: base.Add(-1 * time.Hour)},
		{ID: "c3", AlertKey: "k2", UserID: u.ID, Content: "recent", Kind: "comment", CreatedAt: base.Add(-10 * time.Minute)},
	}
	for i := range seed {
		if err := gdb.db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// window = last 2h → excludes c1; newest first → c3, c2
	got, err := gdb.GetRecentActivity(base.Add(-2*time.Hour), 100)
	if err != nil {
		t.Fatalf("GetRecentActivity: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c3" || got[1].ID != "c2" {
		t.Fatalf("got %d rows %v, want [c3 c2]", len(got), ids(got))
	}
	if got[0].Username != "alice" {
		t.Fatalf("username = %q, want alice", got[0].Username)
	}

	// limit caps the result
	got, err = gdb.GetRecentActivity(base.Add(-4*time.Hour), 1)
	if err != nil {
		t.Fatalf("GetRecentActivity: %v", err)
	}
	if len(got) != 1 || got[0].ID != "c3" {
		t.Fatalf("limit=1 got %v, want [c3]", ids(got))
	}
}

func ids(rows []models.CommentWithUser) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}
