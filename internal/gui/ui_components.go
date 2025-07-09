// internal/gui/ui_components.go
package gui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"notificator/internal/models"
)

// setupUI creates and arranges all UI components
func (aw *AlertsWindow) setupUI() {
	// Create toolbar with enhancements
	toolbar := aw.createEnhancedToolbar()

	// Create enhanced filters with multi-select
	filters := aw.createFilters()

	// Load saved filter state after creating filters
	aw.loadFilterState()

	// Create bulk actions toolbar
	bulkActions := aw.createBulkActionsToolbar()

	// Create alerts table
	tableContainer := aw.createTable()

	// Create status bar
	statusBar := aw.createStatusBar()

	// Layout the main content
	content := container.NewBorder(
		container.NewVBox(toolbar, filters, bulkActions), // Top
		statusBar,      // Bottom
		nil,            // Left
		nil,            // Right
		tableContainer, // Center
	)

	aw.window.SetContent(content)
}

// createEnhancedToolbar creates the main toolbar with theme toggle and polling status
func (aw *AlertsWindow) createEnhancedToolbar() *fyne.Container {
	aw.refreshBtn = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), aw.handleRefresh)
	aw.refreshBtn.Importance = widget.HighImportance

	// Enhanced auto-refresh toggle with polling status
	autoRefreshCheck := widget.NewCheck("Smart auto-refresh", func(checked bool) {
		aw.autoRefresh = checked
		if checked {
			aw.startSmartAutoRefresh()
		} else {
			aw.stopAutoRefresh()
		}
	})
	autoRefreshCheck.SetChecked(true)

	// Polling interval display
	pollingStatusLabel := widget.NewLabel(fmt.Sprintf("(%v)", aw.refreshInterval))
	pollingStatusLabel.TextStyle = fyne.TextStyle{Italic: true}

	// Connection health indicator
	var connectionIndicator *widget.Label
	if aw.connectionHealth.IsHealthy {
		connectionIndicator = widget.NewLabel("🟢")
	} else {
		connectionIndicator = widget.NewLabel("🔴")
	}
	connectionIndicator.Resize(fyne.NewSize(20, 20))

	// Theme toggle button
	aw.themeBtn = aw.createThemeToggle()

	// Show hidden alerts button
	aw.showHiddenBtn = widget.NewButtonWithIcon("Show Hidden Alerts", theme.VisibilityOffIcon(), aw.toggleShowHidden)

	// Group toggle button
	aw.groupToggleBtn = widget.NewButtonWithIcon("Group View", theme.ListIcon(), func() {
		aw.toggleGroupedMode()
	})
	aw.groupToggleBtn.Importance = widget.MediumImportance

	exportBtn := widget.NewButtonWithIcon("Export", theme.DocumentSaveIcon(), aw.handleExport)

	// Column settings button
	columnBtn := widget.NewButtonWithIcon("Columns", theme.ViewFullScreenIcon(), aw.showColumnSettings)

	settingsBtn := widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), aw.handleSettings)

	// Create refresh section with status
	refreshSection := container.NewHBox(
		aw.refreshBtn,
		widget.NewSeparator(),
		autoRefreshCheck,
		pollingStatusLabel,
		connectionIndicator,
	)
	// Background mode toggle button
	backgroundModeBtn := widget.NewButtonWithIcon("Background", theme.VisibilityOffIcon(), func() {
		aw.ToggleBackgroundMode()
	})
	backgroundModeBtn.Importance = widget.MediumImportance

	// Update button state based on current mode
	if aw.IsBackgroundMode() {
		backgroundModeBtn.SetText("Show")
	}
	return container.NewHBox(
		refreshSection,
		widget.NewSeparator(),
		aw.themeBtn,
		widget.NewSeparator(),
		backgroundModeBtn,
		widget.NewSeparator(),
		aw.groupToggleBtn,
		widget.NewSeparator(),
		aw.showHiddenBtn,
		widget.NewSeparator(),
		exportBtn,
		columnBtn,
		settingsBtn,
	)
}

