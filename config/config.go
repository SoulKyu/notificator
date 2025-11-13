package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Alertmanagers  []AlertmanagerConfig `json:"alertmanagers"`
	GUI            GUIConfig            `json:"gui"`
	Notifications  NotificationConfig   `json:"notifications"`
	Polling        PollingConfig        `json:"polling"`
	ColumnWidths   map[string]float32   `json:"column_widths"`
	Backend        BackendConfig        `json:"backend"`
	ResolvedAlerts ResolvedAlertsConfig `json:"resolved_alerts"`
	Statistics     StatisticsConfig     `json:"statistics"`
	WebUI          WebUIConfig          `json:"webui"`
	OAuth          *OAuthPortalConfig   `json:"oauth,omitempty"`
	Sentry         *SentryConfig        `json:"sentry,omitempty"`
}

type BackendConfig struct {
	Enabled    bool           `json:"enabled"`
	GRPCListen string         `json:"grpc_listen"` // Port for gRPC server (e.g., ":50051")
	GRPCClient string         `json:"grpc_client"` // Address for gRPC client (e.g., "localhost:50051")
	HTTPListen string         `json:"http_listen"` // Port for HTTP server (e.g., ":8080")
	Database   DatabaseConfig `json:"database"`
}

type DatabaseConfig struct {
	Type       string `json:"type"` // "sqlite" or "postgres"
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Name       string `json:"name"`
	User       string `json:"user"`
	Password   string `json:"password"`
	SSLMode    string `json:"ssl_mode"`
	SQLitePath string `json:"sqlite_path"`
}

type ResolvedAlertsConfig struct {
	Enabled              bool `json:"enabled"`               // Enable resolved alerts tracking
	NotificationsEnabled bool `json:"notifications_enabled"` // Send notifications for resolved alerts
	RetentionDays        int  `json:"retention_days"`        // How many days to keep resolved alerts (default: 90)
}

type StatisticsConfig struct {
	RetentionDays int `json:"retention_days"` // How many days to keep alert statistics (default: 90)
}

type AlertmanagerConfig struct {
	Name     string            `json:"name"`
	URL      string            `json:"url"`
	Username string            `json:"username"`
	Password string            `json:"password"`
	Token    string            `json:"token"`
	Headers  map[string]string `json:"headers"`
	OAuth    *OAuthConfig      `json:"oauth,omitempty"`
}

type OAuthConfig struct {
	Enabled   bool `json:"enabled"`
	ProxyMode bool `json:"proxy_mode"` // True for OAuth proxy authentication
}

type GUIConfig struct {
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	Title          string            `json:"title"`
	FilterState    FilterStateConfig `json:"filter_state"`
	MinimizeToTray bool              `json:"minimize_to_tray"`
	StartMinimized bool              `json:"start_minimized"`
	ShowTrayIcon   bool              `json:"show_tray_icon"`
	BackgroundMode bool              `json:"background_mode"`
}

type FilterStateConfig struct {
	SearchText            string          `json:"search_text"`
	SelectedAlertmanagers map[string]bool `json:"selected_alertmanagers"`
	SelectedSeverities    map[string]bool `json:"selected_severities"`
	SelectedStatuses      map[string]bool `json:"selected_statuses"`
	SelectedTeams         map[string]bool `json:"selected_teams"`
	SelectedAcks          map[string]bool `json:"selected_acks"`
	SelectedComments      map[string]bool `json:"selected_comments"`
}

type NotificationConfig struct {
	Enabled           bool            `json:"enabled"`
	SoundEnabled      bool            `json:"sound_enabled"`
	SoundPath         string          `json:"sound_path"`
	AudioOutputDevice string          `json:"audio_output_device"`
	ShowSystem        bool            `json:"show_system"`
	CriticalOnly      bool            `json:"critical_only"`
	MaxNotifications  int             `json:"max_notifications"`
	CooldownSeconds   int             `json:"cooldown_seconds"`
	SeverityRules     map[string]bool `json:"severity_rules"`
	RespectFilters    bool            `json:"respect_filters"`
}

