package handlers

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"notificator/internal/models"
	webuimodels "notificator/internal/webui/models"
)

func TestSilenceMatchesAlert(t *testing.T) {
	labels := map[string]string{
		"alertname": "KafkaLagHigh",
		"severity":  "critical",
		"instance":  "kafka-03",
	}

	tests := []struct {
		name     string
		matchers []models.SilenceMatcher
		want     bool
	}{
		{
			name:     "equal matcher hits",
			matchers: []models.SilenceMatcher{{Name: "severity", Value: "critical", IsEqual: true}},
			want:     true,
		},
		{
			name:     "equal matcher misses",
			matchers: []models.SilenceMatcher{{Name: "severity", Value: "warning", IsEqual: true}},
			want:     false,
		},
		{
			name:     "not-equal matcher hits",
			matchers: []models.SilenceMatcher{{Name: "severity", Value: "warning", IsEqual: false}},
			want:     true,
		},
		{
			name:     "not-equal matcher misses",
			matchers: []models.SilenceMatcher{{Name: "severity", Value: "critical", IsEqual: false}},
			want:     false,
		},
		{
			name:     "regex matcher hits",
			matchers: []models.SilenceMatcher{{Name: "instance", Value: "kafka-.*", IsRegex: true, IsEqual: true}},
			want:     true,
		},
		{
			name:     "regex is fully anchored",
			matchers: []models.SilenceMatcher{{Name: "instance", Value: "kafka", IsRegex: true, IsEqual: true}},
			want:     false,
		},
		{
			name:     "negated regex matcher hits",
			matchers: []models.SilenceMatcher{{Name: "instance", Value: "redis-.*", IsRegex: true, IsEqual: false}},
			want:     true,
		},
		{
			name: "all matchers must hold",
			matchers: []models.SilenceMatcher{
				{Name: "alertname", Value: "KafkaLagHigh", IsEqual: true},
				{Name: "severity", Value: "warning", IsEqual: true},
			},
			want: false,
		},
		{
			name: "multi matcher hits",
			matchers: []models.SilenceMatcher{
				{Name: "alertname", Value: "KafkaLagHigh", IsEqual: true},
				{Name: "severity", Value: "critical", IsEqual: true},
				{Name: "instance", Value: "kafka-0[0-9]", IsRegex: true, IsEqual: true},
			},
			want: true,
		},
		{
			name:     "missing label is matched as empty string",
			matchers: []models.SilenceMatcher{{Name: "team", Value: "", IsEqual: true}},
			want:     true,
		},
		{
			name:     "invalid regex never matches",
			matchers: []models.SilenceMatcher{{Name: "instance", Value: "kafka-([", IsRegex: true, IsEqual: true}},
			want:     false,
		},
		{
			name:     "silence without matchers matches nothing",
			matchers: nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			silence := models.Silence{Matchers: tt.matchers}
			if got := silenceMatchesAlert(silence, labels); got != tt.want {
				t.Errorf("silenceMatchesAlert() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A silence lives on one Alertmanager and must never claim identically-labelled alerts
// scraped from another — that guard drives both the matched count and the
// "Suppressing nothing" marker in a multi-Alertmanager setup.
func TestCountMatchedAlertsIsScopedToSource(t *testing.T) {
	silence := models.Silence{Matchers: []models.SilenceMatcher{
		{Name: "alertname", Value: "KafkaLagHigh", IsEqual: true},
	}}
	alerts := []*webuimodels.DashboardAlert{
		{Source: "prod-am", Labels: map[string]string{"alertname": "KafkaLagHigh"}},
		{Source: "staging-am", Labels: map[string]string{"alertname": "KafkaLagHigh"}},
		{Source: "prod-am", Labels: map[string]string{"alertname": "DiskFull"}},
	}

	if got := countMatchedAlerts(silence, "prod-am", alerts); got != 1 {
		t.Errorf("countMatchedAlerts(prod-am) = %d, want 1", got)
	}
	if got := countMatchedAlerts(silence, "staging-am", alerts); got != 1 {
		t.Errorf("countMatchedAlerts(staging-am) = %d, want 1", got)
	}
	if got := countMatchedAlerts(silence, "unknown-am", alerts); got != 0 {
		t.Errorf("countMatchedAlerts(unknown-am) = %d, want 0", got)
	}
}

// Sorting on EndsAt alone reads as "soonest expiry first" but buries every actionable
// silence under the expired ones Alertmanager retains for 120h once the expired filter is on.
func TestSortSilencesPutsLiveFirstAndExpiredNewestFirst(t *testing.T) {
	base := time.Now()
	silences := []webuimodels.Silence{
		{ID: "expired-old", EndsAt: base.Add(-48 * time.Hour), Status: webuimodels.SilenceStatus{State: "expired"}},
		{ID: "active-late", EndsAt: base.Add(6 * time.Hour), Status: webuimodels.SilenceStatus{State: "active"}},
		{ID: "expired-recent", EndsAt: base.Add(-1 * time.Hour), Status: webuimodels.SilenceStatus{State: "expired"}},
		{ID: "pending-soon", EndsAt: base.Add(1 * time.Hour), Status: webuimodels.SilenceStatus{State: "pending"}},
	}

	sortSilences(silences)

	want := []string{"pending-soon", "active-late", "expired-recent", "expired-old"}
	for i, id := range want {
		if silences[i].ID != id {
			t.Fatalf("position %d = %s, want %s", i, silences[i].ID, id)
		}
	}
}

// Alertmanager < 0.22 omits isEqual; decoding it as false would negate every matcher.
func TestSilenceMatcherIsEqualDefaultsToTrue(t *testing.T) {
	var matcher models.SilenceMatcher
	if err := json.Unmarshal([]byte(`{"name":"severity","value":"critical"}`), &matcher); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !matcher.IsEqual {
		t.Error("IsEqual should default to true when absent")
	}

	if err := json.Unmarshal([]byte(`{"name":"severity","value":"critical","isEqual":false}`), &matcher); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if matcher.IsEqual {
		t.Error("IsEqual should stay false when explicitly false")
	}
}

func TestParseExtendedDurationWeeks(t *testing.T) {
	got, err := parseExtendedDuration("2w")
	if err != nil {
		t.Fatalf("parse 2w failed: %v", err)
	}
	if want := 14 * 24 * time.Hour; got != want {
		t.Fatalf("2w = %v, want %v", got, want)
	}
}

func TestCompileDraftMatchers(t *testing.T) {
	rows := []webuimodels.SilenceMatcher{
		{Name: "severity", Value: "critical", IsEqual: true},
		{Name: "", Value: "half-typed row"},
		{Name: "instance", Value: "kafka-(", IsRegex: true, IsEqual: true},
	}

	compiled, errs := compileDraftMatchers(rows, false)
	if len(compiled) != 1 {
		t.Fatalf("compiled %d matchers, want 1 (unnamed row skipped, bad regex dropped)", len(compiled))
	}
	if len(errs) != 1 || errs[0].Index != 2 {
		t.Fatalf("errs = %+v, want one invalid-regex error on index 2", errs)
	}

	_, errs = compileDraftMatchers(rows, true)
	if len(errs) != 2 {
		t.Fatalf("with requireNames, errs = %+v, want errors on the unnamed row and the bad regex", errs)
	}
}

func TestPreviewMatches(t *testing.T) {
	alerts := []*webuimodels.DashboardAlert{
		{Source: "prod", Labels: map[string]string{"alertname": "KafkaLagHigh", "severity": "critical"}},
		{Source: "prod", Labels: map[string]string{"alertname": "DiskFull", "severity": "warning"}},
		{Source: "staging", Labels: map[string]string{"alertname": "KafkaLagHigh", "severity": "critical"}},
	}
	matchers, _ := compileDraftMatchers([]webuimodels.SilenceMatcher{
		{Name: "severity", Value: "critical", IsEqual: true},
	}, false)

	count, sample := previewMatches(matchers, "prod", alerts)
	if count != 1 || len(sample) != 1 || sample[0].Alertname != "KafkaLagHigh" {
		t.Fatalf("count=%d sample=%+v, want the single critical prod alert", count, sample)
	}

	// An empty matcher set must match nothing, not everything: matchLabels over zero
	// matchers is vacuously true for every alert.
	count, _ = previewMatches(nil, "prod", alerts)
	if count != 0 {
		t.Fatalf("empty matcher set matched %d alerts, want 0", count)
	}
}

func TestValidateDraftSources(t *testing.T) {
	known := []string{"prod", "staging"}
	if msg := validateDraftSources(nil, known); msg == "" {
		t.Error("empty sources should be rejected")
	}
	if msg := validateDraftSources([]string{"prod", "nope"}, known); msg == "" {
		t.Error("unknown source should be rejected")
	}
	if msg := validateDraftSources([]string{"prod", "staging"}, known); msg != "" {
		t.Errorf("valid sources rejected: %s", msg)
	}
}

func TestBuildDraftSilence(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	valid := []webuimodels.SilenceMatcher{{Name: "alertname", Value: "Foo", IsEqual: true}}

	t.Run("rejects empty matchers", func(t *testing.T) {
		_, _, err := buildDraftSilence(silenceDraftRequest{Comment: "c", Duration: "1h"}, now)
		if err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("rejects invalid regex with row index", func(t *testing.T) {
		_, matcherErrors, err := buildDraftSilence(silenceDraftRequest{
			Matchers: []webuimodels.SilenceMatcher{valid[0], {Name: "job", Value: "ka(", IsRegex: true, IsEqual: true}},
			Comment:  "c", Duration: "1h",
		}, now)
		if err == nil || len(matcherErrors) != 1 || matcherErrors[0].Index != 1 {
			t.Fatalf("err=%v matcherErrors=%+v, want an error pinned on index 1", err, matcherErrors)
		}
	})

	t.Run("rejects empty comment", func(t *testing.T) {
		_, _, err := buildDraftSilence(silenceDraftRequest{Matchers: valid, Duration: "1h", Comment: "  "}, now)
		if err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("rejects endsAt before startsAt", func(t *testing.T) {
		_, _, err := buildDraftSilence(silenceDraftRequest{
			Matchers: valid, Comment: "c",
			StartsAt: now.Format(time.RFC3339),
			EndsAt:   now.Add(-time.Hour).Format(time.RFC3339),
		}, now)
		if err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("no 30-day cap on creation", func(t *testing.T) {
		silence, _, err := buildDraftSilence(silenceDraftRequest{Matchers: valid, Comment: "c", Duration: "90d"}, now)
		if err != nil {
			t.Fatalf("90d rejected: %v", err)
		}
		if want := now.Add(90 * 24 * time.Hour); !silence.EndsAt.Equal(want) {
			t.Fatalf("EndsAt = %v, want %v", silence.EndsAt, want)
		}
	})

	t.Run("free-form week duration", func(t *testing.T) {
		silence, _, err := buildDraftSilence(silenceDraftRequest{Matchers: valid, Comment: "c", Duration: "2w"}, now)
		if err != nil {
			t.Fatalf("2w rejected: %v", err)
		}
		if want := now.Add(14 * 24 * time.Hour); !silence.EndsAt.Equal(want) {
			t.Fatalf("EndsAt = %v, want %v", silence.EndsAt, want)
		}
	})

	t.Run("future startsAt makes a pending silence window", func(t *testing.T) {
		startsAt := now.Add(6 * time.Hour)
		silence, _, err := buildDraftSilence(silenceDraftRequest{
			Matchers: valid, Comment: "c",
			StartsAt: startsAt.Format(time.RFC3339), Duration: "1h",
		}, now)
		if err != nil {
			t.Fatalf("future startsAt rejected: %v", err)
		}
		if !silence.StartsAt.Equal(startsAt) || !silence.EndsAt.Equal(startsAt.Add(time.Hour)) {
			t.Fatalf("window = %v → %v, want %v → %v", silence.StartsAt, silence.EndsAt, startsAt, startsAt.Add(time.Hour))
		}
	})
}

func TestFanOutSilenceReportsPerSource(t *testing.T) {
	original := createSilenceOn
	defer func() { createSilenceOn = original }()

	createSilenceOn = func(source string, silence models.Silence) (*models.Silence, error) {
		if source == "broken" {
			return nil, errors.New("connection refused")
		}
		created := silence
		created.ID = "sil-" + source
		return &created, nil
	}

	results, allSucceeded := fanOutSilence([]string{"prod", "broken", "staging"}, models.Silence{})
	if allSucceeded {
		t.Fatal("allSucceeded should be false when one source fails")
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if !results[0].Success || results[0].SilenceID != "sil-prod" {
		t.Fatalf("prod result = %+v, want success with its silence ID", results[0])
	}
	if results[1].Success || results[1].Error == "" {
		t.Fatalf("broken result = %+v, want a per-source failure with its error", results[1])
	}
	if !results[2].Success {
		t.Fatalf("staging result = %+v, want success after an earlier failure", results[2])
	}
}

// The preview route shares its first segment with the :id wildcard routes; older gin
// releases panicked on that mix at registration time. Register the real shapes and
// route a request to prove "preview" is not swallowed by ":id".
func TestSilenceRouteShapesDoNotConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1/dashboard")
	hit := ""
	group.GET("/silences", func(c *gin.Context) { hit = "list" })
	group.POST("/silences", func(c *gin.Context) { hit = "create" })
	group.POST("/silences/preview", func(c *gin.Context) { hit = "preview" })
	group.POST("/silences/:id/extend", func(c *gin.Context) { hit = "extend" })
	group.DELETE("/silences/:id", func(c *gin.Context) { hit = "expire" })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/v1/dashboard/silences/preview", nil))
	if hit != "preview" {
		t.Fatalf("POST /silences/preview routed to %q, want the preview handler", hit)
	}
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/v1/dashboard/silences/abc123/extend", nil))
	if hit != "extend" {
		t.Fatalf("POST /silences/:id/extend routed to %q, want the extend handler", hit)
	}
}
