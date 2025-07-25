package webui

import (
	"log"
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

	// Load configuration
	cfg, err := config.LoadConfig("config/config.json")
	if err != nil {
		log.Printf("Warning: Failed to load config, using defaults: %v", err)
		// Create a default config if none exists
		cfg = &config.Config{
			Alertmanagers: []config.AlertmanagerConfig{},
		}
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

	// Static files - use absolute path resolution
	_, currentFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
	staticPath := filepath.Join(projectRoot, "internal", "webui", "static")
	r.Static("/static", staticPath)

	// Health checks
	r.GET("/health", handlers.HealthCheck)
	r.GET("/health/backend", handlers.BackendHealthCheck)
	r.GET("/health/alertmanager", handlers.AlertmanagerHealthCheck)

	// API routes
	api := r.Group("/api/v1")
	{
		// Public auth routes
		auth := api.Group("/auth")
		{
			auth.POST("/login", handlers.Login)
			auth.POST("/register", handlers.Register)
		}

		// Protected auth routes
		authProtected := api.Group("/auth")
		authProtected.Use(authMiddleware.RequireAuth())
		{
			authProtected.POST("/logout", handlers.Logout)
			authProtected.GET("/me", handlers.GetCurrentUser)
			authProtected.GET("/profile", handlers.GetCurrentUser) // Alias for user profile
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
			dashboard.POST("/bulk-action", handlers.BulkActionAlerts)
			dashboard.GET("/settings", handlers.GetDashboardSettings)
			dashboard.POST("/settings", handlers.SaveDashboardSettings)
			dashboard.GET("/alert/:id", handlers.GetAlertDetails)
			dashboard.POST("/alert/:id/comments", handlers.AddAlertComment)
			dashboard.DELETE("/alert/:id/comments/:commentId", handlers.DeleteAlertComment)
			dashboard.GET("/color-preferences", handlers.GetUserColorPreferences)
			dashboard.POST("/color-preferences", handlers.SaveUserColorPreferences)
			dashboard.DELETE("/color-preferences/:id", handlers.DeleteUserColorPreference)
			dashboard.GET("/alert-colors", handlers.GetAlertColors)
			dashboard.GET("/available-labels", handlers.GetAvailableAlertLabels)
			dashboard.DELETE("/remove-resolved-alerts", handlers.RemoveAllResolvedAlerts)
		}
	}

	// Web routes (HTML pages)
	r.GET("/", authMiddleware.OptionalAuth(), handlers.IndexPage)

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
	}

	return r
}
