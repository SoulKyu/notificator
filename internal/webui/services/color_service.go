package services

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"notificator/internal/models"
	"notificator/internal/webui/client"
	webuimodels "notificator/internal/webui/models"
)

type ColorService struct {
	backendClient *client.BackendClient
	colorCache    map[string]*ColorPreferenceCache // userID -> cache
	cacheMutex    sync.RWMutex
	defaultColors map[string]string
	cacheTTL      time.Duration
}

type ColorPreferenceCache struct {
	UserID      string                            `json:"userId"`
	Preferences []webuimodels.UserColorPreference `json:"preferences"`
	LookupMap   map[string]*ColorMatch            `json:"-"` // Pre-computed lookups for performance
	CachedAt    time.Time                         `json:"cachedAt"`
	TTL         time.Duration                     `json:"ttl"`
}

type ColorMatch struct {
	Color              string    `json:"color"`
	ColorType          string    `json:"colorType"`
	Priority           int       `json:"priority"`
	BgLightnessFactor  float64   `json:"bgLightnessFactor"`
	TextDarknessFactor float64   `json:"textDarknessFactor"`
	CreatedAt          time.Time `json:"createdAt"`
}

type AlertColorResult struct {
	BackgroundColor string `json:"backgroundColor"`
	TextColor       string `json:"textColor"`
	BorderColor     string `json:"borderColor"`
	BadgeColor      string `json:"badgeColor"`
	ColorSource     string `json:"colorSource"` // "user", "default", "severity"
}

func NewColorService(backendClient *client.BackendClient) *ColorService {
	return &ColorService{
		backendClient: backendClient,
		colorCache:    make(map[string]*ColorPreferenceCache),
		cacheTTL:      5 * time.Minute,
		defaultColors: map[string]string{
			"critical":         "#dc2626",
			"critical-daytime": "#be123c",
			"warning":          "#d97706",
			"info":             "#2563eb",
			"default":          "#6b7280",
		},
	}
}

func (cs *ColorService) GetAlertColors(alert *models.Alert, sessionID string) *AlertColorResult {
	cache, err := cs.getUserColorCache(sessionID)
	if err != nil {
		return cs.getDefaultSeverityColors(alert)
	}

	colorMatch := cs.findColorMatch(alert, cache)
	if colorMatch == nil {
		return cs.getDefaultSeverityColors(alert)
	}

	return cs.applyCustomColor(colorMatch, alert)
}

