package services

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"notificator/internal/backend/models"
	alertpb "notificator/internal/backend/proto/alert"
	webuimodels "notificator/internal/webui/models"
)

// stubBackend implements only the two fetch methods LoadUserData needs; the
// embedded nil interface panics if anything else is called.
type stubBackend struct {
	hiddenAlertsBackend
	alerts  []*alertpb.UserHiddenAlert
	err     error
	calls   int
	onFetch func()
}

func (b *stubBackend) GetUserHiddenAlerts(string, ...string) ([]*alertpb.UserHiddenAlert, error) {
	b.calls++
	if b.onFetch != nil {
		b.onFetch()
	}
	return b.alerts, b.err
}

func (b *stubBackend) GetUserHiddenRules(string, ...string) ([]*alertpb.UserHiddenRule, error) {
	return nil, b.err
}

func (b *stubBackend) ClearAllHiddenAlerts(string, ...string) error { return nil }

func (b *stubBackend) HideAlert(string, string, string, string, string, ...string) error { return nil }

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

// A failed fetch must not be cached as fresh, or a transient backend outage
// would blank a session's hidden alerts for a full TTL.
func TestHiddenAlertsServiceLoadUserDataRetriesAfterFailedFetch(t *testing.T) {
	backend := &stubBackend{err: errors.New("backend unavailable")}
	s := NewHiddenAlertsService(backend)

	for range 2 {
		if err := s.LoadUserData("sess"); err != nil {
			t.Fatalf("LoadUserData: %v", err)
		}
	}

	if backend.calls != 2 {
		t.Errorf("failed fetch should stay refetchable: got %d backend calls, want 2", backend.calls)
	}
}

// An invalidation landing mid-fetch must discard the in-flight snapshot instead
// of republishing pre-mutation state and marking it fresh for a TTL.
func TestHiddenAlertsServiceLoadUserDataDropsSnapshotInvalidatedMidFetch(t *testing.T) {
	backend := &stubBackend{alerts: []*alertpb.UserHiddenAlert{{Fingerprint: "stale"}}}
	s := NewHiddenAlertsService(backend)
	backend.onFetch = func() { s.InvalidateCache("sess") }

	if err := s.LoadUserData("sess"); err != nil {
		t.Fatalf("LoadUserData: %v", err)
	}

	if _, ok := s.userHiddenAlerts["sess"]; ok {
		t.Error("snapshot predating the mid-fetch invalidation should not be published")
	}
	if time.Since(s.lastAccess["sess"]) < s.cacheTTL {
		t.Error("discarded snapshot should leave the entry stale, not fresh")
	}
}

// Clear-all is a mutation like any other: an in-flight fetch that predates it
// must not republish the pre-clear fingerprint set for a full TTL.
func TestHiddenAlertsServiceLoadUserDataDropsSnapshotClearedMidFetch(t *testing.T) {
	backend := &stubBackend{alerts: []*alertpb.UserHiddenAlert{{Fingerprint: "stale"}}}
	s := NewHiddenAlertsService(backend)
	backend.onFetch = func() { _ = s.ClearAllHiddenAlerts("sess") }

	if err := s.LoadUserData("sess"); err != nil {
		t.Fatalf("LoadUserData: %v", err)
	}

	if _, ok := s.userHiddenAlerts["sess"]; ok {
		t.Error("snapshot predating the mid-fetch clear-all should not be published")
	}
}

// An impersonated mutation targets another user's hidden set, so it must not be
// written into the acting session's snapshot.
func TestHiddenAlertsServiceHideAlertIgnoresCacheWhenImpersonating(t *testing.T) {
	s := NewHiddenAlertsService(&stubBackend{})
	s.userHiddenAlerts["sess"] = map[string]bool{}
	s.lastAccess["sess"] = time.Now()

	if err := s.HideAlert("sess", &webuimodels.DashboardAlert{Fingerprint: "other-user-fp"}, "", "impersonated-user"); err != nil {
		t.Fatalf("HideAlert: %v", err)
	}

	if len(s.userHiddenAlerts["sess"]) != 0 {
		t.Error("impersonated hide should not touch the acting session's snapshot")
	}
	if s.generation["sess"] != 0 {
		t.Error("impersonated hide should not bump the acting session's generation")
	}
}

func TestHiddenAlertsServiceInvalidateCacheRemovesAllMaps(t *testing.T) {
	s := NewHiddenAlertsService(nil)
	s.userHiddenAlerts["sess"] = map[string]bool{"fp": true}
	s.userHiddenRules["sess"] = []models.UserHiddenRule{{ID: "r1"}}
	s.compiledRegexRules["sess"] = map[string]*regexp.Regexp{"r1": regexp.MustCompile("x")}
	s.lastAccess["sess"] = time.Now()

	s.InvalidateCache("sess")

	if len(s.userHiddenAlerts)+len(s.userHiddenRules)+len(s.compiledRegexRules) != 0 {
		t.Error("InvalidateCache should remove the session's cached data")
	}
	// lastAccess is kept but backdated, so the idle sweep can still reclaim the
	// generation counter for sessions whose last action was an invalidation.
	if time.Since(s.lastAccess["sess"]) < s.cacheTTL {
		t.Error("InvalidateCache should leave lastAccess stale, not fresh")
	}
}
