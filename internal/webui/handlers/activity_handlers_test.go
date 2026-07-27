package handlers

import (
	"testing"
	"time"

	"notificator/internal/backend/proto/alert"
	webuimodels "notificator/internal/webui/models"
)

func TestBuildActivityFeedUncachedBehavior(t *testing.T) {
	now := time.Now()
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
	all := buildActivityFeed(events, webuimodels.DashboardFilters{}, resolve, now)
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
	filtered := buildActivityFeed(events, webuimodels.DashboardFilters{Severities: []string{"critical"}}, resolve, now)
	if len(filtered) != 1 || filtered[0].AlertKey != "cached-key" {
		t.Fatalf("active filter: got %v, want [cached-key]", filtered)
	}

	// severity filter that the cached alert fails → nothing
	none := buildActivityFeed(events, webuimodels.DashboardFilters{Severities: []string{"warning"}}, resolve, now)
	if len(none) != 0 {
		t.Fatalf("non-matching filter: got %d, want 0", len(none))
	}
}
