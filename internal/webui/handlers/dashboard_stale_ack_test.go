package handlers

import (
	"testing"
	"time"

	webuimodels "notificator/internal/webui/models"
)

// TestBuildDashboardMetadataStaleAcknowledged guards the stale-ack badge count
// against drifting from the rows the client actually marks stale: it must
// respect the threshold, the OwnedByMe filter, and the classic-mode fixup
// that sources from allAlerts instead of filteredAlerts.
func TestBuildDashboardMetadataStaleAcknowledged(t *testing.T) {
	const userID = "stale-ack-test-user"
	t.Cleanup(func() {
		userSettingsMu.Lock()
		delete(userSettings, userID)
		userSettingsMu.Unlock()
	})
	setThreshold := func(hours int) {
		userSettingsMu.Lock()
		userSettings[userID] = &webuimodels.DashboardSettings{StaleAckThresholdHours: hours}
		userSettingsMu.Unlock()
	}

	old := time.Now().Add(-5 * time.Hour)
	fresh := time.Now().Add(-1 * time.Hour)

	aliceStale := &webuimodels.DashboardAlert{Fingerprint: "alice-stale", IsAcknowledged: true, AcknowledgedBy: "alice", AcknowledgedAt: old}
	bobStale := &webuimodels.DashboardAlert{Fingerprint: "bob-stale", IsAcknowledged: true, AcknowledgedBy: "bob", AcknowledgedAt: old}
	aliceFresh := &webuimodels.DashboardAlert{Fingerprint: "alice-fresh", IsAcknowledged: true, AcknowledgedBy: "alice", AcknowledgedAt: fresh}

	t.Run("threshold 0 disables the count", func(t *testing.T) {
		setThreshold(0)
		allAlerts := []*webuimodels.DashboardAlert{aliceStale, bobStale}
		filters := webuimodels.DashboardFilters{DisplayMode: webuimodels.DisplayModeAcknowledge}
		metadata := buildDashboardMetadata(allAlerts, allAlerts, filters, userID, "", "alice")
		if metadata.Counters.StaleAcknowledged != 0 {
			t.Fatalf("threshold=0: want 0 stale, got %d", metadata.Counters.StaleAcknowledged)
		}
	})

	t.Run("acknowledge mode counts across users, OwnedByMe scopes to the current user", func(t *testing.T) {
		setThreshold(4)
		allAlerts := []*webuimodels.DashboardAlert{aliceStale, bobStale, aliceFresh}

		filters := webuimodels.DashboardFilters{DisplayMode: webuimodels.DisplayModeAcknowledge}
		metadata := buildDashboardMetadata(allAlerts, allAlerts, filters, userID, "", "alice")
		if metadata.Counters.StaleAcknowledged != 2 {
			t.Fatalf("no ownedByMe: want 2 stale (alice+bob), got %d", metadata.Counters.StaleAcknowledged)
		}

		ownedByMe := true
		filteredForAlice := []*webuimodels.DashboardAlert{aliceStale, aliceFresh}
		filters = webuimodels.DashboardFilters{DisplayMode: webuimodels.DisplayModeAcknowledge, OwnedByMe: &ownedByMe}
		metadata = buildDashboardMetadata(allAlerts, filteredForAlice, filters, userID, "", "alice")
		if metadata.Counters.StaleAcknowledged != 1 {
			t.Fatalf("ownedByMe=true: want 1 stale (alice only), got %d", metadata.Counters.StaleAcknowledged)
		}
	})

	t.Run("classic mode sources from allAlerts since filteredAlerts excludes acked rows", func(t *testing.T) {
		setThreshold(4)
		allAlerts := []*webuimodels.DashboardAlert{aliceStale, bobStale}
		filters := webuimodels.DashboardFilters{DisplayMode: webuimodels.DisplayModeClassic}

		metadata := buildDashboardMetadata(allAlerts, nil, filters, userID, "", "alice")
		if metadata.Counters.StaleAcknowledged != 2 {
			t.Fatalf("classic mode: want 2 stale from allAlerts, got %d", metadata.Counters.StaleAcknowledged)
		}
	})
}
