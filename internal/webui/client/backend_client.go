package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	alertpb "notificator/internal/backend/proto/alert"
	authpb "notificator/internal/backend/proto/auth"
	"notificator/internal/webui/models"
)

type BackendClient struct {
	conn             *grpc.ClientConn
	authClient       authpb.AuthServiceClient
	alertClient      alertpb.AlertServiceClient
	statisticsClient alertpb.StatisticsServiceClient
	address          string
}

type AuthResult struct {
	Success   bool   `json:"success"`
	SessionID string `json:"session_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	Email     string `json:"email,omitempty"`
	Error     string `json:"error,omitempty"`
}

type User struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	Email         string  `json:"email"`
	OAuthProvider *string `json:"oauth_provider,omitempty"`
	OAuthID       *string `json:"oauth_id,omitempty"`
}

// IsOAuthUser returns true if the user was created via OAuth
func (u *User) IsOAuthUser() bool {
	return u.OAuthProvider != nil && *u.OAuthProvider != ""
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
	c.statisticsClient = alertpb.NewStatisticsServiceClient(conn)

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

	user := &User{
		ID:       resp.User.Id,
		Username: resp.User.Username,
		Email:    resp.User.Email,
	}

	if resp.User.OauthProvider != "" {
		user.OAuthProvider = &resp.User.OauthProvider
	}
	if resp.User.OauthId != "" {
		user.OAuthID = &resp.User.OauthId
	}

	return user, nil
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

	user := &User{
		ID:       resp.User.Id,
		Username: resp.User.Username,
		Email:    resp.User.Email,
	}

	if resp.User.OauthProvider != "" {
		user.OAuthProvider = &resp.User.OauthProvider
	}
	if resp.User.OauthId != "" {
		user.OAuthID = &resp.User.OauthId
	}

	return user, nil
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
			Id:                 pref.ID,
			UserId:             pref.UserID,
			LabelConditions:    pref.LabelConditions,
			Color:              pref.Color,
			ColorType:          pref.ColorType,
			Priority:           int32(pref.Priority),
			BgLightnessFactor:  float32(pref.BgLightnessFactor),
			TextDarknessFactor: float32(pref.TextDarknessFactor),
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

// OAuth methods

// GetOAuthAuthURL gets the OAuth authorization URL for a provider
func (c *BackendClient) GetOAuthAuthURL(provider, state string) (string, error) {
	if c.authClient == nil {
		return "", fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.OAuthAuthURLRequest{
		Provider: provider,
		State:    state,
	}

	resp, err := c.authClient.GetOAuthAuthURL(ctx, req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("failed to get OAuth auth URL: %s", resp.Error)
	}

	return resp.AuthUrl, nil
}

// OAuthCallback handles the OAuth callback
func (c *BackendClient) OAuthCallback(provider, code, state string) (*AuthResult, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.OAuthCallbackRequest{
		Provider: provider,
		Code:     code,
		State:    state,
	}

	resp, err := c.authClient.OAuthCallback(ctx, req)
	if err != nil {
		return &AuthResult{
			Success: false,
			Error:   fmt.Sprintf("OAuth callback failed: %v", err),
		}, nil
	}

	return &AuthResult{
		Success:   resp.Success,
		SessionID: resp.SessionId,
		UserID:    resp.UserId,
		Username:  resp.Username,
		Email:     resp.Email,
		Error:     resp.Error,
	}, nil
}

// GetOAuthProviders retrieves the list of available OAuth providers
func (c *BackendClient) GetOAuthProviders() ([]map[string]interface{}, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.GetOAuthProvidersRequest{}

	resp, err := c.authClient.GetOAuthProviders(ctx, req)
	if err != nil {
		return nil, err
	}

	var providers []map[string]interface{}
	for _, provider := range resp.Providers {
		providers = append(providers, map[string]interface{}{
			"name":         provider.Name,
			"display_name": provider.DisplayName,
			"enabled":      provider.Enabled,
		})
	}

	return providers, nil
}

// IsOAuthEnabled checks if OAuth is enabled
func (c *BackendClient) IsOAuthEnabled() (bool, error) {
	if c.authClient == nil {
		return false, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.GetOAuthProvidersRequest{}

	resp, err := c.authClient.GetOAuthProviders(ctx, req)
	if err != nil {
		return false, err
	}

	return len(resp.Providers) > 0, nil
}

// GetOAuthConfig retrieves OAuth configuration
func (c *BackendClient) GetOAuthConfig() (map[string]interface{}, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.GetOAuthConfigRequest{}

	resp, err := c.authClient.GetOAuthConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert providers to []map[string]interface{}
	var providers []map[string]interface{}
	for _, provider := range resp.Providers {
		providers = append(providers, map[string]interface{}{
			"name":         provider.Name,
			"display_name": provider.DisplayName,
			"enabled":      provider.Enabled,
		})
	}

	config := map[string]interface{}{
		"enabled":              resp.Enabled,
		"disable_classic_auth": resp.DisableClassicAuth,
		"providers":            providers,
	}

	return config, nil
}

// GetUserGroups retrieves user groups
func (c *BackendClient) GetUserGroups(userID string) ([]map[string]interface{}, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &authpb.GetUserGroupsRequest{
		UserId: userID,
	}

	resp, err := c.authClient.GetUserGroups(ctx, req)
	if err != nil {
		return nil, err
	}

	var groups []map[string]interface{}
	for _, group := range resp.Groups {
		groups = append(groups, map[string]interface{}{
			"id":       group.Id,
			"name":     group.Name,
			"provider": group.Provider,
			"type":     group.Type,
		})
	}

	return groups, nil
}

// SyncUserGroups synchronizes user groups with OAuth provider
func (c *BackendClient) SyncUserGroups(userID, provider string) error {
	if c.authClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.SyncUserGroupsRequest{
		UserId:   userID,
		Provider: provider,
	}

	resp, err := c.authClient.SyncUserGroups(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to sync user groups: %s", resp.Error)
	}

	return nil
}

// Hidden Alerts methods

// GetUserHiddenAlerts retrieves hidden alerts for a user
func (c *BackendClient) GetUserHiddenAlerts(sessionID string) ([]*alertpb.UserHiddenAlert, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetUserHiddenAlertsRequest{
		SessionId: sessionID,
	}

	resp, err := c.alertClient.GetUserHiddenAlerts(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get hidden alerts: %s", resp.Message)
	}

	return resp.HiddenAlerts, nil
}

// HideAlert hides a specific alert for a user
func (c *BackendClient) HideAlert(sessionID, fingerprint, alertName, instance, reason string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.HideAlertRequest{
		SessionId:   sessionID,
		Fingerprint: fingerprint,
		AlertName:   alertName,
		Instance:    instance,
		Reason:      reason,
	}

	resp, err := c.alertClient.HideAlert(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to hide alert: %s", resp.Message)
	}

	return nil
}

// UnhideAlert unhides a specific alert for a user
func (c *BackendClient) UnhideAlert(sessionID, fingerprint string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.UnhideAlertRequest{
		SessionId:   sessionID,
		Fingerprint: fingerprint,
	}

	resp, err := c.alertClient.UnhideAlert(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to unhide alert: %s", resp.Message)
	}

	return nil
}

// ClearAllHiddenAlerts removes all hidden alerts for a user
func (c *BackendClient) ClearAllHiddenAlerts(sessionID string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.ClearAllHiddenAlertsRequest{
		SessionId: sessionID,
	}

	resp, err := c.alertClient.ClearAllHiddenAlerts(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to clear hidden alerts: %s", resp.Message)
	}

	return nil
}

// GetUserHiddenRules retrieves hidden rules for a user
func (c *BackendClient) GetUserHiddenRules(sessionID string) ([]*alertpb.UserHiddenRule, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetUserHiddenRulesRequest{
		SessionId: sessionID,
	}

	resp, err := c.alertClient.GetUserHiddenRules(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get hidden rules: %s", resp.Message)
	}

	return resp.HiddenRules, nil
}

// SaveHiddenRule saves or updates a hidden rule for a user
func (c *BackendClient) SaveHiddenRule(sessionID string, rule *alertpb.UserHiddenRule) (*alertpb.UserHiddenRule, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.SaveHiddenRuleRequest{
		SessionId: sessionID,
		Rule:      rule,
	}

	resp, err := c.alertClient.SaveHiddenRule(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to save hidden rule: %s", resp.Message)
	}

	return resp.Rule, nil
}

// RemoveHiddenRule removes a hidden rule for a user
func (c *BackendClient) RemoveHiddenRule(sessionID, ruleID string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.RemoveHiddenRuleRequest{
		SessionId: sessionID,
		RuleId:    ruleID,
	}

	resp, err := c.alertClient.RemoveHiddenRule(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to remove hidden rule: %s", resp.Message)
	}

	return nil
}

// Sentry Configuration Methods

// SentryConfig represents a user's Sentry configuration
type SentryConfig struct {
	BaseUrl   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GetUserSentryConfig retrieves a user's Sentry configuration
func (c *BackendClient) GetUserSentryConfig(userID string) (*SentryConfig, error) {
	if c.authClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &authpb.GetUserSentryConfigRequest{
		UserId: userID,
	}

	resp, err := c.authClient.GetUserSentryConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		if resp.Error != "" {
			return nil, fmt.Errorf(resp.Error)
		}
		return nil, nil // No config found
	}

	if resp.Config == nil {
		return nil, nil // No config found
	}

	return &SentryConfig{
		BaseUrl:   resp.Config.BaseUrl,
		CreatedAt: resp.Config.CreatedAt.AsTime(),
		UpdatedAt: resp.Config.UpdatedAt.AsTime(),
	}, nil
}

// GetUserSentryToken retrieves a user's decrypted Sentry personal token
func (c *BackendClient) GetUserSentryToken(userID, sessionID string) (string, bool, error) {
	if c.authClient == nil {
		return "", false, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &authpb.GetUserSentryTokenRequest{
		UserId:    userID,
		SessionId: sessionID,
	}

	resp, err := c.authClient.GetUserSentryToken(ctx, req)
	if err != nil {
		return "", false, err
	}

	if !resp.Success {
		if resp.Error != "" {
			return "", false, fmt.Errorf(resp.Error)
		}
		return "", false, nil // No error, but no token
	}

	return resp.PersonalToken, resp.HasToken, nil
}

// SaveUserSentryConfig saves a user's Sentry configuration
func (c *BackendClient) SaveUserSentryConfig(userID, token, baseURL, sessionID string) error {
	if c.authClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &authpb.SaveUserSentryConfigRequest{
		UserId:        userID,
		PersonalToken: token,
		BaseUrl:       baseURL,
		SessionId:     sessionID,
	}

	resp, err := c.authClient.SaveUserSentryConfig(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to save Sentry config: %s", resp.Error)
	}

	return nil
}

// DeleteUserSentryConfig deletes a user's Sentry configuration
func (c *BackendClient) DeleteUserSentryConfig(userID, sessionID string) error {
	if c.authClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &authpb.DeleteUserSentryConfigRequest{
		UserId:    userID,
		SessionId: sessionID,
	}

	resp, err := c.authClient.DeleteUserSentryConfig(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to delete Sentry config: %s", resp.Error)
	}

	return nil
}

// Notification Preferences methods

// NotificationPreferences represents user notification preferences
type NotificationPreferences struct {
	BrowserNotificationsEnabled bool     `json:"browser_notifications_enabled"`
	EnabledSeverities           []string `json:"enabled_severities"`
	SoundNotificationsEnabled   bool     `json:"sound_notifications_enabled"`
}

// GetNotificationPreferences retrieves notification preferences for the authenticated user
func (c *BackendClient) GetNotificationPreferences(sessionID string) (*NotificationPreferences, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetNotificationPreferencesRequest{
		SessionId: sessionID,
	}

	resp, err := c.alertClient.GetNotificationPreferences(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get notification preferences: %s", resp.Message)
	}

	if resp.Preferences == nil {
		// Return default preferences
		return &NotificationPreferences{
			BrowserNotificationsEnabled: false,
			EnabledSeverities:           []string{"critical", "warning"},
			SoundNotificationsEnabled:   true,
		}, nil
	}

	return &NotificationPreferences{
		BrowserNotificationsEnabled: resp.Preferences.BrowserNotificationsEnabled,
		EnabledSeverities:           resp.Preferences.EnabledSeverities,
		SoundNotificationsEnabled:   resp.Preferences.SoundNotificationsEnabled,
	}, nil
}

// SaveNotificationPreferences saves notification preferences for the authenticated user
func (c *BackendClient) SaveNotificationPreferences(sessionID string, prefs *NotificationPreferences) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.SaveNotificationPreferencesRequest{
		SessionId:                   sessionID,
		BrowserNotificationsEnabled: prefs.BrowserNotificationsEnabled,
		EnabledSeverities:           prefs.EnabledSeverities,
		SoundNotificationsEnabled:   prefs.SoundNotificationsEnabled,
	}

	resp, err := c.alertClient.SaveNotificationPreferences(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to save notification preferences: %s", resp.Message)
	}

	return nil
}

// GetFilterPresets gets all filter presets for the current user
func (c *BackendClient) GetFilterPresets(sessionID string, includeShared bool) ([]models.FilterPreset, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetFilterPresetsRequest{
		SessionId:     sessionID,
		IncludeShared: includeShared,
	}

	resp, err := c.alertClient.GetFilterPresets(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get filter presets: %s", resp.Message)
	}

	// Convert protobuf to model
	presets := make([]models.FilterPreset, len(resp.Presets))
	for i, pbPreset := range resp.Presets {
		var filterData models.FilterPresetData
		// Parse the JSON filter data
		if len(pbPreset.FilterData) > 0 {
			if err := json.Unmarshal(pbPreset.FilterData, &filterData); err != nil {
				return nil, fmt.Errorf("failed to unmarshal filter data: %w", err)
			}
		}

		presets[i] = models.FilterPreset{
			ID:          pbPreset.Id,
			UserID:      pbPreset.UserId,
			Name:        pbPreset.Name,
			Description: pbPreset.Description,
			IsShared:    pbPreset.IsShared,
			IsDefault:   pbPreset.IsDefault,
			FilterData:  filterData,
			CreatedAt:   pbPreset.CreatedAt.AsTime(),
			UpdatedAt:   pbPreset.UpdatedAt.AsTime(),
		}
	}

	return presets, nil
}

// SaveFilterPreset creates a new filter preset
func (c *BackendClient) SaveFilterPreset(sessionID, name, description string, isShared bool, filterData models.FilterPresetData) (*models.FilterPreset, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Marshal filter data to JSON
	filterDataBytes, err := json.Marshal(filterData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filter data: %w", err)
	}

	req := &alertpb.SaveFilterPresetRequest{
		SessionId:   sessionID,
		Name:        name,
		Description: description,
		IsShared:    isShared,
		FilterData:  filterDataBytes,
	}

	resp, err := c.alertClient.SaveFilterPreset(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to save filter preset: %s", resp.Message)
	}

	// Convert protobuf to model
	var parsedFilterData models.FilterPresetData
	if len(resp.Preset.FilterData) > 0 {
		if err := json.Unmarshal(resp.Preset.FilterData, &parsedFilterData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal filter data: %w", err)
		}
	}

	preset := &models.FilterPreset{
		ID:          resp.Preset.Id,
		UserID:      resp.Preset.UserId,
		Name:        resp.Preset.Name,
		Description: resp.Preset.Description,
		IsShared:    resp.Preset.IsShared,
		IsDefault:   resp.Preset.IsDefault,
		FilterData:  parsedFilterData,
		CreatedAt:   resp.Preset.CreatedAt.AsTime(),
		UpdatedAt:   resp.Preset.UpdatedAt.AsTime(),
	}

	return preset, nil
}

// UpdateFilterPreset updates an existing filter preset
func (c *BackendClient) UpdateFilterPreset(sessionID, presetID, name, description string, isShared bool, filterData models.FilterPresetData) (*models.FilterPreset, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Marshal filter data to JSON
	filterDataBytes, err := json.Marshal(filterData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filter data: %w", err)
	}

	req := &alertpb.UpdateFilterPresetRequest{
		SessionId:   sessionID,
		PresetId:    presetID,
		Name:        name,
		Description: description,
		IsShared:    isShared,
		FilterData:  filterDataBytes,
	}

	resp, err := c.alertClient.UpdateFilterPreset(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to update filter preset: %s", resp.Message)
	}

	// Convert protobuf to model
	var parsedFilterData models.FilterPresetData
	if len(resp.Preset.FilterData) > 0 {
		if err := json.Unmarshal(resp.Preset.FilterData, &parsedFilterData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal filter data: %w", err)
		}
	}

	preset := &models.FilterPreset{
		ID:          resp.Preset.Id,
		UserID:      resp.Preset.UserId,
		Name:        resp.Preset.Name,
		Description: resp.Preset.Description,
		IsShared:    resp.Preset.IsShared,
		IsDefault:   resp.Preset.IsDefault,
		FilterData:  parsedFilterData,
		CreatedAt:   resp.Preset.CreatedAt.AsTime(),
		UpdatedAt:   resp.Preset.UpdatedAt.AsTime(),
	}

	return preset, nil
}

// DeleteFilterPreset deletes a filter preset
func (c *BackendClient) DeleteFilterPreset(sessionID, presetID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.DeleteFilterPresetRequest{
		SessionId: sessionID,
		PresetId:  presetID,
	}

	resp, err := c.alertClient.DeleteFilterPreset(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to delete filter preset: %s", resp.Message)
	}

	return nil
}

// SetDefaultFilterPreset sets a filter preset as the default
func (c *BackendClient) SetDefaultFilterPreset(sessionID, presetID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.SetDefaultFilterPresetRequest{
		SessionId: sessionID,
		PresetId:  presetID,
	}

	resp, err := c.alertClient.SetDefaultFilterPreset(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to set default filter preset: %s", resp.Message)
	}

	return nil
}

// ==================== Statistics Methods ====================

// QueryStatistics queries alert statistics with filters and aggregations
func (c *BackendClient) QueryStatistics(sessionID string, req *alertpb.QueryStatisticsRequest) (*alertpb.QueryStatisticsResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req.SessionId = sessionID

	resp, err := c.statisticsClient.QueryStatistics(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query statistics: %w", err)
	}

	return resp, nil
}

// SaveOnCallRule saves an on-call rule
func (c *BackendClient) SaveOnCallRule(sessionID string, req *alertpb.SaveOnCallRuleRequest) (*alertpb.OnCallRule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req.SessionId = sessionID

	resp, err := c.statisticsClient.SaveOnCallRule(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to save on-call rule: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to save rule: %s", resp.Message)
	}

	return resp.Rule, nil
}

// GetOnCallRules retrieves all on-call rules for the authenticated user
func (c *BackendClient) GetOnCallRules(sessionID string, activeOnly bool) ([]*alertpb.OnCallRule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &alertpb.GetOnCallRulesRequest{
		SessionId:  sessionID,
		ActiveOnly: activeOnly,
	}

	resp, err := c.statisticsClient.GetOnCallRules(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get on-call rules: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get rules: %s", resp.Message)
	}

	return resp.Rules, nil
}

// GetOnCallRule retrieves a specific on-call rule
func (c *BackendClient) GetOnCallRule(sessionID, ruleID string) (*alertpb.OnCallRule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &alertpb.GetOnCallRuleRequest{
		SessionId: sessionID,
		RuleId:    ruleID,
	}

	resp, err := c.statisticsClient.GetOnCallRule(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get on-call rule: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get rule: %s", resp.Message)
	}

	return resp.Rule, nil
}

// UpdateOnCallRule updates an existing on-call rule
func (c *BackendClient) UpdateOnCallRule(sessionID string, req *alertpb.UpdateOnCallRuleRequest) (*alertpb.OnCallRule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req.SessionId = sessionID

	resp, err := c.statisticsClient.UpdateOnCallRule(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to update on-call rule: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to update rule: %s", resp.Message)
	}

	return resp.Rule, nil
}

// DeleteOnCallRule deletes an on-call rule
func (c *BackendClient) DeleteOnCallRule(sessionID, ruleID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &alertpb.DeleteOnCallRuleRequest{
		SessionId: sessionID,
		RuleId:    ruleID,
	}

	resp, err := c.statisticsClient.DeleteOnCallRule(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete on-call rule: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("failed to delete rule: %s", resp.Message)
	}

	return nil
}

// TestOnCallRule tests an on-call rule without saving it
func (c *BackendClient) TestOnCallRule(sessionID string, req *alertpb.TestOnCallRuleRequest) (*alertpb.TestOnCallRuleResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req.SessionId = sessionID

	resp, err := c.statisticsClient.TestOnCallRule(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to test on-call rule: %w", err)
	}

	return resp, nil
}

// GetStatisticsSummary retrieves a summary of available statistics
func (c *BackendClient) GetStatisticsSummary(sessionID string) (*alertpb.GetStatisticsSummaryResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &alertpb.GetStatisticsSummaryRequest{
		SessionId: sessionID,
	}

	resp, err := c.statisticsClient.GetStatisticsSummary(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics summary: %w", err)
	}

	return resp, nil
}

// ==================== Alert Capture Methods ====================

// CaptureAlertFired captures statistics when an alert fires
func (c *BackendClient) CaptureAlertFired(alert *models.DashboardAlert) error {
	if !c.IsConnected() {
		return fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Build metadata JSON from alert
	metadata := make(map[string]interface{})
	if alert.Labels != nil {
		metadata["labels"] = alert.Labels
	}
	if alert.Annotations != nil {
		metadata["annotations"] = alert.Annotations
	}
	if alert.Source != "" {
		metadata["source"] = alert.Source
	}
	if alert.Instance != "" {
		metadata["instance"] = alert.Instance
	}
	if alert.Team != "" {
		metadata["team"] = alert.Team
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	req := &alertpb.CaptureAlertFiredRequest{
		Fingerprint: alert.Fingerprint,
		AlertName:   alert.AlertName,
		Severity:    alert.Severity,
		StartsAt:    timestamppb.New(alert.StartsAt),
		Metadata:    metadataBytes,
	}

	_, err = c.statisticsClient.CaptureAlertFired(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to capture alert fired: %w", err)
	}

	return nil
}

// UpdateAlertResolved updates statistics when an alert resolves
func (c *BackendClient) UpdateAlertResolved(alert *models.DashboardAlert) error {
	if !c.IsConnected() {
		return fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req := &alertpb.UpdateAlertResolvedRequest{
		Fingerprint: alert.Fingerprint,
		ResolvedAt:  timestamppb.New(alert.ResolvedAt),
	}

	_, err := c.statisticsClient.UpdateAlertResolved(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to update alert resolved: %w", err)
	}

	return nil
}

// UpdateAlertAcknowledged updates statistics when an alert is acknowledged
func (c *BackendClient) UpdateAlertAcknowledged(alert *models.DashboardAlert) error {
	if !c.IsConnected() {
		return fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req := &alertpb.UpdateAlertAcknowledgedRequest{
		Fingerprint:     alert.Fingerprint,
		AcknowledgedAt:  timestamppb.New(alert.AcknowledgedAt),
	}

	_, err := c.statisticsClient.UpdateAlertAcknowledged(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to update alert acknowledged: %w", err)
	}

	return nil
}

// QueryRecentlyResolved queries recently resolved alerts from statistics
func (c *BackendClient) QueryRecentlyResolved(sessionID string, startDate, endDate time.Time, severity []string, team, alertName, searchQuery string, includeSilenced bool, limit, offset int) (map[string]interface{}, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.QueryRecentlyResolvedRequest{
		SessionId:       sessionID,
		StartDate:       timestamppb.New(startDate),
		EndDate:         timestamppb.New(endDate),
		Severity:        severity,
		Team:            team,
		AlertName:       alertName,
		SearchQuery:     searchQuery,
		IncludeSilenced: includeSilenced,
		Limit:           int32(limit),
		Offset:          int32(offset),
	}

	resp, err := c.statisticsClient.QueryRecentlyResolved(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query recently resolved: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("query failed: %s", resp.Message)
	}

	// Convert to map for JSON
	alerts := make([]map[string]interface{}, len(resp.Alerts))
	for i, alert := range resp.Alerts {
		alerts[i] = map[string]interface{}{
			"fingerprint":        alert.Fingerprint,
			"alert_name":         alert.AlertName,
			"severity":           alert.Severity,
			"occurrence_count":   alert.OccurrenceCount,
			"first_fired_at":     alert.FirstFiredAt.AsTime(),
			"last_resolved_at":   alert.LastResolvedAt.AsTime(),
			"total_duration":     alert.TotalDuration,
			"avg_duration":       alert.AvgDuration,
			"total_mttr":         alert.TotalMttr,
			"avg_mttr":           alert.AvgMttr,
			"labels":             alert.Labels,
			"annotations":        alert.Annotations,
			"source":             alert.Source,
			"instance":           alert.Instance,
			"team":               alert.Team,
		}
	}

	return map[string]interface{}{
		"success":     true,
		"alerts":      alerts,
		"total_count": resp.TotalCount,
		"start_date":  resp.StartDate.AsTime(),
		"end_date":    resp.EndDate.AsTime(),
	}, nil
}

// GetAnnotationButtonConfigs retrieves annotation button configurations for a user
func (c *BackendClient) GetAnnotationButtonConfigs(sessionID string) ([]*alertpb.AnnotationButtonConfig, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetAnnotationButtonConfigsRequest{
		SessionId: sessionID,
	}

	resp, err := c.alertClient.GetAnnotationButtonConfigs(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get annotation button configs: %s", resp.Message)
	}

	return resp.Configs, nil
}

// SaveAnnotationButtonConfigs saves annotation button configurations for a user
func (c *BackendClient) SaveAnnotationButtonConfigs(sessionID string, configs []*alertpb.AnnotationButtonConfig) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.SaveAnnotationButtonConfigsRequest{
		SessionId: sessionID,
		Configs:   configs,
	}

	resp, err := c.alertClient.SaveAnnotationButtonConfigs(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to save annotation button configs: %s", resp.Message)
	}

	return nil
}

// CreateAnnotationButtonConfig creates a new annotation button configuration
func (c *BackendClient) CreateAnnotationButtonConfig(sessionID string, config *alertpb.AnnotationButtonConfig) (*alertpb.AnnotationButtonConfig, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.CreateAnnotationButtonConfigRequest{
		SessionId: sessionID,
		Config:    config,
	}

	resp, err := c.alertClient.CreateAnnotationButtonConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to create annotation button config: %s", resp.Message)
	}

	return resp.Config, nil
}

// UpdateAnnotationButtonConfig updates an existing annotation button configuration
func (c *BackendClient) UpdateAnnotationButtonConfig(sessionID string, config *alertpb.AnnotationButtonConfig) (*alertpb.AnnotationButtonConfig, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.UpdateAnnotationButtonConfigRequest{
		SessionId: sessionID,
		Config:    config,
	}

	resp, err := c.alertClient.UpdateAnnotationButtonConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to update annotation button config: %s", resp.Message)
	}

	return resp.Config, nil
}

// DeleteAnnotationButtonConfig deletes an annotation button configuration
func (c *BackendClient) DeleteAnnotationButtonConfig(sessionID, configID string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.DeleteAnnotationButtonConfigRequest{
		SessionId: sessionID,
		ConfigId:  configID,
	}

	resp, err := c.alertClient.DeleteAnnotationButtonConfig(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to delete annotation button config: %s", resp.Message)
	}

	return nil
}

// GetAlertHistory retrieves the occurrence history for an alert fingerprint
func (c *BackendClient) GetAlertHistory(sessionID, fingerprint string, limit int32) ([]*alertpb.AlertStatistic, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.GetAlertHistoryRequest{
		SessionId:   sessionID,
		Fingerprint: fingerprint,
		Limit:       limit,
	}

	resp, err := c.statisticsClient.GetAlertHistory(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get alert history: %s", resp.Message)
	}

	return resp.History, nil
}
