package gui

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"

	"notificator/internal/models"
	"notificator/internal/notifier"
)

// BackgroundNotifier handles notifications in background mode
type BackgroundNotifier struct {
	config            notifier.NotificationConfig
	app               fyne.App
	lastNotifications map[string]time.Time
	mutex             sync.RWMutex
	currentFilters    *notifier.FilterState
	filterMutex       sync.RWMutex

	// Callback to show window when notification is clicked
	onNotificationClick func()
}

// NewBackgroundNotifier creates a new background notification manager
func NewBackgroundNotifier(config notifier.NotificationConfig, app fyne.App, onNotificationClick func()) *BackgroundNotifier {
	return &BackgroundNotifier{
		config:              config,
		app:                 app,
		lastNotifications:   make(map[string]time.Time),
		currentFilters:      &notifier.FilterState{},
		onNotificationClick: onNotificationClick,
	}
}

// UpdateFilters updates the current filter state for notification filtering
func (bn *BackgroundNotifier) UpdateFilters(filters notifier.FilterState) {
	bn.filterMutex.Lock()
	defer bn.filterMutex.Unlock()
	bn.currentFilters = &filters
}

// ProcessAlerts processes alerts and sends background notifications
func (bn *BackgroundNotifier) ProcessAlerts(newAlerts []models.Alert, previousAlerts []models.Alert) {
	if !bn.config.Enabled {
		return
	}

	// Create maps for efficient lookup
	prevAlertsMap := make(map[string]models.Alert)
	for _, alert := range previousAlerts {
		key := bn.getAlertKey(alert)
		prevAlertsMap[key] = alert
	}

	// Check for new or escalated alerts
	var notifiableAlerts []models.Alert

	for _, alert := range newAlerts {
		key := bn.getAlertKey(alert)

		// Skip if alert doesn't match notification rules
		if !bn.shouldNotify(alert) {
			continue
		}

		// Check if alert matches current UI filters (if enabled)
		if !bn.matchesFilters(alert) {
			continue
		}

		// Check if this is a new alert or status change
		if prevAlert, exists := prevAlertsMap[key]; exists {
			// Check if alert escalated
			if bn.isEscalation(prevAlert, alert) {
				notifiableAlerts = append(notifiableAlerts, alert)
			}
		} else {
			// New alert
			if alert.IsActive() {
				notifiableAlerts = append(notifiableAlerts, alert)
			}
		}
	}

	// Send notifications for qualifying alerts
	bn.sendNotifications(notifiableAlerts)
}

// shouldNotify determines if an alert should trigger a notification
func (bn *BackgroundNotifier) shouldNotify(alert models.Alert) bool {
	// Don't notify for silenced alerts
	if alert.IsSilenced() {
		return false
	}

	// Check severity rules
	if enabled, exists := bn.config.SeverityRules[alert.GetSeverity()]; !enabled || !exists {
		return false
	}

	// Critical only mode
	if bn.config.CriticalOnly && alert.GetSeverity() != "critical" {
		return false
	}

	// Check cooldown
	key := bn.getAlertKey(alert)
	bn.mutex.RLock()
	lastNotif, exists := bn.lastNotifications[key]
	bn.mutex.RUnlock()

	if exists && time.Since(lastNotif) < time.Duration(bn.config.CooldownSeconds)*time.Second {
		return false
	}

	return true
}

// matchesFilters checks if an alert matches the current UI filters
func (bn *BackgroundNotifier) matchesFilters(alert models.Alert) bool {
	if !bn.config.RespectFilters {
		return true
	}

	bn.filterMutex.RLock()
	filters := bn.currentFilters
	bn.filterMutex.RUnlock()

	if filters == nil {
		return true
	}

	// Apply search text filter
	if filters.SearchText != "" {
		searchText := strings.ToLower(filters.SearchText)
		searchMatch := strings.Contains(strings.ToLower(alert.GetAlertName()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetSummary()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetTeam()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetInstance()), searchText)
		if !searchMatch {
			return false
		}
	}

	// Apply severity filter
	if filters.SelectedSeverities != nil && !filters.SelectedSeverities["All"] &&
		!filters.SelectedSeverities[alert.GetSeverity()] {
		return false
	}

	// Apply status filter
	if filters.SelectedStatuses != nil && !filters.SelectedStatuses["All"] &&
		!filters.SelectedStatuses[alert.Status.State] {
		return false
	}

	// Apply team filter
	if filters.SelectedTeams != nil && !filters.SelectedTeams["All"] &&
		!filters.SelectedTeams[alert.GetTeam()] {
		return false
	}

	return true
}

