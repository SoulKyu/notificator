package database

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"notificator/internal/backend/models"
)

func newStatsTestDB(t *testing.T) *GormDB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AlertStatistic{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &GormDB{db: db, dbType: "sqlite"}
}

// The same alert occurrence is re-announced whenever the webui rebuilds its
// cache (restart, reconnect): the capture must be idempotent, not an error.
func TestUpsertAlertStatisticIdempotent(t *testing.T) {
	gdb := newStatsTestDB(t)
	fired := time.Date(2026, 7, 15, 7, 21, 42, 0, time.UTC)

	for i := 0; i < 3; i++ {
		stat := &models.AlertStatistic{
			Fingerprint: "9149bd3b074f9cbad7d630188eabf6fc",
			AlertName:   "VaultDbInjectorLeaseExpirationLessThan1DayDev",
			Severity:    "warning",
			FiredAt:     fired,
			Metadata:    models.JSONB("{}"),
		}
		if err := gdb.UpsertAlertStatistic(stat); err != nil {
			t.Fatalf("upsert #%d: %v", i+1, err)
		}
	}

	var count int64
	gdb.db.Model(&models.AlertStatistic{}).Count(&count)
	if count != 1 {
		t.Fatalf("want exactly 1 row after 3 upserts of the same occurrence, got %d", count)
	}

	// a different fired_at is a new occurrence of the same alert → new row
	stat := &models.AlertStatistic{
		Fingerprint: "9149bd3b074f9cbad7d630188eabf6fc",
		AlertName:   "VaultDbInjectorLeaseExpirationLessThan1DayDev",
		Severity:    "warning",
		FiredAt:     fired.Add(time.Hour),
		Metadata:    models.JSONB("{}"),
	}
	if err := gdb.UpsertAlertStatistic(stat); err != nil {
		t.Fatalf("upsert new occurrence: %v", err)
	}
	gdb.db.Model(&models.AlertStatistic{}).Count(&count)
	if count != 2 {
		t.Fatalf("want 2 rows after a distinct fired_at, got %d", count)
	}
}
