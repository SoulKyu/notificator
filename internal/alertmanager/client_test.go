package alertmanager

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"notificator/config"
)

func alertJSON(name string) string {
	return fmt.Sprintf(`[{"fingerprint":"fp-%s","labels":{"alertname":"%s"},"status":{"state":"active"}}]`, name, name)
}

// alertmanagerStub serves a single alert named after the instance, after delay.
func alertmanagerStub(t *testing.T, name string, delay time.Duration) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, alertJSON(name))
	}))
	t.Cleanup(srv.Close)

	return srv
}

// barrierStub only answers once every peer stub is also serving, so a fan-out
// that regressed to a sequential loop deadlocks on the first source and fails
// on its client timeout instead of merely being slow.
func barrierStub(t *testing.T, name string, barrier *sync.WaitGroup) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		barrier.Done()
		barrier.Wait()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, alertJSON(name))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func multiClientFor(urls map[string]string) *MultiClient {
	cfg := &config.Config{}
	for name, url := range urls {
		cfg.Alertmanagers = append(cfg.Alertmanagers, config.AlertmanagerConfig{Name: name, URL: url})
	}

	return NewMultiClient(cfg)
}

func TestFetchAllAlertsDetailedIsConcurrent(t *testing.T) {
	tests := []struct {
		name    string
		sources int
	}{
		{name: "two sources", sources: 2},
		{name: "five sources", sources: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Every source must be in flight at once for any of them to answer,
			// so this asserts observed concurrency rather than wall-clock.
			var barrier sync.WaitGroup
			barrier.Add(tc.sources)

			urls := make(map[string]string, tc.sources)
			for i := 0; i < tc.sources; i++ {
				name := fmt.Sprintf("am-%d", i)
				urls[name] = barrierStub(t, name, &barrier).URL
			}

			alerts, failed := multiClientFor(urls).FetchAllAlertsDetailed()

			if len(failed) != 0 {
				t.Fatalf("sources did not run concurrently (a serial fan-out stalls on the barrier): %v", failed)
			}
			if len(alerts) != tc.sources {
				t.Fatalf("got %d alerts, want %d", len(alerts), tc.sources)
			}
		})
	}
}

func TestFetchAllAlertsDetailedPartialFailure(t *testing.T) {
	// A source whose handler outlives the client timeout stands in for an
	// unreachable Alertmanager.
	hang := make(chan struct{})
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-hang
	}))
	defer func() {
		close(hang)
		dead.Close()
	}()

	urls := map[string]string{
		"healthy-a": alertmanagerStub(t, "healthy-a", 50*time.Millisecond).URL,
		"healthy-b": alertmanagerStub(t, "healthy-b", 50*time.Millisecond).URL,
		"dead":      dead.URL,
	}

	const deadTimeout = 500 * time.Millisecond

	mc := multiClientFor(urls)
	for _, nc := range mc.snapshotClients() {
		if nc.name == "dead" {
			nc.client.HTTPClient.Timeout = deadTimeout
		}
	}

	start := time.Now()
	alerts, failed := mc.FetchAllAlertsDetailed()
	elapsed := time.Since(start)

	if len(alerts) != 2 {
		t.Fatalf("got %d alerts, want the 2 healthy ones", len(alerts))
	}
	for _, a := range alerts {
		if a.Source == "dead" {
			t.Fatalf("dead source contributed alerts: %+v", a)
		}
	}
	if _, ok := failed["dead"]; !ok {
		t.Fatalf("dead source missing from failedSources: %v", failed)
	}
	if len(failed) != 1 {
		t.Fatalf("unexpected failures: %v", failed)
	}
	// Bounded by the single timeout, not timeout + healthy latencies. The bound
	// is derived from the timeout so the intent stays "did not serialise" rather
	// than a hand-tuned number that a slow runner can trip.
	if elapsed > 2*deadTimeout {
		t.Fatalf("fan-out took %s, want it bounded by the dead source timeout %s", elapsed, deadTimeout)
	}
}

