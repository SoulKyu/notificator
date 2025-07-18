// Backend-only main entry point
package main

import (
	"flag"
	"fmt"
	"log"

	"notificator/config"
	"notificator/internal/backend"
)

func main() {
	var (
		configPath = flag.String("config", config.GetConfigPath(), "Path to config file")
		dbType     = flag.String("db", "sqlite", "Database type: sqlite or postgres")
	)
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("🚀 Starting Notificator Backend Server...")
	
	server := backend.NewServer(cfg, *dbType)

	// Always run migrations on startup (they are idempotent)
	fmt.Println("📦 Running database migrations...")
	if err := server.RunMigrations(); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	fmt.Println("✅ Database migrations completed")

	// Start the server
	if err := server.Start(); err != nil {
		log.Fatalf("Backend server failed: %v", err)
	}
}