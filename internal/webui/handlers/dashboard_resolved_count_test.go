package handlers

import (
	"testing"

	webuimodels "notificator/internal/webui/models"
)

// filtersAffectResolvedCount gates the cheap backend-count path against the
// expensive fetch-and-filter path: a filter it fails to recognize would silently
// report an unfiltered Resolved counter in a filtered view.
func TestFiltersAffectResolvedCount(t *testing.T) {
	acknowledged := true
	hasComments := false

	tests := []struct {
		name    string
		filters webuimodels.DashboardFilters
		want    bool
	}{
		{"no filters", webuimodels.DashboardFilters{}, false},
		{"resolved limit alone does not filter", webuimodels.DashboardFilters{ResolvedAlertsLimit: 100}, false},
		{"search", webuimodels.DashboardFilters{Search: "disk"}, true},
		{"alertmanagers", webuimodels.DashboardFilters{Alertmanagers: []string{"prod"}}, true},
		{"severities", webuimodels.DashboardFilters{Severities: []string{"critical"}}, true},
		{"statuses", webuimodels.DashboardFilters{Statuses: []string{"firing"}}, true},
		{"teams", webuimodels.DashboardFilters{Teams: []string{"sre"}}, true},
		{"alert names", webuimodels.DashboardFilters{AlertNames: []string{"HighLoad"}}, true},
		{"acknowledged", webuimodels.DashboardFilters{Acknowledged: &acknowledged}, true},
		{"has comments", webuimodels.DashboardFilters{HasComments: &hasComments}, true},
		{"filter hidden alerts", webuimodels.DashboardFilters{
			FilterHiddenAlerts: []webuimodels.FilterHiddenAlert{{Fingerprint: "abc"}},
		}, true},
		{"filter hidden rules", webuimodels.DashboardFilters{
			FilterHiddenRules: []webuimodels.FilterHiddenRule{{LabelKey: "team"}},
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filtersAffectResolvedCount(tt.filters, ""); got != tt.want {
				t.Errorf("filtersAffectResolvedCount() = %v, want %v", got, tt.want)
			}
		})
	}
}
