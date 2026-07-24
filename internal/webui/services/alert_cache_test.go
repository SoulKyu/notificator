package services

import (
	"errors"
	"sync"
	"testing"
	"time"

	"notificator/internal/alertmanager"
	"notificator/internal/models"
	webuimodels "notificator/internal/webui/models"
)

func TestAlertCache_UpdatedAtTracking(t *testing.T) {
	// Create a cache without dependencies for testing
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)

	// Test 1: UpdatedAt is set when alert is added
	t.Run("UpdatedAt is set when alert is added", func(t *testing.T) {
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-1",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Test alert summary",
		}

		beforeAdd := time.Now()
		cache.UpdateAlert(alert)
		afterAdd := time.Now()

		// Retrieve the alert
		cached, exists := cache.GetAlert(alert.Fingerprint)
		if !exists {
			t.Fatal("Alert should exist in cache after UpdateAlert")
		}

		// UpdatedAt should be set to current time
		if cached.UpdatedAt.Before(beforeAdd) || cached.UpdatedAt.After(afterAdd) {
			t.Errorf("UpdatedAt should be set to current time. Got %v, expected between %v and %v",
				cached.UpdatedAt, beforeAdd, afterAdd)
		}
	})

	// Test 2: UpdatedAt changes when alert is modified
	t.Run("UpdatedAt changes when alert status changes", func(t *testing.T) {
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-2",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Test alert summary",
		}

		// Add the initial alert
		cache.UpdateAlert(alert)

		// Get initial UpdatedAt
		cached, _ := cache.GetAlert(alert.Fingerprint)
		initialUpdatedAt := cached.UpdatedAt

		// Wait a moment to ensure time difference (use nanosecond precision)
		time.Sleep(1 * time.Millisecond)

		// Modify the alert status
		modifiedAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-2",
			Status: webuimodels.AlertStatus{
				State: "resolved", // Changed from firing to resolved
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Test alert summary",
		}

		beforeUpdate := time.Now()
		cache.UpdateAlert(modifiedAlert)
		afterUpdate := time.Now()

		// Retrieve the alert again
		updated, _ := cache.GetAlert(alert.Fingerprint)

		// UpdatedAt should have changed to a time within the update window
		if updated.UpdatedAt.Before(beforeUpdate) || updated.UpdatedAt.After(afterUpdate) {
			t.Errorf("UpdatedAt should be updated to current time after change. Got %v, expected between %v and %v",
				updated.UpdatedAt, beforeUpdate, afterUpdate)
		}

		// UpdatedAt should be after the initial time
		if !updated.UpdatedAt.After(initialUpdatedAt) {
			t.Errorf("UpdatedAt should have increased after change. Initial: %v, New: %v",
				initialUpdatedAt, updated.UpdatedAt)
		}
	})

	// Test 3: UpdatedAt is preserved when alert hasn't changed
	t.Run("UpdatedAt is preserved when alert unchanged", func(t *testing.T) {
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-3",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Test alert summary",
		}

		// Add the initial alert
		cache.UpdateAlert(alert)

		// Get initial UpdatedAt
		cached, _ := cache.GetAlert(alert.Fingerprint)
		initialUpdatedAt := cached.UpdatedAt

		// Wait a moment
		time.Sleep(1 * time.Millisecond)

		// Update with identical alert (no changes)
		identicalAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-3",
			Status: webuimodels.AlertStatus{
				State: "firing", // Same state
			},
			IsAcknowledged: false,                // Same
			CommentCount:   0,                    // Same
			Summary:        "Test alert summary", // Same
		}

		cache.UpdateAlert(identicalAlert)

		// Retrieve the alert again
		updated, _ := cache.GetAlert(alert.Fingerprint)

		// UpdatedAt should be preserved (unchanged) - exact match
		if !updated.UpdatedAt.Equal(initialUpdatedAt) {
			t.Errorf("UpdatedAt should be preserved when alert unchanged. Initial: %v, New: %v",
				initialUpdatedAt, updated.UpdatedAt)
		}
	})

	// Test 4: UpdatedAt changes when IsAcknowledged changes
	t.Run("UpdatedAt changes when IsAcknowledged changes", func(t *testing.T) {
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-4",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Test alert summary",
		}

		cache.UpdateAlert(alert)
		cached, _ := cache.GetAlert(alert.Fingerprint)
		initialUpdatedAt := cached.UpdatedAt

		time.Sleep(1 * time.Millisecond)

		// Change IsAcknowledged
		modifiedAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-4",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: true, // Changed
			CommentCount:   0,
			Summary:        "Test alert summary",
		}

		cache.UpdateAlert(modifiedAlert)

		updated, _ := cache.GetAlert(alert.Fingerprint)
		if !updated.UpdatedAt.After(initialUpdatedAt) {
			t.Error("UpdatedAt should change when IsAcknowledged changes")
		}
	})

	// Test 5: UpdatedAt changes when CommentCount changes
	t.Run("UpdatedAt changes when CommentCount changes", func(t *testing.T) {
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-5",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Test alert summary",
		}

		cache.UpdateAlert(alert)
		cached, _ := cache.GetAlert(alert.Fingerprint)
		initialUpdatedAt := cached.UpdatedAt

		time.Sleep(1 * time.Millisecond)

		// Change CommentCount
		modifiedAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-5",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   1, // Changed
			Summary:        "Test alert summary",
		}

		cache.UpdateAlert(modifiedAlert)

		updated, _ := cache.GetAlert(alert.Fingerprint)
		if !updated.UpdatedAt.After(initialUpdatedAt) {
			t.Error("UpdatedAt should change when CommentCount changes")
		}
	})

	// Test 6: UpdatedAt changes when Summary changes
	t.Run("UpdatedAt changes when Summary changes", func(t *testing.T) {
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-6",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Original summary",
		}

		cache.UpdateAlert(alert)
		cached, _ := cache.GetAlert(alert.Fingerprint)
		initialUpdatedAt := cached.UpdatedAt

		time.Sleep(1 * time.Millisecond)

		// Change Summary
		modifiedAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint-6",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Updated summary", // Changed
		}

		cache.UpdateAlert(modifiedAlert)

		updated, _ := cache.GetAlert(alert.Fingerprint)
		if !updated.UpdatedAt.After(initialUpdatedAt) {
			t.Error("UpdatedAt should change when Summary changes")
		}
	})
}

