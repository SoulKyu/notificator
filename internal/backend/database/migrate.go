package database

import (
	"fmt"
	"log"
)

// RunCustomMigrations runs any custom SQL migrations that can't be handled by AutoMigrate
func (gdb *GormDB) RunCustomMigrations() error {
	log.Println("🔄 Running custom migrations...")

	// Check if UserSentryConfig table needs user_id column type migration
	if err := gdb.migrateUserSentryConfigUserID(); err != nil {
		return fmt.Errorf("failed to migrate UserSentryConfig.user_id: %w", err)
	}

	log.Println("✅ Custom migrations completed")
	return nil
}

// migrateUserSentryConfigUserID handles the migration from numeric user_id to string user_id
func (gdb *GormDB) migrateUserSentryConfigUserID() error {
	// Check if the table exists
	var tableExists bool
	err := gdb.db.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'user_sentry_configs')").Scan(&tableExists).Error
	if err != nil {
		// If we can't check (maybe SQLite), just continue - AutoMigrate will handle it
		log.Printf("Could not check if user_sentry_configs table exists (probably SQLite): %v", err)
		return nil
	}

	if !tableExists {
		log.Println("user_sentry_configs table does not exist yet, will be created by AutoMigrate")
		return nil
	}

	// Check if user_id column is numeric type (needs migration)
	var columnType string
	err = gdb.db.Raw(`
		SELECT data_type 
		FROM information_schema.columns 
		WHERE table_name = 'user_sentry_configs' 
		AND column_name = 'user_id'
	`).Scan(&columnType).Error

	if err != nil {
		// If we can't check column type (maybe SQLite), let AutoMigrate handle it
		log.Printf("Could not check user_id column type (probably SQLite): %v", err)
		return nil
	}

	if columnType == "integer" || columnType == "bigint" {
		log.Println("🔄 Migrating user_sentry_configs.user_id from numeric to varchar(32)")
		
		// Execute the migration SQL for PostgreSQL
		migrationSQL := `
		DO $$
		BEGIN
			-- Add temporary column
			ALTER TABLE user_sentry_configs ADD COLUMN IF NOT EXISTS user_id_new VARCHAR(32);
			
			-- Convert existing data
			UPDATE user_sentry_configs SET user_id_new = CAST(user_id AS VARCHAR(32)) WHERE user_id_new IS NULL;
			
			-- Drop old column and rename new one
			ALTER TABLE user_sentry_configs DROP COLUMN user_id;
			ALTER TABLE user_sentry_configs RENAME COLUMN user_id_new TO user_id;
			
			-- Add constraints
			ALTER TABLE user_sentry_configs ALTER COLUMN user_id SET NOT NULL;
		END
		$$;`

		if err := gdb.db.Exec(migrationSQL).Error; err != nil {
			return fmt.Errorf("failed to execute user_id migration SQL: %w", err)
		}

		log.Println("✅ Successfully migrated user_sentry_configs.user_id to varchar(32)")
	} else {
		log.Println("user_sentry_configs.user_id is already varchar type, no migration needed")
	}

	return nil
}