type PollingConfig struct {
	Interval time.Duration `json:"interval"`
}

type WebUIConfig struct {
	Playground bool `json:"playground"`
}

type SentryConfig struct {
	Enabled     bool   `json:"enabled"`
	BaseURL     string `json:"base_url"`     // Default Sentry instance URL (e.g., "https://sentry.io")
	GlobalToken string `json:"global_token"` // Admin-configured fallback token
}

func DefaultConfig() *Config {
	headers := make(map[string]string)

	oauthConfig := &OAuthConfig{
		Enabled:   false,
		ProxyMode: true,
	}

	return &Config{
		Alertmanagers: []AlertmanagerConfig{
			{
				Name:    "Default",
				URL:     "http://localhost:9093",
				Headers: headers,
				OAuth:   oauthConfig,
			},
		},
		GUI: GUIConfig{
			Width:          1920,
			Height:         1080,
			Title:          "Notificator - Alert Dashboard",
			MinimizeToTray: true,
			StartMinimized: false,
			ShowTrayIcon:   true,
			BackgroundMode: false,
			FilterState: FilterStateConfig{
				SearchText:            "",
				SelectedAlertmanagers: map[string]bool{"All": true},
				SelectedSeverities:    map[string]bool{"All": true},
				SelectedStatuses:      map[string]bool{"All": true},
				SelectedTeams:         map[string]bool{"All": true},
				SelectedAcks:          map[string]bool{"All": true},
				SelectedComments:      map[string]bool{"All": true},
			},
		},
		Notifications: NotificationConfig{
			Enabled:           true,
			SoundEnabled:      true,
			SoundPath:         getDefaultSoundPath(),
			AudioOutputDevice: "default",
			ShowSystem:        true,
			CriticalOnly:      false,
			MaxNotifications:  5,
			CooldownSeconds:   300, // 5 minutes
			SeverityRules: map[string]bool{
				"critical": true,
				"warning":  true,
				"info":     false,
				"unknown":  false,
			},
			RespectFilters: true,
		},
		Polling: PollingConfig{
			Interval: 30 * time.Second,
		},
		Backend: BackendConfig{
			Enabled:    false,
			GRPCListen: ":50051",
			GRPCClient: "localhost:50051",
			HTTPListen: ":8080",
			Database: DatabaseConfig{
				Type:       "sqlite",
				SQLitePath: "./notificator.db",
				Host:       "localhost",
				Port:       5432,
				Name:       "notificator",
				User:       "notificator",
				Password:   "",
				SSLMode:    "disable",
			},
		},
		ResolvedAlerts: ResolvedAlertsConfig{
			Enabled:              true, // Enable by default
			NotificationsEnabled: true, // Send notifications by default
			RetentionDays:        90,   // Keep for 90 days by default
		},
		Statistics: StatisticsConfig{
			RetentionDays: 90, // Keep alert statistics for 90 days by default
		},
		WebUI: WebUIConfig{
			Playground: false, // Playground mode disabled by default
		},

		// OAuth is disabled by default - must be explicitly configured
		OAuth: nil,
	}
}

func getDefaultSoundPath() string {
	return ""
}

func (c *Config) SaveToFile(configPath string) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func GetConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "notificator.json"
	}

	return filepath.Join(home, ".config", "notificator", "config.json")
}

