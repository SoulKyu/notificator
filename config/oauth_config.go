package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type OAuthPortalConfig struct {
	Enabled            bool                       `json:"enabled" mapstructure:"enabled"`
	DisableClassicAuth bool                       `json:"disable_classic_auth" mapstructure:"disable_classic_auth"`
	RedirectURL        string                     `json:"redirect_url" mapstructure:"redirect_url"`
	SessionKey         string                     `json:"session_key" mapstructure:"session_key"`
	Debug              bool                       `json:"debug" mapstructure:"debug"`
	LogLevel           string                     `json:"log_level" mapstructure:"log_level"`
	Providers          map[string]OAuthProvider   `json:"providers" mapstructure:"providers"`
	GroupSync          GroupSyncConfig            `json:"group_sync" mapstructure:"group_sync"`
	Security           OAuthSecurityConfig        `json:"security" mapstructure:"security"`
}

type OAuthProvider struct {
	ClientID        string            `json:"client_id" mapstructure:"client_id"`
	ClientSecret    string            `json:"client_secret" mapstructure:"client_secret"`
	Scopes          []string          `json:"scopes" mapstructure:"scopes"`
	AuthURL         string            `json:"auth_url" mapstructure:"auth_url"`
	TokenURL        string            `json:"token_url" mapstructure:"token_url"`
	UserInfoURL     string            `json:"user_info_url" mapstructure:"user_info_url"`
	GroupsURL       string            `json:"groups_url" mapstructure:"groups_url"`
	GroupScopes     []string          `json:"group_scopes" mapstructure:"group_scopes"`
	GroupMapping    map[string]string `json:"group_mapping" mapstructure:"group_mapping"`
	CustomClaims    map[string]string `json:"custom_claims" mapstructure:"custom_claims"`
	GroupPatterns   map[string]string `json:"group_patterns" mapstructure:"group_patterns"`
	Enabled         bool              `json:"enabled" mapstructure:"enabled"`
}

type GroupSyncConfig struct {
	Enabled        bool          `json:"enabled" mapstructure:"enabled"`
	SyncOnLogin    bool          `json:"sync_on_login" mapstructure:"sync_on_login"`
	CacheTimeout   time.Duration `json:"cache_timeout" mapstructure:"cache_timeout"`
	DefaultRole    string        `json:"default_role" mapstructure:"default_role"`
	ValidateGroups bool          `json:"validate_groups" mapstructure:"validate_groups"`
	AuditChanges   bool          `json:"audit_changes" mapstructure:"audit_changes"`
}

type OAuthSecurityConfig struct {
	StateTimeout       time.Duration `json:"state_timeout" mapstructure:"state_timeout"`
	MaxAuthAttempts    int           `json:"max_auth_attempts" mapstructure:"max_auth_attempts"`
	RateLimit          string        `json:"rate_limit" mapstructure:"rate_limit"`
	RequireHTTPS       bool          `json:"require_https" mapstructure:"require_https"`
	ValidateIssuer     bool          `json:"validate_issuer" mapstructure:"validate_issuer"`
	TokenEncryption    bool          `json:"token_encryption" mapstructure:"token_encryption"`
	CSRFProtection     bool          `json:"csrf_protection" mapstructure:"csrf_protection"`
}

func DefaultOAuthConfig() *OAuthPortalConfig {
	return &OAuthPortalConfig{
		Enabled:            false,
		DisableClassicAuth: true,
		RedirectURL:        "https://localhost:3000/api/v1/oauth",
		SessionKey:         "oauth-session-key-change-me-in-production",
		Debug:              false,
		LogLevel:           "info",
		Providers:          make(map[string]OAuthProvider),
		GroupSync: GroupSyncConfig{
			Enabled:        true,
			SyncOnLogin:    true,
			CacheTimeout:   time.Hour,
			DefaultRole:    "viewer",
			ValidateGroups: true,
			AuditChanges:   false,
		},
		Security: OAuthSecurityConfig{
			StateTimeout:       10 * time.Minute,
			MaxAuthAttempts:    5,
			RateLimit:          "10/minute",
			RequireHTTPS:       true,
			ValidateIssuer:     true,
			TokenEncryption:    true,
			CSRFProtection:     true,
		},
	}
}