// createBulkActionsToolbar creates toolbar for bulk operations on selected alerts
func (aw *AlertsWindow) createBulkActionsToolbar() *fyne.Container {
	// Select all button
	selectAllBtn := widget.NewButtonWithIcon("Select All", theme.CheckButtonIcon(), aw.selectAllAlerts)
	selectAllBtn.Importance = widget.LowImportance

	// Expand/Collapse buttons (only visible in grouped mode)
	expandAllBtn := widget.NewButtonWithIcon("Expand All", theme.MenuExpandIcon(), func() {
		aw.expandAllGroups()
	})
	expandAllBtn.Importance = widget.LowImportance

	collapseAllBtn := widget.NewButtonWithIcon("Collapse All", theme.MenuIcon(), func() {
		aw.collapseAllGroups()
	})
	collapseAllBtn.Importance = widget.LowImportance

	// Hide selected button
	hideSelectedBtn := widget.NewButtonWithIcon("Hide Selected", theme.VisibilityOffIcon(), func() {
		if aw.showHiddenAlerts {
			aw.unhideSelectedAlerts()
		} else {
			aw.hideSelectedAlerts()
		}
	})
	hideSelectedBtn.Importance = widget.WarningImportance

	// Store reference to update button text when view changes
	aw.hideSelectedBtn = hideSelectedBtn

	// Selection count label
	selectionLabel := widget.NewLabel("No alerts selected")
	aw.selectionLabel = selectionLabel

	// Create grouped controls container
	groupedControls := container.NewHBox(
		expandAllBtn,
		collapseAllBtn,
		widget.NewSeparator(),
	)

	// Hide grouped controls initially if not in grouped mode
	if !aw.groupedMode {
		groupedControls.Hide()
	}

	return container.NewHBox(
		selectAllBtn,
		widget.NewSeparator(),
		groupedControls,
		hideSelectedBtn,
		widget.NewSeparator(),
		selectionLabel,
	)
}

// createSeverityBadge creates a styled severity badge with enhanced visual styling
func (aw *AlertsWindow) createSeverityBadge(alert models.Alert) fyne.CanvasObject {
	// Use the new enhanced severity badge for all severity levels
	return aw.createEnhancedSeverityBadgeNew(alert)
}

// createEnhancedSeverityContainer creates a container with border colors and background gradients
func (aw *AlertsWindow) createEnhancedSeverityContainer(badge *widget.Label, severity string) fyne.CanvasObject {
	// Create a card container for enhanced styling
	var card *widget.Card

	switch severity {
	case "critical":
		// Critical alerts: Red border with red gradient background
		card = widget.NewCard("", "", badge)
		card.Resize(fyne.NewSize(120, 40))

		// Create a container with visual enhancements for critical
		criticalContainer := container.NewBorder(nil, nil, nil, nil, card)
		return aw.wrapWithCriticalStyling(criticalContainer)

	case "warning":
		// Warning alerts: Orange/Yellow border with orange gradient background
		card = widget.NewCard("", "", badge)
		card.Resize(fyne.NewSize(120, 40))

		// Create a container with visual enhancements for warning
		warningContainer := container.NewBorder(nil, nil, nil, nil, card)
		return aw.wrapWithWarningStyling(warningContainer)

	default:
		// Default styling for info and other severities
		return badge
	}
}

// wrapWithCriticalStyling adds critical alert styling with red border and gradient
func (aw *AlertsWindow) wrapWithCriticalStyling(content fyne.CanvasObject) fyne.CanvasObject {
	// Create a visual indicator for critical alerts
	criticalIndicator := widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	criticalIndicator.Importance = widget.DangerImportance

	// Create border effect using separators
	topBorder := widget.NewSeparator()
	bottomBorder := widget.NewSeparator()
	leftBorder := widget.NewSeparator()
	rightBorder := widget.NewSeparator()

	// Create the bordered container
	borderedContent := container.NewBorder(
		topBorder,    // top
		bottomBorder, // bottom
		leftBorder,   // left
		rightBorder,  // right
		content,      // center
	)

	// Add padding and background effect
	paddedContainer := container.NewPadded(borderedContent)

	return paddedContainer
}

// wrapWithWarningStyling adds warning alert styling with orange border and gradient
func (aw *AlertsWindow) wrapWithWarningStyling(content fyne.CanvasObject) fyne.CanvasObject {
	// Create a visual indicator for warning alerts
	warningIndicator := widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	warningIndicator.Importance = widget.WarningImportance

	// Create border effect using separators
	topBorder := widget.NewSeparator()
	bottomBorder := widget.NewSeparator()
	leftBorder := widget.NewSeparator()
	rightBorder := widget.NewSeparator()

	// Create the bordered container
	borderedContent := container.NewBorder(
		topBorder,    // top
		bottomBorder, // bottom
		leftBorder,   // left
		rightBorder,  // right
		content,      // center
	)

	// Add padding and background effect
	paddedContainer := container.NewPadded(borderedContent)

	return paddedContainer
}

