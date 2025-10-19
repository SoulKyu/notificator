package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

// SeverityList is a custom type for storing severity preferences as JSON
type SeverityList []string

// Scan implements the sql.Scanner interface for database reading
func (s *SeverityList) Scan(value interface{}) error {
	if value == nil {
		*s = []string{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal SeverityList value")
	}

	return json.Unmarshal(bytes, s)
}

// Value implements the driver.Valuer interface for database writing
func (s SeverityList) Value() (driver.Value, error) {
	if len(s) == 0 {
		return json.Marshal([]string{})
	}
	return json.Marshal(s)
}

// NotificationPreference stores user preferences for browser notifications
type NotificationPreference struct {
	ID        string    `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID    string    `gorm:"type:varchar(255);uniqueIndex;not null" json:"user_id"`

	// Browser notification settings
	BrowserNotificationsEnabled bool         `gorm:"default:false" json:"browser_notifications_enabled"`
	EnabledSeverities          SeverityList `gorm:"type:jsonb" json:"enabled_severities"`
	SoundNotificationsEnabled   bool         `gorm:"default:true" json:"sound_notifications_enabled"`

	// Timestamps
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for GORM
func (NotificationPreference) TableName() string {
	return "notification_preferences"
}

// DefaultNotificationPreference returns a new NotificationPreference with sensible defaults
func DefaultNotificationPreference(userID string) *NotificationPreference {
	return &NotificationPreference{
		UserID:                      userID,
		BrowserNotificationsEnabled: false,                              // Disabled by default until user grants permission
		EnabledSeverities:          SeverityList{"critical", "warning"}, // Default to critical and warning
		SoundNotificationsEnabled:   true,                               // Sound enabled by default
	}
}

// IsSeverityEnabled checks if a specific severity is enabled for notifications
func (n *NotificationPreference) IsSeverityEnabled(severity string) bool {
	if n.EnabledSeverities == nil {
		return false
	}

	for _, s := range n.EnabledSeverities {
		if s == severity {
			return true
		}
	}
	return false
}

// SetEnabledSeverities sets the enabled severities with validation
func (n *NotificationPreference) SetEnabledSeverities(severities []string) {
	// Validate severities
	validSeverities := []string{}
	validValues := map[string]bool{"critical": true, "warning": true, "info": true, "information": true}

	for _, s := range severities {
		if validValues[s] {
			validSeverities = append(validSeverities, s)
		}
	}

	n.EnabledSeverities = SeverityList(validSeverities)
}