func LoadOAuthConfig() (*OAuthPortalConfig, error) {
	cfg := DefaultOAuthConfig()

	// Set up Viper defaults
	setOAuthViperDefaults(cfg)

	// Bind environment variables
	bindOAuthEnvironmentVariables()

	// Load OAuth configuration
	if err := viper.UnmarshalKey("oauth", cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OAuth config: %w", err)
	}

	// Load providers from environment variables
	if err := loadOAuthProvidersFromEnv(cfg); err != nil {
		return nil, fmt.Errorf("failed to load OAuth providers from environment: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("OAuth config validation failed: %w", err)
	}

	return cfg, nil
}

func setOAuthViperDefaults(cfg *OAuthPortalConfig) {
	viper.SetDefault("oauth.enabled", cfg.Enabled)
	viper.SetDefault("oauth.disable_classic_auth", cfg.DisableClassicAuth)
	viper.SetDefault("oauth.redirect_url", cfg.RedirectURL)
	viper.SetDefault("oauth.session_key", cfg.SessionKey)
	viper.SetDefault("oauth.debug", cfg.Debug)
	viper.SetDefault("oauth.log_level", cfg.LogLevel)

	// Group sync defaults
	viper.SetDefault("oauth.group_sync.enabled", cfg.GroupSync.Enabled)
	viper.SetDefault("oauth.group_sync.sync_on_login", cfg.GroupSync.SyncOnLogin)
	viper.SetDefault("oauth.group_sync.cache_timeout", cfg.GroupSync.CacheTimeout)
	viper.SetDefault("oauth.group_sync.default_role", cfg.GroupSync.DefaultRole)
	viper.SetDefault("oauth.group_sync.validate_groups", cfg.GroupSync.ValidateGroups)
	viper.SetDefault("oauth.group_sync.audit_changes", cfg.GroupSync.AuditChanges)

	// Security defaults
	viper.SetDefault("oauth.security.state_timeout", cfg.Security.StateTimeout)
	viper.SetDefault("oauth.security.max_auth_attempts", cfg.Security.MaxAuthAttempts)
	viper.SetDefault("oauth.security.rate_limit", cfg.Security.RateLimit)
	viper.SetDefault("oauth.security.require_https", cfg.Security.RequireHTTPS)
	viper.SetDefault("oauth.security.validate_issuer", cfg.Security.ValidateIssuer)
	viper.SetDefault("oauth.security.token_encryption", cfg.Security.TokenEncryption)
	viper.SetDefault("oauth.security.csrf_protection", cfg.Security.CSRFProtection)
}

func bindOAuthEnvironmentVariables() {
	// Main OAuth settings
	viper.BindEnv("oauth.enabled", "OAUTH_ENABLED")
	viper.BindEnv("oauth.disable_classic_auth", "OAUTH_DISABLE_CLASSIC_AUTH")
	viper.BindEnv("oauth.redirect_url", "OAUTH_REDIRECT_URL")
	viper.BindEnv("oauth.session_key", "OAUTH_SESSION_KEY")
	viper.BindEnv("oauth.debug", "OAUTH_DEBUG")
	viper.BindEnv("oauth.log_level", "OAUTH_LOG_LEVEL")

	// Group sync settings
	viper.BindEnv("oauth.group_sync.enabled", "OAUTH_GROUP_SYNC_ENABLED")
	viper.BindEnv("oauth.group_sync.sync_on_login", "OAUTH_GROUP_SYNC_ON_LOGIN")
	viper.BindEnv("oauth.group_sync.cache_timeout", "OAUTH_GROUP_CACHE_TIMEOUT")
	viper.BindEnv("oauth.group_sync.default_role", "OAUTH_DEFAULT_ROLE")
	viper.BindEnv("oauth.group_sync.validate_groups", "OAUTH_VALIDATE_GROUPS")
	viper.BindEnv("oauth.group_sync.audit_changes", "OAUTH_AUDIT_CHANGES")

	// Security settings
	viper.BindEnv("oauth.security.state_timeout", "OAUTH_STATE_TIMEOUT")
	viper.BindEnv("oauth.security.max_auth_attempts", "OAUTH_MAX_AUTH_ATTEMPTS")
	viper.BindEnv("oauth.security.rate_limit", "OAUTH_RATE_LIMIT")
	viper.BindEnv("oauth.security.require_https", "OAUTH_REQUIRE_HTTPS")
	viper.BindEnv("oauth.security.validate_issuer", "OAUTH_VALIDATE_ISSUER")
	viper.BindEnv("oauth.security.token_encryption", "OAUTH_TOKEN_ENCRYPTION")
	viper.BindEnv("oauth.security.csrf_protection", "OAUTH_CSRF_PROTECTION")
}

func loadOAuthProvidersFromEnv(cfg *OAuthPortalConfig) error {
	// Common providers to check
	providers := []string{"github", "google", "microsoft", "okta"}

	for _, provider := range providers {
		envPrefix := strings.ToUpper(provider)
		
		clientID := os.Getenv(fmt.Sprintf("OAUTH_%s_CLIENT_ID", envPrefix))
		clientSecret := os.Getenv(fmt.Sprintf("OAUTH_%s_CLIENT_SECRET", envPrefix))
		
		if clientID != "" && clientSecret != "" {
			providerConfig := getDefaultProviderConfig(provider)
			providerConfig.ClientID = clientID
			providerConfig.ClientSecret = clientSecret
			providerConfig.Enabled = true

			// Load scopes if provided
			if scopes := os.Getenv(fmt.Sprintf("OAUTH_%s_SCOPES", envPrefix)); scopes != "" {
				providerConfig.Scopes = strings.Split(scopes, ",")
			}

			// Load custom URLs if provided
			if authURL := os.Getenv(fmt.Sprintf("OAUTH_%s_AUTH_URL", envPrefix)); authURL != "" {
				providerConfig.AuthURL = authURL
			}
			if tokenURL := os.Getenv(fmt.Sprintf("OAUTH_%s_TOKEN_URL", envPrefix)); tokenURL != "" {
				providerConfig.TokenURL = tokenURL
			}
			if userInfoURL := os.Getenv(fmt.Sprintf("OAUTH_%s_USER_INFO_URL", envPrefix)); userInfoURL != "" {
				providerConfig.UserInfoURL = userInfoURL
			}
			if groupsURL := os.Getenv(fmt.Sprintf("OAUTH_%s_GROUPS_URL", envPrefix)); groupsURL != "" {
				providerConfig.GroupsURL = groupsURL
			}

			cfg.Providers[provider] = providerConfig
		}
	}

	return nil
}

func getDefaultProviderConfig(provider string) OAuthProvider {
	switch provider {
	case "github":
		return OAuthProvider{
			Scopes:      []string{"user:email", "read:org", "read:user"},
			AuthURL:     "https://github.com/login/oauth/authorize",
			TokenURL:    "https://github.com/login/oauth/access_token",
			UserInfoURL: "https://api.github.com/user",
			GroupsURL:   "https://api.github.com/user/orgs",
			GroupScopes: []string{"read:org"},
			GroupMapping: map[string]string{
				"owner":      "administrator",
				"admin":      "administrator",
				"maintainer": "editor",
				"member":     "viewer",
			},
		}
	case "google":
		return OAuthProvider{
			Scopes: []string{
				"openid",
				"email",
				"profile",
				"https://www.googleapis.com/auth/admin.directory.group.readonly",
			},
			AuthURL:     "https://accounts.google.com/o/oauth2/auth",
			TokenURL:    "https://oauth2.googleapis.com/token",
			UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
			GroupsURL:   "https://admin.googleapis.com/admin/directory/v1/groups",
			GroupScopes: []string{"https://www.googleapis.com/auth/admin.directory.group.readonly"},
			GroupMapping: map[string]string{
				"admin":  "administrator",
				"editor": "editor",
				"viewer": "viewer",
			},
		}
	case "microsoft":
		return OAuthProvider{
			Scopes:      []string{"User.Read", "GroupMember.Read.All"},
			AuthURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			UserInfoURL: "https://graph.microsoft.com/v1.0/me",
			GroupsURL:   "https://graph.microsoft.com/v1.0/me/memberOf",
			GroupScopes: []string{"GroupMember.Read.All"},
			GroupMapping: map[string]string{
				"Administrator": "administrator",
				"Editor":       "editor",
				"Viewer":       "viewer",
			},
		}
	case "okta":
		return OAuthProvider{
			Scopes:      []string{"openid", "email", "profile", "groups"},
			AuthURL:     "", // Must be configured per Okta instance
			TokenURL:    "", // Must be configured per Okta instance
			UserInfoURL: "", // Must be configured per Okta instance
			GroupsURL:   "", // Must be configured per Okta instance
			GroupMapping: map[string]string{
				"Administrators": "administrator",
				"Editors":       "editor",
				"Users":         "viewer",
			},
		}
	default:
		return OAuthProvider{
			Scopes:       []string{"openid", "email", "profile"},
			GroupMapping: map[string]string{},
		}
	}
}

func (cfg *OAuthPortalConfig) Validate() error {
	if !cfg.Enabled {
		return nil
	}

	if cfg.RedirectURL == "" {
		return fmt.Errorf("redirect_url is required when OAuth is enabled")
	}

	if cfg.SessionKey == "" || cfg.SessionKey == "oauth-session-key-change-me-in-production" {
		return fmt.Errorf("session_key must be set to a secure value")
	}

	if len(cfg.Providers) == 0 {
		return fmt.Errorf("at least one OAuth provider must be configured")
	}

	if cfg.Enabled && !cfg.DisableClassicAuth {
		log.Printf("⚠️  WARNING: OAuth is enabled but classic authentication is still allowed. Consider setting OAUTH_DISABLE_CLASSIC_AUTH=true for better security.")
	}

	// Validate each provider
	for name, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}

		if err := provider.Validate(name); err != nil {
			return fmt.Errorf("provider %s validation failed: %w", name, err)
		}
	}

	// Validate security settings
	if cfg.Security.StateTimeout < 1*time.Minute {
		return fmt.Errorf("state_timeout must be at least 1 minute")
	}

	if cfg.Security.MaxAuthAttempts < 1 {
		return fmt.Errorf("max_auth_attempts must be at least 1")
	}

	return nil
}