func TestFanOutDoesNotHoldMutexAcrossHTTP(t *testing.T) {
	release := make(chan struct{})
	// serving proves the fan-out is inside the HTTP call before the writer races
	// it; without it the write lock may be taken before the fetch ever starts and
	// the test passes regardless of where the mutex is held.
	serving := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(serving)
		<-release
		fmt.Fprint(w, `[]`)
	}))
	defer slow.Close()

	mc := multiClientFor(map[string]string{"slow": slow.URL})

	// Deferred so a t.Fatal below still unblocks the handler, letting the
	// deferred slow.Close() return; otherwise a regression reads as a hung
	// package instead of a failed assertion.
	defer close(release)

	go mc.FetchAllAlertsDetailed()

	select {
	case <-serving:
	case <-time.After(5 * time.Second):
		t.Fatal("fan-out never reached the HTTP handler")
	}

	// UpdateFromConfig needs the write lock; it must not wait on the in-flight fetch.
	updated := make(chan struct{})
	go func() {
		mc.UpdateFromConfig(&config.Config{})
		close(updated)
	}()

	select {
	case <-updated:
	case <-time.After(2 * time.Second):
		t.Fatal("UpdateFromConfig blocked behind an in-flight fan-out")
	}
}

func TestTestConnectionUsesHealthFilterAndReusesConnection(t *testing.T) {
	var mu sync.Mutex
	var filter string
	var addrs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		filter = r.URL.Query().Get("filter")
		addrs = append(addrs, r.RemoteAddr)
		mu.Unlock()
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	if err := client.TestConnection(); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if err := client.TestConnection(); err != nil {
		t.Fatalf("second TestConnection: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if filter != healthCheckFilter {
		t.Fatalf("health probe filter = %q, want %q", filter, healthCheckFilter)
	}
	if len(addrs) != 2 {
		t.Fatalf("got %d probes, want 2: %v", len(addrs), addrs)
	}
	// Same client port means the drained body put the connection back in the
	// keep-alive pool; dropping the drain makes the transport dial a fresh one.
	if addrs[0] != addrs[1] {
		t.Fatalf("probe dialled a new connection instead of reusing one: %v", addrs)
	}
}

func TestTestConnectionBoundsDrainOfIgnoredFilter(t *testing.T) {
	// A backend that silently ignores the filter answers with the full alert set.
	// The probe must stop reading at healthCheckDrainLimit, which is observable:
	// an incompletely read body cannot go back to the keep-alive pool, so the
	// second probe dials a new connection. An unbounded drain would reuse it.
	body := strings.Repeat("x", 64*healthCheckDrainLimit)

	var mu sync.Mutex
	var addrs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		addrs = append(addrs, r.RemoteAddr)
		mu.Unlock()
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	for i := 0; i < 2; i++ {
		if err := client.TestConnection(); err != nil {
			t.Fatalf("TestConnection %d: %v", i, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(addrs) != 2 {
		t.Fatalf("got %d probes, want 2: %v", len(addrs), addrs)
	}
	if addrs[0] == addrs[1] {
		t.Fatalf("probe drained the whole %d-byte payload instead of stopping at %d bytes", len(body), healthCheckDrainLimit)
	}
}

func TestGetHealthStatusConcurrent(t *testing.T) {
	const probed = 4

	// Same barrier trick as the fetch fan-out: every probed source must be in
	// flight at once for any of them to answer, so a serial regression stalls
	// on the client timeout instead of merely being slow.
	var barrier sync.WaitGroup
	barrier.Add(probed)

	urls := map[string]string{}
	for i := 0; i < probed; i++ {
		name := fmt.Sprintf("am-%d", i)
		urls[name] = barrierStub(t, name, &barrier).URL
	}
	// The broken source answers immediately and must not join the barrier.
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer broken.Close()
	urls["broken"] = broken.URL

	mc := multiClientFor(urls)

	status := mc.GetHealthStatus()

	if len(status) != len(urls) {
		t.Fatalf("got %d statuses, want %d", len(status), len(urls))
	}
	if status["broken"] {
		t.Fatal("broken source reported healthy")
	}
	for name := range status {
		if name != "broken" && !status[name] {
			t.Fatalf("probes did not run concurrently (a serial fan-out stalls on the barrier): %v", status)
		}
	}

	// GetHealthyClients probes a second round; every Wait above has returned by
	// now, so the WaitGroup is safe to re-arm.
	barrier.Add(probed)
	healthy := mc.GetHealthyClients()
	if len(healthy) != 4 {
		t.Fatalf("got %d healthy clients, want 4: %v", len(healthy), healthy)
	}
	for name, client := range healthy {
		if client == nil || client.BaseURL != urls[name] {
			t.Fatalf("healthy client %q maps to the wrong instance: %+v", name, client)
		}
	}
}
