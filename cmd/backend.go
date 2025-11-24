package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"notificator/config"
	"notificator/internal/backend"
)

// backendCmd represents the backend command
var backendCmd = &cobra.Command{
	Use:   "backend",
	Short: "Start the Notificator backend server",
	Long: `Start the Notificator backend server which provides:
- gRPC API for alert management
- HTTP API for web interface
- Database persistence
- Alert acknowledgment and comments`,
	Run: runBackend,
}

func init() {
	rootCmd.AddCommand(backendCmd)

	// Backend-specific flags
	backendCmd.Flags().String("db-type", "", "Database type: sqlite or postgres (overrides config file)")
	backendCmd.Flags().String("grpc-listen", ":50051", "gRPC server listen address")
	backendCmd.Flags().String("http-listen", ":8080", "HTTP server listen address")
	backendCmd.Flags().Bool("migrate", true, "Run database migrations on startup")

	// Bind flags to viper
	viper.BindPFlag("backend.database.type", backendCmd.Flags().Lookup("db-type"))
	viper.BindPFlag("backend.grpc_listen", backendCmd.Flags().Lookup("grpc-listen"))
	viper.BindPFlag("backend.http_listen", backendCmd.Flags().Lookup("http-listen"))
	viper.BindPFlag("backend.migrate", backendCmd.Flags().Lookup("migrate"))
}

func runBackend(cmd *cobra.Command, args []string) {
	// Load configuration using Viper
	cfg, err := config.LoadConfigWithViper()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Merge headers from environment variables (e.g., METRICS_PROVIDER_HEADERS)
	cfg.MergeHeaders()

	// Get database type from flag first, then fall back to config
	dbType := viper.GetString("backend.database.type")
	if dbType == "" {
		dbType = cfg.Backend.Database.Type
	}

	fmt.Println("🚀 Starting Notificator Backend Server...")
	fmt.Printf("   Config file: %s\n", viper.ConfigFileUsed())
	fmt.Printf("   gRPC Listen: %s\n", cfg.Backend.GRPCListen)
	fmt.Printf("   HTTP Listen: %s\n", cfg.Backend.HTTPListen)
	fmt.Printf("   Database: %s\n", dbType)

	server := backend.NewServer(cfg, dbType)

	// Run migrations if enabled
	if viper.GetBool("backend.migrate") {
		fmt.Println("📦 Running database migrations...")
		if err := server.RunMigrations(); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
		fmt.Println("✅ Database migrations completed")
	}

	// Start the server
	if err := server.Start(); err != nil {
		log.Fatalf("Backend server failed: %v", err)
	}
}
