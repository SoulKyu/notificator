package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	authpb "notificator/internal/backend/proto/auth"
	alertpb "notificator/internal/backend/proto/alert"
	"notificator/internal/webui/models"
)

type BackendClient struct {
	conn        *grpc.ClientConn
	authClient  authpb.AuthServiceClient
	alertClient alertpb.AlertServiceClient
	address     string
}

type AuthResult struct {
	Success     bool   `json:"success"`
	SessionID   string `json:"session_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	Username    string `json:"username,omitempty"`
	Email       string `json:"email,omitempty"`
	Error       string `json:"error,omitempty"`
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

func NewBackendClient(address string) *BackendClient {
	return &BackendClient{
		address: address,
	}
}

func (c *BackendClient) Connect() error {
	conn, err := grpc.NewClient(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to backend: %w", err)
	}

	c.conn = conn
	c.authClient = authpb.NewAuthServiceClient(conn)
	c.alertClient = alertpb.NewAlertServiceClient(conn)

	return nil
}

func (c *BackendClient) IsConnected() bool {
	return c.conn != nil && c.authClient != nil
}

func (c *BackendClient) HealthCheck() error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a simple call to check if backend is responsive
	_, err := c.authClient.GetProfile(ctx, &authpb.GetProfileRequest{
		SessionId: "health-check", // This will fail auth but proves connectivity
	})

	// We expect an auth error, not a connection error
	if err != nil && !isAuthError(err) {
		return fmt.Errorf("backend health check failed: %w", err)
	}

	return nil
}

func isAuthError(err error) bool {
	// Check if error is related to authentication rather than connectivity
	errStr := err.Error()
	return contains(errStr, "invalid session") || 
		   contains(errStr, "unauthorized") || 
		   contains(errStr, "session not found") ||
		   contains(errStr, "authentication")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		   (s == substr || (len(s) > len(substr) && 
		   (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		   hasSubstring(s, substr))))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (c *BackendClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *BackendClient) Login(username, password string) (*AuthResult, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.LoginRequest{
		Username: username,
		Password: password,
	}

	resp, err := c.authClient.Login(ctx, req)
	if err != nil {
		return &AuthResult{
			Success: false,
			Error:   fmt.Sprintf("Login failed: %v", err),
		}, nil
	}

	return &AuthResult{
		Success:   true,
		SessionID: resp.SessionId,
		UserID:    resp.User.Id,
		Username:  resp.User.Username,
		Email:     resp.User.Email,
	}, nil
}

func (c *BackendClient) Register(username, email, password string) (*AuthResult, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.RegisterRequest{
		Username: username,
		Email:    email,
		Password: password,
	}

	resp, err := c.authClient.Register(ctx, req)
	if err != nil {
		return &AuthResult{
			Success: false,
			Error:   fmt.Sprintf("Registration failed: %v", err),
		}, nil
	}

	if !resp.Success {
		return &AuthResult{
			Success: false,
			Error:   resp.Message,
		}, nil
	}

	return &AuthResult{
		Success:  true,
		UserID:   resp.UserId,
		Username: username, // Use provided username since response doesn't include full user
		Email:    email,    // Use provided email
	}, nil
}

func (c *BackendClient) Logout(sessionID string) error {
	if c.authClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.LogoutRequest{
		SessionId: sessionID,
	}

	_, err := c.authClient.Logout(ctx, req)
	return err
}

func (c *BackendClient) ValidateSession(sessionID string) (*User, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.ValidateSessionRequest{
		SessionId: sessionID,
	}

	resp, err := c.authClient.ValidateSession(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.User == nil {
		return nil, fmt.Errorf("invalid session")
	}

	return &User{
		ID:       resp.User.Id,
		Username: resp.User.Username,
		Email:    resp.User.Email,
	}, nil
}

func (c *BackendClient) GetProfile(sessionID string) (*User, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.GetProfileRequest{
		SessionId: sessionID,
	}

	resp, err := c.authClient.GetProfile(ctx, req)
	if err != nil {
		return nil, err
	}

	return &User{
		ID:       resp.User.Id,
		Username: resp.User.Username,
		Email:    resp.User.Email,
	}, nil
}

// Alert acknowledgment and resolution methods

// AddAcknowledgment acknowledges an alert
func (c *BackendClient) AddAcknowledgment(sessionID, alertKey, reason string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.AddAcknowledgmentRequest{
		SessionId: sessionID,
		AlertKey:  alertKey,
		Reason:    reason,
	}

	_, err := c.alertClient.AddAcknowledgment(ctx, req)
	return err
}

// DeleteAcknowledgment removes acknowledgment from an alert
func (c *BackendClient) DeleteAcknowledgment(sessionID, alertKey string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.DeleteAcknowledgmentRequest{
		SessionId: sessionID,
		AlertKey:  alertKey,
	}

	_, err := c.alertClient.DeleteAcknowledgment(ctx, req)
	return err
}

// GetAcknowledgments retrieves acknowledgments for an alert
func (c *BackendClient) GetAcknowledgments(alertKey string) ([]*alertpb.Acknowledgment, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetAcknowledgmentsRequest{
		AlertKey: alertKey,
	}

	resp, err := c.alertClient.GetAcknowledgments(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Acknowledgments, nil
}

// GetAllAcknowledgedAlerts retrieves all acknowledged alerts from the backend
func (c *BackendClient) GetAllAcknowledgedAlerts() (map[string]*alertpb.Acknowledgment, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetAllAcknowledgedAlertsRequest{}

	resp, err := c.alertClient.GetAllAcknowledgedAlerts(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.AcknowledgedAlerts, nil
}

// GetComments retrieves comments for an alert
func (c *BackendClient) GetComments(alertKey string) ([]*alertpb.Comment, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetCommentsRequest{
		AlertKey: alertKey,
	}

	resp, err := c.alertClient.GetComments(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Comments, nil
}

// AddComment adds a comment to an alert
func (c *BackendClient) AddComment(sessionID, alertKey, content string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.AddCommentRequest{
		SessionId: sessionID,
		AlertKey:  alertKey,
		Content:   content,
	}

	_, err := c.alertClient.AddComment(ctx, req)
	return err
}

// DeleteComment removes a comment from an alert
func (c *BackendClient) DeleteComment(sessionID, commentID string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.DeleteCommentRequest{
		SessionId: sessionID,
		CommentId: commentID,
	}

	_, err := c.alertClient.DeleteComment(ctx, req)
	return err
}

// CreateResolvedAlert stores a resolved alert in the backend
func (c *BackendClient) CreateResolvedAlert(fingerprint, source string, alertData, comments, acknowledgments []byte, ttlHours int) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.CreateResolvedAlertRequest{
		Fingerprint:     fingerprint,
		Source:          source,
		AlertData:       alertData,
		Comments:        comments,
		Acknowledgments: acknowledgments,
		TtlHours:        int32(ttlHours),
	}

	resp, err := c.alertClient.CreateResolvedAlert(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to create resolved alert: %s", resp.Message)
	}

	return nil
}

// GetResolvedAlerts retrieves resolved alerts from the backend
func (c *BackendClient) GetResolvedAlerts(limit, offset int) ([]*alertpb.ResolvedAlertInfo, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetResolvedAlertsRequest{
		Limit:  int32(limit),
		Offset: int32(offset),
	}

	resp, err := c.alertClient.GetResolvedAlerts(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get resolved alerts: %s", resp.Message)
	}

	return resp.ResolvedAlerts, nil
}

// GetResolvedAlert retrieves a specific resolved alert by fingerprint
func (c *BackendClient) GetResolvedAlert(fingerprint string) (*alertpb.ResolvedAlertInfo, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetResolvedAlertRequest{
		Fingerprint: fingerprint,
	}

	resp, err := c.alertClient.GetResolvedAlert(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get resolved alert: %s", resp.Message)
	}

	return resp.ResolvedAlert, nil
}

// RemoveAllResolvedAlerts removes all resolved alerts from the backend
func (c *BackendClient) RemoveAllResolvedAlerts() error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.RemoveAllResolvedAlertsRequest{}

	resp, err := c.alertClient.RemoveAllResolvedAlerts(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to remove all resolved alerts: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("failed to remove all resolved alerts: %s", resp.Message)
	}

	return nil
}

// Color Preference methods

// GetUserColorPreferences retrieves user color preferences from the backend
func (c *BackendClient) GetUserColorPreferences(sessionID string) ([]*alertpb.UserColorPreference, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetUserColorPreferencesRequest{
		SessionId: sessionID,
	}

	resp, err := c.alertClient.GetUserColorPreferences(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get color preferences: %s", resp.Message)
	}

	return resp.Preferences, nil
}

// SaveUserColorPreferences saves user color preferences to the backend
func (c *BackendClient) SaveUserColorPreferences(sessionID string, preferences []models.UserColorPreference) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Convert webui models to protobuf models
	var pbPreferences []*alertpb.UserColorPreference
	for _, pref := range preferences {
		pbPreferences = append(pbPreferences, &alertpb.UserColorPreference{
			Id:               pref.ID,
			UserId:           pref.UserID,
			LabelConditions:  pref.LabelConditions,
			Color:            pref.Color,
			ColorType:        pref.ColorType,
			Priority:         int32(pref.Priority),
		})
	}

	req := &alertpb.SaveUserColorPreferencesRequest{
		SessionId:   sessionID,
		Preferences: pbPreferences,
	}

	resp, err := c.alertClient.SaveUserColorPreferences(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to save color preferences: %s", resp.Message)
	}

	return nil
}

// DeleteUserColorPreference deletes a specific user color preference
func (c *BackendClient) DeleteUserColorPreference(sessionID, preferenceID string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.DeleteUserColorPreferenceRequest{
		SessionId:    sessionID,
		PreferenceId: preferenceID,
	}

	resp, err := c.alertClient.DeleteUserColorPreference(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to delete color preference: %s", resp.Message)
	}

	return nil
}