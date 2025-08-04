//go:build !nogui

package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"notificator/config"
	"notificator/internal/alertmanager"
	"notificator/internal/gui"
)

// desktopCmd represents the desktop command
var desktopCmd = &cobra.Command{
	Use:   "desktop",
	Short: "Start the Notificator desktop GUI application",
	Long: `Start the Notificator desktop GUI application which provides:
- Real-time alert monitoring from multiple Alertmanagers
- Desktop notifications for critical alerts
- Alert management interface with acknowledgments and comments
- System tray integration and background monitoring
- Multi-Alertmanager support with connection testing`,
	Run: runDesktop,
}

func init() {
	rootCmd.AddCommand(desktopCmd)

	// Desktop-specific flags
	desktopCmd.Flags().String("db-type", "sqlite", "Database type: sqlite or postgres")

	// Bind flags to viper
	viper.BindPFlag("desktop.database.type", desktopCmd.Flags().Lookup("db-type"))
}

func runDesktop(cmd *cobra.Command, args []string) {
	// Load configuration using Viper
	cfg, err := config.LoadConfigWithViper()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("🖥️  Starting Notificator Desktop Client...")
	fmt.Printf("   Config file: %s\n", viper.ConfigFileUsed())

	runFrontendMode(cfg, viper.ConfigFileUsed())
}

func runFrontendMode(cfg *config.Config, configPath string) {
	// Existing frontend logic
	cfg.MergeHeaders()

	// Check and validate configuration Alertmanagers
	checkAndMigrateConfig(cfg, configPath)

	fmt.Printf("Loaded configuration from: %s\n", configPath)
	fmt.Printf("Configured Alertmanagers: %d\n", len(cfg.Alertmanagers))

	// Display all configured Alertmanagers
	for i, amConfig := range cfg.Alertmanagers {
		fmt.Printf("  [%d] %s: %s\n", i+1, amConfig.Name, amConfig.URL)

		if len(amConfig.Headers) > 0 {
			fmt.Printf("      Custom headers: ")
			for key := range amConfig.Headers {
				fmt.Printf("%s ", key)
			}
			fmt.Println()
		}
	}

	// Create MultiClient from configuration
	multiClient := alertmanager.NewMultiClient(cfg)

	// Test connectivity to all Alertmanagers
	fmt.Println("\nTesting connectivity to all Alertmanagers...")
	connectionResults := multiClient.TestAllConnections()

	healthyCount := 0
	for name, err := range connectionResults {
		if err != nil {
			fmt.Printf("  ❌ %s: %v\n", name, err)
		} else {
			fmt.Printf("  ✅ %s: Connected successfully\n", name)
			healthyCount++
		}
	}

	if healthyCount == 0 {
		fmt.Println("\n❌ No Alertmanagers are accessible!")
		fmt.Println("\n💡 To fix connection issues:")
		fmt.Println("1. Check that Alertmanager URLs are correct and accessible")
		fmt.Println("2. Verify authentication credentials (username/password/token)")
		fmt.Println("3. For OAuth bypass, ensure X-Oauth-Bypass-Token is set:")
		fmt.Println("   export METRICS_PROVIDER_HEADERS=\"X-Oauth-Bypass-Token=your-token\"")
		fmt.Println("4. Check your configuration file at:", configPath)
		os.Exit(1)
	}

	fmt.Printf("\n✅ %d/%d Alertmanagers are healthy\n", healthyCount, len(cfg.Alertmanagers))

	// Fetch alerts from all Alertmanagers for initial validation
	fmt.Println("\nFetching alerts from all Alertmanagers...")
	allAlerts, err := multiClient.FetchAllAlerts()
	if err != nil {
		log.Fatalf("Failed to fetch alerts: %v", err)
	}

	// Count alerts by source and activity
	alertsBySource := make(map[string]int)
	activeBySource := make(map[string]int)
	totalActive := 0

	for _, alertWithSource := range allAlerts {
		alertsBySource[alertWithSource.Source]++
		if alertWithSource.Alert.IsActive() {
			activeBySource[alertWithSource.Source]++
			totalActive++
		}
	}
	fmt.Printf("\n📊 Alert Summary:\n")
	fmt.Printf("  Total alerts: %d (%d active)\n", len(allAlerts), totalActive)

	for source, count := range alertsBySource {
		activeCount := activeBySource[source]
		fmt.Printf("  %s: %d alerts (%d active)\n", source, count, activeCount)
	}

	fmt.Println("\n🚀 Starting GUI...")
	// Pass the MultiClient to the GUI for full multi-Alertmanager support
	window := gui.NewAlertsWindow(multiClient, configPath, cfg)
	window.Show()

	// Save MultiClient reference for future GUI updates
	// TODO: Pass MultiClient to GUI when it's updated to support multiple Alertmanagers
	_ = multiClient

	// Display additional information about other available clients
	allHealthyClients := multiClient.GetHealthyClients()
	if len(allHealthyClients) > 1 {
		fmt.Printf("Note: GUI now supports all %d healthy Alertmanagers:\n", len(allHealthyClients))
		for name, client := range allHealthyClients {
			fmt.Printf("  - %s (%s)\n", name, client.BaseURL)
		}
	}

	// Display detailed statistics for each Alertmanager
	displayAlertmanagerStats(multiClient)
}