// createStatusBadge creates a styled status badge
func (aw *AlertsWindow) createStatusBadge(alert models.Alert) fyne.CanvasObject {
	status := alert.Status.State

	var statusText string
	var importance widget.Importance

	// Enhanced status detection with better silencing logic
	if alert.Status.State == "suppressed" || len(alert.Status.SilencedBy) > 0 {
		statusText = "🔇 SILENCED"
		importance = widget.WarningImportance
	} else if len(alert.Status.InhibitedBy) > 0 {
		statusText = "⏸️ INHIBITED"
		importance = widget.MediumImportance
	} else {
		switch status {
		case "firing":
			statusText = "🔥 FIRING"
			importance = widget.DangerImportance
		case "active":
			statusText = "🔥 FIRING"
			importance = widget.DangerImportance
		case "resolved":
			statusText = "✅ RESOLVED"
			importance = widget.SuccessImportance
		default:
			statusText = "❓ " + strings.ToUpper(status)
			importance = widget.MediumImportance
		}
	}

	badge := widget.NewLabelWithStyle(
		statusText,
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)
	badge.Importance = importance

	return badge
}

// createFilters creates the filter controls with enhanced multi-select
func (aw *AlertsWindow) createFilters() *fyne.Container {
	// Create a search entry
	aw.searchEntry = widget.NewEntry()
	aw.searchEntry.SetPlaceHolder("🔍 Search alerts, teams, summaries, instances, or any text...")
	aw.searchEntry.OnChanged = func(text string) {
		aw.lastActivity = time.Now()
		aw.safeApplyFilters()
		aw.saveFilterState()
	}

	// Make the search entry larger
	aw.searchEntry.Resize(fyne.NewSize(700, 50))

	// Create multi-select filters
	severityOptions := []string{"All", "critical", "warning", "info", "unknown"}
	aw.severityMultiSelect = NewMultiSelectWidget("Severity", severityOptions, aw.window, func(selected map[string]bool) {
		aw.lastActivity = time.Now()
		aw.safeApplyFilters()
		aw.saveFilterState()
	})

	statusOptions := []string{"All", "active", "firing", "resolved", "suppressed"}
	aw.statusMultiSelect = NewMultiSelectWidget("Status", statusOptions, aw.window, func(selected map[string]bool) {
		aw.lastActivity = time.Now()
		aw.safeApplyFilters()
		aw.saveFilterState()
	})

	teamOptions := []string{"All"}
	aw.teamMultiSelect = NewMultiSelectWidget("Team", teamOptions, aw.window, func(selected map[string]bool) {
		aw.lastActivity = time.Now()
		aw.safeApplyFilters()
		aw.saveFilterState()
	})

	clearBtn := widget.NewButtonWithIcon("Clear All Filters", theme.ContentClearIcon(), aw.clearFilters)
	clearBtn.Importance = widget.LowImportance

	// Create search label and container
	searchLabel := widget.NewLabelWithStyle("🔍 Search:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	searchContainer := container.NewBorder(
		nil, nil,
		searchLabel, nil,
		aw.searchEntry,
	)
	searchContainer.Resize(fyne.NewSize(0, 60))

	// Create filters container
	filtersContainer := container.NewHBox(
		aw.severityMultiSelect,
		widget.NewSeparator(),
		aw.statusMultiSelect,
		widget.NewSeparator(),
		aw.teamMultiSelect,
		widget.NewSeparator(),
		clearBtn,
	)

	return container.NewVBox(
		searchContainer,
		widget.NewSeparator(),
		filtersContainer,
	)
}

// createTable creates the alerts table with enhanced visual elements
func (aw *AlertsWindow) createTable() fyne.CanvasObject {
	if aw.groupedMode {
		return aw.createGroupedTable()
	} else {
		return aw.createFlatTable()
	}
}

// createFlatTable creates the traditional flat table
func (aw *AlertsWindow) createFlatTable() fyne.CanvasObject {
	aw.table = widget.NewTable(
		func() (int, int) {
			return len(aw.filteredData) + 1, len(aw.columns)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewLabel("Template"))
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {
			if i.Row == 0 {
				aw.renderTableHeader(i, o)
				return
			}

			if i.Row-1 < len(aw.filteredData) {
				alert := aw.filteredData[i.Row-1]
				dataRowIndex := i.Row - 1
				aw.renderFlatAlertRow(i, o, alert, dataRowIndex)
			}
		},
	)

	aw.applyColumnWidths()

	aw.table.OnSelected = func(id widget.TableCellID) {
		aw.lastActivity = time.Now()

		if id.Col == 0 {
			return
		}

		if id.Row > 0 && id.Row-1 < len(aw.filteredData) {
			alert := aw.filteredData[id.Row-1]
			aw.showAlertDetails(alert)
		}
	}

	return container.NewScroll(aw.table)
}