// FilterAlertsByLastUpdate tests the incremental update filtering logic used in dashboard handlers.
// This function replicates the core algorithm from processIncremental for testing purposes.
func FilterAlertsByLastUpdate(currentAlerts []*webuimodels.DashboardAlert, clientFingerprints map[string]bool, lastUpdate int64) (newAlerts, updatedAlerts []*webuimodels.DashboardAlert, removedAlerts []string) {
	newAlerts = []*webuimodels.DashboardAlert{}
	updatedAlerts = []*webuimodels.DashboardAlert{}
	removedAlerts = []string{}

	// Track current fingerprints for removal detection
	currentFingerprints := make(map[string]bool)

	for _, alert := range currentAlerts {
		currentFingerprints[alert.Fingerprint] = true

		if !clientFingerprints[alert.Fingerprint] {
			// Alert not in client's list = new alert (always include regardless of lastUpdate)
			newAlerts = append(newAlerts, alert)
		} else {
			// Alert exists in client, only include if it was updated since lastUpdate
			// Convert alert's UpdatedAt to milliseconds and compare with lastUpdate
			alertUpdateMs := alert.UpdatedAt.UnixMilli()
			if lastUpdate == 0 || alertUpdateMs > lastUpdate {
				// Include alert if no lastUpdate provided (first sync) or if alert was updated after lastUpdate
				updatedAlerts = append(updatedAlerts, alert)
			}
		}
	}

	// Find removed alerts (in client but not in current)
	for fingerprint := range clientFingerprints {
		if !currentFingerprints[fingerprint] {
			removedAlerts = append(removedAlerts, fingerprint)
		}
	}

	return newAlerts, updatedAlerts, removedAlerts
}

