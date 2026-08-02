package services

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"notificator/internal/alertmanager"
	alertpb "notificator/internal/backend/proto/alert"
	"notificator/internal/models"
	"notificator/internal/webui/client"
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
	clientNames []string
}

func (f *fakeAlertFetcher) FetchAllAlertsDetailed() ([]alertmanager.AlertWithSource, map[string]error) {
	return f.alerts, f.fetchErrors
}

func (f *fakeAlertFetcher) GetClientNames() []string {
	return f.clientNames
}

// TestAlertCache_RefreshKeepsAcknowledgementState covers the SSE regression where
// a refresh cycle compared the cache against a collaboration-free poll result and
// pushed that stripped alert to browsers, flipping acked alerts back to un-acked.
func TestAlertCache_RefreshKeepsAcknowledgementState(t *testing.T) {
	amAlert := alertmanager.AlertWithSource{
		Alert: models.Alert{
			Labels:      map[string]string{"alertname": "HighMemoryUsage"},
			Annotations: map[string]string{"summary": "Memory is high"},
			Status:      models.AlertStatus{State: "firing"},
			StartsAt:    time.Now().Add(-time.Hour),
		},
		Source: "prod",
	}

	cache := NewAlertCache(nil, nil, 90, 10*time.Second)
	cache.alertmanagerClient = &fakeAlertFetcher{alerts: []alertmanager.AlertWithSource{amAlert}}
	cache.refreshAlerts()

	updates := cache.Subscribe()
	defer cache.Unsubscribe(updates)

	fingerprint := cache.convertToDashboardAlert(amAlert.Alert, amAlert.Source).Fingerprint
	acknowledgedAt := time.Now().Add(-time.Minute)
	if !cache.MutateAlert(fingerprint, func(a *webuimodels.DashboardAlert) {
		a.IsAcknowledged = true
		a.AcknowledgedBy = "alice"
		a.AcknowledgedAt = acknowledgedAt
		a.CommentCount = 2
	}) {
		t.Fatal("alert should be cached after the seeding refresh")
	}

	updatedAt := func() time.Time {
		alert, ok := cache.GetAlert(fingerprint)
		if !ok {
			t.Fatal("alert disappeared from the cache")
		}
		return alert.UpdatedAt
	}
	beforeRefresh := updatedAt()

	// The acknowledgement itself is what has to reach browsers, with real state.
	select {
	case update := <-updates:
		if len(update.UpdatedAlerts) != 1 || !update.UpdatedAlerts[0].IsAcknowledged {
			t.Fatalf("acknowledgement should be pushed over SSE with ack state, got %+v", update.UpdatedAlerts)
		}
	case <-time.After(time.Second):
		t.Fatal("acknowledging an alert should push an SSE update")
	}

	// Three cycles with an unchanged Alertmanager payload: nothing should move.
	for i := 0; i < 3; i++ {
		cache.refreshAlerts()

		select {
		case update := <-updates:
			t.Fatalf("cycle %d: unchanged alert should not be pushed over SSE, got %d updated alerts", i, len(update.UpdatedAlerts))
		default:
		}

		cached, _ := cache.GetAlert(fingerprint)
		if !cached.IsAcknowledged || cached.AcknowledgedBy != "alice" || cached.CommentCount != 2 {
			t.Fatalf("cycle %d: collaboration state lost: %+v", i, cached)
		}
		if !cached.UpdatedAt.Equal(beforeRefresh) {
			t.Fatalf("cycle %d: UpdatedAt advanced without a real change", i)
		}
	}

	// A real Alertmanager-side change must still be pushed, with ack state intact.
	changed := amAlert
	changed.Alert.Annotations = map[string]string{"summary": "Memory is critical"}
	cache.alertmanagerClient = &fakeAlertFetcher{alerts: []alertmanager.AlertWithSource{changed}}
	cache.refreshAlerts()

	select {
	case update := <-updates:
		if len(update.UpdatedAlerts) != 1 {
			t.Fatalf("expected 1 updated alert, got %d", len(update.UpdatedAlerts))
		}
		pushed := update.UpdatedAlerts[0]
		if !pushed.IsAcknowledged || pushed.AcknowledgedBy != "alice" || pushed.CommentCount != 2 {
			t.Errorf("SSE payload lost collaboration state: %+v", pushed)
		}
		if pushed.Summary != "Memory is critical" {
			t.Errorf("SSE payload should carry the new summary, got %q", pushed.Summary)
		}
	case <-time.After(time.Second):
		t.Fatal("a changed alert should be pushed over SSE")
	}
}