// checkAndMigrateConfig checks if the configuration needs migration from single to multiple Alertmanagers
func checkAndMigrateConfig(cfg *config.Config, configPath string) {
	// Check if configuration has been migrated
	if len(cfg.Alertmanagers) == 0 {
		fmt.Println("⚠️  No Alertmanagers configured. Please check your configuration file.")
		fmt.Println("Expected format:")
		fmt.Println(`{
  "alertmanagers": [
    {
      "name": "default",
      "url": "http://localhost:9093",
      "username": "",
      "password": "",
      "token": "",
      "headers": {},
	  "oauth": {
	    "enabled": false,
		"proxy_mode": true
	  }
    }
  ]
}`)
		fmt.Printf("Configuration file: %s\n", configPath)
		os.Exit(1)
	}

	// Validate all Alertmanager configurations
	if err := cfg.ValidateAlertmanagers(); err != nil {
		fmt.Printf("❌ Configuration validation failed: %v\n", err)
		fmt.Printf("Please check your configuration file: %s\n", configPath)
		os.Exit(1)
	}

	fmt.Println("✅ Configuration validated successfully")
}

// displayAlertmanagerStats shows detailed statistics about each Alertmanager
func displayAlertmanagerStats(multiClient *alertmanager.MultiClient) {
	fmt.Println("\n📈 Detailed Alertmanager Statistics:")

	allClients := multiClient.GetAllClients()
	for name, client := range allClients {
		fmt.Printf("\n🔹 %s (%s):\n", name, client.BaseURL)

		// Test connection
		if err := client.TestConnection(); err != nil {
			fmt.Printf("  ❌ Connection: Failed (%v)\n", err)
			continue
		}
		fmt.Printf("  ✅ Connection: OK\n")

		// Get alerts
		alerts, err := client.FetchAlerts()
		if err != nil {
			fmt.Printf("  ❌ Alerts: Failed to fetch (%v)\n", err)
			continue
		}

		activeCount := 0
		severityCounts := make(map[string]int)

		for _, alert := range alerts {
			if alert.IsActive() {
				activeCount++
			}

			// Count by severity
			severity := alert.Labels["severity"]
			if severity == "" {
				severity = "unknown"
			}
			severityCounts[severity]++
		}

		fmt.Printf("  📊 Total alerts: %d (%d active)\n", len(alerts), activeCount)

		if len(severityCounts) > 0 {
			fmt.Printf("  📋 By severity: ")
			for severity, count := range severityCounts {
				fmt.Printf("%s=%d ", severity, count)
			}
			fmt.Println()
		}

		// Get silences
		silences, err := client.FetchSilences()
		if err != nil {
			fmt.Printf("  ❌ Silences: Failed to fetch (%v)\n", err)
		} else {
			activeSilences := 0
			for _, silence := range silences {
				if silence.Status.State == "active" {
					activeSilences++
				}
			}
			fmt.Printf("  🔇 Silences: %d (%d active)\n", len(silences), activeSilences)
		}
	}
}