func TestFilterAlertsByLastUpdate(t *testing.T) {
	// Create a base time for consistent testing
	baseTime := time.Now()

	t.Run("New alerts are always included regardless of lastUpdate", func(t *testing.T) {
		// Alert updated 10 seconds ago
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "new-alert-fp",
			UpdatedAt:   baseTime.Add(-10 * time.Second),
		}

		currentAlerts := []*webuimodels.DashboardAlert{alert}
		clientFingerprints := make(map[string]bool) // Client doesn't have this alert

		// Set lastUpdate to 5 seconds ago (after alert's update time)
		lastUpdate := baseTime.Add(-5 * time.Second).UnixMilli()

		newAlerts, updatedAlerts, _ := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		if len(newAlerts) != 1 {
			t.Errorf("Expected 1 new alert, got %d", len(newAlerts))
		}
		if len(updatedAlerts) != 0 {
			t.Errorf("Expected 0 updated alerts, got %d", len(updatedAlerts))
		}
	})

	t.Run("Updated alerts are filtered by lastUpdate", func(t *testing.T) {
		// Alert updated 10 seconds ago (before lastUpdate)
		oldAlert := &webuimodels.DashboardAlert{
			Fingerprint: "old-alert-fp",
			UpdatedAt:   baseTime.Add(-10 * time.Second),
		}

		// Alert updated 2 seconds ago (after lastUpdate)
		recentAlert := &webuimodels.DashboardAlert{
			Fingerprint: "recent-alert-fp",
			UpdatedAt:   baseTime.Add(-2 * time.Second),
		}

		currentAlerts := []*webuimodels.DashboardAlert{oldAlert, recentAlert}
		clientFingerprints := map[string]bool{
			"old-alert-fp":    true,
			"recent-alert-fp": true,
		}

		// Set lastUpdate to 5 seconds ago
		lastUpdate := baseTime.Add(-5 * time.Second).UnixMilli()

		newAlerts, updatedAlerts, _ := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		if len(newAlerts) != 0 {
			t.Errorf("Expected 0 new alerts, got %d", len(newAlerts))
		}
		if len(updatedAlerts) != 1 {
			t.Errorf("Expected 1 updated alert (only recent one), got %d", len(updatedAlerts))
		}
		if len(updatedAlerts) == 1 && updatedAlerts[0].Fingerprint != "recent-alert-fp" {
			t.Errorf("Expected recent-alert-fp to be included, got %s", updatedAlerts[0].Fingerprint)
		}
	})

	t.Run("All alerts returned when lastUpdate is 0 (first sync)", func(t *testing.T) {
		alert1 := &webuimodels.DashboardAlert{
			Fingerprint: "alert-1",
			UpdatedAt:   baseTime.Add(-1 * time.Hour),
		}
		alert2 := &webuimodels.DashboardAlert{
			Fingerprint: "alert-2",
			UpdatedAt:   baseTime.Add(-30 * time.Minute),
		}

		currentAlerts := []*webuimodels.DashboardAlert{alert1, alert2}
		clientFingerprints := map[string]bool{
			"alert-1": true,
			"alert-2": true,
		}

		// lastUpdate = 0 means first sync, include all
		lastUpdate := int64(0)

		newAlerts, updatedAlerts, _ := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		if len(newAlerts) != 0 {
			t.Errorf("Expected 0 new alerts, got %d", len(newAlerts))
		}
		if len(updatedAlerts) != 2 {
			t.Errorf("Expected 2 updated alerts when lastUpdate=0, got %d", len(updatedAlerts))
		}
	})

	t.Run("Removed alerts are always reported", func(t *testing.T) {
		// Server only has one alert
		currentAlert := &webuimodels.DashboardAlert{
			Fingerprint: "current-alert",
			UpdatedAt:   baseTime,
		}

		currentAlerts := []*webuimodels.DashboardAlert{currentAlert}
		// Client has two alerts (one has been removed from server)
		clientFingerprints := map[string]bool{
			"current-alert": true,
			"removed-alert": true,
		}

		lastUpdate := baseTime.Add(-5 * time.Second).UnixMilli()

		_, _, removedAlerts := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		if len(removedAlerts) != 1 {
			t.Errorf("Expected 1 removed alert, got %d", len(removedAlerts))
		}
		if len(removedAlerts) == 1 && removedAlerts[0] != "removed-alert" {
			t.Errorf("Expected removed-alert to be in removedAlerts, got %s", removedAlerts[0])
		}
	})

	t.Run("Mix of new, updated, unchanged, and removed alerts", func(t *testing.T) {
		// New alert (not in client's list)
		newAlert := &webuimodels.DashboardAlert{
			Fingerprint: "brand-new-alert",
			UpdatedAt:   baseTime.Add(-20 * time.Second), // Even old timestamp should be included as new
		}

		// Updated alert (in client's list, updated recently)
		recentlyUpdated := &webuimodels.DashboardAlert{
			Fingerprint: "recently-updated",
			UpdatedAt:   baseTime.Add(-1 * time.Second),
		}

		// Unchanged alert (in client's list, not updated since lastUpdate)
		unchangedAlert := &webuimodels.DashboardAlert{
			Fingerprint: "unchanged-alert",
			UpdatedAt:   baseTime.Add(-1 * time.Hour),
		}

		currentAlerts := []*webuimodels.DashboardAlert{newAlert, recentlyUpdated, unchangedAlert}
		clientFingerprints := map[string]bool{
			"recently-updated": true,
			"unchanged-alert":  true,
			"was-removed":      true, // This alert no longer exists on server
		}

		// Set lastUpdate to 5 seconds ago
		lastUpdate := baseTime.Add(-5 * time.Second).UnixMilli()

		newAlerts, updatedAlerts, removedAlerts := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		if len(newAlerts) != 1 {
			t.Errorf("Expected 1 new alert, got %d", len(newAlerts))
		}
		if len(updatedAlerts) != 1 {
			t.Errorf("Expected 1 updated alert (only recently-updated), got %d", len(updatedAlerts))
		}
		if len(removedAlerts) != 1 {
			t.Errorf("Expected 1 removed alert, got %d", len(removedAlerts))
		}

		// Verify the unchanged alert is NOT in updatedAlerts
		for _, alert := range updatedAlerts {
			if alert.Fingerprint == "unchanged-alert" {
				t.Error("unchanged-alert should NOT be in updatedAlerts since it hasn't changed since lastUpdate")
			}
		}
	})

	t.Run("lastUpdate exact boundary (alert updated at exactly lastUpdate time)", func(t *testing.T) {
		exactTime := baseTime.Add(-5 * time.Second)
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "boundary-alert",
			UpdatedAt:   exactTime,
		}

		currentAlerts := []*webuimodels.DashboardAlert{alert}
		clientFingerprints := map[string]bool{
			"boundary-alert": true,
		}

		// lastUpdate is exactly at the alert's update time
		lastUpdate := exactTime.UnixMilli()

		_, updatedAlerts, _ := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		// Alert updated at exactly lastUpdate time should NOT be included
		// (we only include alerts where UpdatedAt > lastUpdate, not >=)
		if len(updatedAlerts) != 0 {
			t.Errorf("Alert updated at exactly lastUpdate time should not be included, got %d alerts", len(updatedAlerts))
		}
	})

	t.Run("lastUpdate just before alert update time", func(t *testing.T) {
		alertTime := baseTime.Add(-5 * time.Second)
		alert := &webuimodels.DashboardAlert{
			Fingerprint: "just-after-alert",
			UpdatedAt:   alertTime,
		}

		currentAlerts := []*webuimodels.DashboardAlert{alert}
		clientFingerprints := map[string]bool{
			"just-after-alert": true,
		}

		// lastUpdate is 1 millisecond before the alert's update time
		lastUpdate := alertTime.UnixMilli() - 1

		_, updatedAlerts, _ := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		// Alert should be included since it was updated after lastUpdate
		if len(updatedAlerts) != 1 {
			t.Errorf("Alert updated just after lastUpdate should be included, got %d alerts", len(updatedAlerts))
		}
	})

	t.Run("Empty alerts list", func(t *testing.T) {
		currentAlerts := []*webuimodels.DashboardAlert{}
		clientFingerprints := map[string]bool{
			"old-alert-1": true,
			"old-alert-2": true,
		}

		lastUpdate := baseTime.UnixMilli()

		newAlerts, updatedAlerts, removedAlerts := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		if len(newAlerts) != 0 {
			t.Errorf("Expected 0 new alerts, got %d", len(newAlerts))
		}
		if len(updatedAlerts) != 0 {
			t.Errorf("Expected 0 updated alerts, got %d", len(updatedAlerts))
		}
		if len(removedAlerts) != 2 {
			t.Errorf("Expected 2 removed alerts, got %d", len(removedAlerts))
		}
	})

	t.Run("Empty client fingerprints (all alerts are new)", func(t *testing.T) {
		alert1 := &webuimodels.DashboardAlert{
			Fingerprint: "alert-1",
			UpdatedAt:   baseTime,
		}
		alert2 := &webuimodels.DashboardAlert{
			Fingerprint: "alert-2",
			UpdatedAt:   baseTime.Add(-1 * time.Hour),
		}

		currentAlerts := []*webuimodels.DashboardAlert{alert1, alert2}
		clientFingerprints := make(map[string]bool) // Client has no alerts

		lastUpdate := baseTime.Add(-5 * time.Second).UnixMilli()

		newAlerts, updatedAlerts, removedAlerts := FilterAlertsByLastUpdate(currentAlerts, clientFingerprints, lastUpdate)

		if len(newAlerts) != 2 {
			t.Errorf("Expected 2 new alerts (all are new to client), got %d", len(newAlerts))
		}
		if len(updatedAlerts) != 0 {
			t.Errorf("Expected 0 updated alerts, got %d", len(updatedAlerts))
		}
		if len(removedAlerts) != 0 {
			t.Errorf("Expected 0 removed alerts, got %d", len(removedAlerts))
		}
	})
}

