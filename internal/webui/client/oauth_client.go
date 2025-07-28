package client

import (
	"context"
	"fmt"
	"time"

	authpb "notificator/internal/backend/proto/auth"
)

type OAuthAuthResult struct {
	Success   bool   `json:"success"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	SessionID string `json:"session_id"`
	Error     string `json:"error,omitempty"`
}

type OAuthProvider struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
}

func (bc *BackendClient) GetOAuthAuthURL(provider, state string) (string, error) {
	if bc.authClient == nil {
		return "", fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.OAuthAuthURLRequest{
		Provider: provider,
		State:    state,
	}

	resp, err := bc.authClient.GetOAuthAuthURL(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to get OAuth auth URL: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("backend error: %s", resp.Error)
	}

	return resp.AuthUrl, nil
}

func (bc *BackendClient) OAuthCallback(provider, code, state string) (*OAuthAuthResult, error) {
	if bc.authClient == nil {
		return nil, fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.OAuthCallbackRequest{
		Provider: provider,
		Code:     code,
		State:    state,
	}

	resp, err := bc.authClient.OAuthCallback(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("OAuth callback failed: %w", err)
	}

	result := &OAuthAuthResult{
		Success:   resp.Success,
		UserID:    resp.UserId,
		Username:  resp.Username,
		Email:     resp.Email,
		SessionID: resp.SessionId,
		Error:     resp.Error,
	}

	return result, nil
}

func (bc *BackendClient) GetOAuthProviders() ([]map[string]interface{}, error) {
	if bc.authClient == nil {
		return nil, fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.GetOAuthProvidersRequest{}

	resp, err := bc.authClient.GetOAuthProviders(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth providers: %w", err)
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

func (bc *BackendClient) GetUserGroups(userID string) ([]map[string]interface{}, error) {
	if bc.authClient == nil {
		return nil, fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.GetUserGroupsRequest{
		UserId: userID,
	}

	resp, err := bc.authClient.GetUserGroups(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user groups: %w", err)
	}

	var groups []map[string]interface{}
	for _, group := range resp.Groups {
		groups = append(groups, map[string]interface{}{
			"id":          group.Id,
			"name":        group.Name,
			"provider":    group.Provider,
			"type":        group.Type,
			"role":        group.Role,
			"permissions": group.Permissions,
		})
	}

	return groups, nil
}

func (bc *BackendClient) SyncUserGroups(userID, provider string) error {
	if bc.authClient == nil {
		return fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.SyncUserGroupsRequest{
		UserId:   userID,
		Provider: provider,
	}

	resp, err := bc.authClient.SyncUserGroups(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to sync user groups: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("backend error: %s", resp.Error)
	}

	return nil
}

func (bc *BackendClient) IsOAuthEnabled() (bool, error) {
	if bc.authClient == nil {
		return false, fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.GetOAuthConfigRequest{}

	resp, err := bc.authClient.GetOAuthConfig(ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to get OAuth config: %w", err)
	}

	return resp.Enabled, nil
}

func (bc *BackendClient) GetOAuthConfig() (map[string]interface{}, error) {
	if bc.authClient == nil {
		return nil, fmt.Errorf("backend client not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &authpb.GetOAuthConfigRequest{}

	resp, err := bc.authClient.GetOAuthConfig(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth config: %w", err)
	}

	config := map[string]interface{}{
		"enabled":              resp.Enabled,
		"disable_classic_auth": resp.DisableClassicAuth,
		"providers":            make([]map[string]interface{}, 0),
	}

	for _, provider := range resp.Providers {
		config["providers"] = append(config["providers"].([]map[string]interface{}), map[string]interface{}{
			"name":         provider.Name,
			"display_name": provider.DisplayName,
			"enabled":      provider.Enabled,
		})
	}

	return config, nil
}