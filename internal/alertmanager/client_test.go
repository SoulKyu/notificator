package alertmanager

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"notificator/config"
)

// alertmanagerStub serves a single alert named after the instance, after delay.
func alertmanagerStub(t *testing.T, name string, delay time.Duration) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"fingerprint":"fp-%s","labels":{"alertname":"%s"},"status":{"state":"active"}}]`, name, name)
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
	const delay = 300 * time.Millisecond

	tests := []struct {
		name    string
		sources int
	}{
		{name: "two sources", sources: 2},
		{name: "five sources", sources: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			urls := make(map[string]string, tc.sources)
			for i := 0; i < tc.sources; i++ {
				name := fmt.Sprintf("am-%d", i)
				urls[name] = alertmanagerStub(t, name, delay).URL
			}

			start := time.Now()
			alerts, failed := multiClientFor(urls).FetchAllAlertsDetailed()
			elapsed := time.Since(start)

			if len(failed) != 0 {
				t.Fatalf("unexpected failures: %v", failed)
			}
			if len(alerts) != tc.sources {
				t.Fatalf("got %d alerts, want %d", len(alerts), tc.sources)
			}
			// Serial would be sources*delay; concurrent stays near one delay.
			if max := time.Duration(float64(delay) * 1.8); elapsed > max {
				t.Fatalf("fan-out took %s, want under %s (serial would be %s)", elapsed, max, time.Duration(tc.sources)*delay)
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

	mc := multiClientFor(urls)
	for _, nc := range mc.snapshotClients() {
		if nc.name == "dead" {
			nc.client.HTTPClient.Timeout = 500 * time.Millisecond
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
	// Bounded by the single timeout, not timeout + healthy latencies.
	if elapsed > 900*time.Millisecond {
		t.Fatalf("fan-out took %s, want it bounded by the dead source timeout", elapsed)
	}
}

func TestFanOutDoesNotHoldMutexAcrossHTTP(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		fmt.Fprint(w, `[]`)
	}))
	defer slow.Close()

	mc := multiClientFor(map[string]string{"slow": slow.URL})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mc.FetchAllAlertsDetailed()
	}()

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

	close(release)
	wg.Wait()
}

func TestTestConnectionDrainsBody(t *testing.T) {
	var filter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = r.URL.Query().Get("filter")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	if err := client.TestConnection(); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if filter != healthCheckFilter {
		t.Fatalf("health probe filter = %q, want %q", filter, healthCheckFilter)
	}

	// A drained + closed body is reused, so the second probe rides the same connection.
	if err := client.TestConnection(); err != nil {
		t.Fatalf("second TestConnection: %v", err)
	}
}

func TestGetHealthStatusConcurrent(t *testing.T) {
	const delay = 300 * time.Millisecond

	urls := map[string]string{}
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("am-%d", i)
		urls[name] = alertmanagerStub(t, name, delay).URL
	}
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer broken.Close()
	urls["broken"] = broken.URL

	mc := multiClientFor(urls)

	start := time.Now()
	status := mc.GetHealthStatus()
	elapsed := time.Since(start)

	if len(status) != len(urls) {
		t.Fatalf("got %d statuses, want %d", len(status), len(urls))
	}
	if status["broken"] {
		t.Fatal("broken source reported healthy")
	}
	if elapsed > time.Duration(float64(delay)*1.8) {
		t.Fatalf("health fan-out took %s, want near %s", elapsed, delay)
	}

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
