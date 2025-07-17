// internal/gui/backend_server.go
package gui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	alertpb "notificator/internal/backend/proto/alert"
	authpb "notificator/internal/backend/proto/auth"
)

type BackendClient struct {
	// Connection management
	conn        *grpc.ClientConn
	address     string
	isConnected bool

	// gRPC clients
	authClient  authpb.AuthServiceClient
	alertClient alertpb.AlertServiceClient

	// Authentication state
	currentSessionID string
	currentUser      *authpb.User

	// Connection health
	lastPingTime    time.Time
	reconnectTicker *time.Ticker
	healthMutex     sync.RWMutex

	// Callbacks
	onConnectionStateChange func(connected bool)
	onAuthStateChange       func(authenticated bool)

	// Metrics (optional)
	metrics *BackendMetrics
}

func NewBackendClient(address string) *BackendClient {
	bc := &BackendClient{
		address:     address,
		isConnected: false,
	}

	// Don't try initial connection here - wait for callbacks to be set
	// bc.tryConnect() will be called after callbacks are set

	// Start connection health monitoring
	bc.startHealthMonitoring()

	return bc
}

func (bc *BackendClient) tryConnect() {
	bc.healthMutex.Lock()
	defer bc.healthMutex.Unlock()

	// Clean up existing connection
	if bc.conn != nil {
		bc.conn.Close()
		bc.conn = nil
	}

	// Create connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Printf("Attempting to connect to backend at %s...", bc.address)
	
	// Check if address is empty or malformed
	if bc.address == "" {
		log.Printf("ERROR: Backend address is empty!")
		bc.setConnectionState(false)
		return
	}

	conn, err := grpc.DialContext(ctx, bc.address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)

	if err != nil {
		log.Printf("Failed to connect to backend: %v", err)
		bc.setConnectionState(false)
		return
	}

	// Create service clients
	bc.conn = conn
	bc.authClient = authpb.NewAuthServiceClient(conn)
	bc.alertClient = alertpb.NewAlertServiceClient(conn)

	// Test connection with a simple call
	if bc.testConnection() {
		log.Printf("Successfully connected to backend at %s", bc.address)
		bc.setConnectionState(true)
		bc.lastPingTime = time.Now()
	} else {
		log.Printf("Connection test failed")
		bc.setConnectionState(false)
	}
}

func (bc *BackendClient) testConnection() bool {
	if bc.authClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Try to validate an empty session (should fail gracefully without DB lookup)
	_, err := bc.authClient.ValidateSession(ctx, &authpb.ValidateSessionRequest{
		SessionId: "",
	})

	// We expect this to fail, but if it responds, connection is working
	if err != nil {
		// Check if it's a connection error vs application error
		if status.Code(err) == codes.Unavailable {
			return false
		}
		// Application error means connection is working
		return true
	}

	return true
}

func (bc *BackendClient) setConnectionState(connected bool) {
	if bc.isConnected == connected {
		return // No change
	}

	bc.isConnected = connected

	if !connected {
		// Clear authentication state on disconnect
		bc.currentSessionID = ""
		bc.currentUser = nil

		if bc.onAuthStateChange != nil {
			bc.onAuthStateChange(false)
		}
	}

	if bc.onConnectionStateChange != nil {
		bc.onConnectionStateChange(connected)
	}
}

func (bc *BackendClient) startHealthMonitoring() {
	// Stop existing ticker
	if bc.reconnectTicker != nil {
		bc.reconnectTicker.Stop()
	}

	// Create new ticker for every 10 seconds
	bc.reconnectTicker = time.NewTicker(10 * time.Second)

	go func() {
		for range bc.reconnectTicker.C {
			bc.healthMutex.RLock()
			connected := bc.isConnected
			bc.healthMutex.RUnlock()

			if !connected {
				// Try to reconnect
				bc.tryConnect()
			} else {
				// Test existing connection
				if !bc.testConnection() {
					log.Printf("Backend connection health check failed")
					bc.setConnectionState(false)
				} else {
					bc.lastPingTime = time.Now()
				}
			}
		}
	}()
}


func (bc *BackendClient) IsConnected() bool {
	bc.healthMutex.RLock()
	defer bc.healthMutex.RUnlock()
	return bc.isConnected
}

