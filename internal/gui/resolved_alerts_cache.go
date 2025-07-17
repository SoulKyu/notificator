package gui

import (
	"sync"
	"time"
	
	"notificator/internal/models"
)

type ResolvedAlert struct {
	Alert      models.Alert
	ResolvedAt time.Time
	ExpiresAt  time.Time
}

type ResolvedAlertsCache struct {
	alerts     map[string]*ResolvedAlert
	mutex      sync.RWMutex
	defaultTTL time.Duration
}

func NewResolvedAlertsCache(defaultTTL time.Duration) *ResolvedAlertsCache {
	cache := &ResolvedAlertsCache{
		alerts:     make(map[string]*ResolvedAlert),
		defaultTTL: defaultTTL,
	}
	go cache.startCleanupRoutine()
	return cache
}

func (rac *ResolvedAlertsCache) Add(alert models.Alert) {
	rac.mutex.Lock()
	defer rac.mutex.Unlock()
	
	fingerprint := alert.GetFingerprint()
	now := time.Now()
	alert.Status.State = "resolved"
	
	rac.alerts[fingerprint] = &ResolvedAlert{
		Alert:      alert,
		ResolvedAt: now,
		ExpiresAt:  now.Add(rac.defaultTTL),
	}
}

func (rac *ResolvedAlertsCache) GetResolvedAlerts() []ResolvedAlert {
	rac.mutex.RLock()
	defer rac.mutex.RUnlock()
	
	var resolved []ResolvedAlert
	now := time.Now()
	
	for _, alert := range rac.alerts {
		if now.Before(alert.ExpiresAt) {
			resolved = append(resolved, *alert)
		}
	}
	
	return resolved
}

func (rac *ResolvedAlertsCache) Get(fingerprint string) (*ResolvedAlert, bool) {
	rac.mutex.RLock()
	defer rac.mutex.RUnlock()
	
	alert, exists := rac.alerts[fingerprint]
	if !exists || time.Now().After(alert.ExpiresAt) {
		return nil, false
	}
	
	return alert, true
}

func (rac *ResolvedAlertsCache) Remove(fingerprint string) {
	rac.mutex.Lock()
	defer rac.mutex.Unlock()
	
	delete(rac.alerts, fingerprint)
}

func (rac *ResolvedAlertsCache) GetCount() int {
	rac.mutex.RLock()
	defer rac.mutex.RUnlock()
	
	count := 0
	now := time.Now()
	
	for _, alert := range rac.alerts {
		if now.Before(alert.ExpiresAt) {
			count++
		}
	}
	
	return count
}

func (rac *ResolvedAlertsCache) UpdateTTL(newTTL time.Duration) {
	rac.mutex.Lock()
	defer rac.mutex.Unlock()
	rac.defaultTTL = newTTL
}

func (rac *ResolvedAlertsCache) startCleanupRoutine() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		rac.cleanupExpiredAlerts()
	}
}

func (rac *ResolvedAlertsCache) cleanupExpiredAlerts() {
	rac.mutex.Lock()
	defer rac.mutex.Unlock()
	
	now := time.Now()
	toRemove := []string{}
	
	for fingerprint, alert := range rac.alerts {
		if now.After(alert.ExpiresAt) {
			toRemove = append(toRemove, fingerprint)
		}
	}
	
	for _, fingerprint := range toRemove {
		delete(rac.alerts, fingerprint)
	}
}

// Clear removes all resolved alerts from cache
func (rac *ResolvedAlertsCache) Clear() {
	rac.mutex.Lock()
	defer rac.mutex.Unlock()
	
	rac.alerts = make(map[string]*ResolvedAlert)
}