package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// createDashboard creates summary cards showing alert counts
func (aw *AlertsWindow) createDashboard() *fyne.Container {
	// Count alerts by severity and status from filtered data
	criticalCount := 0
	warningCount := 0
	infoCount := 0
	unknownCount := 0
	activeCount := 0
	resolvedCount := 0
	silencedCount := 0
	totalCount := len(aw.filteredData)

	for _, alert := range aw.filteredData {
		if alert.IsActive() {
			activeCount++
		} else if alert.Status.State == "resolved" {
			resolvedCount++
		}

		// Check if silenced
		if alert.Status.State == "suppressed" || len(alert.Status.SilencedBy) > 0 {
			silencedCount++
		}

		switch alert.GetSeverity() {
		case "critical":
			criticalCount++
		case "warning":
			warningCount++
		case "info":
			infoCount++
		default:
			unknownCount++
		}
	}

	// Create summary cards with icons and colors
	criticalCard := aw.createSummaryCard("🔴 Critical", criticalCount, widget.DangerImportance)
	warningCard := aw.createSummaryCard("🟡 Warning", warningCount, widget.WarningImportance)
	infoCard := aw.createSummaryCard("🔵 Info", infoCount, widget.LowImportance)
	activeCard := aw.createSummaryCard("🔥 Active", activeCount, widget.HighImportance)
	totalCard := aw.createSummaryCard("📊 Total", totalCount, widget.MediumImportance)

	dashboard := container.NewHBox(
		criticalCard,
		warningCard,
		infoCard,
		widget.NewSeparator(),
		activeCard,
		totalCard,
	)

	// Store reference for updates
	aw.dashboardCards = dashboard
	return dashboard
}

// createSummaryCard creates a summary card with count and styling
func (aw *AlertsWindow) createSummaryCard(title string, count int, importance widget.Importance) *widget.Card {
	// Create count label with emphasis
	countLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("%d", count),
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)
	countLabel.Importance = importance

	// Create title label
	titleLabel := widget.NewLabelWithStyle(
		title,
		fyne.TextAlignCenter,
		fyne.TextStyle{},
	)

	// Combine in vertical container
	content := container.NewVBox(
		countLabel,
		titleLabel,
	)

	// Create card with minimal padding
	card := widget.NewCard("", "", content)
	card.Resize(fyne.NewSize(120, 80))

	return card
}

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
