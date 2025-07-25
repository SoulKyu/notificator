package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"notificator/internal/models"
	"notificator/internal/webui/client"
	webuimodels "notificator/internal/webui/models"
)

// ColorService handles efficient color preference matching for alerts
type ColorService struct {
	backendClient    *client.BackendClient
	colorCache       map[string]*ColorPreferenceCache // userID -> cache
	cacheMutex       sync.RWMutex
	defaultColors    map[string]string
	cacheTTL         time.Duration
}

// ColorPreferenceCache stores user color preferences with efficient lookup
type ColorPreferenceCache struct {
	UserID      string                         `json:"userId"`
	Preferences []webuimodels.UserColorPreference `json:"preferences"`
	LookupMap   map[string]*ColorMatch         `json:"-"` // Pre-computed lookups for performance
	CachedAt    time.Time                      `json:"cachedAt"`
	TTL         time.Duration                  `json:"ttl"`
}

// ColorMatch represents a matched color for an alert
type ColorMatch struct {
	Color     string    `json:"color"`
	ColorType string    `json:"colorType"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"createdAt"`
}

// AlertColorResult contains the color information for an alert
type AlertColorResult struct {
	BackgroundColor string `json:"backgroundColor"`
	TextColor       string `json:"textColor"`
	BorderColor     string `json:"borderColor"`
	BadgeColor      string `json:"badgeColor"`
	ColorSource     string `json:"colorSource"` // "user", "default", "severity"
}

// NewColorService creates a new color service
func NewColorService(backendClient *client.BackendClient) *ColorService {
	return &ColorService{
		backendClient: backendClient,
		colorCache:    make(map[string]*ColorPreferenceCache),
		cacheTTL:      5 * time.Minute, // Cache for 5 minutes
		defaultColors: map[string]string{
			"critical":          "#dc2626", // red-600
			"critical-daytime":  "#be123c", // rose-700
			"warning":           "#d97706", // amber-600
			"info":              "#2563eb", // blue-600
			"default":           "#6b7280", // gray-500
		},
	}
}

// GetAlertColors returns the color configuration for an alert based on user preferences
func (cs *ColorService) GetAlertColors(alert *models.Alert, sessionID string) *AlertColorResult {
	// Get user color preferences
	cache, err := cs.getUserColorCache(sessionID)
	if err != nil {
		// Fallback to default severity colors
		return cs.getDefaultSeverityColors(alert)
	}

	// Find matching color preference
	colorMatch := cs.findColorMatch(alert, cache)
	if colorMatch == nil {
		// No custom preference found, use default
		return cs.getDefaultSeverityColors(alert)
	}

	// Apply the custom color
	return cs.applyCustomColor(colorMatch, alert)
}

// GetAlertColorsOptimized returns colors for multiple alerts efficiently
func (cs *ColorService) GetAlertColorsOptimized(alerts []*models.Alert, sessionID string) map[string]*AlertColorResult {
	results := make(map[string]*AlertColorResult)
	
	// Get user color preferences once
	cache, err := cs.getUserColorCache(sessionID)
	if err != nil {
		// Fallback to default colors for all alerts
		for _, alert := range alerts {
			fingerprint := alert.GetFingerprint()
			results[fingerprint] = cs.getDefaultSeverityColors(alert)
		}
		return results
	}

	// Process alerts in batch
	for _, alert := range alerts {
		fingerprint := alert.GetFingerprint()
		
		colorMatch := cs.findColorMatch(alert, cache)
		if colorMatch == nil {
			results[fingerprint] = cs.getDefaultSeverityColors(alert)
		} else {
			results[fingerprint] = cs.applyCustomColor(colorMatch, alert)
		}
	}

	return results
}

// InvalidateUserCache removes cached color preferences for a user
func (cs *ColorService) InvalidateUserCache(sessionID string) {
	cs.cacheMutex.Lock()
	defer cs.cacheMutex.Unlock()
	delete(cs.colorCache, sessionID)
}

