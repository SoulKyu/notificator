package handlers

import (
	"testing"

	"notificator/internal/backend/proto/alert"
	webuimodels "notificator/internal/webui/models"
)

func TestBuildActivityFeedUncachedBehavior(t *testing.T) {
	events := []*alert.ActivityEvent{
		{Id: "e1", AlertKey: "cached-key", Kind: "silence", Username: "mathieu", Content: "🔇 x"},
		{Id: "e2", AlertKey: "gone-key", Kind: "resolve", Username: "julie", Content: "✅ y"},
	}
	// cache resolves only "cached-key" → KafkaLagHigh / prod / critical
	resolve := func(key string) (name, source, severity, team string, ok bool) {
		if key == "cached-key" {
			return "KafkaLagHigh", "prod", "critical", "sre", true
		}
		return "", "", "", "", false
	}

	// no alert-level filter → both pass, e2 marked uncached
	all := buildActivityFeed(events, webuimodels.DashboardFilters{}, resolve)
	if len(all) != 2 {
		t.Fatalf("no filter: got %d, want 2", len(all))
	}
	var gone *webuimodels.ActivityEvent
	for i := range all {
		if all[i].AlertKey == "gone-key" {
			gone = &all[i]
		}
	}
	if gone == nil || !gone.Uncached || gone.AlertName != "gone-key" {
		t.Fatalf("uncached event should pass through with key fallback: %+v", gone)
	}

	// severity filter active → uncached e2 hidden, cached e1 kept
	filtered := buildActivityFeed(events, webuimodels.DashboardFilters{Severities: []string{"critical"}}, resolve)
	if len(filtered) != 1 || filtered[0].AlertKey != "cached-key" {
		t.Fatalf("active filter: got %v, want [cached-key]", filtered)
	}

	// severity filter that the cached alert fails → nothing
	none := buildActivityFeed(events, webuimodels.DashboardFilters{Severities: []string{"warning"}}, resolve)
	if len(none) != 0 {
		t.Fatalf("non-matching filter: got %d, want 0", len(none))
	}
}

func TestMatchesActivitySearch(t *testing.T) {
	ev := webuimodels.ActivityEvent{
		Content:   "🔇 Alert silenced for 2h: KafkaLagHigh",
		Username:  "mathieu",
		AlertName: "KafkaLagHigh",
	}

	cases := []struct {
		name string
		term string
		want bool
	}{
		{"empty term matches", "", true},
		{"matches content", "silenced", true},
		{"matches username case-insensitively", "MATHIEU", true},
		{"matches alertName case-insensitively", "kafkalaghigh", true},
		{"non-matching term", "nonexistent", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesActivitySearch(ev, tc.term); got != tc.want {
				t.Errorf("matchesActivitySearch(%q) = %v, want %v", tc.term, got, tc.want)
			}
		})
	}
}
