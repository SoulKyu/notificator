package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	alertpb "notificator/internal/backend/proto/alert"
)

type UserNotificationPreference struct {
	ID                   string                    `json:"id"`
	UserID               string                    `json:"user_id"`
	Enabled              bool                      `json:"enabled"`
	SoundEnabled         bool                      `json:"sound_enabled"`
	BrowserNotifications bool                      `json:"browser_notifications"`
	CooldownSeconds      int                       `json:"cooldown_seconds"`
	MaxNotifications     int                       `json:"max_notifications"`
	RespectFilters       bool                      `json:"respect_filters"`
	SeverityRules        map[string]bool           `json:"severity_rules"`
	SoundConfig          *SoundConfig              `json:"sound_config"`
	CreatedAt            string                    `json:"created_at,omitempty"`
	UpdatedAt            string                    `json:"updated_at,omitempty"`
}

type SoundConfig map[string]interface{}

func (p *UserNotificationPreference) ToProtobuf() *alertpb.UserNotificationPreference {
	pbPref := &alertpb.UserNotificationPreference{
		Id:                   p.ID,
		UserId:               p.UserID,
		Enabled:              p.Enabled,
		SoundEnabled:         p.SoundEnabled,
		BrowserNotifications: p.BrowserNotifications,
		CooldownSeconds:      int32(p.CooldownSeconds),
		MaxNotifications:     int32(p.MaxNotifications),
		RespectFilters:       p.RespectFilters,
		SeverityRules:        p.SeverityRules,
	}

	// Convert sound config if present
	if p.SoundConfig != nil {
		sanitizedConfig := sanitizeSoundConfig(*p.SoundConfig)
		
		if soundConfigJSON, err := json.Marshal(sanitizedConfig); err == nil {
			pbPref.SoundConfigJson = string(soundConfigJSON)
		} else {
			println("Warning: Failed to marshal sound config to JSON:", err.Error())
			println("Original data:", fmt.Sprintf("%+v", *p.SoundConfig))
			println("Sanitized data:", fmt.Sprintf("%+v", sanitizedConfig))
		}
		
		pbPref.SoundConfig = &alertpb.SoundConfig{
			CriticalFrequency: getIntFromSoundConfig(*p.SoundConfig, "critical_frequency", 800),
			CriticalDuration:  getIntFromSoundConfig(*p.SoundConfig, "critical_duration", 200),
			CriticalType:      getStringFromSoundConfig(*p.SoundConfig, "critical_type", "square"),
			WarningFrequency:  getIntFromSoundConfig(*p.SoundConfig, "warning_frequency", 600),
			WarningDuration:   getIntFromSoundConfig(*p.SoundConfig, "warning_duration", 150),
			WarningType:       getStringFromSoundConfig(*p.SoundConfig, "warning_type", "sine"),
			InfoFrequency:     getIntFromSoundConfig(*p.SoundConfig, "info_frequency", 400),
			InfoDuration:      getIntFromSoundConfig(*p.SoundConfig, "info_duration", 100),
			InfoType:          getStringFromSoundConfig(*p.SoundConfig, "info_type", "sine"),
		}
	}

	return pbPref
}

func UserNotificationPreferenceFromProtobuf(pb *alertpb.UserNotificationPreference) *UserNotificationPreference {
	if pb == nil {
		return nil
	}

	pref := &UserNotificationPreference{
		ID:                   pb.Id,
		UserID:               pb.UserId,
		Enabled:              pb.Enabled,
		SoundEnabled:         pb.SoundEnabled,
		BrowserNotifications: pb.BrowserNotifications,
		CooldownSeconds:      int(pb.CooldownSeconds),
		MaxNotifications:     int(pb.MaxNotifications),
		RespectFilters:       pb.RespectFilters,
		SeverityRules:        pb.SeverityRules,
	}

	if pb.SoundConfigJson != "" {
		var soundConfig SoundConfig
		if err := json.Unmarshal([]byte(pb.SoundConfigJson), &soundConfig); err == nil {
			pref.SoundConfig = &soundConfig
		} else {
			println("Warning: Failed to unmarshal sound config JSON:", err.Error())
			println("JSON data:", pb.SoundConfigJson)
			pref.SoundConfig = parseSoundConfigFromStaticFields(pb.SoundConfig)
		}
	} else if pb.SoundConfig != nil {
		pref.SoundConfig = parseSoundConfigFromStaticFields(pb.SoundConfig)
	}

	if pb.CreatedAt != nil {
		pref.CreatedAt = pb.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z")
	}
	if pb.UpdatedAt != nil {
		pref.UpdatedAt = pb.UpdatedAt.AsTime().Format("2006-01-02T15:04:05Z")
	}

	return pref
}

