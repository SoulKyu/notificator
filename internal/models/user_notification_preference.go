package models

import (
	"encoding/json"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type UserNotificationPreference struct {
	ID                   string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserID               string         `gorm:"index;type:varchar(100);not null" json:"user_id"`
	Enabled              bool           `gorm:"default:true" json:"enabled"`
	SoundEnabled         bool           `gorm:"default:true" json:"sound_enabled"`
	BrowserNotifications bool           `gorm:"default:true" json:"browser_notifications"`
	CooldownSeconds      int            `gorm:"default:300" json:"cooldown_seconds"`
	MaxNotifications     int            `gorm:"default:5" json:"max_notifications"`
	RespectFilters       bool           `gorm:"default:true" json:"respect_filters"`
	SeverityRules        datatypes.JSON `gorm:"type:json" json:"severity_rules"`
	SoundConfig          datatypes.JSON `gorm:"type:json" json:"sound_config"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

type SeverityRulesMap map[string]bool

// Dynamic sound config map that can handle any severity types
type SoundConfigMap map[string]interface{}

func (UserNotificationPreference) TableName() string {
	return "user_notification_preferences"
}

func (u *UserNotificationPreference) SetSeverityRules(rules SeverityRulesMap) error {
	jsonData, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	u.SeverityRules = datatypes.JSON(jsonData)
	return nil
}

func (u *UserNotificationPreference) GetSeverityRules() (SeverityRulesMap, error) {
	var rules SeverityRulesMap
	if u.SeverityRules == nil {
		return GetDefaultSeverityRules(), nil
	}

	jsonBytes := []byte(u.SeverityRules)
	if err := json.Unmarshal(jsonBytes, &rules); err != nil {
		return GetDefaultSeverityRules(), err
	}
	return rules, nil
}

func (u *UserNotificationPreference) SetSoundConfig(config SoundConfigMap) error {
	jsonData, err := json.Marshal(config)
	if err != nil {
		return err
	}
	u.SoundConfig = datatypes.JSON(jsonData)
	return nil
}

func (u *UserNotificationPreference) GetSoundConfig() (SoundConfigMap, error) {
	var config SoundConfigMap
	if u.SoundConfig == nil {
		return GetDefaultSoundConfig(), nil
	}

	jsonBytes := []byte(u.SoundConfig)
	if err := json.Unmarshal(jsonBytes, &config); err != nil {
		return GetDefaultSoundConfig(), err
	}
	return config, nil
}

func GetDefaultSeverityRules() SeverityRulesMap {
	// Return empty map - let frontend handle dynamic severities using GetAvailableAlertLabels
	return SeverityRulesMap{}
}

func GetDefaultSoundConfig() SoundConfigMap {
	return SoundConfigMap{
		"critical_frequency": 800,
		"critical_duration":  200,
		"critical_type":      "square",
		"warning_frequency":  600,
		"warning_duration":   150,
		"warning_type":       "sine",
		"info_frequency":     400,
		"info_duration":      100,
		"info_type":          "sine",
		// Dynamic - any other severity types can be added by frontend
	}
}

func CreateDefaultUserNotificationPreference(userID string) *UserNotificationPreference {
	pref := &UserNotificationPreference{
		UserID:               userID,
		Enabled:              true,
		SoundEnabled:         true,
		BrowserNotifications: true,
		CooldownSeconds:      300,
		MaxNotifications:     5,
		RespectFilters:       true,
	}

	pref.SetSeverityRules(GetDefaultSeverityRules())

	pref.SetSoundConfig(GetDefaultSoundConfig())

	return pref
}

type NotificationPreferenceCache struct {
	Preference *UserNotificationPreference `json:"preference"`
	UserID     string                      `json:"user_id"`
	CachedAt   time.Time                   `json:"cached_at"`
	TTL        time.Duration               `json:"ttl"`
}

func (c *NotificationPreferenceCache) IsExpired() bool {
	return time.Since(c.CachedAt) > c.TTL
}

func (c *NotificationPreferenceCache) ShouldNotifyForSeverity(severity string) bool {
	if c.Preference == nil {
		return false
	}

	rules, err := c.Preference.GetSeverityRules()
	if err != nil {
		return false
	}

	enabled, exists := rules[severity]
	return exists && enabled
}