func LoadConfigWithViper() (*Config, error) {
	// Debug: Check if config file is loaded

	cfg := DefaultConfig()
	setViperDefaults(cfg)

	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	alertmanagers := []AlertmanagerConfig{}
	for i := 0; i < 10; i++ { // Support up to 10 alertmanagers
		prefix := fmt.Sprintf("alertmanagers.%d", i)
		url := viper.GetString(prefix + ".url")
		if url != "" {
			am := AlertmanagerConfig{
				Name:     viper.GetString(prefix + ".name"),
				URL:      url,
				Username: viper.GetString(prefix + ".username"),
				Password: viper.GetString(prefix + ".password"),
				Token:    viper.GetString(prefix + ".token"),
				Headers:  make(map[string]string),
			}

			// Handle OAuth config
			if viper.IsSet(prefix + ".oauth.enabled") {
				am.OAuth = &OAuthConfig{
					Enabled:   viper.GetBool(prefix + ".oauth.enabled"),
					ProxyMode: viper.GetBool(prefix + ".oauth.proxy_mode"),
				}
			}

			alertmanagers = append(alertmanagers, am)
		}
	}

	if len(alertmanagers) > 0 {
		cfg.Alertmanagers = alertmanagers

	}

	if cfg.Notifications.SeverityRules == nil {
		cfg.Notifications.SeverityRules = map[string]bool{
			"critical": true,
			"warning":  true,
			"info":     false,
			"unknown":  false,
		}
	}

	initializeFilterStates(cfg)

	// Load Sentry configuration if enabled
	if viper.GetBool("sentry.enabled") {
		cfg.Sentry = &SentryConfig{
			Enabled:     true,
			BaseURL:     viper.GetString("sentry.base_url"),
			GlobalToken: viper.GetString("sentry.global_token"),
		}
		log.Printf("DEBUG: Sentry config loaded - enabled: %v, base_url: %v", cfg.Sentry.Enabled, cfg.Sentry.BaseURL)
	}

	oauthEnabled := viper.GetBool("oauth.enabled")
	log.Printf("DEBUG: OAuth enabled check: %v", oauthEnabled)

	// Check if we have OAuth environment variables (indicates this is a backend/OAuth-enabled service)
	hasOAuthEnvVars := viper.IsSet("oauth.session_key") ||
		os.Getenv("OAUTH_GITHUB_CLIENT_ID") != "" ||
		os.Getenv("OAUTH_GOOGLE_CLIENT_ID") != "" ||
		os.Getenv("OAUTH_MICROSOFT_CLIENT_ID") != ""

	log.Printf("DEBUG: Has OAuth env vars: %v", hasOAuthEnvVars)

	if oauthEnabled && hasOAuthEnvVars {
		log.Printf("DEBUG: OAuth is enabled with providers, loading complete configuration...")
		// OAuth config should now be populated by viper.Unmarshal above
		if cfg.OAuth == nil {
			// If not unmarshaled, create with defaults
			cfg.OAuth = DefaultOAuthConfig()
		} else {
			// Even if OAuth exists, ensure Providers map is initialized
			// Viper unmarshal might create the struct but leave the map as nil
			if cfg.OAuth.Providers == nil {
				cfg.OAuth.Providers = make(map[string]OAuthProvider)
			}
		}

		// Load providers from environment variables
		if err := loadOAuthProvidersFromEnv(cfg.OAuth); err != nil {
			log.Printf("DEBUG: Failed to load OAuth providers: %v", err)
			return nil, fmt.Errorf("failed to load OAuth providers: %w", err)
		}

		// Validate configuration
		if err := cfg.OAuth.Validate(); err != nil {
			log.Printf("DEBUG: OAuth config validation failed: %v", err)
			return nil, fmt.Errorf("OAuth config validation failed: %w", err)
		}

		log.Printf("DEBUG: OAuth config loaded successfully, providers: %d", len(cfg.OAuth.Providers))
	} else if oauthEnabled && !hasOAuthEnvVars {
		log.Printf("DEBUG: OAuth enabled but no local config - will use backend for OAuth info")
		// For webui or services that don't have OAuth environment variables,
		// we still set up a minimal OAuth config so OAuth handlers can function
		// The actual OAuth configuration will be retrieved from backend at runtime
		cfg.OAuth = DefaultOAuthConfig()
		cfg.OAuth.Enabled = true // Allow OAuth handlers to be active
		log.Printf("DEBUG: Created minimal OAuth config for runtime backend lookup")
	} else if !oauthEnabled && !hasOAuthEnvVars {
		log.Printf("DEBUG: No OAuth config locally - creating minimal config for potential backend OAuth")
		// For webui that doesn't have any OAuth environment variables at all,
		// we create a minimal config that allows OAuth handlers to function
		// The handlers will determine at runtime if OAuth is available via backend
		cfg.OAuth = DefaultOAuthConfig()
		cfg.OAuth.Enabled = false // Will be determined at runtime from backend
		log.Printf("DEBUG: Created minimal OAuth config for runtime backend detection")
	} else {
		log.Printf("DEBUG: OAuth is disabled or not configured")
		cfg.OAuth = nil
	}

	return cfg, nil
}

