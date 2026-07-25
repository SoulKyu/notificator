package services

import (
	"regexp"
	"testing"
	"time"

	"notificator/internal/backend/models"
)

func TestColorServiceSweepExpired(t *testing.T) {
	cs := NewColorService(nil)
	cs.colorCache["stale"] = &ColorPreferenceCache{CachedAt: time.Now().Add(-10 * time.Minute), TTL: cs.cacheTTL}
	cs.colorCache["fresh"] = &ColorPreferenceCache{CachedAt: time.Now(), TTL: cs.cacheTTL}

	cs.cacheMutex.Lock()
	cs.sweepExpiredLocked()
	cs.cacheMutex.Unlock()

	if _, ok := cs.colorCache["stale"]; ok {
		t.Error("expired entry should have been swept")
	}
	if _, ok := cs.colorCache["fresh"]; !ok {
		t.Error("fresh entry should have been kept")
	}
}

func TestHiddenAlertsServiceSweepIdleSessions(t *testing.T) {
	s := NewHiddenAlertsService(nil)
	for session, last := range map[string]time.Time{
		"idle":   time.Now().Add(-sessionIdleTTL - time.Hour),
		"active": time.Now(),
	} {
		s.userHiddenAlerts[session] = map[string]bool{"fp": true}
		s.userHiddenRules[session] = []models.UserHiddenRule{{ID: "r1"}}
		s.compiledRegexRules[session] = map[string]*regexp.Regexp{"r1": regexp.MustCompile("x")}
		s.lastAccess[session] = last
	}

	s.mu.Lock()
	s.sweepIdleSessionsLocked()
	s.mu.Unlock()

	if _, ok := s.userHiddenAlerts["idle"]; ok {
		t.Error("idle session should have been swept from userHiddenAlerts")
	}
	if _, ok := s.userHiddenRules["idle"]; ok {
		t.Error("idle session should have been swept from userHiddenRules")
	}
	if _, ok := s.compiledRegexRules["idle"]; ok {
		t.Error("idle session should have been swept from compiledRegexRules")
	}
	if _, ok := s.lastAccess["idle"]; ok {
		t.Error("idle session should have been swept from lastAccess")
	}
	if _, ok := s.userHiddenAlerts["active"]; !ok {
		t.Error("active session should have been kept")
	}
}

// The nil backend client is the assertion: any gRPC fetch dereferences it and
// panics, so "did not panic" means the cached snapshot was served.
func TestHiddenAlertsServiceLoadUserDataServesCacheWithinTTL(t *testing.T) {
	s := NewHiddenAlertsService(nil)
	s.userHiddenAlerts["sess"] = map[string]bool{"fp": true}
	s.lastAccess["sess"] = time.Now()

	if err := s.LoadUserData("sess"); err != nil {
		t.Fatalf("LoadUserData: %v", err)
	}
}

func TestHiddenAlertsServiceLoadUserDataRefetchesWhenStale(t *testing.T) {
	for name, prime := range map[string]func(s *HiddenAlertsService){
		"expired": func(s *HiddenAlertsService) {
			s.userHiddenAlerts["sess"] = map[string]bool{"fp": true}
			s.lastAccess["sess"] = time.Now().Add(-2 * s.cacheTTL)
		},
		"cold": func(s *HiddenAlertsService) {},
		"invalidated": func(s *HiddenAlertsService) {
			s.userHiddenAlerts["sess"] = map[string]bool{"fp": true}
			s.lastAccess["sess"] = time.Now()
			s.InvalidateCache("sess")
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a backend fetch attempt")
				}
			}()
			s := NewHiddenAlertsService(nil)
			prime(s)
			_ = s.LoadUserData("sess")
		})
	}
}

func TestHiddenAlertsServiceInvalidateCacheRemovesAllMaps(t *testing.T) {
	s := NewHiddenAlertsService(nil)
	s.userHiddenAlerts["sess"] = map[string]bool{"fp": true}
	s.userHiddenRules["sess"] = []models.UserHiddenRule{{ID: "r1"}}
	s.compiledRegexRules["sess"] = map[string]*regexp.Regexp{"r1": regexp.MustCompile("x")}
	s.lastAccess["sess"] = time.Now()

	s.InvalidateCache("sess")

	if len(s.userHiddenAlerts)+len(s.userHiddenRules)+len(s.compiledRegexRules)+len(s.lastAccess) != 0 {
		t.Error("InvalidateCache should remove the session from all maps")
	}
}