// createGroupedTable creates the table with grouped alerts
func (aw *AlertsWindow) createGroupedTable() fyne.CanvasObject {
	// Create groups from filtered data
	aw.alertGroups = aw.createGroupedAlertsFromFiltered()
	aw.tableRows = aw.createTableRowsFromGroups(aw.alertGroups)

	aw.table = widget.NewTable(
		func() (int, int) {
			return len(aw.tableRows) + 1, len(aw.columns)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewLabel("Template"))
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {
			if i.Row == 0 {
				aw.renderTableHeader(i, o)
				return
			}

			if i.Row-1 < len(aw.tableRows) {
				row := aw.tableRows[i.Row-1]
				aw.renderTableRow(i, o, row)
			}
		},
	)

	aw.applyColumnWidths()

	aw.table.OnSelected = func(id widget.TableCellID) {
		aw.lastActivity = time.Now()
		aw.handleTableClick(id)
	}

	return container.NewScroll(aw.table)
}

// renderFlatAlertRow renders a row in flat mode
func (aw *AlertsWindow) renderFlatAlertRow(cellID widget.TableCellID, obj fyne.CanvasObject, alert models.Alert, dataRowIndex int) {
	if cellContainer, ok := obj.(*fyne.Container); ok {
		cellContainer.RemoveAll()

		switch cellID.Col {
		case 0: // Checkbox
			checkbox := widget.NewCheck("", func(checked bool) {
				aw.lastActivity = time.Now()
				if checked {
					aw.selectedAlerts[dataRowIndex] = true
				} else {
					delete(aw.selectedAlerts, dataRowIndex)
				}
				aw.updateSelectionLabel()
			})
			checkbox.SetChecked(aw.selectedAlerts[dataRowIndex])
			cellContainer.Add(checkbox)

		case 1: // Alert name
			label := widget.NewLabel(alert.GetAlertName())
			if aw.showHiddenAlerts {
				label.SetText("🙈 " + alert.GetAlertName())
			}
			cellContainer.Add(label)

		case 2: // Severity
			cellContainer.Add(aw.createSeverityBadge(alert))

		case 3: // Status
			cellContainer.Add(aw.createStatusBadge(alert))

		case 4: // Team
			cellContainer.Add(widget.NewLabel(alert.GetTeam()))

		case 5: // Summary
			summary := alert.GetSummary()
			maxLen := int(aw.columns[5].Width / 6)
			if maxLen < 20 {
				maxLen = 20
			}
			if len(summary) > maxLen {
				summary = summary[:maxLen-3] + "..."
			}
			cellContainer.Add(widget.NewLabel(summary))

		case 6: // Duration
			cellContainer.Add(widget.NewLabel(formatDuration(alert.Duration())))

		case 7: // Instance
			cellContainer.Add(widget.NewLabel(alert.GetInstance()))
		}
	}
}

// renderTableHeader renders the table header
func (aw *AlertsWindow) renderTableHeader(cellID widget.TableCellID, obj fyne.CanvasObject) {
	if container, ok := obj.(*fyne.Container); ok {
		container.RemoveAll()
		if cellID.Col < len(aw.columns) {
			if cellID.Col == 0 {
				// Checkbox column header
				headerBtn := widget.NewButton("✓", aw.selectAllAlerts)
				headerBtn.Importance = widget.LowImportance
				container.Add(headerBtn)
			} else {
				// Regular sortable column
				headerText := aw.columns[cellID.Col].Name

				// Add sort indicator
				if aw.sortColumn == cellID.Col-1 {
					if aw.sortAscending {
						headerText += " ↑"
					} else {
						headerText += " ↓"
					}
				}

				headerBtn := widget.NewButton(headerText, func() {
					aw.handleColumnSort(cellID.Col)
				})
				headerBtn.Importance = widget.LowImportance
				container.Add(headerBtn)
			}
		}
	}
}

// Continue with grouped table rendering methods...