// fakeCollabLoader drives the backend side of the collaboration loaders, which
// are the only path left for backend-side ack and comment changes to reach
// browsers now that hasPolledStateChanged ignores those fields.
type fakeCollabLoader struct {
	acks      map[string]*alertpb.Acknowledgment
	acksErr   error
	counts    map[string]int
	countsErr error
}

func (f *fakeCollabLoader) IsConnected() bool { return true }

func (f *fakeCollabLoader) GetAllAcknowledgedAlerts([]string) (map[string]*alertpb.Acknowledgment, error) {
	return f.acks, f.acksErr
}

func (f *fakeCollabLoader) GetCommentCountsBatch([]string) (map[string]int, error) {
	return f.counts, f.countsErr
}

// seedCachedAlert returns a cache holding exactly one firing alert with the
// collaboration seam wired to loader, plus that alert's fingerprint and a
// subscriber with no update pending.
func seedCachedAlert(t *testing.T, loader collabLoader) (*AlertCache, string, chan *webuimodels.DashboardIncrementalUpdate) {
	t.Helper()

	amAlert := alertmanager.AlertWithSource{
		Alert: models.Alert{
			Labels:      map[string]string{"alertname": "HighMemoryUsage"},
			Annotations: map[string]string{"summary": "Memory is high"},
			Status:      models.AlertStatus{State: "firing"},
			StartsAt:    time.Now().Add(-time.Hour),
		},
		Source: "prod",
	}

	cache := NewAlertCache(nil, nil, 90, 10*time.Second)
	cache.alertmanagerClient = &fakeAlertFetcher{alerts: []alertmanager.AlertWithSource{amAlert}}
	// collabClient is still nil here, so the seeding refresh spawns no loader.
	cache.refreshAlerts()
	cache.collabClient = loader

	updates := cache.Subscribe()
	t.Cleanup(func() { cache.Unsubscribe(updates) })

	return cache, cache.convertToDashboardAlert(amAlert.Alert, amAlert.Source).Fingerprint, updates
}

func nextUpdate(t *testing.T, updates chan *webuimodels.DashboardIncrementalUpdate) *webuimodels.DashboardIncrementalUpdate {
	t.Helper()
	select {
	case update := <-updates:
		return update
	case <-time.After(time.Second):
		t.Fatal("expected an SSE update")
		return nil
	}
}

// expectNoUpdate asserts nothing is queued. The loaders notify synchronously, so
// a non-blocking read is enough.
func expectNoUpdate(t *testing.T, updates chan *webuimodels.DashboardIncrementalUpdate) {
	t.Helper()
	select {
	case update := <-updates:
		t.Fatalf("unexpected SSE update: %+v", update.UpdatedAlerts)
	default:
	}
}