func TestAlertCache_HasAlertChanged(t *testing.T) {
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)

	baseAlert := &webuimodels.DashboardAlert{
		Fingerprint: "test-fingerprint",
		Status: webuimodels.AlertStatus{
			State: "firing",
		},
		IsAcknowledged: false,
		CommentCount:   0,
		Summary:        "Test summary",
	}

	t.Run("Returns false for identical alerts", func(t *testing.T) {
		newAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Test summary",
		}

		if cache.hasAlertChanged(baseAlert, newAlert) {
			t.Error("hasAlertChanged should return false for identical alerts")
		}
	})

	t.Run("Returns true when Status changes", func(t *testing.T) {
		newAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint",
			Status: webuimodels.AlertStatus{
				State: "resolved",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Test summary",
		}

		if !cache.hasAlertChanged(baseAlert, newAlert) {
			t.Error("hasAlertChanged should return true when Status changes")
		}
	})

	t.Run("Returns true when IsAcknowledged changes", func(t *testing.T) {
		newAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: true,
			CommentCount:   0,
			Summary:        "Test summary",
		}

		if !cache.hasAlertChanged(baseAlert, newAlert) {
			t.Error("hasAlertChanged should return true when IsAcknowledged changes")
		}
	})

	t.Run("Returns true when CommentCount changes", func(t *testing.T) {
		newAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   5,
			Summary:        "Test summary",
		}

		if !cache.hasAlertChanged(baseAlert, newAlert) {
			t.Error("hasAlertChanged should return true when CommentCount changes")
		}
	})

	t.Run("Returns true when Summary changes", func(t *testing.T) {
		newAlert := &webuimodels.DashboardAlert{
			Fingerprint: "test-fingerprint",
			Status: webuimodels.AlertStatus{
				State: "firing",
			},
			IsAcknowledged: false,
			CommentCount:   0,
			Summary:        "Different summary",
		}

		if !cache.hasAlertChanged(baseAlert, newAlert) {
			t.Error("hasAlertChanged should return true when Summary changes")
		}
	})
}

