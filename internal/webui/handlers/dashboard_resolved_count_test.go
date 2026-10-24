package handlers

import (
	"reflect"
	"testing"
	"time"

	alertpb "notificator/internal/backend/proto/alert"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/services"
)

// stubHiddenBackend feeds HiddenAlertsService without a backend connection. Only
// the two read calls LoadUserData makes are ever exercised.
type stubHiddenBackend struct {
	rules []*alertpb.UserHiddenRule
}

func (s stubHiddenBackend) GetUserHiddenAlerts(string, ...string) ([]*alertpb.UserHiddenAlert, error) {
	return nil, nil
}

func (s stubHiddenBackend) GetUserHiddenRules(string, ...string) ([]*alertpb.UserHiddenRule, error) {
	return s.rules, nil
}

func (s stubHiddenBackend) HideAlert(string, string, string, string, string, *time.Time, ...string) error {
	return nil
}
func (s stubHiddenBackend) UnhideAlert(string, string, ...string) error { return nil }
func (s stubHiddenBackend) SaveHiddenRule(string, *alertpb.UserHiddenRule, ...string) (*alertpb.UserHiddenRule, error) {
	return nil, nil
}
func (s stubHiddenBackend) RemoveHiddenRule(string, string, ...string) error { return nil }
func (s stubHiddenBackend) ClearAllHiddenAlerts(string, ...string) error     { return nil }

// filtersAffectResolvedCount gates the cheap backend-count path against the
// expensive fetch-and-filter path: a filter it fails to recognize would silently
// report an unfiltered Resolved counter in a filtered view.
func TestFiltersAffectResolvedCount(t *testing.T) {
	acknowledged := true
	hasComments := false
	ownedByMe := true
	notOwnedByMe := false

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
		{"label filters", webuimodels.DashboardFilters{
			LabelFilters: []webuimodels.LabelFilter{{Key: "cluster", Value: "prod"}},
		}, true},
		{"owned by me", webuimodels.DashboardFilters{OwnedByMe: &ownedByMe}, true},
		{"owned by me false does not filter", webuimodels.DashboardFilters{OwnedByMe: &notOwnedByMe}, false},
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

// The table above is hand-written per field, so it stays green when
// DashboardFilters grows a field that applyDashboardFilters rejects on and
// filtersAffectResolvedCount ignores. This forces the decision at compile-test
// time instead.
func TestFiltersAffectResolvedCountCoversEveryFilterField(t *testing.T) {
	known := map[string]bool{
		"Search": true, "Alertmanagers": true, "Severities": true,
		"Statuses": true, "Teams": true, "AlertNames": true,
		"Acknowledged": true, "HasComments": true, "LabelFilters": true,
		"OwnedByMe": true,
		"FilterHiddenAlerts": true, "FilterHiddenRules": true,
		// Not filtering predicates in applyDashboardFilters:
		"DisplayMode": true, "ViewMode": true, "ResolvedAlertsLimit": true,
	}

	typ := reflect.TypeOf(webuimodels.DashboardFilters{})
	for i := 0; i < typ.NumField(); i++ {
		if name := typ.Field(i).Name; !known[name] {
			t.Fatalf("DashboardFilters.%s is new: decide whether it can exclude a resolved alert, then update filtersAffectResolvedCount and this list", name)
		}
	}
}

// The global hidden-alerts branch is the one filtersAffectResolvedCount case
// that does not come from the filters struct: a session hiding anything at all
// must take the fetch-and-filter path.
func TestFiltersAffectResolvedCountGlobalHiddenRules(t *testing.T) {
	previous := hiddenAlertsService
	t.Cleanup(func() { hiddenAlertsService = previous })

	const sessionID = "session-with-hidden-rule"

	hiddenAlertsService = services.NewHiddenAlertsService(stubHiddenBackend{})
	if filtersAffectResolvedCount(webuimodels.DashboardFilters{}, sessionID) {
		t.Error("no hidden entries: want false")
	}

	hiddenAlertsService = services.NewHiddenAlertsService(stubHiddenBackend{
		rules: []*alertpb.UserHiddenRule{{Id: "r1", LabelKey: "team", LabelValue: "sre", IsEnabled: true}},
	})
	if !filtersAffectResolvedCount(webuimodels.DashboardFilters{}, sessionID) {
		t.Error("one enabled hidden rule: want true")
	}

	hiddenAlertsService = services.NewHiddenAlertsService(stubHiddenBackend{
		rules: []*alertpb.UserHiddenRule{{Id: "r1", LabelKey: "team", LabelValue: "sre", IsEnabled: false}},
	})
	if filtersAffectResolvedCount(webuimodels.DashboardFilters{}, sessionID) {
		t.Error("only a disabled hidden rule: want false")
	}
}