func TestAlertCache_LoadAcknowledgmentsPushesOnlyRealChanges(t *testing.T) {
	createdAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	firstAck := func() *alertpb.Acknowledgment {
		return &alertpb.Acknowledgment{
			Username:  "alice",
			Reason:    "investigating",
			CreatedAt: timestamppb.New(createdAt),
		}
	}

	tests := []struct {
		name       string
		second     *alertpb.Acknowledgment
		wantPushed bool
	}{
		{
			name:       "identical acknowledgement is pushed once",
			second:     firstAck(),
			wantPushed: false,
		},
		{
			name:       "changed reason is pushed",
			second:     &alertpb.Acknowledgment{Username: "alice", Reason: "escalated", CreatedAt: timestamppb.New(createdAt)},
			wantPushed: true,
		},
		{
			name:       "changed createdAt is pushed",
			second:     &alertpb.Acknowledgment{Username: "alice", Reason: "investigating", CreatedAt: timestamppb.New(createdAt.Add(time.Second))},
			wantPushed: true,
		},
		{
			name:       "re-acknowledgement by another user is pushed",
			second:     &alertpb.Acknowledgment{Username: "bob", Reason: "investigating", CreatedAt: timestamppb.New(createdAt)},
			wantPushed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &fakeCollabLoader{}
			cache, fingerprint, updates := seedCachedAlert(t, loader)

			loader.acks = map[string]*alertpb.Acknowledgment{fingerprint: firstAck()}
			cache.loadAcknowledgmentsEfficiently()

			first := nextUpdate(t, updates).UpdatedAlerts
			if len(first) != 1 || !first[0].IsAcknowledged || first[0].AcknowledgedBy != "alice" {
				t.Fatalf("first load should push the acknowledgement, got %+v", first)
			}

			loader.acks = map[string]*alertpb.Acknowledgment{fingerprint: tt.second}
			cache.loadAcknowledgmentsEfficiently()

			if !tt.wantPushed {
				expectNoUpdate(t, updates)
				return
			}

			second := nextUpdate(t, updates).UpdatedAlerts
			if len(second) != 1 {
				t.Fatalf("expected 1 updated alert, got %d", len(second))
			}
			pushed := second[0]
			if pushed.AcknowledgedBy != tt.second.Username ||
				pushed.AcknowledgeReason != tt.second.Reason ||
				!pushed.AcknowledgedAt.Equal(tt.second.CreatedAt.AsTime()) {
				t.Errorf("pushed payload does not carry the new acknowledgement: %+v", pushed)
			}
		})
	}
}

func TestAlertCache_LoadCommentCountsPushesOnlyRealChanges(t *testing.T) {
	loader := &fakeCollabLoader{}
	cache, fingerprint, updates := seedCachedAlert(t, loader)

	if !cache.MutateAlert(fingerprint, func(a *webuimodels.DashboardAlert) {
		a.IsAcknowledged = true
		a.AcknowledgedBy = "alice"
		a.AcknowledgedAt = time.Now().Add(-time.Minute)
	}) {
		t.Fatal("alert should be cached after the seeding refresh")
	}
	nextUpdate(t, updates) // the acknowledgement push itself

	loader.counts = map[string]int{fingerprint: 2}
	cache.loadCommentCountsEfficiently()

	pushed := nextUpdate(t, updates).UpdatedAlerts
	if len(pushed) != 1 || pushed[0].CommentCount != 2 {
		t.Fatalf("a new comment count should be pushed, got %+v", pushed)
	}
	if !pushed[0].IsAcknowledged || pushed[0].AcknowledgedBy != "alice" {
		t.Errorf("comment-count push lost acknowledgement state: %+v", pushed[0])
	}

	// Same count next cycle: nothing on the wire.
	cache.loadCommentCountsEfficiently()
	expectNoUpdate(t, updates)

	// A failing batch keeps the last known counts and stays silent: a transient
	// backend problem must not wipe every comment badge on connected dashboards.
	loader.countsErr = errors.New("backend down")
	cache.loadCommentCountsEfficiently()
	expectNoUpdate(t, updates)

	cached, ok := cache.GetAlert(fingerprint)
	if !ok {
		t.Fatal("alert disappeared from the cache")
	}
	if cached.CommentCount != 2 {
		t.Errorf("comment counts must survive a failed batch query, got %d", cached.CommentCount)
	}

	// Recovery: a successful response genuinely missing the fingerprint still
	// resolves to 0 — absence in a healthy snapshot means "no comments".
	loader.countsErr = nil
	loader.counts = map[string]int{}
	cache.loadCommentCountsEfficiently()

	pushed = nextUpdate(t, updates).UpdatedAlerts
	if len(pushed) != 1 || pushed[0].CommentCount != 0 {
		t.Fatalf("an absent fingerprint in a healthy response should reset to 0, got %+v", pushed)
	}
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

func TestAlertCache_ResolvedCountInvalidationDuringFetch(t *testing.T) {
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)

	// An invalidation that lands mid-fetch must discard the pre-mutation count,
	// otherwise the cleared counter is served as fresh for resolvedCountTTL.
	gen := cache.resolvedCountGen
	cache.InvalidateResolvedAlertsCount()
	cache.publishResolvedCount(gen, 2500)

	if cache.resolvedCount != 0 || !cache.resolvedCountFetched.IsZero() {
		t.Errorf("stale count published: count = %d, fetched = %v", cache.resolvedCount, cache.resolvedCountFetched)
	}

	// A fetch with no invalidation in flight publishes normally.
	gen = cache.resolvedCountGen
	cache.publishResolvedCount(gen, 2500)

	if cache.resolvedCount != 2500 || cache.resolvedCountFetched.IsZero() {
		t.Errorf("fresh count not published: count = %d, fetched = %v", cache.resolvedCount, cache.resolvedCountFetched)
	}
}

