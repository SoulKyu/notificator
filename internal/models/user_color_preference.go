package models

import (
	"encoding/json"
	"time"
	"gorm.io/gorm"
	"gorm.io/datatypes"
)

// UserColorPreference represents a user-defined color preference for alerts
type UserColorPreference struct {
	ID                  string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserID              string         `gorm:"index;type:varchar(100);not null" json:"user_id"`
	LabelConditions     datatypes.JSON `gorm:"type:json" json:"label_conditions"`  // JSON map of label conditions
	Color               string         `gorm:"type:varchar(50);not null" json:"color"`
	ColorType           string         `gorm:"type:varchar(20);not null;default:'custom'" json:"color_type"` // "severity", "custom", "tailwind"
	Priority            int            `gorm:"default:0" json:"priority"`          // Higher numbers = higher priority
	BgLightnessFactor   float64        `gorm:"type:decimal(3,2);default:0.9" json:"bg_lightness_factor"`    // Background lightness factor (0.0-1.0)
	TextDarknessFactor  float64        `gorm:"type:decimal(3,2);default:0.3" json:"text_darkness_factor"`   // Text darkness factor (0.0-1.0)
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`
}

type LabelConditionsMap map[string]string

// TableName sets the table name for GORM
func (UserColorPreference) TableName() string {
	return "user_color_preferences"
}

// SetLabelConditions converts a map to JSON and stores it
func (u *UserColorPreference) SetLabelConditions(conditions LabelConditionsMap) error {
	jsonData, err := json.Marshal(conditions)
	if err != nil {
		return err
	}
	u.LabelConditions = datatypes.JSON(jsonData)
	return nil
}

// GetLabelConditions retrieves the label conditions as a map
func (u *UserColorPreference) GetLabelConditions() (LabelConditionsMap, error) {
	var conditions LabelConditionsMap
	if u.LabelConditions == nil {
		return conditions, nil
	}
	
	jsonBytes := []byte(u.LabelConditions)
	if err := json.Unmarshal(jsonBytes, &conditions); err != nil {
		return conditions, err
	}
	return conditions, nil
}

// MatchesAlert checks if this color preference matches the given alert
func (u *UserColorPreference) MatchesAlert(alert *Alert) bool {
	conditions, err := u.GetLabelConditions()
	if err != nil {
		return false
	}

	for labelKey, expectedValue := range conditions {
		alertValue, exists := alert.Labels[labelKey]
		if !exists || alertValue != expectedValue {
			return false
		}
	}

	return true
}

type ColorPreferenceCache struct {
	Preferences []UserColorPreference `json:"preferences"`
	UserID      string                `json:"user_id"`
	CachedAt    time.Time             `json:"cached_at"`
	TTL         time.Duration         `json:"ttl"`
}

// IsExpired checks if the cache has expired
func (c *ColorPreferenceCache) IsExpired() bool {
	return time.Since(c.CachedAt) > c.TTL
}

func (c *ColorPreferenceCache) FindColorForAlert(alert *Alert) (string, string, bool) {
	var bestMatch *UserColorPreference
	highestPriority := -1

	for i := range c.Preferences {
		pref := &c.Preferences[i]
		if pref.MatchesAlert(alert) && pref.Priority > highestPriority {
			bestMatch = pref
			highestPriority = pref.Priority
		}
	}

	if bestMatch != nil {
		return bestMatch.Color, bestMatch.ColorType, true
	}

	return "", "", false
}