func setViperDefaults(cfg *Config) {
	// DEBUG: Check what viper has before setting defaults

	// Backend defaults
	viper.SetDefault("backend.enabled", cfg.Backend.Enabled)
	viper.SetDefault("backend.grpc_listen", cfg.Backend.GRPCListen)
	viper.SetDefault("backend.grpc_client", cfg.Backend.GRPCClient)
	viper.SetDefault("backend.http_listen", cfg.Backend.HTTPListen)

	// Database defaults - only set if not already configured from config file or env vars
	// IMPORTANT: Don't set database.type default - let it come from config file
	// if !viper.IsSet("backend.database.type") {
	//	viper.SetDefault("backend.database.type", cfg.Backend.Database.Type)
	// }
	if !viper.IsSet("backend.database.host") {
		viper.SetDefault("backend.database.host", cfg.Backend.Database.Host)
	}
	if !viper.IsSet("backend.database.port") {
		viper.SetDefault("backend.database.port", cfg.Backend.Database.Port)
	}
	if !viper.IsSet("backend.database.name") {
		viper.SetDefault("backend.database.name", cfg.Backend.Database.Name)
	}
	if !viper.IsSet("backend.database.user") {
		viper.SetDefault("backend.database.user", cfg.Backend.Database.User)
	}
	if !viper.IsSet("backend.database.password") {
		viper.SetDefault("backend.database.password", cfg.Backend.Database.Password)
	}
	if !viper.IsSet("backend.database.ssl_mode") {
		viper.SetDefault("backend.database.ssl_mode", cfg.Backend.Database.SSLMode)
	}
	if !viper.IsSet("backend.database.sqlite_path") {
		viper.SetDefault("backend.database.sqlite_path", cfg.Backend.Database.SQLitePath)
	}

	// GUI defaults - only set if not already configured from config file or env vars
	if !viper.IsSet("gui.width") {
		viper.SetDefault("gui.width", cfg.GUI.Width)
	}
	if !viper.IsSet("gui.height") {
		viper.SetDefault("gui.height", cfg.GUI.Height)
	}
	if !viper.IsSet("gui.title") {
		viper.SetDefault("gui.title", cfg.GUI.Title)
	}
	if !viper.IsSet("gui.minimize_to_tray") {
		viper.SetDefault("gui.minimize_to_tray", cfg.GUI.MinimizeToTray)
	}
	if !viper.IsSet("gui.start_minimized") {
		viper.SetDefault("gui.start_minimized", cfg.GUI.StartMinimized)
	}
	if !viper.IsSet("gui.show_tray_icon") {
		viper.SetDefault("gui.show_tray_icon", cfg.GUI.ShowTrayIcon)
	}
	if !viper.IsSet("gui.background_mode") {
		viper.SetDefault("gui.background_mode", cfg.GUI.BackgroundMode)
	}

	// Notification defaults - only set if not already configured from config file or env vars
	if !viper.IsSet("notifications.enabled") {
		viper.SetDefault("notifications.enabled", cfg.Notifications.Enabled)
	}
	if !viper.IsSet("notifications.sound_enabled") {
		viper.SetDefault("notifications.sound_enabled", cfg.Notifications.SoundEnabled)
	}
	if !viper.IsSet("notifications.sound_path") {
		viper.SetDefault("notifications.sound_path", cfg.Notifications.SoundPath)
	}
	if !viper.IsSet("notifications.audio_output_device") {
		viper.SetDefault("notifications.audio_output_device", cfg.Notifications.AudioOutputDevice)
	}
	if !viper.IsSet("notifications.show_system") {
		viper.SetDefault("notifications.show_system", cfg.Notifications.ShowSystem)
	}
	if !viper.IsSet("notifications.critical_only") {
		viper.SetDefault("notifications.critical_only", cfg.Notifications.CriticalOnly)
	}
	if !viper.IsSet("notifications.max_notifications") {
		viper.SetDefault("notifications.max_notifications", cfg.Notifications.MaxNotifications)
	}
	if !viper.IsSet("notifications.cooldown_seconds") {
		viper.SetDefault("notifications.cooldown_seconds", cfg.Notifications.CooldownSeconds)
	}
	if !viper.IsSet("notifications.respect_filters") {
		viper.SetDefault("notifications.respect_filters", cfg.Notifications.RespectFilters)
	}

	// Polling defaults - only set if not already configured from config file or env vars
	if !viper.IsSet("polling.interval") {
		viper.SetDefault("polling.interval", cfg.Polling.Interval)
	}

	// Resolved alerts defaults - only set if not already configured from config file or env vars
	if !viper.IsSet("resolved_alerts.enabled") {
		viper.SetDefault("resolved_alerts.enabled", cfg.ResolvedAlerts.Enabled)
	}
	if !viper.IsSet("resolved_alerts.notifications_enabled") {
		viper.SetDefault("resolved_alerts.notifications_enabled", cfg.ResolvedAlerts.NotificationsEnabled)
	}
	if !viper.IsSet("resolved_alerts.retention_days") {
		viper.SetDefault("resolved_alerts.retention_days", cfg.ResolvedAlerts.RetentionDays)
	}

	// Statistics defaults
	if !viper.IsSet("statistics.retention_days") {
		viper.SetDefault("statistics.retention_days", cfg.Statistics.RetentionDays)
	}

	// WebUI defaults - only set if not already configured from config file or env vars
	if !viper.IsSet("webui.playground") {
		viper.SetDefault("webui.playground", cfg.WebUI.Playground)
	}

	// OAuth defaults - use DefaultOAuthConfig for consistent defaults
	oauthDefaults := DefaultOAuthConfig()
	viper.SetDefault("oauth.enabled", oauthDefaults.Enabled)
	viper.SetDefault("oauth.disable_classic_auth", oauthDefaults.DisableClassicAuth)
	viper.SetDefault("oauth.redirect_url", oauthDefaults.RedirectURL)
	viper.SetDefault("oauth.session_key", oauthDefaults.SessionKey)
	viper.SetDefault("oauth.debug", oauthDefaults.Debug)
	viper.SetDefault("oauth.log_level", oauthDefaults.LogLevel)

	// Sentry defaults
	viper.SetDefault("sentry.enabled", false)
	viper.SetDefault("sentry.base_url", "https://sentry.io")
	viper.SetDefault("sentry.global_token", "")

	// Group sync defaults
	viper.SetDefault("oauth.group_sync.enabled", oauthDefaults.GroupSync.Enabled)
	viper.SetDefault("oauth.group_sync.sync_on_login", oauthDefaults.GroupSync.SyncOnLogin)
	viper.SetDefault("oauth.group_sync.cache_timeout", oauthDefaults.GroupSync.CacheTimeout)
	viper.SetDefault("oauth.group_sync.default_role", oauthDefaults.GroupSync.DefaultRole)
	viper.SetDefault("oauth.group_sync.validate_groups", oauthDefaults.GroupSync.ValidateGroups)
	viper.SetDefault("oauth.group_sync.audit_changes", oauthDefaults.GroupSync.AuditChanges)

	// Security defaults
	viper.SetDefault("oauth.security.state_timeout", oauthDefaults.Security.StateTimeout)
	viper.SetDefault("oauth.security.max_auth_attempts", oauthDefaults.Security.MaxAuthAttempts)
	viper.SetDefault("oauth.security.rate_limit", oauthDefaults.Security.RateLimit)
	viper.SetDefault("oauth.security.require_https", oauthDefaults.Security.RequireHTTPS)
	viper.SetDefault("oauth.security.validate_issuer", oauthDefaults.Security.ValidateIssuer)
	viper.SetDefault("oauth.security.token_encryption", oauthDefaults.Security.TokenEncryption)
	viper.SetDefault("oauth.security.csrf_protection", oauthDefaults.Security.CSRFProtection)

	// Sentry environment variable bindings
	viper.BindEnv("sentry.enabled", "NOTIFICATOR_SENTRY_ENABLED")
	viper.BindEnv("sentry.base_url", "NOTIFICATOR_SENTRY_BASE_URL")
	viper.BindEnv("sentry.global_token", "NOTIFICATOR_SENTRY_GLOBAL_TOKEN")

	// Alertmanager defaults - DISABLED to allow JSON config to work properly
	// The alertmanager configuration should come from the config file, not defaults
	/*
		if len(cfg.Alertmanagers) > 0 {
			am := cfg.Alertmanagers[0]
			if !viper.IsSet("alertmanagers.0.name") {
				viper.SetDefault("alertmanagers.0.name", am.Name)
			}
			if !viper.IsSet("alertmanagers.0.url") {
				viper.SetDefault("alertmanagers.0.url", am.URL)
			}
			if !viper.IsSet("alertmanagers.0.username") {
				viper.SetDefault("alertmanagers.0.username", am.Username)
			}
			if !viper.IsSet("alertmanagers.0.password") {
				viper.SetDefault("alertmanagers.0.password", am.Password)
			}
			if !viper.IsSet("alertmanagers.0.token") {
				viper.SetDefault("alertmanagers.0.token", am.Token)
			}
			if am.OAuth != nil {
				if !viper.IsSet("alertmanagers.0.oauth.enabled") {
					viper.SetDefault("alertmanagers.0.oauth.enabled", am.OAuth.Enabled)
				}
				if !viper.IsSet("alertmanagers.0.oauth.proxy_mode") {
					viper.SetDefault("alertmanagers.0.oauth.proxy_mode", am.OAuth.ProxyMode)
				}
			}
		}
	*/

	// Support common environment variables
	viper.BindEnv("backend.database.host", "DB_HOST", "DATABASE_HOST")
	viper.BindEnv("backend.database.port", "DB_PORT", "DATABASE_PORT")
	viper.BindEnv("backend.database.name", "DB_NAME", "DATABASE_NAME")
	viper.BindEnv("backend.database.user", "DB_USER", "DATABASE_USER")
	viper.BindEnv("backend.database.password", "DB_PASSWORD", "DATABASE_PASSWORD")
	viper.BindEnv("backend.database.ssl_mode", "DB_SSL_MODE", "DATABASE_SSL_MODE")
	viper.BindEnv("backend.database.sqlite_path", "DB_PATH", "DATABASE_PATH")

	// Support DATABASE_URL for full connection string (POSTGRES_URL handled directly by GORM)
	viper.BindEnv("database_url", "DATABASE_URL")

	// Resolved Alerts environment variable bindings
	viper.BindEnv("resolved_alerts.enabled", "NOTIFICATOR_RESOLVED_ALERTS_ENABLED")
	viper.BindEnv("resolved_alerts.notifications_enabled", "NOTIFICATOR_RESOLVED_ALERTS_NOTIFICATIONS_ENABLED")
	viper.BindEnv("resolved_alerts.retention_days", "NOTIFICATOR_RESOLVED_ALERTS_RETENTION_DAYS")

	// Statistics environment variable bindings
	viper.BindEnv("statistics.retention_days", "NOTIFICATOR_STATISTICS_RETENTION_DAYS")

	// WebUI environment variable bindings
	viper.BindEnv("webui.playground", "WEBUI_PLAYGROUND", "NOTIFICATOR_WEBUI_PLAYGROUND")

	// OAuth environment variable bindings
	// Support both OAUTH_* and NOTIFICATOR_OAUTH_* patterns for flexibility
	// Main OAuth settings
	viper.BindEnv("oauth.enabled", "OAUTH_ENABLED", "NOTIFICATOR_OAUTH_ENABLED")
	viper.BindEnv("oauth.disable_classic_auth", "OAUTH_DISABLE_CLASSIC_AUTH", "NOTIFICATOR_OAUTH_DISABLE_CLASSIC_AUTH")
	viper.BindEnv("oauth.redirect_url", "OAUTH_REDIRECT_URL", "NOTIFICATOR_OAUTH_REDIRECT_URL")
	viper.BindEnv("oauth.session_key", "OAUTH_SESSION_KEY", "NOTIFICATOR_OAUTH_SESSION_KEY")
	viper.BindEnv("oauth.debug", "OAUTH_DEBUG", "NOTIFICATOR_OAUTH_DEBUG")
	viper.BindEnv("oauth.log_level", "OAUTH_LOG_LEVEL", "NOTIFICATOR_OAUTH_LOG_LEVEL")

	// Group sync settings
	viper.BindEnv("oauth.group_sync.enabled", "OAUTH_GROUP_SYNC_ENABLED", "NOTIFICATOR_OAUTH_GROUP_SYNC_ENABLED")
	viper.BindEnv("oauth.group_sync.sync_on_login", "OAUTH_GROUP_SYNC_ON_LOGIN", "NOTIFICATOR_OAUTH_GROUP_SYNC_ON_LOGIN")
	viper.BindEnv("oauth.group_sync.cache_timeout", "OAUTH_GROUP_CACHE_TIMEOUT", "NOTIFICATOR_OAUTH_GROUP_CACHE_TIMEOUT")
	viper.BindEnv("oauth.group_sync.default_role", "OAUTH_DEFAULT_ROLE", "NOTIFICATOR_OAUTH_DEFAULT_ROLE")
	viper.BindEnv("oauth.group_sync.validate_groups", "OAUTH_VALIDATE_GROUPS", "NOTIFICATOR_OAUTH_VALIDATE_GROUPS")
	viper.BindEnv("oauth.group_sync.audit_changes", "OAUTH_AUDIT_CHANGES", "NOTIFICATOR_OAUTH_AUDIT_CHANGES")

	// Security settings
	viper.BindEnv("oauth.security.state_timeout", "OAUTH_STATE_TIMEOUT", "NOTIFICATOR_OAUTH_STATE_TIMEOUT")
	viper.BindEnv("oauth.security.max_auth_attempts", "OAUTH_MAX_AUTH_ATTEMPTS", "NOTIFICATOR_OAUTH_MAX_AUTH_ATTEMPTS")
	viper.BindEnv("oauth.security.rate_limit", "OAUTH_RATE_LIMIT", "NOTIFICATOR_OAUTH_RATE_LIMIT")
	viper.BindEnv("oauth.security.require_https", "OAUTH_REQUIRE_HTTPS", "NOTIFICATOR_OAUTH_REQUIRE_HTTPS")
	viper.BindEnv("oauth.security.validate_issuer", "OAUTH_VALIDATE_ISSUER", "NOTIFICATOR_OAUTH_VALIDATE_ISSUER")
	viper.BindEnv("oauth.security.token_encryption", "OAUTH_TOKEN_ENCRYPTION", "NOTIFICATOR_OAUTH_TOKEN_ENCRYPTION")
	viper.BindEnv("oauth.security.csrf_protection", "OAUTH_CSRF_PROTECTION", "NOTIFICATOR_OAUTH_CSRF_PROTECTION")
}