func TestAlertCache_SSEPubSub(t *testing.T) {
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)

	t.Run("Subscribe creates a channel and registers it", func(t *testing.T) {
		initialCount := cache.GetSubscriberCount()
		if initialCount != 0 {
			t.Errorf("Expected 0 initial subscribers, got %d", initialCount)
		}

		ch := cache.Subscribe()
		if ch == nil {
			t.Fatal("Subscribe should return a non-nil channel")
		}

		newCount := cache.GetSubscriberCount()
		if newCount != 1 {
			t.Errorf("Expected 1 subscriber after Subscribe, got %d", newCount)
		}

		// Cleanup
		cache.Unsubscribe(ch)
	})

	t.Run("Unsubscribe removes and closes the channel", func(t *testing.T) {
		ch := cache.Subscribe()
		initialCount := cache.GetSubscriberCount()
		if initialCount != 1 {
			t.Errorf("Expected 1 subscriber, got %d", initialCount)
		}

		cache.Unsubscribe(ch)

		finalCount := cache.GetSubscriberCount()
		if finalCount != 0 {
			t.Errorf("Expected 0 subscribers after Unsubscribe, got %d", finalCount)
		}

		// Verify channel is closed by attempting to read from it
		select {
		case _, ok := <-ch:
			if ok {
				t.Error("Channel should be closed but received value")
			}
		default:
			t.Error("Channel should be closed and receive should not block")
		}
	})

	t.Run("Multiple subscribers can be registered", func(t *testing.T) {
		ch1 := cache.Subscribe()
		ch2 := cache.Subscribe()
		ch3 := cache.Subscribe()

		if cache.GetSubscriberCount() != 3 {
			t.Errorf("Expected 3 subscribers, got %d", cache.GetSubscriberCount())
		}

		cache.Unsubscribe(ch1)
		if cache.GetSubscriberCount() != 2 {
			t.Errorf("Expected 2 subscribers after unsubscribe, got %d", cache.GetSubscriberCount())
		}

		cache.Unsubscribe(ch2)
		cache.Unsubscribe(ch3)
		if cache.GetSubscriberCount() != 0 {
			t.Errorf("Expected 0 subscribers after all unsubscribes, got %d", cache.GetSubscriberCount())
		}
	})

	t.Run("notifySubscribers sends update to all subscribers", func(t *testing.T) {
		ch1 := cache.Subscribe()
		ch2 := cache.Subscribe()
		defer cache.Unsubscribe(ch1)
		defer cache.Unsubscribe(ch2)

		update := &webuimodels.DashboardIncrementalUpdate{
			NewAlerts: []*webuimodels.DashboardAlert{
				{Fingerprint: "test-alert-1"},
			},
			RemovedAlerts:  []string{"removed-alert-1"},
			LastUpdateTime: time.Now().Unix(),
		}

		cache.notifySubscribers(update)

		// Both subscribers should receive the update
		select {
		case received := <-ch1:
			if len(received.NewAlerts) != 1 || received.NewAlerts[0].Fingerprint != "test-alert-1" {
				t.Error("ch1 did not receive expected update")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("ch1 did not receive update in time")
		}

		select {
		case received := <-ch2:
			if len(received.NewAlerts) != 1 || received.NewAlerts[0].Fingerprint != "test-alert-1" {
				t.Error("ch2 did not receive expected update")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("ch2 did not receive update in time")
		}
	})

	t.Run("notifySubscribers does not block on full channel", func(t *testing.T) {
		ch := cache.Subscribe()
		defer cache.Unsubscribe(ch)

		// Fill the channel buffer (buffer size is 10)
		for i := 0; i < 15; i++ {
			update := &webuimodels.DashboardIncrementalUpdate{
				LastUpdateTime: int64(i),
			}
			// This should not block even when channel is full
			cache.notifySubscribers(update)
		}

		// The function should complete without deadlock
		// First 10 updates should be in the channel, rest dropped
		receivedCount := 0
		for {
			select {
			case <-ch:
				receivedCount++
			default:
				goto done
			}
		}
	done:
		if receivedCount != 10 {
			t.Errorf("Expected 10 updates (buffer size), got %d", receivedCount)
		}
	})

	t.Run("notifySubscribers does nothing with no subscribers", func(t *testing.T) {
		// Ensure no subscribers
		if cache.GetSubscriberCount() != 0 {
			t.Fatalf("Expected 0 subscribers, got %d", cache.GetSubscriberCount())
		}

		// This should not panic or block
		update := &webuimodels.DashboardIncrementalUpdate{
			NewAlerts:      []*webuimodels.DashboardAlert{{Fingerprint: "test"}},
			LastUpdateTime: time.Now().Unix(),
		}
		cache.notifySubscribers(update)
		// If we get here without panic, the test passes
	})

	t.Run("Unsubscribe is idempotent for same channel", func(t *testing.T) {
		ch := cache.Subscribe()
		cache.Unsubscribe(ch)

		// Second unsubscribe should not panic
		cache.Unsubscribe(ch)

		if cache.GetSubscriberCount() != 0 {
			t.Errorf("Expected 0 subscribers, got %d", cache.GetSubscriberCount())
		}
	})
}

// fakeAlertFetcher lets tests control what a refresh cycle sees per source.
type fakeAlertFetcher struct {
	alerts      []alertmanager.AlertWithSource
	fetchErrors map[string]error
}

func (f *fakeAlertFetcher) FetchAllAlertsDetailed() ([]alertmanager.AlertWithSource, map[string]error) {
	return f.alerts, f.fetchErrors
}

func TestAlertCache_RefreshWithPartialFetchFailure(t *testing.T) {
	newAlert := func(name, source string) alertmanager.AlertWithSource {
		return alertmanager.AlertWithSource{
			Alert: models.Alert{
				Labels:   map[string]string{"alertname": name},
				Status:   models.AlertStatus{State: "firing"},
				StartsAt: time.Now().Add(-time.Hour),
			},
			Source: source,
		}
	}

	prodAlert := newAlert("ProdAlert", "prod")
	stagingAlert := newAlert("StagingAlert", "staging")

	cache := NewAlertCache(nil, nil, 90, 10*time.Second)
	prodFingerprint := cache.convertToDashboardAlert(prodAlert.Alert, prodAlert.Source).Fingerprint
	stagingFingerprint := cache.convertToDashboardAlert(stagingAlert.Alert, stagingAlert.Source).Fingerprint

	// Seed the cache with one alert per source via a healthy fetch.
	fetcher := &fakeAlertFetcher{alerts: []alertmanager.AlertWithSource{prodAlert, stagingAlert}}
	cache.alertmanagerClient = fetcher
	cache.refreshAlerts()

	if _, ok := cache.GetAlert(prodFingerprint); !ok {
		t.Fatal("prod alert should be cached after healthy refresh")
	}
	if _, ok := cache.GetAlert(stagingFingerprint); !ok {
		t.Fatal("staging alert should be cached after healthy refresh")
	}

	t.Run("Alerts from a failed source survive the cycle", func(t *testing.T) {
		fetcher.alerts = []alertmanager.AlertWithSource{stagingAlert}
		fetcher.fetchErrors = map[string]error{"prod": errors.New("connection refused")}
		cache.refreshAlerts()

		prodCached, ok := cache.GetAlert(prodFingerprint)
		if !ok {
			t.Fatal("prod alert must not be removed when its source failed to answer")
		}
		if prodCached.IsResolved || prodCached.Status.State == "resolved" {
			t.Error("prod alert must not be marked resolved when its source failed to answer")
		}
		if _, ok := cache.GetAlert(stagingFingerprint); !ok {
			t.Error("staging alert should still be cached")
		}
	})

	t.Run("Alerts from a healthy source still resolve normally", func(t *testing.T) {
		// staging no longer reports its original alert while prod is still down.
		fetcher.alerts = []alertmanager.AlertWithSource{newAlert("StagingAlert2", "staging")}
		fetcher.fetchErrors = map[string]error{"prod": errors.New("connection refused")}
		cache.refreshAlerts()

		if _, ok := cache.GetAlert(stagingFingerprint); ok {
			t.Error("staging alert should resolve when its healthy source no longer reports it")
		}
		if _, ok := cache.GetAlert(prodFingerprint); !ok {
			t.Error("prod alert must survive while its source is still failing")
		}
	})

	t.Run("All sources failing leaves the cache untouched", func(t *testing.T) {
		fetcher.alerts = nil
		fetcher.fetchErrors = map[string]error{
			"prod":    errors.New("connection refused"),
			"staging": errors.New("connection refused"),
		}
		cache.refreshAlerts()

		if _, ok := cache.GetAlert(prodFingerprint); !ok {
			t.Error("prod alert must survive a total fetch failure")
		}
	})

	t.Run("Recovered source reconciles again", func(t *testing.T) {
		// prod comes back with no alerts: its cached alert now genuinely resolved.
		fetcher.alerts = []alertmanager.AlertWithSource{}
		fetcher.fetchErrors = nil
		cache.refreshAlerts()

		if _, ok := cache.GetAlert(prodFingerprint); ok {
			t.Error("prod alert should resolve once its source answers without it")
		}
	})
}

// TestAlertCache_ConcurrentRefreshAndReads runs refresh-style writes (mutations
// of cached structs under ac.mu) concurrently with the read accessors, exactly
// like a browser polling the dashboard during the background refresh cycle.
// It fails under -race when GetAlert/GetAllAlerts hand out cache-resident
// pointers instead of snapshots.
func TestAlertCache_ConcurrentRefreshAndReads(t *testing.T) {
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)

	const fingerprint = "race-fingerprint"
	cache.UpdateAlert(&webuimodels.DashboardAlert{
		Fingerprint: fingerprint,
		Status:      webuimodels.AlertStatus{State: "firing"},
		Summary:     "race test",
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: the same field writes loadAcknowledgmentsEfficiently and the
	// resolve pass perform on cache-resident structs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			cache.MutateAlert(fingerprint, func(a *webuimodels.DashboardAlert) {
				a.IsAcknowledged = i%2 == 0
				a.AcknowledgedBy = "alice"
				a.Status.State = "resolved"
				a.CommentCount++
			})
		}
	}()

	// Readers: dereference returned alerts with no lock held, as handlers do.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if alert, ok := cache.GetAlert(fingerprint); ok {
					_ = alert.IsAcknowledged
					_ = alert.AcknowledgedBy
					_ = alert.Status.State
				}
				for _, alert := range cache.GetAllAlerts() {
					_ = alert.CommentCount
					_ = alert.Summary
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestAlertCache_MutateAlert verifies mutations reach the cache while snapshots
// returned by the accessors stay isolated from it.
func TestAlertCache_MutateAlert(t *testing.T) {
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)

	const fingerprint = "mutate-fingerprint"
	cache.UpdateAlert(&webuimodels.DashboardAlert{
		Fingerprint: fingerprint,
		Status:      webuimodels.AlertStatus{State: "firing"},
	})

	if cache.MutateAlert("missing", func(a *webuimodels.DashboardAlert) {}) {
		t.Error("MutateAlert should return false for an unknown fingerprint")
	}

	ok := cache.MutateAlert(fingerprint, func(a *webuimodels.DashboardAlert) {
		a.Status.State = "resolved"
		a.IsResolved = true
		a.CommentCount++
	})
	if !ok {
		t.Fatal("MutateAlert should return true for a cached fingerprint")
	}

	cached, exists := cache.GetAlert(fingerprint)
	if !exists {
		t.Fatal("alert should still be cached")
	}
	if cached.Status.State != "resolved" || !cached.IsResolved || cached.CommentCount != 1 {
		t.Errorf("mutation not visible on immediate read: %+v", cached)
	}

	// Writing through a returned snapshot must not touch the cache.
	cached.CommentCount = 99
	again, _ := cache.GetAlert(fingerprint)
	if again.CommentCount != 1 {
		t.Errorf("accessor returned a cache-resident pointer: CommentCount = %d, want 1", again.CommentCount)
	}
}
