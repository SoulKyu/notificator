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