// createGroupedAlertsFromFiltered creates alert groups from filtered data
func (aw *AlertsWindow) createGroupedAlertsFromFiltered() []AlertGroup {
	// Group alerts by alertname
	groupMap := make(map[string][]models.Alert)

	for _, alert := range aw.filteredData {
		alertName := alert.GetAlertName()
		groupMap[alertName] = append(groupMap[alertName], alert)
	}

	// Convert to AlertGroup slice
	var groups []AlertGroup
	for alertName, alerts := range groupMap {
		// Sort alerts within each group according to current sort settings
		aw.sortAlertsInGroup(alerts)

		group := AlertGroup{
			AlertName:  alertName,
			Alerts:     alerts,
			IsExpanded: false, // Start collapsed
			TotalCount: len(alerts),
		}

		// Count by severity and status
		for _, alert := range alerts {
			if alert.IsActive() {
				group.ActiveCount++
			}

			switch alert.GetSeverity() {
			case "critical":
				group.CriticalCount++
			case "warning":
				group.WarningCount++
			case "info":
				group.InfoCount++
			}
		}

		groups = append(groups, group)
	}

	// Sort groups according to current sort settings, or by criticality if no sort is set
	aw.sortGroups(groups)

	return groups
}

// createTableRowsFromGroups creates table rows from alert groups
func (aw *AlertsWindow) createTableRowsFromGroups(groups []AlertGroup) []TableRow {
	var rows []TableRow
	rowIndex := 0

	for groupIndex, group := range groups {
		// Add group header row
		groupRow := TableRow{
			Type:       "group",
			Group:      &groups[groupIndex], // Use pointer to allow modifications
			GroupIndex: groupIndex,
			RowIndex:   rowIndex,
		}
		rows = append(rows, groupRow)
		rowIndex++

		// Add individual alert rows if expanded
		if group.IsExpanded {
			for alertIndex, alert := range group.Alerts {
				alertCopy := alert // Create copy to avoid pointer issues
				alertRow := TableRow{
					Type:       "alert",
					Alert:      &alertCopy,
					GroupIndex: groupIndex,
					AlertIndex: alertIndex,
					RowIndex:   rowIndex,
				}
				rows = append(rows, alertRow)
				rowIndex++
			}
		}
	}

	return rows
}

// renderTableRow renders a table row (either group or alert)
func (aw *AlertsWindow) renderTableRow(cellID widget.TableCellID, obj fyne.CanvasObject, row TableRow) {
	if cellContainer, ok := obj.(*fyne.Container); ok {
		cellContainer.RemoveAll()

		if row.Type == "group" {
			aw.renderGroupRow(cellID, cellContainer, row)
		} else {
			aw.renderAlertRow(cellID, cellContainer, row)
		}
	}
}

// renderGroupRow renders a group header row
func (aw *AlertsWindow) renderGroupRow(cellID widget.TableCellID, cellContainer *fyne.Container, row TableRow) {
	group := row.Group

	switch cellID.Col {
	case 0: // Checkbox column - group selection
		checkbox := widget.NewCheck("", func(checked bool) {
			aw.lastActivity = time.Now()
			if checked {
				// Select the group row itself for group-level operations
				aw.selectedAlerts[row.RowIndex] = true
				// Also select all individual alerts in the group (without triggering refresh)
				aw.selectGroupAlertsQuietly(row.GroupIndex, true)
			} else {
				// Deselect the group row
				delete(aw.selectedAlerts, row.RowIndex)
				// Also deselect all individual alerts in the group (without triggering refresh)
				aw.selectGroupAlertsQuietly(row.GroupIndex, false)
			}
			aw.updateSelectionLabel()
		})
		// Check if the group itself is selected OR if all alerts in group are selected
		groupSelected := aw.selectedAlerts[row.RowIndex]
		allAlertsSelected := aw.areAllGroupAlertsSelected(row.GroupIndex)
		checkbox.SetChecked(groupSelected || allAlertsSelected)
		cellContainer.Add(checkbox)

	case 1: // Alert name with expand/collapse button
		var expandIcon fyne.Resource
		if group.IsExpanded {
			expandIcon = theme.MenuDropDownIcon()
		} else {
			expandIcon = theme.MenuDropUpIcon()
		}

		expandBtn := widget.NewButtonWithIcon("", expandIcon, func() {
			aw.toggleGroupExpansion(row.GroupIndex)
		})
		expandBtn.Importance = widget.LowImportance

		nameLabel := widget.NewLabelWithStyle(
			fmt.Sprintf("📋 %s", group.AlertName),
			fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true},
		)

		cellContainer.Add(container.NewHBox(expandBtn, nameLabel))

	case 2: // Severity summary
		severityText := aw.createSeveritySummary(group)
		severityLabel := widget.NewRichText(&widget.TextSegment{
			Text:  severityText,
			Style: widget.RichTextStyle{},
		})
		cellContainer.Add(severityLabel)

	case 3: // Status summary
		statusText := aw.createStatusSummary(group)
		statusLabel := widget.NewLabel(statusText)
		cellContainer.Add(statusLabel)

	case 4: // Team (first team found)
		team := "Mixed"
		if len(group.Alerts) > 0 {
			team = group.Alerts[0].GetTeam()
			// Check if all alerts have same team
			for _, alert := range group.Alerts[1:] {
				if alert.GetTeam() != team {
					team = "Mixed"
					break
				}
			}
		}
		teamLabel := widget.NewLabel(team)
		cellContainer.Add(teamLabel)

	case 5: // Summary (count of alerts)
		summaryText := fmt.Sprintf("📊 %d alerts", group.TotalCount)
		if group.ActiveCount > 0 {
			summaryText += fmt.Sprintf(" (%d active)", group.ActiveCount)
		}
		summaryLabel := widget.NewLabel(summaryText)
		cellContainer.Add(summaryLabel)

	case 6: // Duration (newest alert)
		if len(group.Alerts) > 0 {
			// Find the most recent alert
			newest := group.Alerts[0]
			for _, alert := range group.Alerts[1:] {
				if alert.StartsAt.After(newest.StartsAt) {
					newest = alert
				}
			}
			durationLabel := widget.NewLabel(formatDuration(newest.Duration()))
			cellContainer.Add(durationLabel)
		} else {
			cellContainer.Add(widget.NewLabel("-"))
		}

	case 7: // Instance count
		instanceCount := len(aw.getUniqueInstances(group.Alerts))
		instanceLabel := widget.NewLabel(fmt.Sprintf("%d instances", instanceCount))
		cellContainer.Add(instanceLabel)
	}
}

