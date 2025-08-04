package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/microsoft"

	"notificator/config"
	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
)

type OAuthService struct {
	db         *database.GormDB
	config     *config.OAuthPortalConfig
	clients    map[string]*oauth2.Config
	httpClient *http.Client
}

func (s *OAuthService) GetConfig() *config.OAuthPortalConfig {
	return s.config
}

type Database interface {
	CreateOAuthUser(provider, oauthID string, userInfo *models.OAuthUserInfo) (*models.User, error)
	UpdateOAuthUser(userID string, userInfo *models.OAuthUserInfo) (*models.User, error)
	GetUserByOAuthID(provider, oauthID string) (*models.User, error)
	LinkOAuthToExistingUser(userID, provider, oauthID string, userInfo *models.OAuthUserInfo) error

	SyncUserGroups(userID, provider string, groups []models.OAuthGroupInfo) error
	GetUserGroups(userID string) ([]models.UserGroup, error)
	GetUserGroupsByProvider(userID, provider string) ([]models.UserGroup, error)

	StoreOAuthToken(userID, provider string, accessToken, refreshToken, tokenType string, expiresAt *time.Time, scopes []string) error
	GetOAuthToken(userID, provider string) (*models.OAuthToken, error)
	RefreshOAuthToken(userID, provider, accessToken, refreshToken string, expiresAt *time.Time) error
	DeleteOAuthToken(userID, provider string) error

	CreateOAuthState(provider, state, sessionID string, expiresAt time.Time) error
	ValidateAndDeleteOAuthState(provider, state string) (*models.OAuthState, error)

	CreateOAuthSession(provider, state, redirectURI string, scopes []string, expiresAt time.Time) (*models.OAuthSession, error)
	GetOAuthSession(state string) (*models.OAuthSession, error)
	UpdateOAuthSession(sessionID, userID string) error

	StoreGroupCache(userID, provider string, groups []models.OAuthGroupInfo, expiresAt time.Time) error
	GetGroupCache(userID, provider string) (*models.OAuthGroupCache, error)

	LogOAuthActivity(userID *string, provider, action string, success bool, errorMsg, ipAddress, userAgent string, metadata map[string]interface{}) error
}

func NewOAuthService(db *database.GormDB, cfg *config.OAuthPortalConfig) (*OAuthService, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("OAuth is not enabled")
	}

	service := &OAuthService{
		db:         db,
		config:     cfg,
		clients:    make(map[string]*oauth2.Config),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	if err := service.initializeProviders(); err != nil {
		return nil, fmt.Errorf("failed to initialize OAuth providers: %w", err)
	}

	return service, nil
}

func (s *OAuthService) initializeProviders() error {
	for name, provider := range s.config.Providers {
		if !provider.Enabled {
			continue
		}

		if err := provider.Validate(name); err != nil {
			return fmt.Errorf("provider %s validation failed: %w", name, err)
		}

		oauthConfig := &oauth2.Config{
			ClientID:     provider.ClientID,
			ClientSecret: provider.ClientSecret,
			Scopes:       provider.Scopes,
			Endpoint:     s.getOAuthEndpoint(name, provider),
			RedirectURL:  fmt.Sprintf("%s/%s/callback", s.config.RedirectURL, name),
		}

		s.clients[name] = oauthConfig
		log.Printf("✅ Initialized OAuth provider: %s", name)
	}

	if len(s.clients) == 0 {
		return fmt.Errorf("no OAuth providers are enabled")
	}

	return nil
}

func (s *OAuthService) getOAuthEndpoint(name string, provider config.OAuthProvider) oauth2.Endpoint {
	switch name {
	case "github":
		return github.Endpoint
	case "google":
		return google.Endpoint
	case "microsoft":
		return microsoft.AzureADEndpoint("")
	default:
		return oauth2.Endpoint{
			AuthURL:  provider.AuthURL,
			TokenURL: provider.TokenURL,
		}
	}
}

