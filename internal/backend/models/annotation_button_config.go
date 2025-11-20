package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Validation constants
const (
	MaxLabelLength          = 100
	MaxAnnotationKeys       = 20
	MaxAnnotationKeyLength  = 100
	MinDisplayOrder         = -1000
	MaxDisplayOrder         = 1000
)

// Color validation regex - accepts hex colors (#RGB or #RRGGBB)
var validColorRegex = regexp.MustCompile(`^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$`)

// Annotation key validation - alphanumeric, underscore, hyphen, dot
var validAnnotationKeyRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-\.]{0,99}$`)

// AnnotationKeyList is a custom type for storing annotation keys as JSON array
type AnnotationKeyList []string

// Scan implements the sql.Scanner interface for database reading
func (a *AnnotationKeyList) Scan(value interface{}) error {
	if value == nil {
		*a = []string{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal AnnotationKeyList value")
	}

	return json.Unmarshal(bytes, a)
}

// Value implements the driver.Valuer interface for database writing
func (a AnnotationKeyList) Value() (driver.Value, error) {
	if len(a) == 0 {
		return json.Marshal([]string{})
	}
	return json.Marshal(a)
}

// AnnotationButtonConfig stores user configuration for annotation buttons
type AnnotationButtonConfig struct {
	ID             string            `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserID         string            `gorm:"type:varchar(255);index;not null" json:"user_id"`

	// Button display configuration
	Label          string            `gorm:"type:varchar(100);not null" json:"label"`
	AnnotationKeys AnnotationKeyList `gorm:"type:jsonb" json:"annotation_keys"` // List of annotation keys to check
	Color          string            `gorm:"type:varchar(50);not null" json:"color"` // Hex color or tailwind color
	Icon           string            `gorm:"type:varchar(50)" json:"icon"` // Icon identifier (optional)

	// Display order and status
	DisplayOrder   int               `gorm:"default:0;index:idx_user_display_order" json:"display_order"` // Lower numbers appear first
	Enabled        bool              `gorm:"default:true" json:"enabled"`

	// Button type (for default vs custom)
	ButtonType     string            `gorm:"type:varchar(20);default:'custom'" json:"button_type"` // "default" or "custom"

	// Timestamps
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	DeletedAt      gorm.DeletedAt    `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for GORM
func (AnnotationButtonConfig) TableName() string {
	return "annotation_button_configs"
}

// BeforeCreate generates a UUID for the ID field before creating a new record
func (a *AnnotationButtonConfig) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// DefaultAnnotationButtonConfigs returns the default button configurations for a new user
func DefaultAnnotationButtonConfigs(userID string) []AnnotationButtonConfig {
	return []AnnotationButtonConfig{
		{
			UserID:         userID,
			Label:          "Documentation",
			AnnotationKeys: AnnotationKeyList{"documentation", "runbook_url", "runbook"},
			Color:          "#4f46e5", // Indigo-600
			Icon:           "book-open",
			DisplayOrder:   0,
			Enabled:        true,
			ButtonType:     "default",
		},
		{
			UserID:         userID,
			Label:          "Grafana",
			AnnotationKeys: AnnotationKeyList{"grafana", "grafana_url"},
			Color:          "#ea580c", // Orange-600
			Icon:           "chart-bar",
			DisplayOrder:   1,
			Enabled:        true,
			ButtonType:     "default",
		},
		{
			UserID:         userID,
			Label:          "Sentry",
			AnnotationKeys: AnnotationKeyList{"sentry", "sentry_url"},
			Color:          "#9333ea", // Purple-600
			Icon:           "bug",
			DisplayOrder:   2,
			Enabled:        true,
			ButtonType:     "default",
		},
	}
}

// FindMatchingAnnotation checks if any of the configured annotation keys exist in the provided annotations map
// Returns the first matching key and its value, or empty strings if no match found
func (a *AnnotationButtonConfig) FindMatchingAnnotation(annotations map[string]string) (string, string) {
	if !a.Enabled || annotations == nil {
		return "", ""
	}

	for _, key := range a.AnnotationKeys {
		if value, exists := annotations[key]; exists && value != "" {
			return key, value
		}
	}

	return "", ""
}

// HasMatchingAnnotation checks if any of the configured annotation keys exist in the provided annotations
func (a *AnnotationButtonConfig) HasMatchingAnnotation(annotations map[string]string) bool {
	key, _ := a.FindMatchingAnnotation(annotations)
	return key != ""
}

// Validate validates the annotation button configuration
func (a *AnnotationButtonConfig) Validate() error {
	// Validate label
	if len(a.Label) == 0 {
		return errors.New("label is required")
	}
	if len(a.Label) > MaxLabelLength {
		return errors.New("label exceeds maximum length of 100 characters")
	}

	// Validate color format
	if !validColorRegex.MatchString(a.Color) {
		return errors.New("invalid color format: must be hex color (#RGB or #RRGGBB)")
	}

	// Validate annotation keys
	if len(a.AnnotationKeys) == 0 {
		return errors.New("at least one annotation key is required")
	}
	if len(a.AnnotationKeys) > MaxAnnotationKeys {
		return errors.New("too many annotation keys (maximum 20)")
	}

	for i, key := range a.AnnotationKeys {
		if len(key) == 0 {
			return errors.New("annotation key cannot be empty")
		}
		if len(key) > MaxAnnotationKeyLength {
			return errors.New("annotation key exceeds maximum length of 100 characters")
		}
		if !validAnnotationKeyRegex.MatchString(key) {
			return errors.New("invalid annotation key format: must start with letter and contain only alphanumeric, underscore, hyphen, or dot characters")
		}
		// Check for duplicates
		for j := i + 1; j < len(a.AnnotationKeys); j++ {
			if a.AnnotationKeys[j] == key {
				return errors.New("duplicate annotation key: " + key)
			}
		}
	}

	// Validate display order
	if a.DisplayOrder < MinDisplayOrder || a.DisplayOrder > MaxDisplayOrder {
		return errors.New("display order must be between -1000 and 1000")
	}

	// Validate button type
	if a.ButtonType != "default" && a.ButtonType != "custom" {
		return errors.New("button type must be 'default' or 'custom'")
	}

	return nil
}

// SanitizeColor ensures the color is a valid hex color or returns a default
func SanitizeColor(color string) string {
	if validColorRegex.MatchString(color) {
		return color
	}
	return "#6366f1" // Default indigo-600
}
