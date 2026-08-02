package handlers

import (
	"testing"
	"time"

	webuimodels "notificator/internal/webui/models"
)

func alertWithLabels(fp string, labels map[string]string) *webuimodels.DashboardAlert {
	return &webuimodels.DashboardAlert{
		Fingerprint: fp,
		Labels:      labels,
		StartsAt:    time.Now(),
	}
}

func TestComputeRelatedAlertGroups(t *testing.T) {
	target := alertWithLabels("target", map[string]string{
		"cluster":   "prod-eu",
		"namespace": "payments",
		"env":       "prod",
	})

	others := []*webuimodels.DashboardAlert{
		// Only one other alert shares "namespace=payments" — not a pattern, dropped.
		alertWithLabels("a1", map[string]string{"cluster": "prod-eu", "namespace": "payments", "env": "prod"}),
		// Three more share "cluster=prod-eu" only — real signal, kept.
		alertWithLabels("a2", map[string]string{"cluster": "prod-eu", "namespace": "checkout", "env": "prod"}),
		alertWithLabels("a3", map[string]string{"cluster": "prod-eu", "namespace": "checkout", "env": "prod"}),
		alertWithLabels("a4", map[string]string{"cluster": "prod-eu", "namespace": "checkout", "env": "prod"}),
		// Different cluster so cluster=prod-eu doesn't cover the whole active set,
		// but still env=prod like every other alert here.
		alertWithLabels("a5", map[string]string{"cluster": "prod-us", "namespace": "checkout", "env": "prod"}),
		// env=prod is shared by every other alert — covers the whole estate, dropped.
	}

	groups := computeRelatedAlertGroups(target, others)

	if len(groups) != 1 {
		t.Fatalf("expected 1 correlating label group, got %d: %+v", len(groups), groups)
	}

	g := groups[0]
	if g.LabelKey != "cluster" || g.LabelValue != "prod-eu" {
		t.Fatalf("expected cluster=prod-eu group, got %s=%s", g.LabelKey, g.LabelValue)
	}
	if g.Count != 4 {
		t.Fatalf("expected count 4 (a1..a4), got %d", g.Count)
	}
	if len(g.Alerts) != 4 {
		t.Fatalf("expected 4 alerts listed, got %d", len(g.Alerts))
	}
}

func TestComputeRelatedAlertGroupsOrdersByCountDesc(t *testing.T) {
	target := alertWithLabels("target", map[string]string{
		"cluster":   "prod-eu",
		"alertname": "DiskFull",
	})

	others := []*webuimodels.DashboardAlert{
		alertWithLabels("a1", map[string]string{"cluster": "prod-eu", "alertname": "DiskFull"}),
		alertWithLabels("a2", map[string]string{"cluster": "prod-eu", "alertname": "Other"}),
		alertWithLabels("a3", map[string]string{"cluster": "prod-eu", "alertname": "Other"}),
		alertWithLabels("a4", map[string]string{"cluster": "other", "alertname": "DiskFull"}),
	}

	groups := computeRelatedAlertGroups(target, others)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(groups), groups)
	}
	if groups[0].LabelKey != "cluster" || groups[0].Count != 3 {
		t.Fatalf("expected cluster group first with count 3, got %+v", groups[0])
	}
	if groups[1].LabelKey != "alertname" || groups[1].Count != 2 {
		t.Fatalf("expected alertname group second with count 2, got %+v", groups[1])
	}
}

func TestComputeRelatedAlertGroupsEmptyWhenAlone(t *testing.T) {
	target := alertWithLabels("target", map[string]string{"cluster": "prod-eu"})

	if groups := computeRelatedAlertGroups(target, nil); groups != nil {
		t.Fatalf("expected no groups when no other alerts are firing, got %+v", groups)
	}
}

func TestComputeRelatedAlertGroupsCapsAlertsPerGroup(t *testing.T) {
	target := alertWithLabels("target", map[string]string{"cluster": "prod-eu"})

	var others []*webuimodels.DashboardAlert
	for i := range maxRelatedAlertsPerGroup + 5 {
		others = append(others, alertWithLabels(string(rune('a'+i)), map[string]string{"cluster": "prod-eu"}))
	}
	// A handful of outliers so cluster=prod-eu stays well under the "whole active
	// set" ratio and isn't dropped as degenerate.
	for i := range 5 {
		others = append(others, alertWithLabels("outlier"+string(rune('a'+i)), map[string]string{"cluster": "other"}))
	}

	groups := computeRelatedAlertGroups(target, others)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Count != maxRelatedAlertsPerGroup+5 {
		t.Fatalf("expected count to reflect all matches, got %d", groups[0].Count)
	}
	if len(groups[0].Alerts) != maxRelatedAlertsPerGroup {
		t.Fatalf("expected alerts capped at %d, got %d", maxRelatedAlertsPerGroup, len(groups[0].Alerts))
	}
	if !groups[0].Truncated {
		t.Fatal("expected Truncated=true when count exceeds the cap")
	}
}