// getUserColorCache gets or creates cached color preferences for a user
func (cs *ColorService) getUserColorCache(sessionID string) (*ColorPreferenceCache, error) {
	cs.cacheMutex.RLock()
	cache, exists := cs.colorCache[sessionID]
	cs.cacheMutex.RUnlock()

	// Check if cache exists and is still valid
	if exists && time.Since(cache.CachedAt) < cache.TTL {
		return cache, nil
	}

	// Need to refresh cache
	cs.cacheMutex.Lock()
	defer cs.cacheMutex.Unlock()

	// Double-check after acquiring write lock
	if cache, exists := cs.colorCache[sessionID]; exists && time.Since(cache.CachedAt) < cache.TTL {
		return cache, nil
	}

	// Fetch preferences from backend
	preferences, err := cs.backendClient.GetUserColorPreferences(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user color preferences: %w", err)
	}

	// Convert protobuf preferences to webui models
	var webuiPrefs []webuimodels.UserColorPreference
	for _, pref := range preferences {
		webuiPref := webuimodels.UserColorPreference{
			ID:              pref.Id,
			UserID:          pref.UserId,
			LabelConditions: pref.LabelConditions,
			Color:           pref.Color,
			ColorType:       pref.ColorType,
			Priority:        int(pref.Priority),
			CreatedAt:       pref.CreatedAt.AsTime(),
			UpdatedAt:       pref.UpdatedAt.AsTime(),
		}
		webuiPrefs = append(webuiPrefs, webuiPref)
	}

	// Create cache with optimized lookup
	cache = &ColorPreferenceCache{
		UserID:      sessionID,
		Preferences: webuiPrefs,
		LookupMap:   cs.buildLookupMap(webuiPrefs),
		CachedAt:    time.Now(),
		TTL:         cs.cacheTTL,
	}

	cs.colorCache[sessionID] = cache
	return cache, nil
}

// buildLookupMap creates an optimized lookup structure for fast color matching
func (cs *ColorService) buildLookupMap(preferences []webuimodels.UserColorPreference) map[string]*ColorMatch {
	lookupMap := make(map[string]*ColorMatch)

	// Sort preferences by priority (higher priority first), then by creation time (older first)
	sortedPrefs := make([]webuimodels.UserColorPreference, len(preferences))
	copy(sortedPrefs, preferences)
	
	// Sort by priority (descending), then by creation time (ascending) for deterministic order
	for i := 0; i < len(sortedPrefs); i++ {
		for j := 0; j < len(sortedPrefs)-1-i; j++ {
			swap := false
			if sortedPrefs[j].Priority < sortedPrefs[j+1].Priority {
				swap = true
			} else if sortedPrefs[j].Priority == sortedPrefs[j+1].Priority {
				// Same priority: older rule (earlier CreatedAt) comes first
				if sortedPrefs[j].CreatedAt.After(sortedPrefs[j+1].CreatedAt) {
					swap = true
				}
			}
			if swap {
				sortedPrefs[j], sortedPrefs[j+1] = sortedPrefs[j+1], sortedPrefs[j]
			}
		}
	}

	// Build lookup keys for each preference
	for _, pref := range sortedPrefs {
		lookupKey := cs.buildLookupKey(pref.LabelConditions)
		
		// Only store if this key doesn't exist (higher priority already set)
		if _, exists := lookupMap[lookupKey]; !exists {
			lookupMap[lookupKey] = &ColorMatch{
				Color:     pref.Color,
				ColorType: pref.ColorType,
				Priority:  pref.Priority,
				CreatedAt: pref.CreatedAt,
			}
		}
	}

	return lookupMap
}

// buildLookupKey creates a consistent key from label conditions
func (cs *ColorService) buildLookupKey(conditions map[string]string) string {
	if len(conditions) == 0 {
		return "*" // Matches all
	}

	// Create a consistent key by sorting conditions
	keyData := make(map[string]string)
	for k, v := range conditions {
		keyData[k] = v
	}

	jsonKey, _ := json.Marshal(keyData)
	return string(jsonKey)
}

