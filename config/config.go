package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	// Alertmanager configurations, supports multiple instances
	Alertmanagers []AlertmanagerConfig `json:"alertmanagers"`

	// GUI configuration
	GUI GUIConfig `json:"gui"`

	// Notification configuration
	Notifications NotificationConfig `json:"notifications"`

	// Polling configuration
	Polling PollingConfig `json:"polling"`

	// Column configuration for GUI
	ColumnWidths    map[string]float32 `json:"column_widths"`
	Backend         BackendConfig      `json:"backend"`
	ResolvedAlerts  ResolvedAlertsConfig `json:"resolved_alerts"`
}

type BackendConfig struct {
	Enabled    bool           `json:"enabled"`
	GRPCListen string         `json:"grpc_listen"`  // Port for gRPC server (e.g., ":50051")
	GRPCClient string         `json:"grpc_client"`  // Address for gRPC client (e.g., "localhost:50051")
	HTTPListen string         `json:"http_listen"`  // Port for HTTP server (e.g., ":8080")
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

// ResolvedAlertsConfig contains resolved alerts settings
type ResolvedAlertsConfig struct {
	Enabled              bool          `json:"enabled"`                // Enable resolved alerts tracking
	NotificationsEnabled bool          `json:"notifications_enabled"`  // Send notifications for resolved alerts
	RetentionDuration    time.Duration `json:"retention_duration"`     // How long to keep resolved alerts
}

// AlertmanagerConfig contains Alertmanager-specific settings
type AlertmanagerConfig struct {
	Name     string            `json:"name"`
	URL      string            `json:"url"`
	Username string            `json:"username"`
	Password string            `json:"password"`
	Token    string            `json:"token"`
	Headers  map[string]string `json:"headers"`
	OAuth    *OAuthConfig      `json:"oauth,omitempty"`
}

// OAuthConfig contains OAuth-specific settings
type OAuthConfig struct {
	Enabled   bool `json:"enabled"`
	ProxyMode bool `json:"proxy_mode"` // True for OAuth proxy authentication
}

// GUIConfig contains GUI-specific settings
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

// FilterStateConfig contains the state of filters
type FilterStateConfig struct {
	SearchText            string          `json:"search_text"`
	SelectedAlertmanagers map[string]bool `json:"selected_alertmanagers"`
	SelectedSeverities    map[string]bool `json:"selected_severities"`
	SelectedStatuses      map[string]bool `json:"selected_statuses"`
	SelectedTeams         map[string]bool `json:"selected_teams"`
	SelectedAcks          map[string]bool `json:"selected_acks"`
	SelectedComments      map[string]bool `json:"selected_comments"`
}

// NotificationConfig contains notification settings
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

// PollingConfig contains polling settings
type PollingConfig struct {
	Interval time.Duration `json:"interval"`
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
			Enabled:              true,                // Enable by default
			NotificationsEnabled: true,                // Send notifications by default
			RetentionDuration:    1 * time.Hour,       // Keep for 1 hour by default
		},
	}
}

// getDefaultSoundPath returns a platform-appropriate default sound path
func getDefaultSoundPath() string {
	// This will be empty by default, causing the system to use built-in sounds
	return ""
}

// LoadConfig loads configuration from a file, creating it with defaults if it doesn't exist
func LoadConfig(configPath string) (*Config, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config := DefaultConfig()
		if err := config.SaveToFile(configPath); err != nil {
			return nil, fmt.Errorf("failed to create default config: %w", err)
		}
		return config, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if config.Notifications.SeverityRules == nil {
		config.Notifications.SeverityRules = map[string]bool{
			"critical": true,
			"warning":  true,
			"info":     false,
			"unknown":  false,
		}
	}

	// Initialize filter state if missing (backward compatibility)
	if config.GUI.FilterState.SelectedSeverities == nil {
		config.GUI.FilterState.SelectedSeverities = map[string]bool{"All": true}
	}
	if config.GUI.FilterState.SelectedStatuses == nil {
		config.GUI.FilterState.SelectedStatuses = map[string]bool{"All": true}
	}
	if config.GUI.FilterState.SelectedTeams == nil {
		config.GUI.FilterState.SelectedTeams = map[string]bool{"All": true}
	}
	if config.GUI.FilterState.SelectedAcks == nil {
		config.GUI.FilterState.SelectedAcks = map[string]bool{"All": true}
	}
	if config.GUI.FilterState.SelectedComments == nil {
		config.GUI.FilterState.SelectedComments = map[string]bool{"All": true}
	}

	return &config, nil
}

