package webui

import (
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
	alertCache := services.NewAlertCache(amClient, backendClient)
	handlers.SetAlertCache(alertCache)
	alertCache.Start()

	// Initialize color service for dynamic alert coloring
	colorService := services.NewColorService(backendClient)
	handlers.SetColorService(colorService)

	// Create auth middleware
	authMiddleware := middleware.NewAuthMiddleware(backendClient)

	// Session secret - in production, use environment variable
	sessionSecret := "your-secret-key-change-in-production"

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
			dashboard.GET("/alert/:id", handlers.GetAlertDetails)
			dashboard.POST("/alert/:id/comments", handlers.AddAlertComment)
			dashboard.DELETE("/alert/:id/comments/:commentId", handlers.DeleteAlertComment)
			dashboard.GET("/color-preferences", handlers.GetUserColorPreferences)
			dashboard.POST("/color-preferences", handlers.SaveUserColorPreferences)
			dashboard.DELETE("/color-preferences/:id", handlers.DeleteUserColorPreference)
			dashboard.GET("/notification-preferences", handlers.GetUserNotificationPreferences)
			dashboard.POST("/notification-preferences", handlers.SaveUserNotificationPreferences)
			dashboard.GET("/alert-colors", handlers.GetAlertColors)
			dashboard.GET("/available-labels", handlers.GetAvailableAlertLabels)
			dashboard.DELETE("/remove-resolved-alerts", handlers.RemoveAllResolvedAlerts)
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
		protectedPages.GET("/alert/:id", handlers.StandaloneAlertPage) // Standalone alert page
		protectedPages.GET("/profile", handlers.ProfilePage)
	}

	return r
}
