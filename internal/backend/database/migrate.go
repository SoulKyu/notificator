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

	// Add column_configs field to filter_presets table
	if err := gdb.migrateColumnConfigs(); err != nil {
		return fmt.Errorf("failed to migrate column configs: %w", err)
	}

	// Create user_column_preferences table if needed
	if err := gdb.migrateUserColumnPreferences(); err != nil {
		return fmt.Errorf("failed to migrate user column preferences: %w", err)
	}

	log.Println("✅ Custom migrations completed")
	return nil
}

// cleanupDuplicateStatistics removes duplicate alert statistics before unique constraint is applied
func (gdb *GormDB) cleanupDuplicateStatistics() error {
	log.Println("🧹 Cleaning up duplicate alert statistics...")

	// Detect database type
	dbName := gdb.db.Dialector.Name()

	// First, check if the alert_statistics table exists
	var tableExists int
	var err error

	if dbName == "sqlite" {
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM sqlite_master
			WHERE type='table' AND name='alert_statistics'
		`).Scan(&tableExists).Error
	} else {
		// PostgreSQL
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_name='alert_statistics'
		`).Scan(&tableExists).Error
	}

	if err != nil || tableExists == 0 {
		log.Println("ℹ️  alert_statistics table doesn't exist yet, skipping duplicate cleanup")
		return nil
	}

	// Check if unique constraint already exists
	var constraintExists int

	if dbName == "sqlite" {
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM sqlite_master
			WHERE type='index' AND name='idx_unique_fingerprint_fired'
			AND tbl_name='alert_statistics'
		`).Scan(&constraintExists).Error
	} else {
		// PostgreSQL
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM pg_indexes
			WHERE indexname='idx_unique_fingerprint_fired'
			AND tablename='alert_statistics'
		`).Scan(&constraintExists).Error
	}

	if err == nil && constraintExists > 0 {
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

// migrateColumnConfigs adds the column_configs field to filter_presets table
func (gdb *GormDB) migrateColumnConfigs() error {
	log.Println("🔄 Migrating filter presets to include column configs...")

	// Detect database type
	dbName := gdb.db.Dialector.Name()

	// Check if filter_presets table exists
	var tableExists int
	var err error

	if dbName == "sqlite" {
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM sqlite_master
			WHERE type='table' AND name='filter_presets'
		`).Scan(&tableExists).Error
	} else {
		// PostgreSQL
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_name='filter_presets'
		`).Scan(&tableExists).Error
	}

	if err != nil || tableExists == 0 {
		log.Println("ℹ️  filter_presets table doesn't exist yet, skipping column_configs migration")
		return nil
	}

	// Check if column_configs column exists
	var columnExists int

	if dbName == "sqlite" {
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM pragma_table_info('filter_presets')
			WHERE name = 'column_configs'
		`).Scan(&columnExists).Error
	} else {
		// PostgreSQL
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name='filter_presets' AND column_name='column_configs'
		`).Scan(&columnExists).Error
	}

	if err == nil && columnExists > 0 {
		log.Println("ℹ️  column_configs column already exists")
		return nil
	}

	// Add column_configs column (use appropriate type for database)
	var alterQuery string
	if dbName == "sqlite" {
		alterQuery = `
			ALTER TABLE filter_presets
			ADD COLUMN column_configs TEXT DEFAULT '[]'
		`
	} else {
		// PostgreSQL uses JSONB
		alterQuery = `
			ALTER TABLE filter_presets
			ADD COLUMN column_configs JSONB DEFAULT '[]'::jsonb
		`
	}

	result := gdb.db.Exec(alterQuery)
	if result.Error != nil {
		return fmt.Errorf("failed to add column_configs column: %w", result.Error)
	}

	log.Println("✅ Added column_configs column to filter_presets table")
	return nil
}

// migrateUserColumnPreferences creates the user_column_preferences table if it doesn't exist
func (gdb *GormDB) migrateUserColumnPreferences() error {
	log.Println("🔄 Migrating user column preferences table...")

	// Detect database type
	dbName := gdb.db.Dialector.Name()

	// Check if table already exists
	var tableExists int
	var err error

	if dbName == "sqlite" {
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM sqlite_master
			WHERE type='table' AND name='user_column_preferences'
		`).Scan(&tableExists).Error
	} else {
		// PostgreSQL
		err = gdb.db.Raw(`
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_name='user_column_preferences'
		`).Scan(&tableExists).Error
	}

	if err == nil && tableExists > 0 {
		log.Println("ℹ️  user_column_preferences table already exists")
		return nil
	}

	// Create the table with appropriate data types
	var createQuery string
	if dbName == "sqlite" {
		createQuery = `
			CREATE TABLE IF NOT EXISTS user_column_preferences (
				user_id VARCHAR(32) PRIMARY KEY,
				column_configs TEXT NOT NULL DEFAULT '[]',
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)
		`
	} else {
		// PostgreSQL
		createQuery = `
			CREATE TABLE IF NOT EXISTS user_column_preferences (
				user_id VARCHAR(32) PRIMARY KEY,
				column_configs JSONB NOT NULL DEFAULT '[]'::jsonb,
				created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
			)
		`
	}

	result := gdb.db.Exec(createQuery)
	if result.Error != nil {
		return fmt.Errorf("failed to create user_column_preferences table: %w", result.Error)
	}

	// Create index on user_id
	result = gdb.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_user_column_preferences_user_id
		ON user_column_preferences(user_id)
	`)

	if result.Error != nil {
		return fmt.Errorf("failed to create index on user_column_preferences: %w", result.Error)
	}

	log.Println("✅ Created user_column_preferences table")
	return nil
}