// renderAlertRow renders an individual alert row (indented)
func (aw *AlertsWindow) renderAlertRow(cellID widget.TableCellID, container *fyne.Container, row TableRow) {
	alert := row.Alert

	switch cellID.Col {
	case 0: // Checkbox column
		checkbox := widget.NewCheck("", func(checked bool) {
			aw.lastActivity = time.Now()
			if checked {
				aw.selectedAlerts[row.RowIndex] = true
			} else {
				delete(aw.selectedAlerts, row.RowIndex)
			}
			aw.updateSelectionLabel()
		})
		checkbox.SetChecked(aw.selectedAlerts[row.RowIndex])
		container.Add(checkbox)

	case 1: // Alert name (indented)
		nameLabel := widget.NewLabel("    └─ " + alert.GetAlertName())
		if aw.showHiddenAlerts {
			nameLabel.SetText("    └─ 🙈 " + alert.GetAlertName())
		}
		container.Add(nameLabel)

	case 2: // Severity badge
		container.Add(aw.createSeverityBadge(*alert))

	case 3: // Status badge
		container.Add(aw.createStatusBadge(*alert))

	case 4: // Team
		teamLabel := widget.NewLabel(alert.GetTeam())
		container.Add(teamLabel)

	case 5: // Summary (truncated)
		summary := alert.GetSummary()
		maxLen := int(aw.columns[5].Width / 6)
		if maxLen < 20 {
			maxLen = 20
		}
		if len(summary) > maxLen {
			summary = summary[:maxLen-3] + "..."
		}
		summaryLabel := widget.NewLabel(summary)
		container.Add(summaryLabel)

	case 6: // Duration
		durationLabel := widget.NewLabel(formatDuration(alert.Duration()))
		container.Add(durationLabel)

	case 7: // Instance
		instanceLabel := widget.NewLabel(alert.GetInstance())
		container.Add(instanceLabel)
	}
}

// createSeveritySummary creates a summary of severities in the group
func (aw *AlertsWindow) createSeveritySummary(group *AlertGroup) string {
	var parts []string

	if group.CriticalCount > 0 {
		parts = append(parts, fmt.Sprintf("🔴 %d", group.CriticalCount))
	}
	if group.WarningCount > 0 {
		parts = append(parts, fmt.Sprintf("🟡 %d", group.WarningCount))
	}
	if group.InfoCount > 0 {
		parts = append(parts, fmt.Sprintf("🔵 %d", group.InfoCount))
	}

	if len(parts) == 0 {
		return "⚪ " + fmt.Sprint(group.TotalCount)
	}

	return strings.Join(parts, " ")
}

// createStatusSummary creates a summary of statuses in the group
func (aw *AlertsWindow) createStatusSummary(group *AlertGroup) string {
	firing := 0
	resolved := 0
	suppressed := 0

	for _, alert := range group.Alerts {
		switch alert.Status.State {
		case "active":
			firing++
		case "firing":
			firing++
		case "resolved":
			resolved++
		case "suppressed":
			suppressed++
		}
	}

	var parts []string
	if firing > 0 {
		parts = append(parts, fmt.Sprintf("🔥 %d", firing))
	}
	if resolved > 0 {
		parts = append(parts, fmt.Sprintf("✅ %d", resolved))
	}
	if suppressed > 0 {
		parts = append(parts, fmt.Sprintf("🔇 %d", suppressed))
	}

	return strings.Join(parts, " ")
}