// findColorMatch finds the best matching color preference for an alert
func (cs *ColorService) findColorMatch(alert *models.Alert, cache *ColorPreferenceCache) *ColorMatch {
	// Try exact match first
	exactKey := cs.buildLookupKey(alert.Labels)
	if match, exists := cache.LookupMap[exactKey]; exists {
		return match
	}

	// Try partial matches - consider both specificity and priority
	bestMatch := (*ColorMatch)(nil)
	bestMatchCount := 0
	bestPriority := -1

	for _, pref := range cache.Preferences {
		matchCount := 0
		allMatch := true

		// Check if all conditions in preference match the alert
		for labelKey, expectedValue := range pref.LabelConditions {
			if alertValue, exists := alert.Labels[labelKey]; exists {
				// Handle severity normalization for matching
				if labelKey == "severity" {
					normalizedExpected := cs.normalizeSeverity(expectedValue)
					normalizedAlert := cs.normalizeSeverity(alertValue)
					if normalizedAlert == normalizedExpected {
						matchCount++
					} else {
						allMatch = false
						break
					}
				} else if alertValue == expectedValue {
					matchCount++
				} else {
					allMatch = false
					break
				}
			} else {
				allMatch = false
				break
			}
		}

		// If all conditions match, check if this is a better match
		if allMatch {
			isBetterMatch := false
			
			// First priority: more specific matches (more conditions)
			if matchCount > bestMatchCount {
				isBetterMatch = true
			} else if matchCount == bestMatchCount {
				// Same specificity: higher priority wins
				if pref.Priority > bestPriority {
					isBetterMatch = true
				} else if pref.Priority == bestPriority {
					// Same priority and specificity: use creation time as tie-breaker (older rule wins)
					// This ensures deterministic behavior
					if bestMatch == nil || pref.CreatedAt.Before(bestMatch.CreatedAt) {
						isBetterMatch = true
					}
				}
			}
			
			if isBetterMatch {
				bestMatch = &ColorMatch{
					Color:     pref.Color,
					ColorType: pref.ColorType,
					Priority:  pref.Priority,
					CreatedAt: pref.CreatedAt,
				}
				bestMatchCount = matchCount
				bestPriority = pref.Priority
			}
		}
	}

	// Check for wildcard match (empty conditions)
	if bestMatch == nil {
		if match, exists := cache.LookupMap["*"]; exists {
			return match
		}
	}

	return bestMatch
}

// getDefaultSeverityColors returns default colors based on alert severity
func (cs *ColorService) getDefaultSeverityColors(alert *models.Alert) *AlertColorResult {
	severity := alert.GetSeverity()
	
	var baseColor string
	if color, exists := cs.defaultColors[severity]; exists {
		baseColor = color
	} else {
		baseColor = cs.defaultColors["default"]
	}

	return &AlertColorResult{
		BackgroundColor: cs.lightenColor(baseColor, 0.9),
		TextColor:       cs.darkenColor(baseColor, 0.3),
		BorderColor:     baseColor,
		BadgeColor:      baseColor,
		ColorSource:     "severity",
	}
}

// applyCustomColor applies a custom color match to create full color result
func (cs *ColorService) applyCustomColor(match *ColorMatch, alert *models.Alert) *AlertColorResult {
	baseColor := match.Color

	switch match.ColorType {
	case "tailwind":
		// Handle Tailwind CSS classes
		return &AlertColorResult{
			BackgroundColor: baseColor + "-100",
			TextColor:       baseColor + "-800",
			BorderColor:     baseColor + "-500",
			BadgeColor:      baseColor + "-500",
			ColorSource:     "user",
		}
	case "severity":
		// Use severity color system
		return cs.getDefaultSeverityColors(alert)
	default:
		// Custom hex color
		return &AlertColorResult{
			BackgroundColor: cs.lightenColor(baseColor, 0.9),
			TextColor:       cs.darkenColor(baseColor, 0.3),
			BorderColor:     baseColor,
			BadgeColor:      baseColor,
			ColorSource:     "user",
		}
	}
}

// lightenColor lightens a hex color by the given factor (0-1)
func (cs *ColorService) lightenColor(hexColor string, factor float64) string {
	// Simple implementation - in production you'd want a proper color manipulation library
	// For now, return a lightened version by adding opacity or using CSS
	return hexColor + fmt.Sprintf("%02x", int(255*(1-factor)))
}

// darkenColor darkens a hex color by the given factor (0-1)
func (cs *ColorService) darkenColor(hexColor string, factor float64) string {
	// Simple implementation - in production you'd want a proper color manipulation library
	return hexColor
}

// normalizeSeverity normalizes severity values for consistent matching
func (cs *ColorService) normalizeSeverity(severity string) string {
	switch strings.ToLower(severity) {
	case "information":
		return "info"
	case "critical-daytime":
		return "critical"
	default:
		return strings.ToLower(severity)
	}
}