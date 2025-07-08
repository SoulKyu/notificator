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
	// Alertmanager configuration
	Alertmanager AlertmanagerConfig `json:"alertmanager"`

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
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Title  string `json:"title"`
}

// NotificationConfig contains notification settings
type NotificationConfig struct {
	Enabled          bool            `json:"enabled"`
	SoundEnabled     bool            `json:"sound_enabled"`
	SoundPath        string          `json:"sound_path"`
	ShowSystem       bool            `json:"show_system"`
	CriticalOnly     bool            `json:"critical_only"`
	MaxNotifications int             `json:"max_notifications"`
	CooldownSeconds  int             `json:"cooldown_seconds"`
	SeverityRules    map[string]bool `json:"severity_rules"`
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
		Alertmanager: AlertmanagerConfig{
			URL:     "http://localhost:9093",
			Headers: headers,
			OAuth:   oauthConfig,
		},
		GUI: GUIConfig{
			Width:  1200,
			Height: 800,
			Title:  "Notificator - Alert Dashboard",
		},
		Notifications: NotificationConfig{
			Enabled:          true,
			SoundEnabled:     true,
			SoundPath:        getDefaultSoundPath(),
			ShowSystem:       true,
			CriticalOnly:     false,
			MaxNotifications: 5,
			CooldownSeconds:  300, // 5 minutes
			SeverityRules: map[string]bool{
				"critical": true,
				"warning":  true,
				"info":     false,
				"unknown":  false,
			},
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

	if c.Alertmanager.Headers == nil {
		c.Alertmanager.Headers = make(map[string]string)
	}

	for key, value := range envHeaders {
		c.Alertmanager.Headers[key] = value
	}
}
