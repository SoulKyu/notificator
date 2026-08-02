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

// activeSet mirrors what the handler feeds the helper: the classic view (firing,
// unacknowledged) with the target included.
func activeSet(target *webuimodels.DashboardAlert, others ...*webuimodels.DashboardAlert) []*webuimodels.DashboardAlert {
	return append([]*webuimodels.DashboardAlert{target}, others...)
}

func TestComputeRelatedAlertGroups(t *testing.T) {
	target := alertWithLabels("target", map[string]string{
		"cluster":   "prod-eu",
		"namespace": "payments",
		"env":       "prod",
	})

	active := activeSet(target,
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
	)

	groups := computeRelatedAlertGroups(target, active)

	if len(groups) != 1 {
		t.Fatalf("expected 1 correlating label group, got %d: %+v", len(groups), groups)
	}

	g := groups[0]
	if g.LabelKey != "cluster" || g.LabelValue != "prod-eu" {
		t.Fatalf("expected cluster=prod-eu group, got %s=%s", g.LabelKey, g.LabelValue)
	}
	// The open alert is one of the rows "Filter dashboard on this" would show.
	if g.Count != 5 {
		t.Fatalf("expected count 5 (target + a1..a4), got %d", g.Count)
	}
	if g.OtherCount != 4 {
		t.Fatalf("expected otherCount 4 (a1..a4), got %d", g.OtherCount)
	}
	if len(g.Alerts) != 4 {
		t.Fatalf("expected 4 alerts listed, got %d", len(g.Alerts))
	}
	for _, a := range g.Alerts {
		if a.Fingerprint == target.Fingerprint {
			t.Fatal("the open alert must not be listed among its own related alerts")
		}
	}
}

// The panel counts what the dashboard shows: acknowledged and resolved alerts are
// gone from the classic view, so they must not inflate a group either.
func TestComputeRelatedAlertGroupsCountsOnlyTheClassicView(t *testing.T) {
	target := alertWithLabels("target", map[string]string{"zone": "qa", "role": "db"})

	firing := []*webuimodels.DashboardAlert{
		alertWithLabels("f1", map[string]string{"zone": "qa", "role": "web"}),
		alertWithLabels("f2", map[string]string{"zone": "qa", "role": "web"}),
		alertWithLabels("f3", map[string]string{"zone": "other", "role": "web"}),
	}

	resolved := alertWithLabels("r1", map[string]string{"zone": "qa"})
	resolved.IsResolved = true
	acked := alertWithLabels("k1", map[string]string{"zone": "qa"})
	acked.IsAcknowledged = true

	// The handler filters with isClassicViewAlert before calling the helper.
	var active []*webuimodels.DashboardAlert
	for _, a := range append(activeSet(target, firing...), resolved, acked) {
		if isClassicViewAlert(a) {
			active = append(active, a)
		}
	}

	groups := computeRelatedAlertGroups(target, active)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d: %+v", len(groups), groups)
	}
	if groups[0].LabelKey != "zone" || groups[0].Count != 3 {
		t.Fatalf("expected zone group counting target+f1+f2 only, got %+v", groups[0])
	}
}

func TestComputeRelatedAlertGroupsOrdersByCountDesc(t *testing.T) {
	target := alertWithLabels("target", map[string]string{
		"cluster":   "prod-eu",
		"alertname": "DiskFull",
	})

	active := activeSet(target,
		alertWithLabels("a1", map[string]string{"cluster": "prod-eu", "alertname": "DiskFull"}),
		alertWithLabels("a2", map[string]string{"cluster": "prod-eu", "alertname": "Other"}),
		alertWithLabels("a3", map[string]string{"cluster": "prod-eu", "alertname": "Other"}),
		alertWithLabels("a4", map[string]string{"cluster": "other", "alertname": "DiskFull"}),
	)

	groups := computeRelatedAlertGroups(target, active)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(groups), groups)
	}
	if groups[0].LabelKey != "cluster" || groups[0].Count != 4 {
		t.Fatalf("expected cluster group first with count 4, got %+v", groups[0])
	}
	if groups[1].LabelKey != "alertname" || groups[1].Count != 3 {
		t.Fatalf("expected alertname group second with count 3, got %+v", groups[1])
	}
}

func TestComputeRelatedAlertGroupsEmptyWhenAlone(t *testing.T) {
	target := alertWithLabels("target", map[string]string{"cluster": "prod-eu"})

	if groups := computeRelatedAlertGroups(target, activeSet(target)); len(groups) != 0 {
		t.Fatalf("expected no groups when no other alerts are firing, got %+v", groups)
	}
	if groups := computeRelatedAlertGroups(target, nil); len(groups) != 0 {
		t.Fatalf("expected no groups for an empty active set, got %+v", groups)
	}
}

func TestComputeRelatedAlertGroupsCapsAlertsPerGroup(t *testing.T) {
	target := alertWithLabels("target", map[string]string{"cluster": "prod-eu"})

	others := []*webuimodels.DashboardAlert{}
	for i := range maxRelatedAlertsPerGroup + 5 {
		others = append(others, alertWithLabels(string(rune('a'+i)), map[string]string{"cluster": "prod-eu"}))
	}
	// A handful of outliers so cluster=prod-eu stays well under the "whole active
	// set" ratio and isn't dropped as degenerate.
	for i := range 5 {
		others = append(others, alertWithLabels("outlier"+string(rune('a'+i)), map[string]string{"cluster": "other"}))
	}

	groups := computeRelatedAlertGroups(target, activeSet(target, others...))
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Count != maxRelatedAlertsPerGroup+6 {
		t.Fatalf("expected count to reflect target + all matches, got %d", groups[0].Count)
	}
	if groups[0].OtherCount != maxRelatedAlertsPerGroup+5 {
		t.Fatalf("expected otherCount to reflect every match, got %d", groups[0].OtherCount)
	}
	if len(groups[0].Alerts) != maxRelatedAlertsPerGroup {
		t.Fatalf("expected alerts capped at %d, got %d", maxRelatedAlertsPerGroup, len(groups[0].Alerts))
	}
	if !groups[0].Truncated {
		t.Fatal("expected Truncated=true when count exceeds the cap")
	}
}

// A group's count is only trustworthy if the dashboard filter it hands over
// selects exactly those alerts.
func TestRelatedGroupCountMatchesLabelFilteredDashboard(t *testing.T) {
	target := alertWithLabels("target", map[string]string{"cluster": "prod", "team": "sre"})
	active := activeSet(target,
		alertWithLabels("a1", map[string]string{"cluster": "prod"}),
		alertWithLabels("a2", map[string]string{"cluster": "prod"}),
		// prod-eu must not be caught by a cluster=prod filter (substring search did)
		alertWithLabels("a3", map[string]string{"cluster": "prod-eu"}),
		alertWithLabels("a4", map[string]string{"cluster": "prod-eu"}),
		alertWithLabels("a5", map[string]string{"cluster": "staging"}),
	)

	groups := computeRelatedAlertGroups(target, active)
	if len(groups) != 1 || groups[0].LabelKey != "cluster" {
		t.Fatalf("expected a single cluster group, got %+v", groups)
	}

	filter := []webuimodels.LabelFilter{{Key: groups[0].LabelKey, Value: groups[0].LabelValue}}
	rows := 0
	for _, alert := range active {
		if alertMatchesLabelFilters(alert, filter) {
			rows++
		}
	}

	if rows != groups[0].Count {
		t.Fatalf("group count %d but the same label filter yields %d rows", groups[0].Count, rows)
	}
}
