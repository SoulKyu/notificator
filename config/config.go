package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	ColumnWidths map[string]float32 `json:"column_widths"`
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
