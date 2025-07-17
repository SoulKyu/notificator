package gui

import (
	"fmt"
)

// updateDashboard refreshes the dashboard cards with current data
func (aw *AlertsWindow) updateDashboard() {
	aw.updateStatusBarMetrics()
}

// updateStatusBarMetrics updates the status bar with current filtered data metrics
func (aw *AlertsWindow) updateStatusBarMetrics() {
	if aw.statusBarMetrics == nil {
		return
	}

	// Count alerts by severity and status from filtered data
	criticalCount := 0
	warningCount := 0
	infoCount := 0
	activeCount := 0
	totalCount := len(aw.filteredData)

	for _, alert := range aw.filteredData {
		if alert.IsActive() {
			activeCount++
		}

		switch alert.GetSeverity() {
		case "critical":
			criticalCount++
		case "warning":
			warningCount++
		case "info":
			infoCount++
		}
	}

	// Update the labels
	aw.statusBarMetrics.criticalLabel.SetText(fmt.Sprintf("%d", criticalCount))
	aw.statusBarMetrics.warningLabel.SetText(fmt.Sprintf("%d", warningCount))
	aw.statusBarMetrics.infoLabel.SetText(fmt.Sprintf("%d", infoCount))
	aw.statusBarMetrics.activeLabel.SetText(fmt.Sprintf("%d", activeCount))
	aw.statusBarMetrics.totalLabel.SetText(fmt.Sprintf("%d", totalCount))
}
