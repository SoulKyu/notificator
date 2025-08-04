package gui

import (
	"fmt"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

// TrayManager handles system tray functionality
type TrayManager struct {
	app            fyne.App
	window         fyne.Window
	alertsWindow   *AlertsWindow
	backgroundMode bool
	trayMenu       *fyne.Menu
}

// NewTrayManager creates a new system tray manager
func NewTrayManager(app fyne.App, window fyne.Window, alertsWindow *AlertsWindow) *TrayManager {
	tm := &TrayManager{
		app:          app,
		window:       window,
		alertsWindow: alertsWindow,
	}

	tm.setupSystemTray()
	return tm
}

// setupSystemTray configures the system tray icon and menu
func (tm *TrayManager) setupSystemTray() {
	if desk, ok := tm.app.(desktop.App); ok {
		// Create tray menu items
		showItem := fyne.NewMenuItem("Show Notificator", func() {
			tm.ShowWindow()
		})
		showItem.Icon = theme.ComputerIcon()

		hideItem := fyne.NewMenuItem("Hide to Background", func() {
			tm.HideToBackground()
		})
		hideItem.Icon = theme.VisibilityOffIcon()

		backgroundModeItem := fyne.NewMenuItem("Background Mode", func() {
			tm.ToggleBackgroundMode()
		})
		backgroundModeItem.Icon = theme.ViewRefreshIcon()
		backgroundModeItem.Checked = tm.backgroundMode

		settingsItem := fyne.NewMenuItem("Settings", func() {
			tm.ShowWindow()
			tm.alertsWindow.showNotificationSettings()
		})
		settingsItem.Icon = theme.SettingsIcon()

		// Status submenu - we'll update this dynamically
		alertCountItem := fyne.NewMenuItem(fmt.Sprintf("Alerts: %d", len(tm.alertsWindow.filteredData)), nil)
		activeCountItem := fyne.NewMenuItem(fmt.Sprintf("Active: %d", tm.getActiveAlertCount()), nil)
		criticalCountItem := fyne.NewMenuItem(fmt.Sprintf("Critical: %d", tm.getCriticalAlertCount()), nil)

		statusItem := fyne.NewMenuItem("Status", nil)
		statusItem.ChildMenu = fyne.NewMenu("",
			alertCountItem,
			activeCountItem,
			criticalCountItem,
		)

		quitItem := fyne.NewMenuItem("Quit", func() {
			tm.app.Quit()
		})
		quitItem.Icon = theme.CancelIcon()

		// Create main tray menu
		tm.trayMenu = fyne.NewMenu("Notificator",
			showItem,
			hideItem,
			fyne.NewMenuItemSeparator(),
			backgroundModeItem,
			fyne.NewMenuItemSeparator(),
			statusItem,
			settingsItem,
			fyne.NewMenuItemSeparator(),
			quitItem,
		)

		// Set the system tray
		desk.SetSystemTrayMenu(tm.trayMenu)

		// Set up window close intercept to hide instead of close
		tm.window.SetCloseIntercept(func() {
			tm.HideToBackground()
		})

		log.Println("System tray initialized with window lifecycle management")
	} else {
		log.Println("System tray not supported on this platform")
	}
}

// ShowWindow brings the main window to the foreground
func (tm *TrayManager) ShowWindow() {
	tm.window.Show()
	tm.window.RequestFocus()

	tm.backgroundMode = false
	tm.updateTrayStatus()
	log.Println("Window shown from system tray")
}

// HideToBackground hides the main window but keeps the app running
func (tm *TrayManager) HideToBackground() {
	tm.window.Hide()
	tm.backgroundMode = true
	tm.updateTrayStatus()

	// Show a notification to inform user about background mode
	if tm.alertsWindow.notificationConfig.ShowSystem {
		notification := fyne.NewNotification(
			"Notificator - Background Mode",
			"App is now running in background. Click notifications to show window or use system tray.",
		)
		tm.app.SendNotification(notification)
	}

	log.Println("App hidden to background mode")
}

// ToggleBackgroundMode toggles between normal and background mode
func (tm *TrayManager) ToggleBackgroundMode() {
	if tm.backgroundMode {
		tm.ShowWindow()
	} else {
		tm.HideToBackground()
	}
}

// updateTrayStatus updates the system tray menu with enhanced visual indicators
func (tm *TrayManager) updateTrayStatus() {
	if tm.trayMenu == nil {
		return
	}

	// Update the background mode menu item checked state
	for _, item := range tm.trayMenu.Items {
		if item.Label == "Background Mode" {
			item.Checked = tm.backgroundMode
			break
		}
	}

	// Get alert counts for visual indicators
	totalAlerts := len(tm.alertsWindow.filteredData)
	activeAlerts := tm.getActiveAlertCount()
	criticalAlerts := tm.getCriticalAlertCount()
	warningAlerts := tm.getWarningAlertCount()

	// Update status information with enhanced visual indicators
	for _, item := range tm.trayMenu.Items {
		if item.Label == "Status" && item.ChildMenu != nil {
			// Update status submenu items with visual indicators
			if len(item.ChildMenu.Items) >= 3 {
				// Enhanced status display with icons and colors
				item.ChildMenu.Items[0].Label = fmt.Sprintf("📊 Total Alerts: %d", totalAlerts)

				if activeAlerts > 0 {
					item.ChildMenu.Items[1].Label = fmt.Sprintf("🔔 Active: %d", activeAlerts)
				} else {
					item.ChildMenu.Items[1].Label = fmt.Sprintf("✅ Active: %d", activeAlerts)
				}

				if criticalAlerts > 0 {
					item.ChildMenu.Items[2].Label = fmt.Sprintf("🚨 Critical: %d", criticalAlerts)
				} else {
					item.ChildMenu.Items[2].Label = fmt.Sprintf("🟢 Critical: %d", criticalAlerts)
				}

				// Add warning alerts status if we have enough menu items or create new one
				if len(item.ChildMenu.Items) >= 4 {
					if warningAlerts > 0 {
						item.ChildMenu.Items[3].Label = fmt.Sprintf("⚠️ Warnings: %d", warningAlerts)
					} else {
						item.ChildMenu.Items[3].Label = fmt.Sprintf("🟡 Warnings: %d", warningAlerts)
					}
				} else {
					// Add warning count item if it doesn't exist
					warningItem := fyne.NewMenuItem(fmt.Sprintf("⚠️ Warnings: %d", warningAlerts), nil)
					item.ChildMenu.Items = append(item.ChildMenu.Items, warningItem)
				}

				// Add health summary if we have more space
				if len(item.ChildMenu.Items) >= 5 {
					healthStatus := tm.getSystemHealthStatus()
					item.ChildMenu.Items[4].Label = healthStatus
				} else {
					healthItem := fyne.NewMenuItem(tm.getSystemHealthStatus(), nil)
					item.ChildMenu.Items = append(item.ChildMenu.Items, healthItem)
				}
			}
			break
		}
	}
}

// getActiveAlertCount returns the number of active alerts
func (tm *TrayManager) getActiveAlertCount() int {
	count := 0
	for _, alert := range tm.alertsWindow.filteredData {
		if alert.IsActive() {
			count++
		}
	}
	return count
}

// getCriticalAlertCount returns the number of critical alerts
func (tm *TrayManager) getCriticalAlertCount() int {
	count := 0
	for _, alert := range tm.alertsWindow.filteredData {
		if alert.GetSeverity() == "critical" && alert.IsActive() {
			count++
		}
	}
	return count
}

// getWarningAlertCount returns the number of warning alerts
func (tm *TrayManager) getWarningAlertCount() int {
	count := 0
	for _, alert := range tm.alertsWindow.filteredData {
		if alert.GetSeverity() == "warning" && alert.IsActive() {
			count++
		}
	}
	return count
}

// getSystemHealthStatus returns a visual health status summary
func (tm *TrayManager) getSystemHealthStatus() string {
	criticalCount := tm.getCriticalAlertCount()
	warningCount := tm.getWarningAlertCount()

	if criticalCount > 0 {
		return fmt.Sprintf("🔴 System: CRITICAL (%d)", criticalCount)
	} else if warningCount > 0 {
		return fmt.Sprintf("🟡 System: WARNING (%d)", warningCount)
	} else {
		return "🟢 System: HEALTHY"
	}
}

// IsBackgroundMode returns true if the app is currently in background mode
func (tm *TrayManager) IsBackgroundMode() bool {
	return tm.backgroundMode
}

// UpdateAlertCounts should be called when alert data changes
func (tm *TrayManager) UpdateAlertCounts() {
	tm.updateTrayStatus()
}

// HandleWindowClose determines what happens when user closes the window
func (tm *TrayManager) HandleWindowClose() bool {
	// Check if we should minimize to tray based on configuration
	if tm.alertsWindow.originalConfig != nil && tm.alertsWindow.originalConfig.GUI.MinimizeToTray {
		tm.HideToBackground()
		return false // Prevent actual close
	}

	// If minimize to tray is not configured, allow normal close
	return true // Allow normal close
}