func TestAlertCache_ResolvedCountCacheAndFallback(t *testing.T) {
	// Both branches return before touching the backend client, so a nil one is
	// enough to exercise them.
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)

	// A fresh count is served from the cache - that skipped query is the whole
	// point of the counter cache.
	cache.resolvedCount = 42
	cache.resolvedCountFetched = time.Now()
	if got := cache.GetResolvedAlertsCount(); got != 42 {
		t.Errorf("cache hit: got %d, want 42", got)
	}

	// Stale with no reachable backend must serve the last known value, not 0:
	// returning 0 would blank the Resolved tile on every backend blip.
	cache.InvalidateResolvedAlertsCount()
	if got := cache.GetResolvedAlertsCount(); got != 42 {
		t.Errorf("stale, no backend: got %d, want 42", got)
	}
}

func TestAlertCache_GetLiveAlert(t *testing.T) {
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)

	const fingerprint = "live-fingerprint"
	cache.UpdateAlert(&webuimodels.DashboardAlert{
		Fingerprint: fingerprint,
		Status:      webuimodels.AlertStatus{State: "suppressed", SilencedBy: []string{"silence-1"}},
	})

	if got := cache.GetLiveAlert("missing"); got != nil {
		t.Errorf("GetLiveAlert should return nil for a fingerprint absent from the live cache, got %+v", got)
	}

	live := cache.GetLiveAlert(fingerprint)
	if live == nil {
		t.Fatal("GetLiveAlert should return the cached alert")
	}
	if live.Status.State != "suppressed" || len(live.Status.SilencedBy) != 1 {
		t.Errorf("unexpected live status: %+v", live.Status)
	}

	// Snapshot semantics: writing through the result must not touch the cache.
	live.Status.State = "firing"
	if again := cache.GetLiveAlert(fingerprint); again.Status.State != "suppressed" {
		t.Errorf("GetLiveAlert returned a cache-resident pointer: State = %q, want \"suppressed\"", again.Status.State)
	}
}

// TestAlertCache_AcknowledgmentDoesNotSurviveResolution walks the resolve →
// re-fire cycle: an acknowledged alert resolves, the backend drops the live ack
// row along with it, and the next firing of the same labels — same fingerprint —
// must come back un-acknowledged instead of vanishing from the classic dashboard.
func TestAlertCache_AcknowledgmentDoesNotSurviveResolution(t *testing.T) {
	firing := alertmanager.AlertWithSource{
		Alert: models.Alert{
			Labels:   map[string]string{"alertname": "HighMemoryUsage", "instance": "db-1"},
			Status:   models.AlertStatus{State: "firing"},
			StartsAt: time.Now().Add(-time.Hour),
		},
		Source: "prod",
	}

	cache := NewAlertCache(nil, nil, 90, 10*time.Second)
	fingerprint := cache.convertToDashboardAlert(firing.Alert, firing.Source).Fingerprint

	fetcher := &fakeAlertFetcher{alerts: []alertmanager.AlertWithSource{firing}}
	cache.alertmanagerClient = fetcher
	cache.refreshAlerts()

	acked := map[string]*alertpb.Acknowledgment{
		fingerprint: {
			Username:  "alice",
			Reason:    "looking into it",
			CreatedAt: timestamppb.New(time.Now().Add(-30 * time.Minute)),
		},
	}
	cache.applyAcknowledgments(acked)

	alert, ok := cache.GetAlert(fingerprint)
	if !ok || !alert.IsAcknowledged || alert.AcknowledgedBy != "alice" {
		t.Fatalf("alert should be acknowledged by alice, got %+v", alert)
	}

	// A still-firing acknowledged alert stays acknowledged across refresh cycles.
	cache.refreshAlerts()
	cache.applyAcknowledgments(acked)
	if alert, _ := cache.GetAlert(fingerprint); !alert.IsAcknowledged {
		t.Error("a still-firing acknowledged alert must stay acknowledged across refreshes")
	}

	// The alert resolves and leaves the cache.
	fetcher.alerts = nil
	cache.refreshAlerts()
	if _, ok := cache.GetAlert(fingerprint); ok {
		t.Fatal("alert should leave the cache once its source stops reporting it")
	}

	// It fires again with identical labels; the backend no longer holds the ack.
	fetcher.alerts = []alertmanager.AlertWithSource{firing}
	cache.refreshAlerts()
	cache.applyAcknowledgments(map[string]*alertpb.Acknowledgment{})

	refired, ok := cache.GetAlert(fingerprint)
	if !ok {
		t.Fatal("re-firing alert should be back in the cache")
	}
	if refired.IsAcknowledged {
		t.Errorf("re-firing alert inherited the previous incident's acknowledgment: by=%q at=%v", refired.AcknowledgedBy, refired.AcknowledgedAt)
	}
	if refired.AcknowledgedBy != "" || !refired.AcknowledgedAt.IsZero() || refired.AcknowledgeReason != "" {
		t.Errorf("acknowledgment fields not cleared: %+v", refired)
	}
}

