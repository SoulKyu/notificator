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

func TestMentionsUsername(t *testing.T) {
	cases := []struct {
		name    string
		content string
		user    string
		want    bool
	}{
		{"simple mention", "this is the payment DB, @marie owns it", "marie", true},
		{"case-insensitive", "hey @Bob check this out", "bob", true},
		{"case-insensitive target", "hey @bob check this out", "Bob", true},
		{"no mention", "just a regular comment", "bob", false},
		{"does not match longer handle", "@bobby is on call", "bob", false},
		{"matches at end of content", "assigning to @bob", "bob", true},
		{"matches with trailing punctuation", "cc @bob, please check", "bob", true},
		{"empty username never matches", "@bob is here", "", false},
		{"does not match mention embedded after a longer token", "Please email ops-team@bob for the runbook", "bob", false},
		{"does not match mention embedded after a longer token in hostname", "contact db01@bob.internal now", "bob", false},
		{"does not match a longer non-ascii handle", "second handover to @bobé only", "bob", false},
		{"matches the non-ascii handle itself", "second handover to @bobé only", "bobé", true},
		{"non-ascii matching is case-insensitive", "ping @BOBÉ about this", "bobé", true},
		{"shorter ascii handle does not match a non-ascii user", "@bob is on call", "bobé", false},
		{"does not match a longer handle across a non-ascii tail", "@marié owns the runbook", "marie", false},
		{"non-ascii handle after a longer token is an address", "mail ops@bobé.internal", "bobé", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mentionsUsername(tc.content, tc.user); got != tc.want {
				t.Errorf("mentionsUsername(%q, %q) = %v, want %v", tc.content, tc.user, got, tc.want)
			}
		})
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