func initializeFilterStates(cfg *Config) {
	if cfg.GUI.FilterState.SelectedSeverities == nil {
		cfg.GUI.FilterState.SelectedSeverities = map[string]bool{"All": true}
	}
	if cfg.GUI.FilterState.SelectedStatuses == nil {
		cfg.GUI.FilterState.SelectedStatuses = map[string]bool{"All": true}
	}
	if cfg.GUI.FilterState.SelectedTeams == nil {
		cfg.GUI.FilterState.SelectedTeams = map[string]bool{"All": true}
	}
	if cfg.GUI.FilterState.SelectedAcks == nil {
		cfg.GUI.FilterState.SelectedAcks = map[string]bool{"All": true}
	}
	if cfg.GUI.FilterState.SelectedComments == nil {
		cfg.GUI.FilterState.SelectedComments = map[string]bool{"All": true}
	}
}

// Format: "key1=value1,key2=value2"
func ParseHeadersFromEnv(envVar string) map[string]string {
	headers := make(map[string]string)

	envValue := os.Getenv(envVar)
	if envValue == "" {
		return headers
	}

	pairs := strings.Split(envValue, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" && value != "" {
				headers[key] = value
			}
		}
	}

	return headers
}

func (c *Config) MergeHeaders() {
	envHeaders := ParseHeadersFromEnv("METRICS_PROVIDER_HEADERS")
	for i := range c.Alertmanagers {
		if c.Alertmanagers[i].Headers == nil {
			c.Alertmanagers[i].Headers = make(map[string]string)
		}

		for key, value := range envHeaders {
			c.Alertmanagers[i].Headers[key] = value
		}
	}
}