// SaveToFile saves the configuration to a file
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

// GetConfigPath returns the default config file path
func GetConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "notificator.json"
	}

	return filepath.Join(home, ".config", "notificator", "config.json")
}

// LoadConfigWithViper loads configuration using Viper with environment variable support
func LoadConfigWithViper() (*Config, error) {
	// Create default config
	cfg := DefaultConfig()

	// Set up Viper defaults from the default config
	setViperDefaults(cfg)
	
	// Debug: Log what Viper sees for alertmanager config
	fmt.Printf("DEBUG: Viper alertmanagers.0.url = %s\n", viper.GetString("alertmanagers.0.url"))
	fmt.Printf("DEBUG: Viper alertmanagers.0.name = %s\n", viper.GetString("alertmanagers.0.name"))

	// Unmarshal into config struct
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	
	// Manually handle alertmanager array from environment variables
	// This is needed because Viper doesn't handle arrays well from env vars
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
	
	// Override alertmanagers if any were found in env vars
	if len(alertmanagers) > 0 {
		cfg.Alertmanagers = alertmanagers
		fmt.Printf("DEBUG: Loaded %d alertmanagers from environment\n", len(alertmanagers))
	}

	// Post-process configuration
	if cfg.Notifications.SeverityRules == nil {
		cfg.Notifications.SeverityRules = map[string]bool{
			"critical": true,
			"warning":  true,
			"info":     false,
			"unknown":  false,
		}
	}

	// Initialize filter state if missing (backward compatibility)
	initializeFilterStates(cfg)

	return cfg, nil
}

// setViperDefaults sets default values in Viper from the default config
func setViperDefaults(cfg *Config) {
	// Backend defaults
	viper.SetDefault("backend.enabled", cfg.Backend.Enabled)
	viper.SetDefault("backend.grpc_listen", cfg.Backend.GRPCListen)
	viper.SetDefault("backend.grpc_client", cfg.Backend.GRPCClient)
	viper.SetDefault("backend.http_listen", cfg.Backend.HTTPListen)
	
	// Database defaults
	viper.SetDefault("backend.database.type", cfg.Backend.Database.Type)
	viper.SetDefault("backend.database.host", cfg.Backend.Database.Host)
	viper.SetDefault("backend.database.port", cfg.Backend.Database.Port)
	viper.SetDefault("backend.database.name", cfg.Backend.Database.Name)
	viper.SetDefault("backend.database.user", cfg.Backend.Database.User)
	viper.SetDefault("backend.database.password", cfg.Backend.Database.Password)
	viper.SetDefault("backend.database.ssl_mode", cfg.Backend.Database.SSLMode)
	viper.SetDefault("backend.database.sqlite_path", cfg.Backend.Database.SQLitePath)

	// GUI defaults
	viper.SetDefault("gui.width", cfg.GUI.Width)
	viper.SetDefault("gui.height", cfg.GUI.Height)
	viper.SetDefault("gui.title", cfg.GUI.Title)
	viper.SetDefault("gui.minimize_to_tray", cfg.GUI.MinimizeToTray)
	viper.SetDefault("gui.start_minimized", cfg.GUI.StartMinimized)
	viper.SetDefault("gui.show_tray_icon", cfg.GUI.ShowTrayIcon)
	viper.SetDefault("gui.background_mode", cfg.GUI.BackgroundMode)

	// Notification defaults
	viper.SetDefault("notifications.enabled", cfg.Notifications.Enabled)
	viper.SetDefault("notifications.sound_enabled", cfg.Notifications.SoundEnabled)
	viper.SetDefault("notifications.sound_path", cfg.Notifications.SoundPath)
	viper.SetDefault("notifications.audio_output_device", cfg.Notifications.AudioOutputDevice)
	viper.SetDefault("notifications.show_system", cfg.Notifications.ShowSystem)
	viper.SetDefault("notifications.critical_only", cfg.Notifications.CriticalOnly)
	viper.SetDefault("notifications.max_notifications", cfg.Notifications.MaxNotifications)
	viper.SetDefault("notifications.cooldown_seconds", cfg.Notifications.CooldownSeconds)
	viper.SetDefault("notifications.respect_filters", cfg.Notifications.RespectFilters)

	// Polling defaults
	viper.SetDefault("polling.interval", cfg.Polling.Interval)

	// Resolved alerts defaults
	viper.SetDefault("resolved_alerts.enabled", cfg.ResolvedAlerts.Enabled)
	viper.SetDefault("resolved_alerts.notifications_enabled", cfg.ResolvedAlerts.NotificationsEnabled)
	viper.SetDefault("resolved_alerts.retention_duration", cfg.ResolvedAlerts.RetentionDuration)

	// Alertmanager defaults (first one only)
	if len(cfg.Alertmanagers) > 0 {
		am := cfg.Alertmanagers[0]
		viper.SetDefault("alertmanagers.0.name", am.Name)
		viper.SetDefault("alertmanagers.0.url", am.URL)
		viper.SetDefault("alertmanagers.0.username", am.Username)
		viper.SetDefault("alertmanagers.0.password", am.Password)
		viper.SetDefault("alertmanagers.0.token", am.Token)
		if am.OAuth != nil {
			viper.SetDefault("alertmanagers.0.oauth.enabled", am.OAuth.Enabled)
			viper.SetDefault("alertmanagers.0.oauth.proxy_mode", am.OAuth.ProxyMode)
		}
	}

	// Support common environment variables
	viper.BindEnv("backend.database.host", "DB_HOST", "DATABASE_HOST")
	viper.BindEnv("backend.database.port", "DB_PORT", "DATABASE_PORT")
	viper.BindEnv("backend.database.name", "DB_NAME", "DATABASE_NAME")
	viper.BindEnv("backend.database.user", "DB_USER", "DATABASE_USER")
	viper.BindEnv("backend.database.password", "DB_PASSWORD", "DATABASE_PASSWORD")
	viper.BindEnv("backend.database.ssl_mode", "DB_SSL_MODE", "DATABASE_SSL_MODE")
	viper.BindEnv("backend.database.sqlite_path", "DB_PATH", "DATABASE_PATH")
	
	// Support DATABASE_URL for full connection string
	viper.BindEnv("database_url", "DATABASE_URL")
}

