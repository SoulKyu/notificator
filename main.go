// main.go - Modified
package main

import (
	"flag"
	"fmt"
	"log"

	"notificator/config"
	"notificator/internal/alertmanager"
	"notificator/internal/backend"
	"notificator/internal/gui"
)

func main() {
	var (
		backendMode = flag.Bool("backend", false, "Run in backend mode (GRPC server)")
		configPath  = flag.String("config", config.GetConfigPath(), "Path to config file")
		migrate     = flag.Bool("migrate", false, "Run database migrations (backend mode only)")
		dbType      = flag.String("db", "sqlite", "Database type: sqlite or postgres")
	)
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *backendMode {
		fmt.Println("🚀 Starting Notificator Backend Server...")
		runBackendMode(cfg, *migrate, *dbType)
	} else {
		fmt.Println("🖥️  Starting Notificator Desktop Client...")
		runFrontendMode(cfg, *configPath)
	}
}

func runBackendMode(cfg *config.Config, migrate bool, dbType string) {
	server := backend.NewServer(cfg, dbType)

	if migrate {
		if err := server.RunMigrations(); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
		fmt.Println("✅ Database migrations completed")
		return
	}

	if err := server.Start(); err != nil {
		log.Fatalf("Backend server failed: %v", err)
	}
}

func runFrontendMode(cfg *config.Config, configPath string) {
	// Existing frontend logic
	cfg.MergeHeaders()

	client := alertmanager.NewClientWithConfig(
		cfg.Alertmanager.URL,
		cfg.Alertmanager.Username,
		cfg.Alertmanager.Password,
		cfg.Alertmanager.Token,
		cfg.Alertmanager.Headers,
	)

	window := gui.NewAlertsWindow(client, configPath, cfg)
	window.Show()
}