// getUniqueInstances returns unique instances from a group of alerts
func (aw *AlertsWindow) getUniqueInstances(alerts []models.Alert) []string {
	instanceMap := make(map[string]bool)
	for _, alert := range alerts {
		instance := alert.GetInstance()
		if instance != "unknown" && instance != "" {
			instanceMap[instance] = true
		}
	}

	var instances []string
	for instance := range instanceMap {
		instances = append(instances, instance)
	}

	return instances
}

// createStatusBar creates the bottom status bar with enhanced metrics and polling info
func (aw *AlertsWindow) createStatusBar() *fyne.Container {
	aw.statusLabel = widget.NewLabel("Ready")
	aw.lastUpdate = widget.NewLabel("Never")

	// Create metric labels that will be updated dynamically
	criticalLabel := widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	criticalLabel.Importance = widget.DangerImportance

	warningLabel := widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	warningLabel.Importance = widget.WarningImportance

	infoLabel := widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	infoLabel.Importance = widget.LowImportance

	activeLabel := widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	activeLabel.Importance = widget.HighImportance

	totalLabel := widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	totalLabel.Importance = widget.MediumImportance

	// Hidden count label
	aw.hiddenCountLabel = widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	aw.hiddenCountLabel.Importance = widget.MediumImportance

	// Connection status indicator
	connectionStatusLabel := widget.NewLabel("🟢 Connected")
	if !aw.connectionHealth.IsHealthy {
		connectionStatusLabel.SetText("🔴 Disconnected")
		connectionStatusLabel.Importance = widget.DangerImportance
	}

	// View mode indicator
	viewModeLabel := widget.NewLabel("📋 Flat")
	if aw.groupedMode {
		viewModeLabel.SetText("📁 Grouped")
	}

	// Filter notification status indicator
	filterNotifLabel := widget.NewLabel("🔔 All")
	if aw.notificationConfig.RespectFilters {
		filterNotifLabel.SetText("🔔 Filtered")
		filterNotifLabel.Importance = widget.LowImportance
	}

	// Store references for updates
	aw.statusBarMetrics = &StatusBarMetrics{
		criticalLabel: criticalLabel,
		warningLabel:  warningLabel,
		infoLabel:     infoLabel,
		activeLabel:   activeLabel,
		totalLabel:    totalLabel,
	}

	// Polling info
	pollingInfo := widget.NewLabel(fmt.Sprintf("Polling: %v", aw.refreshInterval))
	pollingInfo.TextStyle = fyne.TextStyle{Italic: true}

	return container.NewHBox(
		aw.statusLabel,
		widget.NewSeparator(),
		widget.NewLabel("🔴"),
		criticalLabel,
		widget.NewSeparator(),
		widget.NewLabel("🟡"),
		warningLabel,
		widget.NewSeparator(),
		widget.NewLabel("🔵"),
		infoLabel,
		widget.NewSeparator(),
		widget.NewLabel("🔥"),
		activeLabel,
		widget.NewSeparator(),
		widget.NewLabel("📊"),
		totalLabel,
		widget.NewSeparator(),
		widget.NewLabel("🙈"),
		aw.hiddenCountLabel,
		widget.NewSeparator(),
		viewModeLabel,
		widget.NewSeparator(),
		filterNotifLabel,
		widget.NewSeparator(),
		connectionStatusLabel,
		widget.NewSeparator(),
		pollingInfo,
		widget.NewSeparator(),
		widget.NewLabel("Last update:"),
		aw.lastUpdate,
	)
}

// applyColumnWidths applies the current column width configuration to the table
func (aw *AlertsWindow) applyColumnWidths() {
	if aw.table == nil {
		return
	}

	for i, col := range aw.columns {
		aw.table.SetColumnWidth(i, col.Width)
	}
}

