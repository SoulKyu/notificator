package webui

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"notificator/config"
	"notificator/internal/alertmanager"
	"notificator/internal/webui/client"
	"notificator/internal/webui/handlers"
	"notificator/internal/webui/middleware"
	"notificator/internal/webui/services"

	"github.com/gin-gonic/gin"
)

func SetupRouter(backendAddress string) *gin.Engine {
	r := gin.New()

	// Load configuration with Viper to support environment variables
	cfg, err := config.LoadConfigWithViper()
	if err != nil {
		log.Printf("Warning: Failed to load config, using defaults: %v", err)
		// Create a default config if none exists
		cfg = &config.Config{
			Alertmanagers: []config.AlertmanagerConfig{},
		}
	}

	// Merge headers from environment variables (e.g., METRICS_PROVIDER_HEADERS)
	cfg.MergeHeaders()

	// Log the loaded configuration for debugging
	log.Printf("Loaded %d alertmanagers", len(cfg.Alertmanagers))
	for i, am := range cfg.Alertmanagers {
		log.Printf("Alertmanager %d: Name=%s, URL=%s", i, am.Name, am.URL)
	}

	// Initialize Alertmanager multi-client
	amClient := alertmanager.NewMultiClient(cfg)
	handlers.SetAlertmanagerClient(amClient)

	// Initialize backend client
	backendClient := client.NewBackendClient(backendAddress)
	err = backendClient.Connect()
	if err != nil {
		// For now, continue without backend - will show connection errors
		log.Fatalf("Backend is mandatory on webui %w", err)
	}

	// Set backend client for handlers
	handlers.SetBackendClient(backendClient)
	handlers.SetFilterPresetBackendClient(backendClient)

	// Fetch OAuth configuration from backend and update local config
	oauthEnabled, err := backendClient.IsOAuthEnabled()
	if err != nil {
		log.Printf("Warning: Failed to get OAuth config from backend: %v", err)
		oauthEnabled = false
	}
	log.Printf("DEBUG: OAuth enabled check: %v", oauthEnabled)

	if oauthEnabled {
		log.Printf("DEBUG: OAuth is enabled on backend, updating local config")
		// Ensure OAuth config exists
		if cfg.OAuth == nil {
			cfg.OAuth = config.DefaultOAuthConfig()
		}
		cfg.OAuth.Enabled = true

		// Get full OAuth configuration from backend
		oauthConfig, err := backendClient.GetOAuthConfig()
		if err != nil {
			log.Printf("Warning: Failed to get full OAuth config from backend: %v", err)
		} else {
			log.Printf("DEBUG: Retrieved OAuth config from backend: enabled=%v, providers=%d",
				oauthConfig["enabled"], len(oauthConfig["providers"].([]map[string]interface{})))
		}
	} else {
		log.Printf("DEBUG: OAuth is disabled on backend")
		if cfg.OAuth != nil {
			cfg.OAuth.Enabled = false
		}
	}

	// Initialize alert cache for new dashboard
	alertCache := services.NewAlertCache(amClient, backendClient, cfg.ResolvedAlerts.RetentionDays)
	handlers.SetAlertCache(alertCache)
	alertCache.Start()

	// Initialize color service for dynamic alert coloring
	colorService := services.NewColorService(backendClient)
	handlers.SetColorService(colorService)

	// Initialize hidden alerts service
	hiddenAlertsService := services.NewHiddenAlertsService(backendClient)
	handlers.SetHiddenAlertsService(hiddenAlertsService)

	// Initialize Sentry service if enabled
	if cfg.Sentry != nil && cfg.Sentry.Enabled {
		sentryService := services.NewSentryService(cfg.Sentry, backendClient)
		handlers.SetSentryService(sentryService)
		log.Printf("🔗 Sentry integration enabled for %s", cfg.Sentry.BaseURL)
	}

	// Create auth middleware
	authMiddleware := middleware.NewAuthMiddleware(backendClient)

	// Session secret - read from environment variable or generate random
	sessionSecret := os.Getenv("NOTIFICATOR_SESSION_SECRET")
	if sessionSecret == "" {
		// Generate a random session secret for this instance
		// WARNING: Sessions will not persist across restarts without a configured secret
		sessionSecret = generateRandomSecret(32)
		log.Println("⚠️  WARNING: No NOTIFICATOR_SESSION_SECRET configured - using random secret")
		log.Println("⚠️  Sessions will NOT persist across restarts. Set NOTIFICATOR_SESSION_SECRET environment variable.")
	} else {
		log.Println("✅ Using configured session secret from NOTIFICATOR_SESSION_SECRET")
	}

	// Middleware
	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.LoggingMiddleware())
	r.Use(gin.Recovery())
	r.Use(middleware.SessionMiddleware(sessionSecret))

	// Static files - handle both development and container environments
	var staticPath string
	if _, err := os.Stat("./internal/webui/static"); err == nil {
		// Running from project root (development)
		staticPath = "./internal/webui/static"
	} else if _, err := os.Stat("internal/webui/static"); err == nil {
		// Running from container root
		staticPath = "internal/webui/static"
	} else {
		// Fallback to runtime.Caller method
		_, currentFile, _, _ := runtime.Caller(0)
		projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
		staticPath = filepath.Join(projectRoot, "internal", "webui", "static")
	}

	log.Printf("Serving static files from: %s", staticPath)
	r.Static("/static", staticPath)

	// Health checks
	r.GET("/health", handlers.HealthCheck)
	r.GET("/health/backend", handlers.BackendHealthCheck)
	r.GET("/health/alertmanager", handlers.AlertmanagerHealthCheck)

	// Static file health check
	r.GET("/health/static", func(c *gin.Context) {
		cssPath := filepath.Join(staticPath, "css", "output.css")
		if _, err := os.Stat(cssPath); err == nil {
			c.JSON(200, gin.H{"status": "ok", "css_path": cssPath, "static_path": staticPath})
		} else {
			c.JSON(500, gin.H{"status": "error", "error": err.Error(), "css_path": cssPath, "static_path": staticPath})
		}
	})

	// API routes
	api := r.Group("/api/v1")
	{
		// Public auth routes
		auth := api.Group("/auth")
		{
			auth.POST("/login", handlers.Login)
			auth.POST("/register", handlers.Register)
		}

		// OAuth routes (public)
		oauth := api.Group("/oauth")
		{
			oauth.GET("/providers", handlers.GetOAuthProviders)
			oauth.GET("/:provider/login", handlers.OAuthLogin)
			oauth.GET("/:provider/callback", handlers.OAuthCallback)
		}

		// Protected auth routes
		authProtected := api.Group("/auth")
		authProtected.Use(authMiddleware.RequireAuth())
		{
			authProtected.POST("/logout", handlers.Logout)
			authProtected.GET("/me", handlers.GetCurrentUser)
			authProtected.GET("/profile", handlers.GetCurrentUser) // Alias for user profile
		}

		// Protected OAuth routes
		oauthProtected := api.Group("/oauth")
		oauthProtected.Use(authMiddleware.RequireAuth())
		{
			oauthProtected.GET("/groups", handlers.GetUserGroups)
			oauthProtected.POST("/sync-groups", handlers.SyncUserGroups)
		}

		// Protected alert routes
		alerts := api.Group("/alerts")
		alerts.Use(authMiddleware.OptionalAuth()) // Optional auth for now
		{
			alerts.GET("", handlers.GetAlerts)
			// Note: Individual alert endpoint removed - use dashboard API instead
		}

		// New dashboard API routes
		dashboard := api.Group("/dashboard")
		dashboard.Use(authMiddleware.OptionalAuth()) // Optional auth for now
		{
			dashboard.GET("/data", handlers.GetDashboardData)
			dashboard.GET("/incremental", handlers.GetDashboardIncremental)
			dashboard.POST("/incremental", handlers.PostDashboardIncremental)
			dashboard.POST("/bulk-action", handlers.BulkActionAlerts)
			dashboard.GET("/settings", handlers.GetDashboardSettings)
			dashboard.POST("/settings", handlers.SaveDashboardSettings)
			dashboard.GET("/alert/:fingerprint", handlers.GetAlertDetails)
			dashboard.GET("/alert/:fingerprint/history", handlers.HandleGetAlertHistory)
			dashboard.POST("/alert/:fingerprint/comments", handlers.AddAlertComment)
			dashboard.DELETE("/alert/:fingerprint/comments/:commentId", handlers.DeleteAlertComment)
			dashboard.POST("/alerts/bulk-status", handlers.GetBulkAlertStatus)
			dashboard.POST("/alerts/bulk-colors", handlers.GetBulkAlertColors)
			dashboard.GET("/color-preferences", handlers.GetUserColorPreferences)
			dashboard.POST("/color-preferences", handlers.SaveUserColorPreferences)
			dashboard.DELETE("/color-preferences/:id", handlers.DeleteUserColorPreference)
			dashboard.GET("/alert-colors", handlers.GetAlertColors)
			dashboard.GET("/available-labels", handlers.GetAvailableAlertLabels)
			dashboard.GET("/available-fields", handlers.GetAvailableFields)
			dashboard.GET("/column-preferences", handlers.GetUserColumnPreferences)
			dashboard.PUT("/column-preferences", handlers.SaveUserColumnPreferences)
			dashboard.PATCH("/column-preferences/width", handlers.UpdateColumnWidth)
			dashboard.DELETE("/remove-resolved-alerts", handlers.RemoveAllResolvedAlerts)

			// Hidden alerts routes
			dashboard.GET("/hidden-alerts", handlers.GetUserHiddenAlerts)
			dashboard.POST("/hidden-alerts", handlers.HideAlert)
			dashboard.DELETE("/hidden-alerts/:fingerprint", handlers.UnhideAlert)
			dashboard.DELETE("/hidden-alerts", handlers.ClearAllHiddenAlerts)

			// Hidden rules routes
			dashboard.GET("/hidden-rules", handlers.GetUserHiddenRules)
			dashboard.POST("/hidden-rules", handlers.CreateHiddenRule)
			dashboard.PUT("/hidden-rules/:id", handlers.UpdateHiddenRule)
			dashboard.DELETE("/hidden-rules/:id", handlers.DeleteHiddenRule)

			// Filter presets routes
			dashboard.GET("/filter-presets", handlers.GetFilterPresets)
			dashboard.GET("/filter-presets/default", handlers.GetDefaultFilterPreset)
			dashboard.POST("/filter-presets", handlers.CreateFilterPreset)
			dashboard.PUT("/filter-presets/:id", handlers.UpdateFilterPreset)
			dashboard.DELETE("/filter-presets/:id", handlers.DeleteFilterPreset)
			dashboard.POST("/filter-presets/:id/default", handlers.SetDefaultFilterPreset)

			// Sentry integration routes
			dashboard.POST("/sentry/test-connection", handlers.TestSentryConnection)
			dashboard.GET("/sentry/:fingerprint", handlers.GetSentryDataForAlert)
			dashboard.GET("/sentry-config", handlers.GetUserSentryConfig)
			dashboard.POST("/sentry-token", handlers.SaveUserSentryToken)
			dashboard.DELETE("/sentry-token", handlers.DeleteUserSentryToken)

			// Annotation button config routes
			dashboard.GET("/annotation-buttons", handlers.GetAnnotationButtonConfigs)
			dashboard.POST("/annotation-buttons", handlers.SaveAnnotationButtonConfigs)
			dashboard.POST("/annotation-buttons/single", handlers.CreateAnnotationButtonConfig)
			dashboard.PUT("/annotation-buttons/:id", handlers.UpdateAnnotationButtonConfig)
			dashboard.DELETE("/annotation-buttons/:id", handlers.DeleteAnnotationButtonConfig)
		}

		// Notification preferences routes
		notifications := api.Group("/notifications")
		notifications.Use(authMiddleware.RequireAuth())
		{
			notifications.GET("/preferences", handlers.GetNotificationPreferences)
			notifications.POST("/preferences", handlers.SaveNotificationPreferences)
		}

		// Statistics and On-Call Rules routes
		statistics := api.Group("/statistics")
		statistics.Use(authMiddleware.RequireAuth())
		{
			// Query statistics
			statistics.POST("/query", handlers.QueryStatistics)
			statistics.GET("/summary", handlers.GetStatisticsSummary)
			statistics.POST("/recently-resolved", handlers.QueryRecentlyResolved)
			statistics.GET("/alert/:fingerprint", handlers.GetResolvedAlertDetails)
			statistics.POST("/alerts-by-name", handlers.GetAlertsByName)

			// On-call rules CRUD
			statistics.GET("/rules", handlers.GetOnCallRules)
			statistics.GET("/rules/:id", handlers.GetOnCallRule)
			statistics.POST("/rules", handlers.SaveOnCallRule)
			statistics.PUT("/rules/:id", handlers.UpdateOnCallRule)
			statistics.DELETE("/rules/:id", handlers.DeleteOnCallRule)
			statistics.POST("/rules/test", handlers.TestOnCallRule)
		}
	}

	// Web routes (HTML pages)
	// Conditionally serve playground or index page based on config
	if cfg.WebUI.Playground {
		r.GET("/", authMiddleware.OptionalAuth(), handlers.PlaygroundPage)
	} else {
		r.GET("/", authMiddleware.OptionalAuth(), handlers.IndexPage)
	}

	// Public pages (redirect if already authenticated)
	publicPages := r.Group("/")
	publicPages.Use(authMiddleware.RedirectIfAuth("/dashboard"))
	{
		publicPages.GET("/login", handlers.LoginPage)
		publicPages.GET("/register", handlers.RegisterPage)
	}

	// Protected pages (redirect if not authenticated)
	protectedPages := r.Group("/")
	protectedPages.Use(authMiddleware.RedirectIfNotAuth("/login"))
	{
		protectedPages.GET("/dashboard", handlers.DashboardPage)
		protectedPages.GET("/dashboard/alert/:id", handlers.DashboardPage) // Show dashboard with modal
		protectedPages.GET("/profile", handlers.ProfilePage)
		protectedPages.GET("/statistics", handlers.StatisticsDashboardPage)
		protectedPages.GET("/statistics/rules", handlers.OnCallRulesPage)
	}

	return r
}

// generateRandomSecret generates a cryptographically secure random secret
func generateRandomSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a static secret if random generation fails
		log.Printf("⚠️  Failed to generate random secret: %v, using fallback", err)
		return "fallback-secret-notificator-insecure-change-me"
	}
	return base64.URLEncoding.EncodeToString(bytes)
}
