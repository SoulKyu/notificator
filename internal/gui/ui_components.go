package gui

import (
	"strings"

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

	// Create filters
	filters := aw.createFilters()

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

// createEnhancedToolbar creates the main toolbar with theme toggle
func (aw *AlertsWindow) createEnhancedToolbar() *fyne.Container {
	aw.refreshBtn = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), aw.handleRefresh)
	aw.refreshBtn.Importance = widget.HighImportance

	autoRefreshCheck := widget.NewCheck("Auto-refresh (30s)", func(checked bool) {
		aw.autoRefresh = checked
		if checked {
			aw.startAutoRefresh()
		} else {
			aw.stopAutoRefresh()
		}
	})
	autoRefreshCheck.SetChecked(true)

	// Theme toggle button
	aw.themeBtn = aw.createThemeToggle()

	// Show hidden alerts button
	aw.showHiddenBtn = widget.NewButtonWithIcon("Show Hidden Alerts", theme.VisibilityOffIcon(), aw.toggleShowHidden)

	exportBtn := widget.NewButtonWithIcon("Export", theme.DocumentSaveIcon(), aw.handleExport)

	// Column settings button
	columnBtn := widget.NewButtonWithIcon("Columns", theme.ViewFullScreenIcon(), aw.showColumnSettings)

	settingsBtn := widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), aw.handleSettings)

	return container.NewHBox(
		aw.refreshBtn,
		widget.NewSeparator(),
		autoRefreshCheck,
		widget.NewSeparator(),
		aw.themeBtn,
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

	return container.NewHBox(
		selectAllBtn,
		widget.NewSeparator(),
		hideSelectedBtn,
		widget.NewSeparator(),
		selectionLabel,
	)
}

