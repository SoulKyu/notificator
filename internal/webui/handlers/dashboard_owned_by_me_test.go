package handlers

import (
	"testing"

	webuimodels "notificator/internal/webui/models"
)

// TestApplyDashboardFiltersOwnedByMe guards the shift-handover worklist: with
// ownedByMe set, only alerts acknowledged by the current user must survive.
func TestApplyDashboardFiltersOwnedByMe(t *testing.T) {
	alerts := []*webuimodels.DashboardAlert{
		{Fingerprint: "mine", IsAcknowledged: true, AcknowledgedBy: "alice"},
		{Fingerprint: "theirs", IsAcknowledged: true, AcknowledgedBy: "bob"},
		{Fingerprint: "unacked"},
	}
	ownedByMe := true

	got := applyDashboardFilters(alerts, webuimodels.DashboardFilters{OwnedByMe: &ownedByMe}, "", "alice")

	if len(got) != 1 || got[0].Fingerprint != "mine" {
		t.Fatalf("ownedByMe=true: want only [mine], got %v", fingerprints(got))
	}

	got = applyDashboardFilters(alerts, webuimodels.DashboardFilters{}, "", "alice")
	if len(got) != 3 {
		t.Fatalf("no ownedByMe filter: want all 3 alerts, got %v", fingerprints(got))
	}
}

func fingerprints(alerts []*webuimodels.DashboardAlert) []string {
	out := make([]string, len(alerts))
	for i, a := range alerts {
		out[i] = a.Fingerprint
	}
	return out
}
