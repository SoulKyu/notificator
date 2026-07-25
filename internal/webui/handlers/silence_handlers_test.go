package handlers

import (
	"encoding/json"
	"testing"

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