// createSeverityBadge creates a styled severity badge
func (aw *AlertsWindow) createSeverityBadge(alert models.Alert) fyne.CanvasObject {
	severity := alert.GetSeverity()

	var severityText string
	var importance widget.Importance

	switch severity {
	case "critical":
		severityText = "🔴 CRITICAL"
		importance = widget.DangerImportance
	case "warning":
		severityText = "🟡 WARNING"
		importance = widget.WarningImportance
	case "info":
		severityText = "🔵 INFO"
		importance = widget.LowImportance
	default:
		severityText = "⚪ " + strings.ToUpper(severity)
		importance = widget.MediumImportance
	}

	badge := widget.NewLabelWithStyle(
		severityText,
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)
	badge.Importance = importance

	return badge
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

// createFilters creates the filter controls with enhanced search
func (aw *AlertsWindow) createFilters() *fyne.Container {
	// Create a simple search entry without autocomplete
	aw.searchEntry = widget.NewEntry()
	aw.searchEntry.SetPlaceHolder("🔍 Search alerts, teams, summaries, instances, or any text...")
	aw.searchEntry.OnChanged = func(text string) {
		aw.safeApplyFilters()
	}

	// Make the search entry much larger and more focusable
	aw.searchEntry.Resize(fyne.NewSize(700, 50))

	// Severity filter
	aw.severitySelect = widget.NewSelect([]string{"All", "critical", "warning", "info", "unknown"}, func(selected string) {
		aw.safeApplyFilters()
	})
	aw.severitySelect.SetSelected("All")

	// Status filter
	aw.statusSelect = widget.NewSelect([]string{"All", "firing", "resolved", "suppressed"}, func(selected string) {
		aw.safeApplyFilters()
	})
	aw.statusSelect.SetSelected("All")

	// Team filter (will be populated dynamically)
	aw.teamSelect = widget.NewSelect([]string{"All"}, func(selected string) {
		aw.safeApplyFilters()
	})
	aw.teamSelect.SetSelected("All")

	clearBtn := widget.NewButtonWithIcon("Clear", theme.ContentClearIcon(), aw.clearFilters)
	clearBtn.Importance = widget.LowImportance

	// Create search label
	searchLabel := widget.NewLabelWithStyle("🔍 Search:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	// Create a border container to give the search field more space
	searchContainer := container.NewBorder(
		nil, nil, // top, bottom
		searchLabel, nil, // left, right
		aw.searchEntry, // center - this will expand to fill available space
	)

	// Set minimum size for the search container to make it more prominent
	searchContainer.Resize(fyne.NewSize(0, 60)) // Height of 60px

	filtersContainer := container.NewHBox(
		widget.NewLabelWithStyle("Severity:", fyne.TextAlignLeading, fyne.TextStyle{}),
		aw.severitySelect,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Status:", fyne.TextAlignLeading, fyne.TextStyle{}),
		aw.statusSelect,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Team:", fyne.TextAlignLeading, fyne.TextStyle{}),
		aw.teamSelect,
		widget.NewSeparator(),
		clearBtn,
	)

	// Use a VBox to stack search on top and other filters below
	return container.NewVBox(
		searchContainer,
		widget.NewSeparator(),
		filtersContainer,
	)
}

// createTable creates the alerts table with enhanced visual elements
func (aw *AlertsWindow) createTable() fyne.CanvasObject {
	aw.table = widget.NewTable(
		func() (int, int) {
			// Header + data rows, columns count
			return len(aw.filteredData) + 1, len(aw.columns)
		},
		func() fyne.CanvasObject {
			// Return container for badge columns, label for others
			return container.NewHBox(widget.NewLabel("Template"))
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {
			if i.Row == 0 {
				// Header row with sorting
				if container, ok := o.(*fyne.Container); ok {
					container.RemoveAll()
					if i.Col < len(aw.columns) {
						if i.Col == 0 {
							// Checkbox column header
							headerBtn := widget.NewButton("✓", aw.selectAllAlerts)
							headerBtn.Importance = widget.LowImportance
							container.Add(headerBtn)
						} else {
							// Regular sortable column
							headerText := aw.columns[i.Col].Name

							// Add sort indicator (adjust for checkbox column)
							if aw.sortColumn == i.Col-1 {
								if aw.sortAscending {
									headerText += " ↑"
								} else {
									headerText += " ↓"
								}
							}

							headerBtn := widget.NewButton(headerText, func() {
								aw.handleColumnSort(i.Col)
							})
							headerBtn.Importance = widget.LowImportance
							container.Add(headerBtn)
						}
					}
				}
				return
			}

			// Data rows
			if i.Row-1 < len(aw.filteredData) {
				alert := aw.filteredData[i.Row-1]
				dataRowIndex := i.Row - 1

				if cellContainer, ok := o.(*fyne.Container); ok {
					cellContainer.RemoveAll()

					switch i.Col {
					case 0: // Checkbox column
						checkbox := widget.NewCheck("", func(checked bool) {
							if checked {
								aw.selectedAlerts[dataRowIndex] = true
							} else {
								delete(aw.selectedAlerts, dataRowIndex)
							}
							// Update selection label when checkbox changes
							aw.updateSelectionLabel()
						})
						checkbox.SetChecked(aw.selectedAlerts[dataRowIndex])
						cellContainer.Add(checkbox)

					case 1: // Alert name
						label := widget.NewLabel(alert.GetAlertName())
						// Add visual indicator if alert is hidden (when viewing hidden alerts)
						if aw.showHiddenAlerts {
							label.SetText("🙈 " + alert.GetAlertName())
						}
						cellContainer.Add(label)

					case 2: // Severity - use badge
						cellContainer.Add(aw.createSeverityBadge(alert))

					case 3: // Status - use badge
						cellContainer.Add(aw.createStatusBadge(alert))

					case 4: // Team
						label := widget.NewLabel(alert.GetTeam())
						cellContainer.Add(label)

					case 5: // Summary
						summary := alert.GetSummary()
						maxLen := int(aw.columns[5].Width / 6)
						if maxLen < 20 {
							maxLen = 20
						}
						if len(summary) > maxLen {
							summary = summary[:maxLen-3] + "..."
						}
						label := widget.NewLabel(summary)
						cellContainer.Add(label)

					case 6: // Duration
						duration := alert.Duration()
						label := widget.NewLabel(formatDuration(duration))
						cellContainer.Add(label)

					case 7: // Instance
						label := widget.NewLabel(alert.GetInstance())
						cellContainer.Add(label)
					}
				}
			}
		},
	)

	// Apply column widths
	aw.applyColumnWidths()

	// Add double-click handler for alert details
	aw.table.OnSelected = func(id widget.TableCellID) {
		// Skip checkbox column clicks
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

// createStatusBar creates the bottom status bar with enhanced metrics
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

	// Store references for updates
	aw.statusBarMetrics = &StatusBarMetrics{
		criticalLabel: criticalLabel,
		warningLabel:  warningLabel,
		infoLabel:     infoLabel,
		activeLabel:   activeLabel,
		totalLabel:    totalLabel,
	}

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