func (bc *BackendClient) IsLoggedIn() bool {
	bc.healthMutex.RLock()
	defer bc.healthMutex.RUnlock()
	return bc.currentSessionID != "" && bc.currentUser != nil
}

func (bc *BackendClient) GetCurrentUser() *authpb.User {
	bc.healthMutex.RLock()
	defer bc.healthMutex.RUnlock()
	return bc.currentUser
}

func (bc *BackendClient) SetConnectionStateCallback(callback func(connected bool)) {
	bc.onConnectionStateChange = callback
}

func (bc *BackendClient) SetAuthStateCallback(callback func(authenticated bool)) {
	bc.onAuthStateChange = callback
}


func (bc *BackendClient) Register(username, password, email string) (*authpb.RegisterResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("Attempting to register user: %s", username)

	resp, err := bc.authClient.Register(ctx, &authpb.RegisterRequest{
		Username: username,
		Password: password,
		Email:    email,
	})

	if err != nil {
		log.Printf("Registration failed: %v", err)
		return nil, err
	}

	log.Printf("Registration response: success=%v, message=%s", resp.Success, resp.Message)
	return resp, nil
}

func (bc *BackendClient) AddAcknowledgment(alertKey, reason string) (*alertpb.AddAcknowledgmentResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	if !bc.IsLoggedIn() {
		return nil, fmt.Errorf("not logged in")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("Adding acknowledgment to alert: %s", alertKey)

	return bc.alertClient.AddAcknowledgment(ctx, &alertpb.AddAcknowledgmentRequest{
		SessionId: bc.currentSessionID,
		AlertKey:  alertKey,
		Reason:    reason,
	})
}

func (bc *BackendClient) GetAcknowledgments(alertKey string) (*alertpb.GetAcknowledgmentsResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return bc.alertClient.GetAcknowledgments(ctx, &alertpb.GetAcknowledgmentsRequest{
		AlertKey: alertKey,
	})
}

func (bc *BackendClient) DeleteAcknowledgment(alertKey string) (*alertpb.DeleteAcknowledgmentResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	if !bc.IsLoggedIn() {
		return nil, fmt.Errorf("not logged in")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("Removing acknowledgment from alert: %s", alertKey)

	return bc.alertClient.DeleteAcknowledgment(ctx, &alertpb.DeleteAcknowledgmentRequest{
		SessionId: bc.currentSessionID,
		AlertKey:  alertKey,
	})
}


func (bc *BackendClient) GetConnectionStatus() ConnectionStatus {
	bc.healthMutex.RLock()
	defer bc.healthMutex.RUnlock()

	status := ConnectionStatus{
		Connected:     bc.isConnected,
		Address:       bc.address,
		LastPingTime:  bc.lastPingTime,
		Authenticated: bc.currentSessionID != "",
		CurrentUser:   bc.currentUser,
	}

	return status
}

type ConnectionStatus struct {
	Connected     bool
	Address       string
	LastPingTime  time.Time
	Authenticated bool
	CurrentUser   *authpb.User
}

func (cs ConnectionStatus) String() string {
	if !cs.Connected {
		return fmt.Sprintf("Disconnected from %s", cs.Address)
	}

	if cs.Authenticated && cs.CurrentUser != nil {
		return fmt.Sprintf("Connected to %s as %s (last ping: %v ago)",
			cs.Address,
			cs.CurrentUser.Username,
			time.Since(cs.LastPingTime).Round(time.Second))
	}

	return fmt.Sprintf("Connected to %s (not authenticated, last ping: %v ago)",
		cs.Address,
		time.Since(cs.LastPingTime).Round(time.Second))
}

func (bc *BackendClient) Reconnect() {
	log.Printf("Manual reconnect requested")
	go bc.tryConnect()
}

func (bc *BackendClient) Close() {
	log.Printf("Closing backend client...")

	// Stop health monitoring
	if bc.reconnectTicker != nil {
		bc.reconnectTicker.Stop()
		bc.reconnectTicker = nil
	}

	// Logout if authenticated
	if bc.IsLoggedIn() {
		if err := bc.Logout(); err != nil {
			log.Printf("Error during logout: %v", err)
		}
	}

	// Close connection
	bc.healthMutex.Lock()
	if bc.conn != nil {
		bc.conn.Close()
		bc.conn = nil
	}
	bc.isConnected = false
	bc.healthMutex.Unlock()

	log.Printf("Backend client closed")
}


func NewBackendClientForAlertsWindow(address string, alertsWindow *AlertsWindow) *BackendClient {
	bc := NewBackendClient(address)

	// Set up callbacks to update UI
	bc.SetConnectionStateCallback(func(connected bool) {
		if alertsWindow != nil {
			alertsWindow.scheduleUpdate(func() {
				alertsWindow.updateBackendConnectionStatus(connected)
			})
		}
	})

	bc.SetAuthStateCallback(func(authenticated bool) {
		if alertsWindow != nil {
			alertsWindow.scheduleUpdate(func() {
				alertsWindow.updateUserInterface()
			})
		}
	})

	return bc
}

type BackendManager struct {
	client       *BackendClient
	alertsWindow *AlertsWindow
	enabled      bool
}

func NewBackendManager(alertsWindow *AlertsWindow, address string) *BackendManager {
	if address == "" {
		log.Printf("Backend address not configured, running in standalone mode")
		return &BackendManager{
			alertsWindow: alertsWindow,
			enabled:      false,
		}
	}

	bm := &BackendManager{
		alertsWindow: alertsWindow,
		enabled:      true,
	}

	// Create backend client
	bm.client = NewBackendClientForAlertsWindow(address, alertsWindow)

	return bm
}

func (bm *BackendManager) IsEnabled() bool {
	return bm.enabled
}

func (bm *BackendManager) GetClient() *BackendClient {
	return bm.client
}

func (bm *BackendManager) Close() {
	if bm.client != nil {
		bm.client.Close()
		bm.client = nil
	}
}


func (aw *AlertsWindow) updateBackendConnectionStatus(connected bool) {
	if aw.backendClient == nil {
		return
	}

	status := aw.backendClient.GetConnectionStatus()

	if connected {
		if status.Authenticated {
			aw.setStatus(fmt.Sprintf("🔗 Backend: Connected as %s", status.CurrentUser.Username))
		} else {
			aw.setStatus("🔗 Backend: Connected (not authenticated)")
		}
	} else {
		aw.setStatus("🔗 Backend: Disconnected")
	}

	// Update menu items or other UI elements based on connection state
	aw.updateBackendMenuItems(connected, status.Authenticated)
}

func (aw *AlertsWindow) updateBackendMenuItems(connected, authenticated bool) {
	// This would typically update menu items to show/hide:
	// - Login/Logout options
	// - Backend connection status
	// - Collaborative features

	// For now, just log the state
	log.Printf("Backend menu update: connected=%v, authenticated=%v", connected, authenticated)
}

func (aw *AlertsWindow) showBackendStatus() {
	if aw.backendClient == nil {
		aw.setStatus("Backend not configured")
		return
	}

	status := aw.backendClient.GetConnectionStatus()
	statusText := status.String()

	// Show in a dialog or update status bar
	aw.setStatus(statusText)
	log.Printf("Backend status: %s", statusText)
}

func (aw *AlertsWindow) ensureBackendConnection() error {
	if aw.backendClient == nil {
		return fmt.Errorf("backend not configured")
	}

	if !aw.backendClient.IsConnected() {
		// Try to reconnect
		aw.backendClient.Reconnect()

		// Wait a bit for connection
		time.Sleep(2 * time.Second)

		if !aw.backendClient.IsConnected() {
			return fmt.Errorf("failed to connect to backend")
		}
	}

	return nil
}

func (aw *AlertsWindow) getAlertKey(alert interface{}) string {
	// This would generate a unique key based on alert properties
	// For now, return a placeholder
	return "alert_key_placeholder"
}


type BackendHealthChecker struct {
	client   *BackendClient
	interval time.Duration
	stopChan chan struct{}
}

func NewBackendHealthChecker(client *BackendClient, interval time.Duration) *BackendHealthChecker {
	return &BackendHealthChecker{
		client:   client,
		interval: interval,
		stopChan: make(chan struct{}),
	}
}

func (bhc *BackendHealthChecker) Start() {
	go func() {
		ticker := time.NewTicker(bhc.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if bhc.client.IsLoggedIn() {
					// Validate session periodically
					if _, err := bhc.client.ValidateSession(); err != nil {
						log.Printf("Session validation failed: %v", err)
					}
				}
			case <-bhc.stopChan:
				return
			}
		}
	}()
}

func (bhc *BackendHealthChecker) Stop() {
	close(bhc.stopChan)
}

type BackendMetrics struct {
	ConnectionAttempts   int64
	SuccessfulLogins     int64
	FailedLogins         int64
	CommentsAdded        int64
	AcknowledgmentsAdded int64
	mutex                sync.RWMutex
}

func NewBackendMetrics() *BackendMetrics {
	return &BackendMetrics{}
}

func (bm *BackendMetrics) RecordConnectionAttempt() {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()
	bm.ConnectionAttempts++
}

func (bm *BackendMetrics) RecordSuccessfulLogin() {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()
	bm.SuccessfulLogins++
}

func (bm *BackendMetrics) RecordFailedLogin() {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()
	bm.FailedLogins++
}

func (bm *BackendMetrics) RecordCommentAdded() {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()
	bm.CommentsAdded++
}

func (bm *BackendMetrics) RecordAcknowledgmentAdded() {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()
	bm.AcknowledgmentsAdded++
}

func (bm *BackendMetrics) GetMetrics() BackendMetricsSnapshot {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	return BackendMetricsSnapshot{
		ConnectionAttempts:   bm.ConnectionAttempts,
		SuccessfulLogins:     bm.SuccessfulLogins,
		FailedLogins:         bm.FailedLogins,
		CommentsAdded:        bm.CommentsAdded,
		AcknowledgmentsAdded: bm.AcknowledgmentsAdded,
	}
}

type BackendMetricsSnapshot struct {
	ConnectionAttempts   int64
	SuccessfulLogins     int64
	FailedLogins         int64
	CommentsAdded        int64
	AcknowledgmentsAdded int64
}

func (bms BackendMetricsSnapshot) String() string {
	return fmt.Sprintf("Connections: %d, Logins: %d/%d, Comments: %d, Acks: %d",
		bms.ConnectionAttempts,
		bms.SuccessfulLogins,
		bms.FailedLogins,
		bms.CommentsAdded,
		bms.AcknowledgmentsAdded)
}


func (bc *BackendClient) EnableMetrics() {
	if bc.metrics == nil {
		bc.metrics = NewBackendMetrics()
	}
}

func (bc *BackendClient) GetMetrics() *BackendMetricsSnapshot {
	if bc.metrics == nil {
		return nil
	}

	snapshot := bc.metrics.GetMetrics()
	return &snapshot
}

type BackendClientWithMetrics struct {
	*BackendClient
	metrics *BackendMetrics
}




type BackendConfig struct {
	Address           string
	Enabled           bool
	ConnectionTimeout time.Duration
	RetryInterval     time.Duration
	MaxRetries        int
	EnableMetrics     bool
}

func DefaultBackendConfig() BackendConfig {
	return BackendConfig{
		Address:           "localhost:50051",
		Enabled:           true,
		ConnectionTimeout: 5 * time.Second,
		RetryInterval:     10 * time.Second,
		MaxRetries:        3,
		EnableMetrics:     true,
	}
}

func NewBackendClientFromConfig(config BackendConfig) *BackendClient {
	if !config.Enabled {
		return nil
	}

	bc := NewBackendClient(config.Address)

	if config.EnableMetrics {
		bc.EnableMetrics()
	}

	return bc
}


func (aw *AlertsWindow) setupBackendIntegration(config BackendConfig) {
	if !config.Enabled {
		log.Printf("Backend integration disabled")
		return
	}

	// Create backend client
	aw.backendClient = NewBackendClientFromConfig(config)

	if aw.backendClient == nil {
		log.Printf("Failed to create backend client")
		return
	}

	// Set up callbacks
	aw.backendClient.SetConnectionStateCallback(func(connected bool) {
		fyne.Do(func() {
			aw.updateBackendConnectionStatus(connected)
		})
	})

	aw.backendClient.SetAuthStateCallback(func(authenticated bool) {
		fyne.Do(func() {
			aw.updateUserInterface()
		})
	})

	// Start health checking
	healthChecker := NewBackendHealthChecker(aw.backendClient, 30*time.Second)
	healthChecker.Start()

	// Store health checker for cleanup
	aw.backendHealthChecker = healthChecker

	log.Printf("Backend integration setup complete")
}

func (aw *AlertsWindow) cleanupBackendIntegration() {
	if aw.backendHealthChecker != nil {
		aw.backendHealthChecker.Stop()
		aw.backendHealthChecker = nil
	}

	if aw.backendClient != nil {
		aw.backendClient.Close()
		aw.backendClient = nil
	}

	log.Printf("Backend integration cleanup complete")
}

// showBackendMetrics displays backend metrics
func (aw *AlertsWindow) showBackendMetrics() {
	if aw.backendClient == nil {
		return
	}

	metrics := aw.backendClient.GetMetrics()
	if metrics == nil {
		return
	}

	log.Printf("Backend metrics: %s", metrics.String())
}


type BackendError struct {
	Code    int
	Message string
	Details string
}

func (be BackendError) Error() string {
	if be.Details != "" {
		return fmt.Sprintf("Backend error %d: %s (%s)", be.Code, be.Message, be.Details)
	}
	return fmt.Sprintf("Backend error %d: %s", be.Code, be.Message)
}

var (
	ErrBackendNotConnected = BackendError{
		Code:    1001,
		Message: "Not connected to backend",
	}

	ErrBackendNotAuthenticated = BackendError{
		Code:    1002,
		Message: "Not authenticated",
	}

	ErrBackendTimeout = BackendError{
		Code:    1003,
		Message: "Request timeout",
	}

	ErrBackendUnavailable = BackendError{
		Code:    1004,
		Message: "Backend service unavailable",
	}
)

// handleBackendError handles backend errors consistently
func (aw *AlertsWindow) handleBackendError(err error) {
	if err == nil {
		return
	}

	var backendErr BackendError
	if be, ok := err.(BackendError); ok {
		backendErr = be
	} else {
		backendErr = BackendError{
			Code:    1000,
			Message: "Unknown backend error",
			Details: err.Error(),
		}
	}

	// Log the error
	log.Printf("Backend error: %v", backendErr)

	// Update UI based on error type
	switch backendErr.Code {
	case 1001: // Not connected
		aw.setStatus("❌ Backend: Not connected")
	case 1002: // Not authenticated
		aw.setStatus("❌ Backend: Authentication required")
	case 1003: // Timeout
		aw.setStatus("❌ Backend: Request timeout")
	case 1004: // Unavailable
		aw.setStatus("❌ Backend: Service unavailable")
	default:
		aw.setStatus("❌ Backend: Error")
	}
}

func (aw *AlertsWindow) initializeBackend(config BackendConfig) {
	aw.setupBackendIntegration(config)

	// Set up cleanup on window close
	if aw.window != nil {
		aw.window.SetOnClosed(func() {
			aw.cleanupBackendIntegration()
		})
	}
}



// hasBackendSupport checks if backend support is available
func (aw *AlertsWindow) hasBackendSupport() bool {
	return aw.backendClient != nil && aw.backendClient.IsConnected()
}

// requiresAuthentication checks if an operation requires authentication
func (aw *AlertsWindow) requiresAuthentication() bool {
	return aw.hasBackendSupport() && !aw.backendClient.IsLoggedIn()
}

// showLoginPrompt shows login prompt for protected operations
func (aw *AlertsWindow) showLoginPrompt() {
	if aw.requiresAuthentication() {
		aw.showAuthDialog()
	}
}

// withBackendOperation wraps operations that require backend
func (aw *AlertsWindow) withBackendOperation(operation func() error) error {
	if !aw.hasBackendSupport() {
		return fmt.Errorf("backend not available")
	}

	if !aw.backendClient.IsLoggedIn() {
		return fmt.Errorf("authentication required")
	}

	return operation()
}


// addAlertComment adds a comment to an alert
func (aw *AlertsWindow) addAlertComment(alertKey, content string) error {
	return aw.withBackendOperation(func() error {
		_, err := aw.backendClient.AddComment(alertKey, content)
		return err
	})
}

// acknowledgeAlert acknowledges an alert
func (aw *AlertsWindow) acknowledgeAlert(alertKey, reason string) error {
	return aw.withBackendOperation(func() error {
		_, err := aw.backendClient.AddAcknowledgment(alertKey, reason)
		return err
	})
}

// getAlertComments gets comments for an alert
func (aw *AlertsWindow) getAlertComments(alertKey string) ([]*alertpb.Comment, error) {
	if !aw.hasBackendSupport() {
		return nil, fmt.Errorf("backend not available")
	}

	resp, err := aw.backendClient.GetComments(alertKey)
	if err != nil {
		return nil, err
	}

	return resp.Comments, nil
}

// getAlertAcknowledgments gets acknowledgments for an alert
func (aw *AlertsWindow) getAlertAcknowledgments(alertKey string) ([]*alertpb.Acknowledgment, error) {
	if !aw.hasBackendSupport() {
		return nil, fmt.Errorf("backend not available")
	}

	resp, err := aw.backendClient.GetAcknowledgments(alertKey)
	if err != nil {
		return nil, err
	}

	return resp.Acknowledgments, nil
}


// getBackendDiagnostics returns diagnostic information about backend
func (aw *AlertsWindow) getBackendDiagnostics() map[string]interface{} {
	diagnostics := make(map[string]interface{})

	if aw.backendClient == nil {
		diagnostics["status"] = "not_configured"
		return diagnostics
	}

	status := aw.backendClient.GetConnectionStatus()
	diagnostics["connected"] = status.Connected
	diagnostics["authenticated"] = status.Authenticated
	diagnostics["address"] = status.Address
	diagnostics["last_ping"] = status.LastPingTime.Format(time.RFC3339)

	if status.CurrentUser != nil {
		diagnostics["current_user"] = status.CurrentUser.Username
	}

	if metrics := aw.backendClient.GetMetrics(); metrics != nil {
		diagnostics["metrics"] = map[string]interface{}{
			"connection_attempts":   metrics.ConnectionAttempts,
			"successful_logins":     metrics.SuccessfulLogins,
			"failed_logins":         metrics.FailedLogins,
			"comments_added":        metrics.CommentsAdded,
			"acknowledgments_added": metrics.AcknowledgmentsAdded,
		}
	}

	return diagnostics
}

// exportBackendDiagnostics exports diagnostics to string format
func (aw *AlertsWindow) exportBackendDiagnostics() string {
	diagnostics := aw.getBackendDiagnostics()

	var result strings.Builder
	result.WriteString("Backend Diagnostics:\n")
	result.WriteString("==================\n")

	for key, value := range diagnostics {
		result.WriteString(fmt.Sprintf("%s: %v\n", key, value))
	}

	return result.String()
}

func (bc *BackendClient) Login(username, password string) (*authpb.LoginResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("Attempting to login user: %s", username)

	resp, err := bc.authClient.Login(ctx, &authpb.LoginRequest{
		Username: username,
		Password: password,
	})

	if err != nil {
		log.Printf("Login failed: %v", err)
		return nil, err
	}

	if resp.Success {
		// Update authentication state
		bc.healthMutex.Lock()
		bc.currentSessionID = resp.SessionId
		bc.currentUser = resp.User
		bc.healthMutex.Unlock()

		log.Printf("Login successful for user: %s", username)

		// Notify auth state change
		if bc.onAuthStateChange != nil {
			bc.onAuthStateChange(true)
		}
	} else {
		log.Printf("Login failed: %s", resp.Message)
	}

	return resp, nil
}

func (bc *BackendClient) Logout() error {
	if !bc.IsConnected() {
		return fmt.Errorf("not connected to backend")
	}

	sessionID := bc.currentSessionID
	if sessionID == "" {
		return fmt.Errorf("not logged in")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("Attempting to logout...")

	resp, err := bc.authClient.Logout(ctx, &authpb.LogoutRequest{
		SessionId: sessionID,
	})

	if err != nil {
		log.Printf("Logout failed: %v", err)
		return err
	}

	if resp.Success {
		// Clear authentication state
		bc.healthMutex.Lock()
		bc.currentSessionID = ""
		bc.currentUser = nil
		bc.healthMutex.Unlock()

		log.Printf("Logout successful")

		// Notify auth state change
		if bc.onAuthStateChange != nil {
			bc.onAuthStateChange(false)
		}
	} else {
		log.Printf("Logout failed: %s", resp.Message)
	}

	return nil
}

func (bc *BackendClient) ValidateSession() (*authpb.ValidateSessionResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	sessionID := bc.currentSessionID
	if sessionID == "" {
		return nil, fmt.Errorf("no session to validate")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := bc.authClient.ValidateSession(ctx, &authpb.ValidateSessionRequest{
		SessionId: sessionID,
	})

	if err != nil {
		log.Printf("Session validation failed: %v", err)
		return nil, err
	}

	if !resp.Valid {
		// Session invalid, clear auth state
		bc.healthMutex.Lock()
		bc.currentSessionID = ""
		bc.currentUser = nil
		bc.healthMutex.Unlock()

		if bc.onAuthStateChange != nil {
			bc.onAuthStateChange(false)
		}
	}

	return resp, nil
}

func (bc *BackendClient) GetProfile() (*authpb.GetProfileResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	sessionID := bc.currentSessionID
	if sessionID == "" {
		return nil, fmt.Errorf("not logged in")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return bc.authClient.GetProfile(ctx, &authpb.GetProfileRequest{
		SessionId: sessionID,
	})
}

func (bc *BackendClient) SearchUsers(query string, limit int32) (*authpb.SearchUsersResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return bc.authClient.SearchUsers(ctx, &authpb.SearchUsersRequest{
		Query: query,
		Limit: limit,
	})
}


func (bc *BackendClient) AddComment(alertKey, content string) (*alertpb.AddCommentResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	if !bc.IsLoggedIn() {
		return nil, fmt.Errorf("not logged in")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("Adding comment to alert: %s", alertKey)

	return bc.alertClient.AddComment(ctx, &alertpb.AddCommentRequest{
		SessionId: bc.currentSessionID,
		AlertKey:  alertKey,
		Content:   content,
	})
}

func (bc *BackendClient) GetComments(alertKey string) (*alertpb.GetCommentsResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return bc.alertClient.GetComments(ctx, &alertpb.GetCommentsRequest{
		AlertKey: alertKey,
	})
}

func (bc *BackendClient) DeleteComment(commentId string) (*alertpb.DeleteCommentResponse, error) {
	if !bc.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	if !bc.IsLoggedIn() {
		return nil, fmt.Errorf("not logged in")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("Deleting comment: %s", commentId)

	return bc.alertClient.DeleteComment(ctx, &alertpb.DeleteCommentRequest{
		SessionId: bc.currentSessionID,
		CommentId: commentId,
	})
}

func (bc *BackendClient) SubscribeToAlertUpdates(alertKey string, updateHandler func(*alertpb.AlertUpdate)) error {
	if !bc.IsConnected() {
		return fmt.Errorf("not connected to backend")
	}

	if !bc.IsLoggedIn() {
		return fmt.Errorf("not logged in")
	}

	log.Printf("Subscribing to updates for alert: %s", alertKey)

	// Create the subscription request
	req := &alertpb.SubscribeToAlertUpdatesRequest{
		SessionId: bc.currentSessionID,
		AlertKey:  alertKey,
	}

	// Start the streaming call
	stream, err := bc.alertClient.SubscribeToAlertUpdates(context.Background(), req)
	if err != nil {
		log.Printf("Failed to create subscription: %v", err)
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Handle the stream in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in alert updates stream: %v", r)
			}
		}()

		for {
			update, err := stream.Recv()
			if err != nil {
				log.Printf("Stream ended or error occurred: %v", err)
				return
			}

			log.Printf("Received update for alert %s: %v", alertKey, update.UpdateType)

			// Call the update handler
			if updateHandler != nil {
				fyne.Do(func() {
					updateHandler(update)
				})
			}
		}
	}()

	return nil
}