// TestAlertCache_CaptureAlertFiredSnapshot reproduces #161: a refresh that
// sees a brand-new alert used to hand the CaptureAlertFired goroutine the
// pointer it had just stored in ac.alerts, rather than a snapshot. The very
// next refresh cycle mutates that same struct's Status and Annotations under
// ac.mu, in updateExistingAlert, while the goroutine reads them with no lock
// held. Run with -race: this fails on the pre-fix code (dashAlert handed
// straight to CaptureAlertFired) and passes once the goroutine gets its own
// copy.
func TestAlertCache_CaptureAlertFiredSnapshot(t *testing.T) {
	// grpc.NewClient connects lazily, so this never dials: IsConnected() is
	// true immediately, and CaptureAlertFired reads its alert argument before
	// it ever touches the network.
	// Connect requires the shared service token since the gRPC auth interceptor
	// landed; any 32+ char value satisfies the client-side length check (the
	// lazy grpc.NewClient never dials, so it is never validated server-side).
	t.Setenv("NOTIFICATOR_SERVICE_TOKEN", "0123456789abcdef0123456789abcdef0123456789abcdef")
	backendClient := client.NewBackendClient("127.0.0.1:1")
	if err := backendClient.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	cache := NewAlertCache(nil, backendClient, 90, 10*time.Second)

	var firing []alertmanager.AlertWithSource
	for i := 0; i < 5; i++ {
		firing = append(firing, alertmanager.AlertWithSource{
			Alert: models.Alert{
				Labels:      map[string]string{"alertname": fmt.Sprintf("Race%d", i)},
				Annotations: map[string]string{"summary": "before"},
				Status:      models.AlertStatus{State: "firing"},
				StartsAt:    time.Now(),
			},
			Source: "prod",
		})
	}

	cache.alertmanagerClient = &fakeAlertFetcher{alerts: firing}
	cache.refreshAlerts() // inserts the alerts, spawns a CaptureAlertFired goroutine per alert

	silenced := make([]alertmanager.AlertWithSource, len(firing))
	for i, a := range firing {
		silenced[i] = a
		silenced[i].Alert.Annotations = map[string]string{"summary": "after"}
		silenced[i].Alert.Status = models.AlertStatus{State: "suppressed", SilencedBy: []string{"sil-1"}}
	}
	cache.alertmanagerClient = &fakeAlertFetcher{alerts: silenced}
	cache.refreshAlerts() // same fingerprints: rewrites Status/Annotations on the cached structs

	// Give the CaptureAlertFired goroutines time to run their (unsynchronized,
	// pre-fix) field reads before the test process exits.
	time.Sleep(300 * time.Millisecond)
}

