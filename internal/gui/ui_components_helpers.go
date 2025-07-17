// internal/gui/ui_components_helpers.go
package gui

import (
	"notificator/internal/models"
)

// getGroupHighestSeverity returns the highest severity in a group
func (aw *AlertsWindow) getGroupHighestSeverity(group AlertGroup) string {
	severityOrder := map[string]int{"critical": 0, "warning": 1, "info": 2, "unknown": 3}
	highestSeverity := "unknown"
	highestOrder := 4

	for _, alert := range group.Alerts {
		severity := alert.GetSeverity()
		if order, exists := severityOrder[severity]; exists && order < highestOrder {
			highestSeverity = severity
			highestOrder = order
		}
	}

	return highestSeverity
}

// getGroupNewestAlert returns the newest alert in a group
func (aw *AlertsWindow) getGroupNewestAlert(group AlertGroup) *models.Alert {
	if len(group.Alerts) == 0 {
		return nil
	}

	newest := &group.Alerts[0]
	for i := 1; i < len(group.Alerts); i++ {
		if group.Alerts[i].StartsAt.After(newest.StartsAt) {
			newest = &group.Alerts[i]
		}
	}

	return newest
}