// initializeFilterStates initializes filter states for backward compatibility
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

// ParseHeadersFromEnv parses headers from environment variable
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

// MergeHeaders merges environment headers with config headers
// Environment headers take precedence
func (c *Config) MergeHeaders() {
	envHeaders := ParseHeadersFromEnv("METRICS_PROVIDER_HEADERS")

	// Apply environment headers to all alertmanagers
	for i := range c.Alertmanagers {
		if c.Alertmanagers[i].Headers == nil {
			c.Alertmanagers[i].Headers = make(map[string]string)
		}

		for key, value := range envHeaders {
			c.Alertmanagers[i].Headers[key] = value
		}
	}
}

// GetAlertmanagerByName returns an Alertmanager configuration by name
func (c *Config) GetAlertmanagerByName(name string) *AlertmanagerConfig {
	for i := range c.Alertmanagers {
		if c.Alertmanagers[i].Name == name {
			return &c.Alertmanagers[i]
		}
	}
	return nil
}

// GetAlertmanagerByURL returns an Alertmanager configuration by URL
func (c *Config) GetAlertmanagerByURL(url string) *AlertmanagerConfig {
	for i := range c.Alertmanagers {
		if c.Alertmanagers[i].URL == url {
			return &c.Alertmanagers[i]
		}
	}
	return nil
}

// AddAlertmanager adds a new Alertmanager configuration
func (c *Config) AddAlertmanager(config AlertmanagerConfig) {
	// Ensure unique name
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

// RemoveAlertmanager removes an Alertmanager configuration by name
func (c *Config) RemoveAlertmanager(name string) bool {
	for i := range c.Alertmanagers {
		if c.Alertmanagers[i].Name == name {
			c.Alertmanagers = append(c.Alertmanagers[:i], c.Alertmanagers[i+1:]...)
			return true
		}
	}
	return false
}

// GetAlertmanagerNames returns a list of all Alertmanager names
func (c *Config) GetAlertmanagerNames() []string {
	names := make([]string, len(c.Alertmanagers))
	for i, am := range c.Alertmanagers {
		names[i] = am.Name
	}
	return names
}

// ValidateAlertmanagers validates all Alertmanager configurations
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