// sortAlertsInGroup sorts alerts within a group according to current sort settings
func (aw *AlertsWindow) sortAlertsInGroup(alerts []models.Alert) {
	if len(alerts) <= 1 {
		return
	}

	sort.Slice(alerts, func(i, j int) bool {
		var result bool

		switch aw.sortColumn {
		case 0: // Alert name
			result = strings.Compare(alerts[i].GetAlertName(), alerts[j].GetAlertName()) < 0
		case 1: // Severity
			severityOrder := map[string]int{"critical": 0, "warning": 1, "info": 2, "unknown": 3}
			sev1, exists1 := severityOrder[alerts[i].GetSeverity()]
			if !exists1 {
				sev1 = 4
			}
			sev2, exists2 := severityOrder[alerts[j].GetSeverity()]
			if !exists2 {
				sev2 = 4
			}
			result = sev1 < sev2
		case 2: // Status
			result = strings.Compare(alerts[i].Status.State, alerts[j].Status.State) < 0
		case 3: // Team
			result = strings.Compare(alerts[i].GetTeam(), alerts[j].GetTeam()) < 0
		case 4: // Summary
			result = strings.Compare(alerts[i].GetSummary(), alerts[j].GetSummary()) < 0
		case 5: // Duration
			result = alerts[i].Duration() < alerts[j].Duration()
		case 6: // Instance
			result = strings.Compare(alerts[i].GetInstance(), alerts[j].GetInstance()) < 0
		default:
			// Default sort by severity then start time
			severityOrder := map[string]int{"critical": 0, "warning": 1, "info": 2, "unknown": 3}
			sev1, exists1 := severityOrder[alerts[i].GetSeverity()]
			if !exists1 {
				sev1 = 4
			}
			sev2, exists2 := severityOrder[alerts[j].GetSeverity()]
			if !exists2 {
				sev2 = 4
			}
			if sev1 != sev2 {
				result = sev1 < sev2
			} else {
				result = alerts[i].StartsAt.After(alerts[j].StartsAt)
			}
		}

		if !aw.sortAscending {
			result = !result
		}

		return result
	})
}

// sortGroups sorts alert groups according to current sort settings
func (aw *AlertsWindow) sortGroups(groups []AlertGroup) {
	if len(groups) <= 1 {
		return
	}

	sort.Slice(groups, func(i, j int) bool {
		var result bool

		switch aw.sortColumn {
		case 0: // Alert name (group name)
			result = strings.Compare(groups[i].AlertName, groups[j].AlertName) < 0
		case 1: // Severity (by highest severity in group)
			// Get highest severity for each group
			sev1 := aw.getGroupHighestSeverity(groups[i])
			sev2 := aw.getGroupHighestSeverity(groups[j])
			severityOrder := map[string]int{"critical": 0, "warning": 1, "info": 2, "unknown": 3}
			sevOrder1, exists1 := severityOrder[sev1]
			if !exists1 {
				sevOrder1 = 4
			}
			sevOrder2, exists2 := severityOrder[sev2]
			if !exists2 {
				sevOrder2 = 4
			}
			result = sevOrder1 < sevOrder2
		case 2: // Status (by active count)
			result = groups[i].ActiveCount > groups[j].ActiveCount
		case 3: // Team (by first team in group)
			team1 := "unknown"
			team2 := "unknown"
			if len(groups[i].Alerts) > 0 {
				team1 = groups[i].Alerts[0].GetTeam()
			}
			if len(groups[j].Alerts) > 0 {
				team2 = groups[j].Alerts[0].GetTeam()
			}
			result = strings.Compare(team1, team2) < 0
		case 4: // Summary (by alert count)
			result = groups[i].TotalCount > groups[j].TotalCount
		case 5: // Duration (by newest alert in group)
			newest1 := aw.getGroupNewestAlert(groups[i])
			newest2 := aw.getGroupNewestAlert(groups[j])
			if newest1 != nil && newest2 != nil {
				result = newest1.Duration() < newest2.Duration()
			} else if newest1 != nil {
				result = true
			} else {
				result = false
			}
		case 6: // Instance (by instance count)
			instances1 := len(aw.getUniqueInstances(groups[i].Alerts))
			instances2 := len(aw.getUniqueInstances(groups[j].Alerts))
			result = instances1 > instances2
		default:
			// Default sort by criticality (critical first, then by name)
			if groups[i].CriticalCount != groups[j].CriticalCount {
				result = groups[i].CriticalCount > groups[j].CriticalCount
			} else if groups[i].WarningCount != groups[j].WarningCount {
				result = groups[i].WarningCount > groups[j].WarningCount
			} else {
				result = strings.Compare(groups[i].AlertName, groups[j].AlertName) < 0
			}
		}

		if !aw.sortAscending {
			result = !result
		}

		return result
	})
}

// getGroupHighestSeverity returns the highest severity in a group
func (aw *AlertsWindow) getGroupHighestSeverity(group AlertGroup) string {
	if group.CriticalCount > 0 {
		return "critical"
	}
	if group.WarningCount > 0 {
		return "warning"
	}
	if group.InfoCount > 0 {
		return "info"
	}
	return "unknown"
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