func (cs *ColorService) GetAlertColorsOptimized(alerts []*models.Alert, sessionID string) map[string]*AlertColorResult {
	results := make(map[string]*AlertColorResult)

	cache, err := cs.getUserColorCache(sessionID)
	if err != nil {
		for _, alert := range alerts {
			fingerprint := alert.GetFingerprint()
			results[fingerprint] = cs.getDefaultSeverityColors(alert)
		}
		return results
	}

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

func (cs *ColorService) InvalidateUserCache(sessionID string) {
	cs.cacheMutex.Lock()
	defer cs.cacheMutex.Unlock()
	delete(cs.colorCache, sessionID)
}

func (cs *ColorService) getUserColorCache(sessionID string) (*ColorPreferenceCache, error) {
	cs.cacheMutex.RLock()
	cache, exists := cs.colorCache[sessionID]
	cs.cacheMutex.RUnlock()

	if exists && time.Since(cache.CachedAt) < cache.TTL {
		return cache, nil
	}

	cs.cacheMutex.Lock()
	defer cs.cacheMutex.Unlock()

	if cache, exists := cs.colorCache[sessionID]; exists && time.Since(cache.CachedAt) < cache.TTL {
		return cache, nil
	}

	preferences, err := cs.backendClient.GetUserColorPreferences(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user color preferences: %w", err)
	}

	var webuiPrefs []webuimodels.UserColorPreference
	for _, pref := range preferences {
		webuiPref := webuimodels.UserColorPreference{
			ID:                 pref.Id,
			UserID:             pref.UserId,
			LabelConditions:    pref.LabelConditions,
			Color:              pref.Color,
			ColorType:          pref.ColorType,
			Priority:           int(pref.Priority),
			BgLightnessFactor:  float64(pref.BgLightnessFactor),
			TextDarknessFactor: float64(pref.TextDarknessFactor),
			CreatedAt:          pref.CreatedAt.AsTime(),
			UpdatedAt:          pref.UpdatedAt.AsTime(),
		}
		webuiPrefs = append(webuiPrefs, webuiPref)
	}

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

func (cs *ColorService) buildLookupMap(preferences []webuimodels.UserColorPreference) map[string]*ColorMatch {
	lookupMap := make(map[string]*ColorMatch)

	sortedPrefs := make([]webuimodels.UserColorPreference, len(preferences))
	copy(sortedPrefs, preferences)

	for i := 0; i < len(sortedPrefs); i++ {
		for j := 0; j < len(sortedPrefs)-1-i; j++ {
			swap := false
			if sortedPrefs[j].Priority < sortedPrefs[j+1].Priority {
				swap = true
			} else if sortedPrefs[j].Priority == sortedPrefs[j+1].Priority {
				if sortedPrefs[j].CreatedAt.After(sortedPrefs[j+1].CreatedAt) {
					swap = true
				}
			}
			if swap {
				sortedPrefs[j], sortedPrefs[j+1] = sortedPrefs[j+1], sortedPrefs[j]
			}
		}
	}

	for _, pref := range sortedPrefs {
		lookupKey := cs.buildLookupKey(pref.LabelConditions)

		if _, exists := lookupMap[lookupKey]; !exists {
			lookupMap[lookupKey] = &ColorMatch{
				Color:              pref.Color,
				ColorType:          pref.ColorType,
				Priority:           pref.Priority,
				BgLightnessFactor:  pref.BgLightnessFactor,
				TextDarknessFactor: pref.TextDarknessFactor,
				CreatedAt:          pref.CreatedAt,
			}
		}
	}

	return lookupMap
}

func (cs *ColorService) buildLookupKey(conditions map[string]string) string {
	if len(conditions) == 0 {
		return "*"
	}

	keyData := make(map[string]string)
	for k, v := range conditions {
		keyData[k] = v
	}

	jsonKey, _ := json.Marshal(keyData)
	return string(jsonKey)
}

func (cs *ColorService) findColorMatch(alert *models.Alert, cache *ColorPreferenceCache) *ColorMatch {
	exactKey := cs.buildLookupKey(alert.Labels)
	if match, exists := cache.LookupMap[exactKey]; exists {
		return match
	}

	bestMatch := (*ColorMatch)(nil)
	bestMatchCount := 0
	bestPriority := -1

	for _, pref := range cache.Preferences {
		matchCount := 0
		allMatch := true

		for labelKey, expectedValue := range pref.LabelConditions {
			if alertValue, exists := alert.Labels[labelKey]; exists {
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

		if allMatch {
			isBetterMatch := false

			if matchCount > bestMatchCount {
				isBetterMatch = true
			} else if matchCount == bestMatchCount {
				if pref.Priority > bestPriority {
					isBetterMatch = true
				} else if pref.Priority == bestPriority {
					if bestMatch == nil || pref.CreatedAt.Before(bestMatch.CreatedAt) {
						isBetterMatch = true
					}
				}
			}

			if isBetterMatch {
				bestMatch = &ColorMatch{
					Color:              pref.Color,
					ColorType:          pref.ColorType,
					Priority:           pref.Priority,
					BgLightnessFactor:  pref.BgLightnessFactor,
					TextDarknessFactor: pref.TextDarknessFactor,
					CreatedAt:          pref.CreatedAt,
				}
				bestMatchCount = matchCount
				bestPriority = pref.Priority
			}
		}
	}

	if bestMatch == nil {
		if match, exists := cache.LookupMap["*"]; exists {
			return match
		}
	}

	return bestMatch
}

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

func (cs *ColorService) applyCustomColor(match *ColorMatch, alert *models.Alert) *AlertColorResult {
	baseColor := match.Color

	switch match.ColorType {
	case "tailwind":
		return &AlertColorResult{
			BackgroundColor: baseColor + "-100",
			TextColor:       baseColor + "-800",
			BorderColor:     baseColor + "-500",
			BadgeColor:      baseColor + "-500",
			ColorSource:     "user",
		}
	case "severity":
		return cs.getDefaultSeverityColors(alert)
	default:
		bgLightness := match.BgLightnessFactor
		if bgLightness < 0 {
			bgLightness = 0.9
		}

		textDarkness := match.TextDarknessFactor
		if textDarkness < 0 {
			textDarkness = 0.3
		}

		return &AlertColorResult{
			BackgroundColor: cs.lightenColor(baseColor, bgLightness),
			TextColor:       cs.darkenColor(baseColor, textDarkness),
			BorderColor:     baseColor,
			BadgeColor:      baseColor,
			ColorSource:     "user",
		}
	}
}

func (cs *ColorService) lightenColor(hexColor string, factor float64) string {
	return hexColor + fmt.Sprintf("%02x", int(255*(1-factor)))
}

func (cs *ColorService) darkenColor(hexColor string, factor float64) string {
	if !strings.HasPrefix(hexColor, "#") || len(hexColor) != 7 {
		return hexColor
	}

	rHex := hexColor[1:3]
	gHex := hexColor[3:5]
	bHex := hexColor[5:7]

	r, err1 := strconv.ParseInt(rHex, 16, 64)
	g, err2 := strconv.ParseInt(gHex, 16, 64)
	b, err3 := strconv.ParseInt(bHex, 16, 64)

	if err1 != nil || err2 != nil || err3 != nil {
		return hexColor
	}

	r = int64(float64(r) * (1 - factor))
	g = int64(float64(g) * (1 - factor))
	b = int64(float64(b) * (1 - factor))

	if r < 0 {
		r = 0
	}
	if g < 0 {
		g = 0
	}
	if b < 0 {
		b = 0
	}
	if r > 255 {
		r = 255
	}
	if g > 255 {
		g = 255
	}
	if b > 255 {
		b = 255
	}

	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

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