// isEscalation checks if an alert has escalated in severity
func (bn *BackgroundNotifier) isEscalation(oldAlert, newAlert models.Alert) bool {
	severityOrder := map[string]int{
		"info":     1,
		"warning":  2,
		"critical": 3,
	}

	oldSev := severityOrder[oldAlert.GetSeverity()]
	newSev := severityOrder[newAlert.GetSeverity()]

	return newSev > oldSev
}

// sendNotifications sends notifications for the given alerts
func (bn *BackgroundNotifier) sendNotifications(alerts []models.Alert) {
	if len(alerts) == 0 {
		return
	}

	// Limit number of simultaneous notifications
	if len(alerts) > bn.config.MaxNotifications {
		alerts = alerts[:bn.config.MaxNotifications]
	}

	for _, alert := range alerts {
		go bn.sendSingleNotification(alert)
	}
}

// sendSingleNotification sends a notification for a single alert
func (bn *BackgroundNotifier) sendSingleNotification(alert models.Alert) {
	key := bn.getAlertKey(alert)

	// Update last notification time
	bn.mutex.Lock()
	bn.lastNotifications[key] = time.Now()
	bn.mutex.Unlock()

	// Send system notification with click handling
	if bn.config.ShowSystem {
		bn.sendClickableNotification(alert)
	}

	log.Printf("Background notification sent for alert: %s (severity: %s)", alert.GetAlertName(), alert.GetSeverity())
}

// sendClickableNotification sends a notification that can restore the window when clicked
func (bn *BackgroundNotifier) sendClickableNotification(alert models.Alert) {
	var title string
	var message string

	// Add severity emoji to title for visual distinction
	switch alert.GetSeverity() {
	case "critical":
		title = "🔴 Critical Alert - Notificator"
		message = fmt.Sprintf("CRITICAL: %s\n%s\n\n💡 Use system tray to show Notificator window",
			alert.GetAlertName(),
			alert.GetSummary())
	case "warning":
		title = "🟡 Warning Alert - Notificator"
		message = fmt.Sprintf("WARNING: %s\n%s\n\n💡 Use system tray to show Notificator window",
			alert.GetAlertName(),
			alert.GetSummary())
	case "info":
		title = "🔵 Info Alert - Notificator"
		message = fmt.Sprintf("INFO: %s\n%s\n\n💡 Use system tray to show Notificator window",
			alert.GetAlertName(),
			alert.GetSummary())
	default:
		title = "⚪ Alert - Notificator"
		message = fmt.Sprintf("%s\n%s\n\n💡 Use system tray to show Notificator window",
			alert.GetAlertName(),
			alert.GetSummary())
	}

	// Add instance information if available
	if instance := alert.GetInstance(); instance != "" && instance != "unknown" {
		message += fmt.Sprintf("\nInstance: %s", instance)
	}

	// Add team information if available
	if team := alert.GetTeam(); team != "" && team != "unknown" {
		message += fmt.Sprintf("\nTeam: %s", team)
	}

	// Create notification
	notification := fyne.NewNotification(title, message)

	// Note: Fyne doesn't support click callbacks directly
	// However, the system tray provides the mechanism for users to restore the window
	// The notification message guides users to use the system tray

	if bn.app != nil {
		bn.app.SendNotification(notification)
	}
}

// getAlertKey creates a unique key for an alert
func (bn *BackgroundNotifier) getAlertKey(alert models.Alert) string {
	return fmt.Sprintf("%s_%s_%s",
		alert.GetAlertName(),
		alert.GetInstance(),
		alert.Labels["job"])
}
