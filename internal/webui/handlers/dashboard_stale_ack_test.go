package handlers

import (
	"testing"
	"time"

	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/services"
)

// installAlertCache points the package-level cache at a throwaway one holding
// these alerts. It is never Start()ed, so nothing refreshes in the background
// and no backend is contacted; buildDashboardMetadata needs it because the
// stale-ack badge sources the acknowledged store directly.
func installAlertCache(t *testing.T, alerts ...*webuimodels.DashboardAlert) {
	t.Helper()

	previous := alertCache
	cache := services.NewAlertCache(nil, nil, 0, 0)
	for _, alert := range alerts {
		cached := *alert
		cache.UpdateAlert(&cached)
	}
	alertCache = cache
	t.Cleanup(func() { alertCache = previous })
}

// TestBuildDashboardMetadataStaleAcknowledged guards the stale-ack badge count
// against drifting from the rows the client actually marks stale: it must
// respect the threshold and the OwnedByMe filter, and it must count the
// Acknowledged view's rows from *every* display mode - the badge is rendered in
// all of them.
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
		installAlertCache(t, aliceStale, bobStale)
		allAlerts := []*webuimodels.DashboardAlert{aliceStale, bobStale}
		filters := webuimodels.DashboardFilters{DisplayMode: webuimodels.DisplayModeAcknowledge}
		metadata := buildDashboardMetadata(allAlerts, allAlerts, filters, userID, "", "alice")
		if metadata.Counters.StaleAcknowledged != 0 {
			t.Fatalf("threshold=0: want 0 stale, got %d", metadata.Counters.StaleAcknowledged)
		}
	})

	t.Run("acknowledge mode counts across users, OwnedByMe scopes to the current user", func(t *testing.T) {
		setThreshold(4)
		installAlertCache(t, aliceStale, bobStale, aliceFresh)
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

	t.Run("resolved acks count, because the acknowledge view still renders and marks them", func(t *testing.T) {
		setThreshold(4)
		resolvedStale := &webuimodels.DashboardAlert{Fingerprint: "resolved-stale", IsAcknowledged: true, IsResolved: true, AcknowledgedBy: "alice", AcknowledgedAt: old}
		neverAcked := &webuimodels.DashboardAlert{Fingerprint: "never-acked"}
		alerts := []*webuimodels.DashboardAlert{aliceStale, resolvedStale, neverAcked}
		installAlertCache(t, alerts...)

		filters := webuimodels.DashboardFilters{DisplayMode: webuimodels.DisplayModeAcknowledge}
		metadata := buildDashboardMetadata(alerts, alerts, filters, userID, "", "alice")
		if metadata.Counters.StaleAcknowledged != 2 {
			t.Fatalf("want 2 stale (active + resolved ack), got %d", metadata.Counters.StaleAcknowledged)
		}
	})

	t.Run("acked without an ack timestamp is never stale", func(t *testing.T) {
		setThreshold(4)
		noTimestamp := &webuimodels.DashboardAlert{Fingerprint: "no-ts", IsAcknowledged: true, AcknowledgedBy: "alice"}
		alerts := []*webuimodels.DashboardAlert{noTimestamp}
		installAlertCache(t, alerts...)

		filters := webuimodels.DashboardFilters{DisplayMode: webuimodels.DisplayModeAcknowledge}
		metadata := buildDashboardMetadata(alerts, alerts, filters, userID, "", "alice")
		if metadata.Counters.StaleAcknowledged != 0 {
			t.Fatalf("zero ack time: want 0 stale, got %d", metadata.Counters.StaleAcknowledged)
		}
	})

	// time.Duration(hours) * time.Hour overflows int64 long before math.MaxInt,
	// which used to push staleCutoff into the future and count every ack while
	// the client (plain float milliseconds) marked none of them.
	t.Run("out-of-range threshold counts nothing instead of overflowing the cutoff", func(t *testing.T) {
		setThreshold(9999999999)
		alerts := []*webuimodels.DashboardAlert{aliceStale, bobStale, aliceFresh}
		installAlertCache(t, alerts...)
		filters := webuimodels.DashboardFilters{DisplayMode: webuimodels.DisplayModeAcknowledge}
		metadata := buildDashboardMetadata(alerts, alerts, filters, userID, "", "alice")
		if metadata.Counters.StaleAcknowledged != 0 {
			t.Fatalf("out-of-range threshold: want 0 stale, got %d", metadata.Counters.StaleAcknowledged)
		}
	})

	// The badge is a preview of the Acknowledged view, so it must read the same
	// number from every mode the dashboard can be parked in. Each of these modes
	// hands buildDashboardMetadata a different allAlerts slice (classic: the
	// active store; resolved: the resolved store; hidden/full: a mix) - none of
	// which is the acked set, which is why the count must not be derived from it.
	t.Run("every non-acknowledge mode counts the acknowledge view's rows", func(t *testing.T) {
		setThreshold(4)
		installAlertCache(t, aliceStale, bobStale, aliceFresh)

		resolvedStore := []*webuimodels.DashboardAlert{
			{Fingerprint: "resolved-stale", IsAcknowledged: true, IsResolved: true, AcknowledgedBy: "carol", AcknowledgedAt: old},
		}
		activeStore := []*webuimodels.DashboardAlert{aliceStale, bobStale, aliceFresh}

		modes := []struct {
			mode      webuimodels.DashboardDisplayMode
			allAlerts []*webuimodels.DashboardAlert
		}{
			{webuimodels.DisplayModeClassic, activeStore},
			{webuimodels.DisplayModeResolved, resolvedStore},
			{webuimodels.DisplayModeHidden, append(append([]*webuimodels.DashboardAlert{}, activeStore...), resolvedStore...)},
			{webuimodels.DisplayModeFull, append(append([]*webuimodels.DashboardAlert{}, activeStore...), resolvedStore...)},
		}

		for _, tc := range modes {
			t.Run(string(tc.mode), func(t *testing.T) {
				filters := webuimodels.DashboardFilters{DisplayMode: tc.mode}
				metadata := buildDashboardMetadata(tc.allAlerts, nil, filters, userID, "", "alice")
				if metadata.Counters.StaleAcknowledged != 2 {
					t.Fatalf("%s: want the acknowledge view's 2 stale acks, got %d", tc.mode, metadata.Counters.StaleAcknowledged)
				}
			})
		}
	})
}