func (provider *OAuthProvider) Validate(name string) error {
	if provider.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}

	if provider.ClientSecret == "" {
		return fmt.Errorf("client_secret is required")
	}

	if provider.AuthURL == "" {
		return fmt.Errorf("auth_url is required")
	}

	if provider.TokenURL == "" {
		return fmt.Errorf("token_url is required")
	}

	if provider.UserInfoURL == "" {
		return fmt.Errorf("user_info_url is required")
	}

	if len(provider.Scopes) == 0 {
		return fmt.Errorf("at least one scope is required")
	}

	return nil
}

func (cfg *OAuthPortalConfig) IsProviderEnabled(provider string) bool {
	if !cfg.Enabled {
		return false
	}

	p, exists := cfg.Providers[provider]
	return exists && p.Enabled
}

func (cfg *OAuthPortalConfig) IsClassicAuthAllowed() bool {
	if !cfg.Enabled {
		return true
	}
	return !cfg.DisableClassicAuth
}

func (cfg *OAuthPortalConfig) GetProvider(provider string) (OAuthProvider, bool) {
	p, exists := cfg.Providers[provider]
	return p, exists && p.Enabled
}

func (cfg *OAuthPortalConfig) GetEnabledProviders() []string {
	var enabled []string
	for name, provider := range cfg.Providers {
		if provider.Enabled {
			enabled = append(enabled, name)
		}
	}
	return enabled
}

func (cfg *OAuthPortalConfig) GetGroupCacheTimeout() time.Duration {
	if cfg.GroupSync.CacheTimeout == 0 {
		return time.Hour
	}
	return cfg.GroupSync.CacheTimeout
}

func (cfg *OAuthPortalConfig) ShouldSyncGroups(provider string) bool {
	if !cfg.GroupSync.Enabled {
		return false
	}

	p, exists := cfg.Providers[provider]
	return exists && p.Enabled && p.GroupsURL != ""
}

func (cfg *OAuthPortalConfig) GetDefaultRole() string {
	if cfg.GroupSync.DefaultRole == "" {
		return "viewer"
	}
	return cfg.GroupSync.DefaultRole
}

func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

func parseBool(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
}

func parseInt(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}