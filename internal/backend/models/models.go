package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID           string     `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Username     string     `gorm:"uniqueIndex;not null;size:100" json:"username"`
	Email        string     `gorm:"size:255" json:"email"`
	PasswordHash string     `gorm:"size:255" json:"-"` // Never serialize
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLogin    *time.Time `json:"last_login,omitempty"`

	// OAuth fields
	OAuthProvider *string `gorm:"size:50" json:"oauth_provider,omitempty"`
	OAuthID       *string `gorm:"size:255;index" json:"oauth_id,omitempty"`
	OAuthEmail    *string `gorm:"size:255" json:"oauth_email,omitempty"`
	EmailVerified bool    `gorm:"default:false" json:"email_verified"`

	Sessions        []Session        `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Comments        []Comment        `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Acknowledgments []Acknowledgment `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = GenerateID()
	}
	return nil
}

func (u *User) IsOAuthUser() bool {
	return u.OAuthProvider != nil && u.OAuthID != nil
}

func (u *User) HasPassword() bool {
	return u.PasswordHash != ""
}

func (u *User) CanLogin() bool {
	return u.HasPassword() || u.IsOAuthUser()
}

func (u *User) GetAuthMethod() string {
	if u.IsOAuthUser() {
		return "oauth:" + *u.OAuthProvider
	}
	if u.HasPassword() {
		return "password"
	}
	return "none"
}

type Session struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	UserID    string    `gorm:"not null;size:32;index" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

type Comment struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	AlertKey  string    `gorm:"not null;size:500;index" json:"alert_key"`
	UserID    string    `gorm:"not null;size:32" json:"user_id"`
	Content   string    `gorm:"not null;type:text" json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (c *Comment) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = GenerateID()
	}
	return nil
}

