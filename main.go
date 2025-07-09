package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"notificator/config"
	"notificator/internal/alertmanager"
	"notificator/internal/gui"
)

func main() {
	configPath := config.GetConfigPath()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	cfg.MergeHeaders()

	fmt.Printf("Loaded configuration from: %s\n", configPath)
	fmt.Printf("Alertmanager URL: %s\n", cfg.Alertmanager.URL)

	if len(cfg.Alertmanager.Headers) > 0 {
		fmt.Printf("Custom headers configured: ")
		for key := range cfg.Alertmanager.Headers {
			fmt.Printf("%s ", key)
		}
		fmt.Println()
	}

	var client *alertmanager.Client

	if cfg.Alertmanager.OAuth != nil && cfg.Alertmanager.OAuth.Enabled && cfg.Alertmanager.OAuth.ProxyMode {
		fmt.Println("OAuth proxy mode enabled - setting up proxy authentication...")
		client = alertmanager.NewClientWithProxyAuth(cfg.Alertmanager.URL)

		ctx := context.Background()
		if err := client.ProxyAuthManager.Authenticate(ctx); err != nil {
			log.Fatalf("OAuth proxy authentication failed: %v", err)
		}
		fmt.Println("✓ OAuth proxy authentication successful")

	} else {
		fmt.Println("Using traditional authentication (headers/tokens)...")
		client = alertmanager.NewClientWithConfig(
			cfg.Alertmanager.URL,
			cfg.Alertmanager.Username,
			cfg.Alertmanager.Password,
			cfg.Alertmanager.Token,
			cfg.Alertmanager.Headers,
		)

		// Only test OAuth bypass if we have an OAuth bypass token configured
		if bypassToken, hasToken := cfg.Alertmanager.Headers["X-Oauth-Bypass-Token"]; hasToken && bypassToken != "" {
			fmt.Println()
			if err := client.TestOAuthBypass(); err != nil {
				fmt.Printf("OAuth bypass test failed: %v\n", err)
				fmt.Println("\n💡 To fix this, you have two options:")
				fmt.Println("Option 1 - Use OAuth bypass token:")
				fmt.Println("1. Get your OAuth bypass token from your infrastructure team")
				fmt.Println("2. Set it via environment variable:")
				fmt.Println("   export METRICS_PROVIDER_HEADERS=\"X-Oauth-Bypass-Token=your-token\"")
				fmt.Println("3. Or add it to your config file at:", configPath)
				fmt.Println()
				fmt.Println("Option 2 - Enable OAuth authentication:")
				fmt.Println("1. Add OAuth configuration to your config file:")
				fmt.Println("   \"oauth\": {")
				fmt.Println("     \"enabled\": true,")
				fmt.Println("     \"client_id\": \"your-client-id\",")
				fmt.Println("     \"client_secret\": \"your-client-secret\",")
				fmt.Println("     \"auth_url\": \"https://your-oauth-provider/auth\",")
				fmt.Println("     \"token_url\": \"https://your-oauth-provider/token\",")
				fmt.Println("     \"redirect_url\": \"http://localhost:8080/callback\",")
				fmt.Println("     \"scopes\": [\"read\"]")
				fmt.Println("   }")
				fmt.Println()

				fmt.Println("=== DEBUG REQUEST ===")
				client.DebugRequest("/api/v2/alerts")

				os.Exit(1)
			}
		} else {
			// No OAuth bypass token configured - check if any authentication is configured
			hasAuth := cfg.Alertmanager.Username != "" || cfg.Alertmanager.Password != "" || cfg.Alertmanager.Token != "" || len(cfg.Alertmanager.Headers) > 0

			if hasAuth {
				fmt.Println("Authentication configured (username/password/token/headers)")
			} else {
				fmt.Println("No authentication configured - connecting directly to alertmanager")
			}
		}
	}

	client.DebugURL()

	fmt.Println("Testing connection to Alertmanager...")
	if err := client.TestConnection(); err != nil {
		log.Fatalf("Failed to connect to Alertmanager: %v", err)
	}
	fmt.Println("✓ Connected to Alertmanager successfully")

	fmt.Println("Fetching alerts for initial validation...")
	alerts, err := client.FetchAlerts()
	if err != nil {
		log.Fatalf("Failed to fetch alerts: %v", err)
	}

	activeCount := 0
	for _, alert := range alerts {
		if alert.IsActive() {
			activeCount++
		}
	}

	fmt.Printf("✓ Found %d alerts (%d active)\n", len(alerts), activeCount)
	fmt.Println("🚀 Starting GUI...")

	window := gui.NewAlertsWindow(client, configPath, cfg)
	window.Show()
}
