package database

import (
	"fmt"
	"log"
)

// RunCustomMigrations runs any custom SQL migrations that can't be handled by AutoMigrate
func (gdb *GormDB) RunCustomMigrations() error {
	log.Println("🔄 Running custom migrations...")

	// Clean up duplicate alert statistics before adding unique constraint
	if err := gdb.cleanupDuplicateStatistics(); err != nil {
		return fmt.Errorf("failed to cleanup duplicate statistics: %w", err)
	}

	log.Println("✅ Custom migrations completed")
	return nil
}

// cleanupDuplicateStatistics removes duplicate alert statistics before unique constraint is applied
func (gdb *GormDB) cleanupDuplicateStatistics() error {
	log.Println("🧹 Cleaning up duplicate alert statistics...")

	// First, check if the alert_statistics table exists
	var tableExists bool
	err := gdb.db.Raw(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_name = 'alert_statistics'
		)
	`).Scan(&tableExists).Error

	if err != nil || !tableExists {
		log.Println("ℹ️  alert_statistics table doesn't exist yet, skipping duplicate cleanup")
		return nil
	}

	// Check if unique constraint already exists
	var constraintExists bool
	err = gdb.db.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'alert_statistics'
			AND indexname = 'idx_unique_fingerprint_fired'
		)
	`).Scan(&constraintExists).Error

	if err == nil && constraintExists {
		log.Println("ℹ️  Unique constraint already exists, skipping duplicate cleanup")
		return nil
	}

	// Delete duplicate records, keeping only the most recent one (by created_at)
	// For duplicates with same (fingerprint, fired_at):
	// - Keep the record with the latest created_at (most recent insertion)
	// - Delete all others
	result := gdb.db.Exec(`
		DELETE FROM alert_statistics
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
					ROW_NUMBER() OVER (
						PARTITION BY fingerprint, fired_at
						ORDER BY created_at DESC
					) as row_num
				FROM alert_statistics
			) t
			WHERE row_num > 1
		)
	`)

	if result.Error != nil {
		return fmt.Errorf("failed to delete duplicate statistics: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		log.Printf("✅ Deleted %d duplicate alert statistic records", result.RowsAffected)
	} else {
		log.Println("ℹ️  No duplicate alert statistics found")
	}

	return nil
}