type Acknowledgment struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	AlertKey  string    `gorm:"not null;size:500;index" json:"alert_key"`
	UserID    string    `gorm:"not null;size:32" json:"user_id"`
	Reason    string    `gorm:"not null;type:text" json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (a *Acknowledgment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = GenerateID()
	}
	return nil
}

func (User) TableName() string           { return "users" }
func (Session) TableName() string        { return "sessions" }
func (Comment) TableName() string        { return "comments" }
func (Acknowledgment) TableName() string { return "acknowledgments" }

type CommentWithUser struct {
	Comment
	Username string `json:"username"`
}

type AcknowledgmentWithUser struct {
	Acknowledgment
	Username string `json:"username"`
}

type JSONB json.RawMessage

func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*j = JSONB(v)
	case string:
		*j = JSONB(v)
	default:
		return fmt.Errorf("cannot scan %T into JSONB", value)
	}
	return nil
}

type ResolvedAlert struct {
	ID          string `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Fingerprint string `gorm:"not null;size:500;index" json:"fingerprint"`

	AlertData JSONB `gorm:"type:jsonb;not null" json:"alert_data"`

	// Preserved relationships
	Comments        JSONB `gorm:"type:jsonb" json:"comments,omitempty"`
	Acknowledgments JSONB `gorm:"type:jsonb" json:"acknowledgments,omitempty"`

	ResolvedAt time.Time `gorm:"not null;index" json:"resolved_at"`
	ExpiresAt  time.Time `gorm:"not null;index" json:"expires_at"`
	Source     string    `gorm:"not null;size:255" json:"source"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ra *ResolvedAlert) BeforeCreate(tx *gorm.DB) error {
	if ra.ID == "" {
		ra.ID = GenerateID()
	}
	return nil
}

func (ResolvedAlert) TableName() string { return "resolved_alerts" }

// FilterPreset represents a saved filter configuration for the dashboard
type FilterPreset struct {
	ID            string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID        string    `gorm:"not null;size:32;index" json:"user_id"`
	Name          string    `gorm:"not null;size:255" json:"name"`
	Description   string    `gorm:"type:text" json:"description,omitempty"`
	IsShared      bool      `gorm:"default:false;index" json:"is_shared"`
	IsDefault     bool      `gorm:"default:false" json:"is_default"`
	FilterData    JSONB     `gorm:"type:jsonb;not null" json:"filter_data"`    // Type handled by Scanner/Valuer
	ColumnConfigs JSONB     `gorm:"type:jsonb" json:"column_configs,omitempty"` // Column configuration
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (fp *FilterPreset) BeforeCreate(tx *gorm.DB) error {
	if fp.ID == "" {
		fp.ID = GenerateID()
	}
	return nil
}

func (FilterPreset) TableName() string { return "filter_presets" }

// ColumnConfig represents a single column configuration in the dashboard table
type ColumnConfig struct {
	ID        string `json:"id"`         // Unique ID: "col_alertname", "col_custom_env"
	Label     string `json:"label"`      // Display name: "Alert Name", "Environment"
	FieldType string `json:"field_type"` // "system", "label", "annotation"
	FieldPath string `json:"field_path"` // "alertName", "labels.environment", "annotations.summary"
	Formatter string `json:"formatter"`  // "text", "badge", "duration", "timestamp", "count", "checkbox", "actions"
	Width     int    `json:"width"`      // Column width in pixels (50-800)
	Sortable  bool   `json:"sortable"`   // Can be sorted
	Visible   bool   `json:"visible"`    // Show/hide toggle
	Order     int    `json:"order"`      // Display order (0-based)
	Resizable bool   `json:"resizable"`  // Can be resized
	Critical  bool   `json:"critical"`   // Cannot be deleted (but can be hidden/reordered)
}

// DefaultColumnConfigs returns the default column configuration for new presets
func DefaultColumnConfigs() []ColumnConfig {
	return []ColumnConfig{
		{ID: "col_select", Label: "", FieldType: "system", FieldPath: "select", Formatter: "checkbox", Width: 50, Sortable: false, Visible: true, Order: 0, Resizable: false, Critical: true},
		{ID: "col_alertname", Label: "Alert Name", FieldType: "system", FieldPath: "alertName", Formatter: "text", Width: 300, Sortable: true, Visible: true, Order: 1, Resizable: true, Critical: true},
		{ID: "col_actions", Label: "Actions", FieldType: "system", FieldPath: "actions", Formatter: "actions", Width: 100, Sortable: false, Visible: true, Order: 2, Resizable: false, Critical: true},
		{ID: "col_instance", Label: "Instance", FieldType: "system", FieldPath: "instance", Formatter: "text", Width: 350, Sortable: true, Visible: true, Order: 3, Resizable: true, Critical: false},
		{ID: "col_severity", Label: "Severity", FieldType: "system", FieldPath: "severity", Formatter: "badge", Width: 150, Sortable: true, Visible: true, Order: 4, Resizable: true, Critical: false},
		{ID: "col_status", Label: "Status", FieldType: "system", FieldPath: "status", Formatter: "badge", Width: 150, Sortable: true, Visible: true, Order: 5, Resizable: true, Critical: false},
		{ID: "col_comments", Label: "Comments", FieldType: "system", FieldPath: "commentCount", Formatter: "count", Width: 130, Sortable: false, Visible: true, Order: 6, Resizable: true, Critical: false},
		{ID: "col_team", Label: "Team", FieldType: "system", FieldPath: "team", Formatter: "text", Width: 200, Sortable: true, Visible: true, Order: 7, Resizable: true, Critical: false},
		{ID: "col_summary", Label: "Summary", FieldType: "system", FieldPath: "summary", Formatter: "text", Width: 400, Sortable: false, Visible: true, Order: 8, Resizable: true, Critical: false},
		{ID: "col_duration", Label: "Duration", FieldType: "system", FieldPath: "duration", Formatter: "duration", Width: 150, Sortable: true, Visible: true, Order: 9, Resizable: true, Critical: false},
		{ID: "col_source", Label: "Alertmanager", FieldType: "system", FieldPath: "source", Formatter: "text", Width: 180, Sortable: true, Visible: true, Order: 10, Resizable: true, Critical: false},
	}
}

// ValidateColumnConfig validates a column configuration
func ValidateColumnConfig(config *ColumnConfig) error {
	// Check required fields
	if config.ID == "" {
		return fmt.Errorf("column ID is required")
	}
	if config.FieldPath == "" {
		return fmt.Errorf("field path is required")
	}

	// Validate width
	if config.Width < 50 || config.Width > 800 {
		return fmt.Errorf("column width must be between 50 and 800 pixels")
	}

	// Validate formatter
	validFormatters := map[string]bool{
		"text": true, "badge": true, "duration": true,
		"timestamp": true, "count": true, "checkbox": true, "actions": true,
	}
	if !validFormatters[config.Formatter] {
		return fmt.Errorf("invalid formatter: %s", config.Formatter)
	}

	// Validate field type
	if config.FieldType != "system" && config.FieldType != "label" && config.FieldType != "annotation" {
		return fmt.Errorf("field type must be 'system', 'label', or 'annotation'")
	}

	return nil
}

// ValidateColumnConfigs validates a slice of column configurations
func ValidateColumnConfigs(configs []ColumnConfig) error {
	if len(configs) == 0 {
		return nil // Empty is valid (will use defaults)
	}

	// Check for duplicate IDs
	ids := make(map[string]bool)
	orders := make(map[int]bool)

	for _, config := range configs {
		// Validate individual config
		if err := ValidateColumnConfig(&config); err != nil {
			return fmt.Errorf("invalid column config '%s': %w", config.ID, err)
		}

		// Check duplicate ID
		if ids[config.ID] {
			return fmt.Errorf("duplicate column ID: %s", config.ID)
		}
		ids[config.ID] = true

		// Check duplicate order
		if orders[config.Order] {
			return fmt.Errorf("duplicate column order: %d", config.Order)
		}
		orders[config.Order] = true
	}

	return nil
}

// UserDefaultFilterPreset represents the default filter preset for a user
// This allows users to set any preset (including shared ones) as their default
type UserDefaultFilterPreset struct {
	UserID          string    `gorm:"primaryKey;type:varchar(32);index" json:"user_id"`
	FilterPresetID  string    `gorm:"not null;type:varchar(32);index" json:"filter_preset_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	User         User         `gorm:"foreignKey:UserID" json:"user,omitempty"`
	FilterPreset FilterPreset `gorm:"foreignKey:FilterPresetID" json:"filter_preset,omitempty"`
}

func (UserDefaultFilterPreset) TableName() string { return "user_default_filter_presets" }

// UserColumnPreference stores user's default column configuration
// This serves as the base column config, which can be overridden by filter presets
type UserColumnPreference struct {
	UserID        string    `gorm:"primaryKey;type:varchar(32);index" json:"user_id"`
	ColumnConfigs JSONB     `gorm:"type:jsonb;not null" json:"column_configs"` // Array of ColumnConfig
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (UserColumnPreference) TableName() string { return "user_column_preferences" }

func GenerateID() string {
	return generateRandomString(32)
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