func (c *Config) GetAlertmanagerByName(name string) *AlertmanagerConfig {
	for i := range c.Alertmanagers {
		if c.Alertmanagers[i].Name == name {
			return &c.Alertmanagers[i]
		}
	}
	return nil
}

func (c *Config) GetAlertmanagerByURL(url string) *AlertmanagerConfig {
	for i := range c.Alertmanagers {
		if c.Alertmanagers[i].URL == url {
			return &c.Alertmanagers[i]
		}
	}
	return nil
}

func (c *Config) AddAlertmanager(config AlertmanagerConfig) {
	if c.GetAlertmanagerByName(config.Name) != nil {
		// Find a unique name
		baseName := config.Name
		counter := 1
		for c.GetAlertmanagerByName(config.Name) != nil {
			config.Name = fmt.Sprintf("%s_%d", baseName, counter)
			counter++
		}
	}
	c.Alertmanagers = append(c.Alertmanagers, config)
}

func (c *Config) RemoveAlertmanager(name string) bool {
	for i := range c.Alertmanagers {
		if c.Alertmanagers[i].Name == name {
			c.Alertmanagers = append(c.Alertmanagers[:i], c.Alertmanagers[i+1:]...)
			return true
		}
	}
	return false
}

func (c *Config) GetAlertmanagerNames() []string {
	names := make([]string, len(c.Alertmanagers))
	for i, am := range c.Alertmanagers {
		names[i] = am.Name
	}
	return names
}

func (c *Config) ValidateAlertmanagers() error {
	if len(c.Alertmanagers) == 0 {
		return fmt.Errorf("at least one Alertmanager must be configured")
	}

	names := make(map[string]bool)
	urls := make(map[string]bool)

	for i, am := range c.Alertmanagers {
		if am.Name == "" {
			return fmt.Errorf("alertmanager at index %d has no name", i)
		}
		names[am.Name] = true

		if am.URL == "" {
			return fmt.Errorf("alertmanager '%s' has no URL", am.Name)
		}
		urls[am.URL] = true
	}

	return nil
}
