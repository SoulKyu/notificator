package services

import (
	"path/filepath"
	"testing"
	"time"

	"notificator/config"
	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
)

// TestOnCallFilterOverlap verifies the on-call time-of-day filter counts an alert
// whose ACTIVE interval overlaps the 18:00-08:00 window, even when it fired during
// the day. Regression test for daytime-fired, long-running alerts being dropped from
// the statistics dashboard. Runs on SQLite (UTC), so it exercises the overlap math;
// the timezone wrapping is PostgreSQL-only.
func TestOnCallFilterOverlap(t *testing.T) {
	db, err := database.NewGormDB("sqlite", config.DatabaseConfig{
		SQLitePath: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.GetDB().AutoMigrate(&models.AlertStatistic{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	meta := models.JSONB(`{"labels":{"team":"team-infrastructure"}}`)
	mk := func(name string, fired, resolved time.Time) *models.AlertStatistic {
		r := resolved
		return &models.AlertStatistic{
			Fingerprint: name, AlertName: name, Severity: "critical",
			Metadata: meta, FiredAt: fired, ResolvedAt: &r,
		}
	}
	fixtures := []*models.AlertStatistic{
		// Tue 11:22 -> Wed 09:59 : fired daytime, spans the on-call night. INCLUDE.
		mk("spans-night",
			time.Date(2026, 6, 30, 11, 22, 0, 0, time.UTC),
			time.Date(2026, 7, 1, 9, 59, 0, 0, time.UTC)),
		// Mon 09:00 -> Mon 15:00 : entirely daytime weekday. EXCLUDE.
		mk("daytime-only",
			time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 29, 15, 0, 0, 0, time.UTC)),
		// Mon 20:00 -> Mon 23:00 : fired inside the window. INCLUDE.
		mk("evening",
			time.Date(2026, 6, 29, 20, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 29, 23, 0, 0, 0, time.UTC)),
	}
	for _, f := range fixtures {
		if err := db.GetDB().Create(f).Error; err != nil {
			t.Fatalf("insert %s: %v", f.Fingerprint, err)
		}
	}

	svc := NewStatisticsQueryService(db)
	for _, mode := range []string{"same_hours", "full_weekends"} {
		resp, err := svc.QueryStatistics(&QueryRequest{
			StartDate:         time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC),
			EndDate:           time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
			FilterByTimeOfDay: true,
			TimeOfDayStart:    "18:00",
			TimeOfDayEnd:      "08:00",
			WeekendMode:       mode,
			Limit:             100,
		})
		if err != nil {
			t.Fatalf("query (%s): %v", mode, err)
		}
		if resp.TotalAlerts != 2 {
			t.Errorf("mode %s: got %d alerts, want 2 (spans-night + evening; daytime-only excluded)", mode, resp.TotalAlerts)
		}
	}
}
