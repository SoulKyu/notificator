package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"notificator/config"
	"notificator/internal/models"
)

// alertWithSentryURL builds the minimal alert the Sentry data path consumes.
func alertWithSentryURL(sentryURL string) *models.Alert {
	return &models.Alert{
		Labels:      map[string]string{"alertname": "SentryProbe"},
		Annotations: map[string]string{"sentry": sentryURL},
	}
}

// The sentry annotation is attacker-influenceable alert payload: a URL pointing
// anywhere but the configured instance must produce NO outbound request at all —
// every request carries the viewer's bearer token.
func TestSentryDataRefusesForeignHosts(t *testing.T) {
	var attackerHits atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerHits.Add(1)
	}))
	defer attacker.Close()

	service := NewSentryService(&config.SentryConfig{
		Enabled:     true,
		BaseURL:     "https://sentry.example.com",
		GlobalToken: "super-secret-token",
	}, nil)

	urls := map[string]string{
		"foreign host":      attacker.URL + "/organizations/org/projects/proj/?project=39",
		"scheme downgrade":  "http://sentry.example.com/organizations/org/projects/proj/",
		"non-http scheme":   "ftp://sentry.example.com/organizations/org/projects/proj/",
		"label fallback": "", // filled below, exercises the Labels path
	}

	for name, sentryURL := range urls {
		t.Run(name, func(t *testing.T) {
			var alert *models.Alert
			if name == "label fallback" {
				alert = &models.Alert{
					Labels: map[string]string{"sentry": attacker.URL + "/organizations/org/projects/proj/"},
				}
			} else {
				alert = alertWithSentryURL(sentryURL)
			}

			data := service.GetSentryDataForAlert(alert, "user-1", "session-1")

			if !data.HasSentryLabel {
				t.Fatal("HasSentryLabel should stay true so the modal can explain the refusal")
			}
			if !strings.Contains(data.Error, "not allowed") {
				t.Fatalf("Error = %q, want an explicit host-not-allowed message", data.Error)
			}
		})
	}

	if hits := attackerHits.Load(); hits != 0 {
		t.Fatalf("attacker host received %d request(s); the token left the building", hits)
	}
}

// The configured instance keeps working: requests flow, and only to that host.
func TestSentryDataAllowsConfiguredHost(t *testing.T) {
	var hits atomic.Int32
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer super-secret-token" {
			t.Errorf("Authorization = %q, want the configured bearer token", got)
		}
		w.Write([]byte(`[]`))
	}))
	defer instance.Close()

	service := NewSentryService(&config.SentryConfig{
		Enabled:     true,
		BaseURL:     instance.URL,
		GlobalToken: "super-secret-token",
	}, nil)

	data := service.GetSentryDataForAlert(
		alertWithSentryURL(instance.URL+"/organizations/org/projects/proj/?project=39"),
		"user-1", "session-1")

	if strings.Contains(data.Error, "not allowed") {
		t.Fatalf("configured host was refused: %s", data.Error)
	}
	if hits.Load() == 0 {
		t.Fatal("no request reached the configured instance — the positive control failed")
	}
}
