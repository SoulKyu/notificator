package services

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	alertpb "notificator/internal/backend/proto/alert"
	"notificator/internal/models"
)

type stubColorFetcher struct {
	mu      sync.Mutex
	calls   int
	delay   time.Duration
	err     error
	inFetch chan string   // if set, receives sessionID when a fetch starts
	release chan struct{} // if set, fetch blocks until closed
}

func (s *stubColorFetcher) GetUserColorPreferences(sessionID string, _ ...string) ([]*alertpb.UserColorPreference, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.inFetch != nil {
		s.inFetch <- sessionID
	}
	if s.release != nil {
		<-s.release
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return nil, s.err
}

func (s *stubColorFetcher) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newTestColorService(fetcher colorPreferencesFetcher) *ColorService {
	cs := NewColorService(nil)
	cs.backendClient = fetcher
	return cs
}

// Distinct cold sessions must fetch in parallel, not serialize behind the
// process-wide lock: 10 concurrent 200ms fetches ≈ 200ms, not 2s.
func TestColorCacheConcurrentMissesRunInParallel(t *testing.T) {
	stub := &stubColorFetcher{delay: 200 * time.Millisecond}
	cs := newTestColorService(stub)

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := cs.getUserColorCache(fmt.Sprintf("session-%d", i)); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Fatalf("10 concurrent misses took %v; fetches are serialized", elapsed)
	}
	if got := stub.callCount(); got != 10 {
		t.Fatalf("expected 10 backend calls, got %d", got)
	}
}

// A session with a fresh cache entry must not block behind another session's
// in-flight fetch.
func TestColorCacheFreshEntryNotBlockedByColdFetch(t *testing.T) {
	stub := &stubColorFetcher{
		inFetch: make(chan string, 1),
		release: make(chan struct{}),
	}
	cs := newTestColorService(stub)
	cs.colorCache["fresh"] = &ColorPreferenceCache{
		UserID:   "fresh",
		CachedAt: time.Now(),
		TTL:      cs.cacheTTL,
	}

	go cs.getUserColorCache("cold")
	<-stub.inFetch // cold fetch is now in flight, lock must not be held

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := cs.getUserColorCache("fresh"); err != nil {
			t.Errorf("fresh entry returned error: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("fresh session blocked behind another session's fetch")
	}
	close(stub.release)
}

// Failed fetches are negative-cached for 15s: repeated calls within the
// window hit the backend exactly once.
func TestColorCacheNegativeCaching(t *testing.T) {
	stub := &stubColorFetcher{err: errors.New("backend down")}
	cs := newTestColorService(stub)

	for i := 0; i < 5; i++ {
		if _, err := cs.getUserColorCache("user"); err == nil {
			t.Fatal("expected error")
		}
	}
	if got := stub.callCount(); got != 1 {
		t.Fatalf("expected 1 backend call within negative-cache window, got %d", got)
	}

	// Age the failure marker past the 15s window instead of sleeping.
	cs.cacheMutex.Lock()
	cs.colorCache["user"].CachedAt = time.Now().Add(-colorFetchErrTTL - time.Second)
	cs.cacheMutex.Unlock()

	if _, err := cs.getUserColorCache("user"); err == nil {
		t.Fatal("expected error")
	}
	if got := stub.callCount(); got != 2 {
		t.Fatalf("expected 2 backend calls after window expiry, got %d", got)
	}
}

// InvalidateUserCache clears a cached failure so the next call retries.
func TestInvalidateUserCacheClearsNegativeEntry(t *testing.T) {
	stub := &stubColorFetcher{err: errors.New("backend down")}
	cs := newTestColorService(stub)

	cs.getUserColorCache("user")
	cs.getUserColorCache("user")
	if got := stub.callCount(); got != 1 {
		t.Fatalf("expected 1 backend call, got %d", got)
	}

	cs.InvalidateUserCache("user")

	cs.getUserColorCache("user")
	if got := stub.callCount(); got != 2 {
		t.Fatalf("expected fresh RPC after invalidation, got %d calls", got)
	}
}

// On error, callers still degrade to default severity colors.
func TestGetAlertColorsFallsBackOnError(t *testing.T) {
	stub := &stubColorFetcher{err: errors.New("backend down")}
	cs := newTestColorService(stub)

	alert := &models.Alert{Labels: map[string]string{"severity": "critical"}}
	result := cs.GetAlertColors(alert, "user")
	if result == nil || result.ColorSource != "severity" {
		t.Fatalf("expected severity fallback colors, got %+v", result)
	}
}
