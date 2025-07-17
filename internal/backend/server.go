package backend

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"notificator/config"
	"notificator/internal/backend/database"
	alertpb "notificator/internal/backend/proto/alert"
	authpb "notificator/internal/backend/proto/auth"
	"notificator/internal/backend/services"
)

// Server represents the backend server
type Server struct {
	authService  *services.AuthServiceGorm
	alertService *services.AlertServiceGorm
	db           *database.GormDB
	config       *config.Config
	dbType       string
	grpcServer   *grpc.Server
	httpServer   *http.Server
}

// NewServer creates a new backend server instance
func NewServer(cfg *config.Config, dbType string) *Server {
	return &Server{
		config: cfg,
		dbType: dbType,
	}
}

// Start starts the backend server
func (s *Server) Start() error {
	// Initialize database
	if err := s.initDatabase(); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Auto-run migrations if tables don't exist
	if err := s.db.AutoMigrate(); err != nil {
		return fmt.Errorf("failed to run auto-migrations: %w", err)
	}

	// Initialize services
	s.initServices()

	// Start gRPC server
	if err := s.startGRPCServer(); err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}

	// Start HTTP server (optional, for health checks or REST API)
	if err := s.startHTTPServer(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Setup graceful shutdown
	shutdownChan := make(chan struct{})
	s.setupGracefulShutdown(shutdownChan)

	// Block until shutdown signal received
	<-shutdownChan
	return nil
}

// initDatabase initializes the database connection
func (s *Server) initDatabase() error {
	var dbConfig config.DatabaseConfig
	if s.config.Backend.Database.Type != "" {
		dbConfig = s.config.Backend.Database
	} else {
		// Default database configuration
		dbConfig = config.DatabaseConfig{
			Type:       s.dbType,
			SQLitePath: "./notificator.db",
			Host:       "localhost",
			Port:       5432,
			Name:       "notificator",
			User:       "postgres",
			Password:   "postgres",
			SSLMode:    "disable",
		}
	}

	db, err := database.NewGormDB(s.dbType, dbConfig)
	if err != nil {
		return err
	}

	s.db = db
	return nil
}

// initServices initializes all gRPC services
func (s *Server) initServices() {
	s.authService = services.NewAuthServiceGorm(s.db)
	s.alertService = services.NewAlertServiceGorm(s.db)
}

// startGRPCServer starts the gRPC server
func (s *Server) startGRPCServer() error {
	// Get gRPC listen address
	listenAddr := s.config.Backend.GRPCListen
	if listenAddr == "" {
		listenAddr = ":50051" // Default gRPC port
	}

	// Create listener
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}

	// Create gRPC server with options
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(s.loggingUnaryInterceptor),
	}

	s.grpcServer = grpc.NewServer(opts...)

	// Register services
	authpb.RegisterAuthServiceServer(s.grpcServer, s.authService)
	alertpb.RegisterAlertServiceServer(s.grpcServer, s.alertService)

	// Enable reflection for debugging (remove in production)
	reflection.Register(s.grpcServer)

	log.Printf("🚀 gRPC server starting on %s", listenAddr)

	// Start server in a goroutine
	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve gRPC server: %v", err)
		}
	}()

	return nil
}

// startHTTPServer starts an HTTP server for health checks
func (s *Server) startHTTPServer() error {
	// HTTP server for health checks and metrics
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", s.healthCheckHandler)
	mux.HandleFunc("/metrics", s.metricsHandler)

	// Get HTTP listen address
	httpAddr := s.config.Backend.HTTPListen
	if httpAddr == "" {
		httpAddr = ":8080" // Default HTTP port
	}

	s.httpServer = &http.Server{
		Addr:    httpAddr,
		Handler: mux,
	}

	log.Printf("🌐 HTTP server starting on %s", httpAddr)

	// Start HTTP server in a goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to serve HTTP server: %v", err)
		}
	}()

	return nil
}

// setupGracefulShutdown sets up graceful shutdown handling
func (s *Server) setupGracefulShutdown(shutdownChan chan struct{}) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("🛑 Shutting down servers...")

		// Shutdown gRPC server
		if s.grpcServer != nil {
			s.grpcServer.GracefulStop()
		}

		// Shutdown HTTP server
		if s.httpServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.httpServer.Shutdown(ctx); err != nil {
				log.Printf("HTTP server shutdown error: %v", err)
			}
		}

		// Close database connection
		if s.db != nil {
			if err := s.db.Close(); err != nil {
				log.Printf("Database close error: %v", err)
			}
		}

		log.Println("✅ Servers shut down gracefully")
		close(shutdownChan)
	}()
}

// RunMigrations runs database migrations
func (s *Server) RunMigrations() error {
	// Initialize database first
	if err := s.initDatabase(); err != nil {
		return fmt.Errorf("failed to initialize database for migrations: %w", err)
	}

	// Run migrations
	if err := s.db.AutoMigrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// Close closes the server and cleans up resources
func (s *Server) Close() error {
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}

	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return err
		}
	}

	if s.db != nil {
		return s.db.Close()
	}

	return nil
}

// loggingUnaryInterceptor logs gRPC requests
func (s *Server) loggingUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()

	// Call the handler
	resp, err := handler(ctx, req)

	// Log the request
	duration := time.Since(start)
	status := "OK"
	if err != nil {
		status = "ERROR"
	}

	log.Printf("[gRPC] %s %s %v %s", info.FullMethod, status, duration, getClientIP(ctx))

	return resp, err
}

// healthCheckHandler handles health check requests
func (s *Server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check database connection
	if err := s.db.HealthCheck(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"unhealthy","database":"down","error":"%v"}`, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"healthy","database":"up"}`)
}

// metricsHandler handles metrics requests
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get basic metrics
	stats, err := s.db.GetStatistics()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"failed to get metrics: %v"}`, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{
		"users": %d,
		"active_sessions": %d,
		"total_comments": %d,
		"total_acknowledgments": %d,
		"timestamp": "%s"
	}`, stats["users"], stats["active_sessions"], stats["comments"], stats["acknowledgments"], time.Now().Format(time.RFC3339))
}

// getClientIP extracts client IP from gRPC context
func getClientIP(ctx context.Context) string {
	// This is a simplified implementation
	// In production, you might want to handle X-Forwarded-For headers
	return "unknown"
}

// Additional helper methods

// IsHealthy checks if the server is healthy
func (s *Server) IsHealthy() bool {
	if s.db == nil {
		return false
	}
	return s.db.HealthCheck() == nil
}

// GetStats returns server statistics
func (s *Server) GetStats() (*ServerStats, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	stats, err := s.db.GetStatistics()
	if err != nil {
		return nil, err
	}

	return &ServerStats{
		Users:           stats["users"],
		ActiveSessions:  stats["active_sessions"],
		Comments:        stats["comments"],
		Acknowledgments: stats["acknowledgments"],
		StartTime:       time.Now().Add(-time.Hour), // Placeholder
	}, nil
}

// ServerStats represents server statistics
type ServerStats struct {
	Users           int64     `json:"users"`
	ActiveSessions  int64     `json:"active_sessions"`
	Comments        int64     `json:"comments"`
	Acknowledgments int64     `json:"acknowledgments"`
	StartTime       time.Time `json:"start_time"`
}