func (s *OAuthService) GetAuthURL(provider, state string) (string, error) {
	client, exists := s.clients[provider]
	if !exists {
		return "", fmt.Errorf("provider %s not configured", provider)
	}

	expiresAt := time.Now().Add(s.config.Security.StateTimeout)
	if err := s.db.CreateOAuthState(provider, state, "", expiresAt); err != nil {
		return "", fmt.Errorf("failed to create OAuth state: %w", err)
	}

	authURL := client.AuthCodeURL(state, oauth2.AccessTypeOffline)

	log.Printf("📝 Generated OAuth URL for provider %s", provider)
	return authURL, nil
}

func (s *OAuthService) ExchangeCodeForToken(provider, code, state string) (*oauth2.Token, error) {
	_, err := s.db.ValidateAndDeleteOAuthState(provider, state)
	if err != nil {
		return nil, fmt.Errorf("invalid OAuth state: %w", err)
	}

	client, exists := s.clients[provider]
	if !exists {
		return nil, fmt.Errorf("provider %s not configured", provider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := client.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}

	log.Printf("✅ Successfully exchanged code for token (provider: %s)", provider)
	return token, nil
}

func (s *OAuthService) GetUserInfo(provider string, token *oauth2.Token) (*models.OAuthUserInfo, error) {
	providerConfig, exists := s.config.Providers[provider]
	if !exists {
		return nil, fmt.Errorf("provider %s not configured", provider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := s.clients[provider].Client(ctx, token)

	resp, err := client.Get(providerConfig.UserInfoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read user info response: %w", err)
	}

	userInfo, err := s.parseUserInfo(provider, body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}

	if s.config.ShouldSyncGroups(provider) {
		groups, err := s.getUserGroups(provider, token, client)
		if err != nil {
			log.Printf("⚠️ Failed to get user groups for %s: %v", provider, err)
		} else {
			userInfo.Groups = groups
		}
	}

	userInfo.Provider = provider

	log.Printf("✅ Retrieved user info for %s (provider: %s)", userInfo.Username, provider)
	return userInfo, nil
}

func (s *OAuthService) parseUserInfo(provider string, body []byte) (*models.OAuthUserInfo, error) {
	switch provider {
	case "github":
		return s.parseGitHubUserInfo(body)
	case "google":
		return s.parseGoogleUserInfo(body)
	case "microsoft":
		return s.parseMicrosoftUserInfo(body)
	default:
		return s.parseGenericUserInfo(body)
	}
}

func (s *OAuthService) parseGitHubUserInfo(body []byte) (*models.OAuthUserInfo, error) {
	var githubUser struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}

	if err := json.Unmarshal(body, &githubUser); err != nil {
		return nil, err
	}

	return &models.OAuthUserInfo{
		ID:            fmt.Sprintf("%d", githubUser.ID),
		Username:      githubUser.Login,
		Name:          githubUser.Name,
		Email:         githubUser.Email,
		AvatarURL:     githubUser.AvatarURL,
		EmailVerified: githubUser.Email != "",
	}, nil
}

func (s *OAuthService) parseGoogleUserInfo(body []byte) (*models.OAuthUserInfo, error) {
	var googleUser struct {
		Sub           string `json:"sub"`
		Name          string `json:"name"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Picture       string `json:"picture"`
	}

	if err := json.Unmarshal(body, &googleUser); err != nil {
		return nil, err
	}

	username := googleUser.Email
	if idx := strings.Index(username, "@"); idx != -1 {
		username = username[:idx]
	}

	return &models.OAuthUserInfo{
		ID:            googleUser.Sub,
		Username:      username,
		Name:          googleUser.Name,
		Email:         googleUser.Email,
		AvatarURL:     googleUser.Picture,
		EmailVerified: googleUser.EmailVerified,
	}, nil
}

func (s *OAuthService) parseMicrosoftUserInfo(body []byte) (*models.OAuthUserInfo, error) {
	var msUser struct {
		ID                string `json:"id"`
		UserPrincipalName string `json:"userPrincipalName"`
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
	}

	if err := json.Unmarshal(body, &msUser); err != nil {
		return nil, err
	}

	email := msUser.Mail
	if email == "" {
		email = msUser.UserPrincipalName
	}

	username := email
	if idx := strings.Index(username, "@"); idx != -1 {
		username = username[:idx]
	}

	return &models.OAuthUserInfo{
		ID:            msUser.ID,
		Username:      username,
		Name:          msUser.DisplayName,
		Email:         email,
		EmailVerified: true,
	}, nil
}

func (s *OAuthService) parseGenericUserInfo(body []byte) (*models.OAuthUserInfo, error) {
	var genericUser map[string]interface{}
	if err := json.Unmarshal(body, &genericUser); err != nil {
		return nil, err
	}

	userInfo := &models.OAuthUserInfo{
		CustomClaims: genericUser,
	}

	if id, ok := genericUser["id"].(string); ok {
		userInfo.ID = id
	} else if sub, ok := genericUser["sub"].(string); ok {
		userInfo.ID = sub
	}

	if username, ok := genericUser["username"].(string); ok {
		userInfo.Username = username
	} else if login, ok := genericUser["login"].(string); ok {
		userInfo.Username = login
	} else if email, ok := genericUser["email"].(string); ok {
		if idx := strings.Index(email, "@"); idx != -1 {
			userInfo.Username = email[:idx]
		}
	}

	if name, ok := genericUser["name"].(string); ok {
		userInfo.Name = name
	} else if displayName, ok := genericUser["display_name"].(string); ok {
		userInfo.Name = displayName
	}

	if email, ok := genericUser["email"].(string); ok {
		userInfo.Email = email
	}

	if emailVerified, ok := genericUser["email_verified"].(bool); ok {
		userInfo.EmailVerified = emailVerified
	}

	if avatarURL, ok := genericUser["avatar_url"].(string); ok {
		userInfo.AvatarURL = avatarURL
	} else if picture, ok := genericUser["picture"].(string); ok {
		userInfo.AvatarURL = picture
	}

	return userInfo, nil
}

func (s *OAuthService) getUserGroups(provider string, token *oauth2.Token, client *http.Client) ([]models.OAuthGroupInfo, error) {
	providerConfig := s.config.Providers[provider]
	if providerConfig.GroupsURL == "" {
		return nil, fmt.Errorf("groups URL not configured for provider %s", provider)
	}

	switch provider {
	case "github":
		return s.getGitHubGroups(token, client)
	case "google":
		return s.getGoogleGroups(token, client)
	case "microsoft":
		return s.getMicrosoftGroups(token, client)
	default:
		return s.getGenericGroups(provider, token, client)
	}
}

func (s *OAuthService) getGitHubGroups(token *oauth2.Token, client *http.Client) ([]models.OAuthGroupInfo, error) {
	resp, err := client.Get("https://api.github.com/user/orgs")
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub organizations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub orgs request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GitHub orgs response: %w", err)
	}

	var orgs []struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		AvatarURL string `json:"avatar_url"`
	}

	if err := json.Unmarshal(body, &orgs); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub orgs: %w", err)
	}

	var groups []models.OAuthGroupInfo
	for _, org := range orgs {
		groups = append(groups, models.OAuthGroupInfo{
			ID:        fmt.Sprintf("%d", org.ID),
			Name:      org.Login,
			Type:      "organization",
			Role:      org.Role,
			AvatarURL: org.AvatarURL,
		})
	}

	log.Printf("✅ Retrieved %d GitHub organizations", len(groups))
	return groups, nil
}

func (s *OAuthService) getGoogleGroups(token *oauth2.Token, client *http.Client) ([]models.OAuthGroupInfo, error) {
	resp, err := client.Get("https://admin.googleapis.com/admin/directory/v1/groups?domain=example.com")
	if err != nil {
		return nil, fmt.Errorf("failed to get Google groups: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google groups request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Google groups response: %w", err)
	}

	var groupsResp struct {
		Groups []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"groups"`
	}

	if err := json.Unmarshal(body, &groupsResp); err != nil {
		return nil, fmt.Errorf("failed to parse Google groups: %w", err)
	}

	var groups []models.OAuthGroupInfo
	for _, group := range groupsResp.Groups {
		groups = append(groups, models.OAuthGroupInfo{
			ID:   group.ID,
			Name: group.Email,
			Type: "group",
		})
	}

	log.Printf("✅ Retrieved %d Google groups", len(groups))
	return groups, nil
}

func (s *OAuthService) getMicrosoftGroups(token *oauth2.Token, client *http.Client) ([]models.OAuthGroupInfo, error) {
	resp, err := client.Get("https://graph.microsoft.com/v1.0/me/memberOf")
	if err != nil {
		return nil, fmt.Errorf("failed to get Microsoft groups: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Microsoft groups request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Microsoft groups response: %w", err)
	}

	var groupsResp struct {
		Value []struct {
			ID          string   `json:"id"`
			DisplayName string   `json:"displayName"`
			GroupTypes  []string `json:"groupTypes"`
		} `json:"value"`
	}

	if err := json.Unmarshal(body, &groupsResp); err != nil {
		return nil, fmt.Errorf("failed to parse Microsoft groups: %w", err)
	}

	var groups []models.OAuthGroupInfo
	for _, group := range groupsResp.Value {
		groupType := "group"
		if len(group.GroupTypes) > 0 {
			groupType = group.GroupTypes[0]
		}

		groups = append(groups, models.OAuthGroupInfo{
			ID:   group.ID,
			Name: group.DisplayName,
			Type: groupType,
		})
	}

	log.Printf("✅ Retrieved %d Microsoft groups", len(groups))
	return groups, nil
}

func (s *OAuthService) getGenericGroups(provider string, token *oauth2.Token, client *http.Client) ([]models.OAuthGroupInfo, error) {
	providerConfig := s.config.Providers[provider]

	resp, err := client.Get(providerConfig.GroupsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get groups from %s: %w", provider, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("groups request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read groups response: %w", err)
	}

	var groups []models.OAuthGroupInfo
	if err := json.Unmarshal(body, &groups); err != nil {
		var groupNames []string
		if err2 := json.Unmarshal(body, &groupNames); err2 != nil {
			return nil, fmt.Errorf("failed to parse groups: %w", err)
		}

		for _, name := range groupNames {
			groups = append(groups, models.OAuthGroupInfo{
				Name: name,
				Type: "group",
			})
		}
	}

	log.Printf("✅ Retrieved %d groups from %s", len(groups), provider)
	return groups, nil
}

func (s *OAuthService) CreateOrUpdateOAuthUser(provider string, userInfo *models.OAuthUserInfo) (*models.User, error) {
	existingUser, err := s.db.GetUserByOAuthID(provider, userInfo.ID)
	if err == nil {
		user, err := s.db.UpdateOAuthUser(existingUser.ID, userInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to update existing OAuth user: %w", err)
		}

		if s.config.ShouldSyncGroups(provider) && len(userInfo.Groups) > 0 {
			if err := s.db.SyncUserGroups(user.ID, provider, userInfo.Groups); err != nil {
				log.Printf("⚠️ Failed to sync groups for user %s: %v", user.ID, err)
			}
		}

		log.Printf("✅ Updated existing OAuth user: %s", user.Username)
		return user, nil
	}

	user, err := s.db.CreateOAuthUser(provider, userInfo.ID, userInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create OAuth user: %w", err)
	}

	log.Printf("✅ Created new OAuth user: %s", user.Username)
	return user, nil
}

func (s *OAuthService) LogActivity(userID *string, provider, action string, success bool, errorMsg, ipAddress, userAgent string, metadata map[string]interface{}) {
	if err := s.db.LogOAuthActivity(userID, provider, action, success, errorMsg, ipAddress, userAgent, metadata); err != nil {
		log.Printf("⚠️ Failed to log OAuth activity: %v", err)
	}
}

func (s *OAuthService) GetEnabledProviders() []string {
	return s.config.GetEnabledProviders()
}

func (s *OAuthService) IsProviderEnabled(provider string) bool {
	return s.config.IsProviderEnabled(provider)
}

func (s *OAuthService) GetProviderConfig(provider string) (config.OAuthProvider, bool) {
	return s.config.GetProvider(provider)
}

func (s *OAuthService) ValidateConfiguration() error {
	return s.config.Validate()
}

func (s *OAuthService) Cleanup() error {
	log.Println("🧹 Running OAuth cleanup...")

	log.Println("✅ OAuth cleanup completed")
	return nil
}