func GetDefaultNotificationPreference() *UserNotificationPreference {
	return &UserNotificationPreference{
		Enabled:              true,
		SoundEnabled:         true,
		BrowserNotifications: true,
		CooldownSeconds:      300,
		MaxNotifications:     5,
		RespectFilters:       true,
		SeverityRules: map[string]bool{
			// Empty map - let frontend populate with dynamic severities from GetAvailableAlertLabels
		},
		SoundConfig: &SoundConfig{},
	}
}

func getIntFromSoundConfig(config SoundConfig, key string, defaultValue int) int32 {
	if value, exists := config[key]; exists {
		switch v := value.(type) {
		case int:
			return int32(v)
		case int32:
			return v
		case int64:
			return int32(v)
		case float64:
			return int32(v)
		case float32:
			return int32(v)
		}
	}
	return int32(defaultValue)
}

func getStringFromSoundConfig(config SoundConfig, key string, defaultValue string) string {
	if value, exists := config[key]; exists {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return defaultValue
}

func parseSoundConfigFromStaticFields(pbSoundConfig *alertpb.SoundConfig) *SoundConfig {
	if pbSoundConfig == nil {
		return nil
	}
	
	soundConfig := make(SoundConfig)
	soundConfig["critical_frequency"] = int(pbSoundConfig.CriticalFrequency)
	soundConfig["critical_duration"] = int(pbSoundConfig.CriticalDuration)
	soundConfig["critical_type"] = pbSoundConfig.CriticalType
	soundConfig["warning_frequency"] = int(pbSoundConfig.WarningFrequency)
	soundConfig["warning_duration"] = int(pbSoundConfig.WarningDuration)
	soundConfig["warning_type"] = pbSoundConfig.WarningType
	soundConfig["info_frequency"] = int(pbSoundConfig.InfoFrequency)
	soundConfig["info_duration"] = int(pbSoundConfig.InfoDuration)
	soundConfig["info_type"] = pbSoundConfig.InfoType
	
	return &soundConfig
}

func sanitizeSoundConfig(config SoundConfig) SoundConfig {
	sanitized := make(SoundConfig)
	validTypes := map[string]bool{"sine": true, "square": true, "triangle": true, "sawtooth": true}
	
	for key, value := range config {
		switch {
		case key == "" || value == nil:
			continue
			
		case strings.HasSuffix(key, "_frequency"):
			if freq, ok := sanitizeInt(value, 100, 2000, 500); ok {
				sanitized[key] = freq
			}
			
		case strings.HasSuffix(key, "_duration"):
			if dur, ok := sanitizeInt(value, 50, 5000, 150); ok {
				sanitized[key] = dur
			}
			
		case strings.HasSuffix(key, "_type"):
			if typeStr, ok := value.(string); ok && validTypes[typeStr] {
				sanitized[key] = typeStr
			} else {
				sanitized[key] = "sine"
			}
			
		default:
			if isSerializable(value) {
				sanitized[key] = value
			}
		}
	}
	
	return sanitized
}

func sanitizeInt(value interface{}, min, max, defaultVal int) (int, bool) {
	var intVal int
	
	switch v := value.(type) {
	case int:
		intVal = v
	case int32:
		intVal = int(v)
	case int64:
		intVal = int(v)
	case float32:
		intVal = int(v)
	case float64:
		intVal = int(v)
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			intVal = parsed
		} else {
			return defaultVal, true
		}
	default:
		return defaultVal, true
	}
	
	if intVal < min || intVal > max {
		return defaultVal, true
	}
	
	return intVal, true
}

func isSerializable(value interface{}) bool {
	switch value.(type) {
	case nil, bool, int, int32, int64, float32, float64, string:
		return true
	default:
		return false
	}
}