// TestAlertCache_GetSourceStatuses covers the success -> failure -> recovery
// transitions the dashboard's Sources pill relies on, plus a source that has
// never answered since startup (unreachable, not merely stale).
func TestAlertCache_GetSourceStatuses(t *testing.T) {
	prodAlert := alertmanager.AlertWithSource{
		Alert: models.Alert{
			Labels:   map[string]string{"alertname": "HighMemoryUsage"},
			Status:   models.AlertStatus{State: "firing"},
			StartsAt: time.Now().Add(-time.Hour),
		},
		Source: "prod",
	}

	fetcher := &fakeAlertFetcher{clientNames: []string{"prod", "dead"}}
	cache := NewAlertCache(nil, nil, 90, 10*time.Second)
	cache.alertmanagerClient = fetcher

	// "dead" never answers from the very first cycle onward.
	fetcher.fetchErrors = map[string]error{"dead": errors.New("connection refused")}

	// 1. success: prod reports one alert, dead fails.
	fetcher.alerts = []alertmanager.AlertWithSource{prodAlert}
	cache.refreshAlerts()

	statuses := cache.GetSourceStatuses()
	prod, ok := statuses["prod"]
	if !ok {
		t.Fatal("prod should be tracked after a successful poll")
	}
	if prod.state() != "live" || prod.ConsecutiveFailures != 0 || prod.AlertCount != 1 {
		t.Errorf("prod after success: expected live/0/1, got state=%s failures=%d count=%d", prod.state(), prod.ConsecutiveFailures, prod.AlertCount)
	}
	if prod.LastSuccessAt.IsZero() {
		t.Error("prod LastSuccessAt should be set after a successful poll")
	}

	dead, ok := statuses["dead"]
	if !ok {
		t.Fatal("dead should be tracked even though it has never succeeded")
	}
	if dead.state() != "unreachable" {
		t.Errorf("dead never succeeded: expected unreachable, got %s", dead.state())
	}
	if !dead.LastSuccessAt.IsZero() {
		t.Error("dead has never succeeded, LastSuccessAt must stay zero")
	}
	if dead.LastError == "" {
		t.Error("dead should carry the last fetch error")
	}

	// 2. failure: prod starts failing too. Below the threshold it stays live.
	fetcher.fetchErrors = map[string]error{
		"prod": errors.New("timeout"),
		"dead": errors.New("connection refused"),
	}
	fetcher.alerts = nil
	cache.refreshAlerts()
	cache.refreshAlerts()

	prod = cache.GetSourceStatuses()["prod"]
	if prod.state() != "live" || prod.ConsecutiveFailures != 2 {
		t.Errorf("prod after 2 failures: expected live/2, got state=%s failures=%d", prod.state(), prod.ConsecutiveFailures)
	}

	// A third consecutive failure crosses the staleness threshold.
	cache.refreshAlerts()
	prod = cache.GetSourceStatuses()["prod"]
	if prod.state() != "stale" || prod.ConsecutiveFailures != 3 {
		t.Errorf("prod after 3 failures: expected stale/3, got state=%s failures=%d", prod.state(), prod.ConsecutiveFailures)
	}
	if prod.LastError == "" {
		t.Error("prod should carry the last fetch error once stale")
	}

	// 3. recovery: prod answers again, its cached alert count reflects the kept alerts.
	fetcher.fetchErrors = map[string]error{"dead": errors.New("connection refused")}
	fetcher.alerts = []alertmanager.AlertWithSource{prodAlert}
	cache.refreshAlerts()

	prod = cache.GetSourceStatuses()["prod"]
	if prod.state() != "live" || prod.ConsecutiveFailures != 0 {
		t.Errorf("prod after recovery: expected live/0, got state=%s failures=%d", prod.state(), prod.ConsecutiveFailures)
	}
	if prod.LastError != "" {
		t.Errorf("prod after recovery: LastError should be cleared, got %q", prod.LastError)
	}

	views := cache.GetSourceStatusViews()
	if views["prod"].State != "live" {
		t.Errorf("GetSourceStatusViews should expose the computed state, got %+v", views["prod"])
	}
	if views["dead"].State != "unreachable" {
		t.Errorf("GetSourceStatusViews should expose unreachable, got %+v", views["dead"])
	}
}
