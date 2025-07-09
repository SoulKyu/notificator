package gui

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"notificator/internal/models"
)

// HiddenAlertsCache manages the persistence of hidden alerts
type HiddenAlertsCache struct {
	filePath     string
	hiddenAlerts map[string]HiddenAlertInfo
	mutex        sync.RWMutex
}

// HiddenAlertInfo stores information about a hidden alert
type HiddenAlertInfo struct {
	AlertName string            `json:"alert_name"`
	Instance  string            `json:"instance"`
	Labels    map[string]string `json:"labels"`
	HiddenAt  time.Time         `json:"hidden_at"`
	HiddenBy  string            `json:"hidden_by"`
	Reason    string            `json:"reason,omitempty"`
}

// NewHiddenAlertsCache creates a new hidden alerts cache
func NewHiddenAlertsCache(configPath string) *HiddenAlertsCache {
	cache := &HiddenAlertsCache{
		filePath:     getHiddenAlertsCachePath(configPath),
		hiddenAlerts: make(map[string]HiddenAlertInfo),
	}

	// Load existing hidden alerts
	cache.load()

	return cache
}

// getHiddenAlertsCachePath returns the path for the hidden alerts cache file
func getHiddenAlertsCachePath(configPath string) string {
	if configPath == "" {
		return "hidden_alerts.json"
	}

	dir := filepath.Dir(configPath)
	return filepath.Join(dir, "hidden_alerts.json")
}

// generateAlertKey creates a unique key for an alert based on its identifying characteristics
func (cache *HiddenAlertsCache) generateAlertKey(alert models.Alert) string {
	// Use alertname + instance as the primary key
	// This allows hiding specific instances of an alert
	alertName := alert.GetAlertName()
	instance := alert.GetInstance()

	// For alerts without instance, use alertname + a hash of key labels
	if instance == "unknown" || instance == "" {
		// Include some key labels to make the key more specific
		job := ""
		if jobLabel, exists := alert.Labels["job"]; exists {
			job = jobLabel
		}
		return fmt.Sprintf("%s::%s", alertName, job)
	}

	return fmt.Sprintf("%s::%s", alertName, instance)
}

// IsHidden checks if an alert is hidden
func (cache *HiddenAlertsCache) IsHidden(alert models.Alert) bool {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	key := cache.generateAlertKey(alert)
	_, exists := cache.hiddenAlerts[key]
	return exists
}

// HideAlert adds an alert to the hidden list
func (cache *HiddenAlertsCache) HideAlert(alert models.Alert, reason string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	key := cache.generateAlertKey(alert)

	hiddenInfo := HiddenAlertInfo{
		AlertName: alert.GetAlertName(),
		Instance:  alert.GetInstance(),
		Labels:    alert.Labels,
		HiddenAt:  time.Now(),
		HiddenBy:  "notificator-user", // Could be made configurable
		Reason:    reason,
	}

	cache.hiddenAlerts[key] = hiddenInfo

	// Save to file
	return cache.save()
}

// UnhideAlert removes an alert from the hidden list
func (cache *HiddenAlertsCache) UnhideAlert(alert models.Alert) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	key := cache.generateAlertKey(alert)
	delete(cache.hiddenAlerts, key)

	// Save to file
	return cache.save()
}

// GetHiddenAlerts returns a list of all hidden alerts
func (cache *HiddenAlertsCache) GetHiddenAlerts() []HiddenAlertInfo {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	var hiddenList []HiddenAlertInfo
	for _, info := range cache.hiddenAlerts {
		hiddenList = append(hiddenList, info)
	}

	return hiddenList
}

// ClearAll removes all hidden alerts
func (cache *HiddenAlertsCache) ClearAll() error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cache.hiddenAlerts = make(map[string]HiddenAlertInfo)

	// Save to file
	return cache.save()
}

// GetHiddenCount returns the number of hidden alerts
func (cache *HiddenAlertsCache) GetHiddenCount() int {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	return len(cache.hiddenAlerts)
}

// load reads hidden alerts from the cache file
func (cache *HiddenAlertsCache) load() {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	// Check if file exists
	if _, err := os.Stat(cache.filePath); os.IsNotExist(err) {
		// File doesn't exist, start with empty cache
		log.Printf("Hidden alerts cache file doesn't exist, starting fresh: %s", cache.filePath)
		return
	}

	// Read file
	data, err := os.ReadFile(cache.filePath)
	if err != nil {
		log.Printf("Failed to read hidden alerts cache: %v", err)
		return
	}

	// Parse JSON
	if err := json.Unmarshal(data, &cache.hiddenAlerts); err != nil {
		log.Printf("Failed to parse hidden alerts cache: %v", err)
		// Reset to empty cache on parse error
		cache.hiddenAlerts = make(map[string]HiddenAlertInfo)
		return
	}

	log.Printf("Loaded %d hidden alerts from cache", len(cache.hiddenAlerts))
}

// save writes hidden alerts to the cache file
func (cache *HiddenAlertsCache) save() error {
	// Ensure directory exists
	dir := filepath.Dir(cache.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(cache.hiddenAlerts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hidden alerts: %w", err)
	}

	// Write to file
	if err := os.WriteFile(cache.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write hidden alerts cache: %w", err)
	}

	log.Printf("Saved %d hidden alerts to cache", len(cache.hiddenAlerts))
	return nil
}

// CleanupExpired removes hidden alerts for alerts that haven't been seen in a while
// This is optional - you might want to keep hidden alerts indefinitely
func (cache *HiddenAlertsCache) CleanupExpired(maxAge time.Duration) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var removed int

	for key, info := range cache.hiddenAlerts {
		if info.HiddenAt.Before(cutoff) {
			delete(cache.hiddenAlerts, key)
			removed++
		}
	}

	if removed > 0 {
		log.Printf("Cleaned up %d expired hidden alerts", removed)
		return cache.save()
	}

	return nil
}
