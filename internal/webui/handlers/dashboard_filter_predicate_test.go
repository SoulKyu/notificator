package handlers

import (
	"testing"

	webuimodels "notificator/internal/webui/models"
)

func TestAlertPassesAlertLevelFilters(t *testing.T) {
	alert := &webuimodels.DashboardAlert{
		AlertName: "KafkaLagHigh", Source: "prod", Severity: "critical", Team: "sre",
	}

	if !alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{}) {
		t.Fatal("no filters should pass")
	}
	if !alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{Severities: []string{"critical"}}) {
		t.Fatal("matching severity should pass")
	}
	if alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{Severities: []string{"warning"}}) {
		t.Fatal("non-matching severity should fail")
	}
	if alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{Teams: []string{"dba"}}) {
		t.Fatal("non-matching team should fail")
	}
	if !alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{
		Alertmanagers: []string{"prod"}, AlertNames: []string{"KafkaLagHigh"},
	}) {
		t.Fatal("matching source+name should pass")
	}
}

func TestAlertMatchesLabelFilters(t *testing.T) {
	alert := &webuimodels.DashboardAlert{Labels: map[string]string{"cluster": "prod", "team": "sre"}}

	if !alertMatchesLabelFilters(alert, nil) {
		t.Fatal("no label filter should pass")
	}
	if !alertMatchesLabelFilters(alert, []webuimodels.LabelFilter{{Key: "cluster", Value: "prod"}}) {
		t.Fatal("exact label value should pass")
	}
	// The whole point of the filter: a substring of the value is not a match.
	if alertMatchesLabelFilters(alert, []webuimodels.LabelFilter{{Key: "cluster", Value: "pro"}}) {
		t.Fatal("partial label value must not match")
	}
	if alertMatchesLabelFilters(alert, []webuimodels.LabelFilter{{Key: "zone", Value: "prod"}}) {
		t.Fatal("value on another label key must not match")
	}
	if alertMatchesLabelFilters(alert, []webuimodels.LabelFilter{
		{Key: "cluster", Value: "prod"}, {Key: "team", Value: "dba"},
	}) {
		t.Fatal("every label filter must match")
	}
}
