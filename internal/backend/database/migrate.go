package database

import (
	"log"
)

// RunCustomMigrations runs any custom SQL migrations that can't be handled by AutoMigrate
func (gdb *GormDB) RunCustomMigrations() error {
	log.Println("🔄 Running custom migrations...")

	log.Println("✅ Custom migrations completed")
	return nil
}
