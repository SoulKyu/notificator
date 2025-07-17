package gui

import (
	"fmt"
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"notificator/internal/audio"
	"notificator/internal/models"
	"notificator/internal/notifier"
)

// showNotificationSettings displays the notification configuration dialog
func (aw *AlertsWindow) showNotificationSettings() {
	content := container.NewVBox()

	// Header
	title := widget.NewLabelWithStyle("Settings", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	content.Add(title)
	content.Add(widget.NewSeparator())

	// Create tabs for different settings categories
	tabs := container.NewAppTabs()

	// Notification Settings Tab
	notificationTab := aw.createNotificationSettingsTab()
	tabs.Append(container.NewTabItem("Notifications", notificationTab))

	// Polling Settings Tab
	pollingTab := aw.createPollingSettingsTab()
	tabs.Append(container.NewTabItem("Polling", pollingTab))

	// Hidden Alerts Management Tab
	hiddenAlertsTab := aw.createHiddenAlertsManagementTab()
	tabs.Append(container.NewTabItem("Hidden Alerts", hiddenAlertsTab))

	// Resolved Alerts Settings Tab
	resolvedAlertsTab := aw.createResolvedAlertsSettingsTab()
	tabs.Append(container.NewTabItem("Resolved Alerts", resolvedAlertsTab))

	content.Add(tabs)

	// Create scrollable container
	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(600, 700))

	// Show dialog
	settingsDialog := dialog.NewCustom("Settings", "Close", scroll, aw.window)
	settingsDialog.Resize(fyne.NewSize(650, 750))
	settingsDialog.Show()
}

// createPollingSettingsTab creates the polling configuration tab
func (aw *AlertsWindow) createPollingSettingsTab() *fyne.Container {
	content := container.NewVBox()

	// Auto-refresh toggle
	autoRefreshCheck := widget.NewCheck("Enable auto-refresh", func(checked bool) {
		aw.autoRefresh = checked
		if checked {
			aw.startSmartAutoRefresh()
		} else {
			aw.stopAutoRefresh()
		}
	})
	autoRefreshCheck.SetChecked(aw.autoRefresh)
	content.Add(autoRefreshCheck)

	content.Add(widget.NewSeparator())

	// Refresh interval settings
	intervalLabel := widget.NewLabelWithStyle("Refresh Interval Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(intervalLabel)

	// Current interval display
	currentIntervalLabel := widget.NewLabel(fmt.Sprintf("Current interval: %v", aw.refreshInterval))
	content.Add(currentIntervalLabel)

	// Refresh interval selection
	refreshIntervals := []string{"10 seconds", "15 seconds", "30 seconds", "1 minute", "2 minutes", "5 minutes"}

	refreshSelect := widget.NewSelect(refreshIntervals, func(selected string) {
		var interval time.Duration
		switch selected {
		case "10 seconds":
			interval = 10 * time.Second
		case "15 seconds":
			interval = 15 * time.Second
		case "30 seconds":
			interval = 30 * time.Second
		case "1 minute":
			interval = 60 * time.Second
		case "2 minutes":
			interval = 2 * 60 * time.Second
		case "5 minutes":
			interval = 5 * 60 * time.Second
		default:
			interval = 30 * time.Second
		}

		aw.updateRefreshInterval(interval)
		currentIntervalLabel.SetText(fmt.Sprintf("Current interval: %v", interval))
	})

	// Set current selection based on current interval
	currentSelection := "30 seconds"
	switch aw.refreshInterval {
	case 10 * time.Second:
		currentSelection = "10 seconds"
	case 15 * time.Second:
		currentSelection = "15 seconds"
	case 30 * time.Second:
		currentSelection = "30 seconds"
	case 60 * time.Second:
		currentSelection = "1 minute"
	case 2 * 60 * time.Second:
		currentSelection = "2 minutes"
	case 5 * 60 * time.Second:
		currentSelection = "5 minutes"
	}
	refreshSelect.SetSelected(currentSelection)

	content.Add(widget.NewLabel("Base refresh interval:"))
	content.Add(refreshSelect)

	// Explanation of adaptive polling
	adaptiveExplanation := widget.NewRichTextFromMarkdown(`**Adaptive Polling:**

The application automatically adjusts polling speed based on alert activity:

• **15 seconds** - When critical alerts are present
• **20 seconds** - When many alerts (>5) are active  
• **30 seconds** - When some alerts are active
• **60 seconds** - When no alerts are active

**Smart Background Refresh:**
• Faster refresh when you're actively using the app
• Slower refresh when the app is idle
• Reduces resource usage while maintaining responsiveness`)
	adaptiveExplanation.Wrapping = fyne.TextWrapWord

	content.Add(widget.NewSeparator())
	content.Add(adaptiveExplanation)

	content.Add(widget.NewSeparator())

	// Connection health status
	healthLabel := widget.NewLabelWithStyle("Connection Health", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(healthLabel)

	// Connection status display
	var healthStatusLabel *widget.Label
	if aw.connectionHealth.IsHealthy {
		healthStatusLabel = widget.NewLabel("✅ Connection healthy")
		healthStatusLabel.Importance = widget.SuccessImportance
	} else {
		healthStatusLabel = widget.NewLabel(fmt.Sprintf("⚠️ Connection issues (%d failures)", aw.connectionHealth.FailureCount))
		healthStatusLabel.Importance = widget.WarningImportance
	}
	content.Add(healthStatusLabel)

	lastSuccessLabel := widget.NewLabel(fmt.Sprintf("Last successful: %s", aw.connectionHealth.LastSuccessful.Format("15:04:05")))
	content.Add(lastSuccessLabel)

	// Test connection button
	testConnBtn := widget.NewButton("Test Connection Now", func() {
		aw.setStatus("Testing connection...")
		go func() {
			_, err := aw.client.FetchAlerts()
			fyne.Do(func() {
				if err != nil {
					aw.setStatus("Connection test failed")
					healthStatusLabel.SetText("❌ Connection test failed")
					healthStatusLabel.Importance = widget.DangerImportance
					dialog.ShowError(fmt.Errorf("connection test failed: %v", err), aw.window)
				} else {
					aw.setStatus("Connection test successful")
					healthStatusLabel.SetText("✅ Connection healthy")
					healthStatusLabel.Importance = widget.SuccessImportance
					aw.connectionHealth.LastSuccessful = time.Now()
					aw.connectionHealth.IsHealthy = true
					aw.connectionHealth.FailureCount = 0
					lastSuccessLabel.SetText(fmt.Sprintf("Last successful: %s", time.Now().Format("15:04:05")))
				}
			})
		}()
	})
	content.Add(testConnBtn)

	return content
}

// createNotificationSettingsTab creates the notification settings tab content
func (aw *AlertsWindow) createNotificationSettingsTab() *fyne.Container {
	content := container.NewVBox()

	// Enable notifications
	enabledCheck := widget.NewCheck("Enable notifications", func(checked bool) {
		aw.notificationConfig.Enabled = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.updateNotificationFilters() // Update filters when recreating notifier
		aw.saveNotificationConfig()
	})
	enabledCheck.SetChecked(aw.notificationConfig.Enabled)
	content.Add(enabledCheck)

	// Enable system notifications
	systemCheck := widget.NewCheck("Show system notifications", func(checked bool) {
		aw.notificationConfig.ShowSystem = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.updateNotificationFilters() // Update filters when recreating notifier
		aw.saveNotificationConfig()
	})
	systemCheck.SetChecked(aw.notificationConfig.ShowSystem)
	content.Add(systemCheck)

	// Enable sound
	soundCheck := widget.NewCheck("Enable sound notifications", func(checked bool) {
		aw.notificationConfig.SoundEnabled = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.updateNotificationFilters() // Update filters when recreating notifier
		aw.saveNotificationConfig()
	})
	soundCheck.SetChecked(aw.notificationConfig.SoundEnabled)
	content.Add(soundCheck)

	content.Add(widget.NewSeparator())

	// Respect filters option
	respectFiltersCheck := widget.NewCheck("Only notify for filtered alerts", func(checked bool) {
		aw.notificationConfig.RespectFilters = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.updateNotificationFilters() // Update filters when recreating notifier
		aw.saveNotificationConfig()
	})
	respectFiltersCheck.SetChecked(aw.notificationConfig.RespectFilters)
	content.Add(respectFiltersCheck)

	// Explanation of filter-based notifications
	filterExplanation := widget.NewRichTextFromMarkdown(`**Filter-Based Notifications:**

When enabled, you will only receive notifications for alerts that match your current UI filters:

• **Search text** - Only alerts matching your search will notify
• **Severity filters** - Only selected severities will trigger notifications  
• **Status filters** - Only alerts with selected statuses will notify
• **Team filters** - Only alerts from selected teams will notify

This helps reduce notification noise by focusing only on what you're currently monitoring in the UI.

**Example:** If you filter to show only "critical" alerts from "backend" team, you'll only get notifications for those alerts, even if other alerts are firing.`)
	filterExplanation.Wrapping = fyne.TextWrapWord
	content.Add(filterExplanation)

	content.Add(widget.NewSeparator())

	// Critical only mode
	criticalOnlyCheck := widget.NewCheck("Critical alerts only", func(checked bool) {
		aw.notificationConfig.CriticalOnly = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.updateNotificationFilters() // Update filters when recreating notifier
		aw.saveNotificationConfig()
	})
	criticalOnlyCheck.SetChecked(aw.notificationConfig.CriticalOnly)
	content.Add(criticalOnlyCheck)

	content.Add(widget.NewSeparator())

	// Severity rules
	severityTitle := widget.NewLabelWithStyle("Notify for these severities:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(severityTitle)

	for severity, enabled := range aw.notificationConfig.SeverityRules {
		severityCopy := severity // Capture for closure
		check := widget.NewCheck(severity, func(checked bool) {
			aw.notificationConfig.SeverityRules[severityCopy] = checked
			aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
			aw.updateNotificationFilters() // Update filters when recreating notifier
			aw.saveNotificationConfig()
		})
		check.SetChecked(enabled)
		content.Add(check)
	}

	content.Add(widget.NewSeparator())

	// Cooldown settings with explanation
	cooldownLabel := widget.NewLabel(fmt.Sprintf("Cooldown: %d seconds (%s)", aw.notificationConfig.CooldownSeconds, formatCooldownTime(aw.notificationConfig.CooldownSeconds)))
	cooldownSlider := widget.NewSlider(30, 1800) // 30 seconds to 30 minutes
	cooldownSlider.SetValue(float64(aw.notificationConfig.CooldownSeconds))
	cooldownSlider.Step = 30
	cooldownSlider.OnChanged = func(value float64) {
		aw.notificationConfig.CooldownSeconds = int(value)
		cooldownLabel.SetText(fmt.Sprintf("Cooldown: %d seconds (%s)", int(value), formatCooldownTime(int(value))))
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.updateNotificationFilters() // Update filters when recreating notifier
		aw.saveNotificationConfig()
	}

	cooldownExplanation := widget.NewRichTextFromMarkdown(`**Cooldown prevents notification spam:**
- Same alert fires at 10:00 AM → ✅ You get notified
- Same alert still firing at 10:02 AM → ❌ No notification (cooldown active)  
- Same alert still firing after cooldown → ✅ You get notified again`)

	cooldownContainer := container.NewVBox(
		widget.NewLabel("Notification cooldown:"),
		cooldownLabel,
		cooldownSlider,
		cooldownExplanation,
	)
	content.Add(cooldownContainer)

	content.Add(widget.NewSeparator())

	// Max notifications with explanation
	maxNotifLabel := widget.NewLabel(fmt.Sprintf("Max simultaneous: %d notifications", aw.notificationConfig.MaxNotifications))
	maxNotifSlider := widget.NewSlider(1, 20)
	maxNotifSlider.SetValue(float64(aw.notificationConfig.MaxNotifications))
	maxNotifSlider.Step = 1
	maxNotifSlider.OnChanged = func(value float64) {
		aw.notificationConfig.MaxNotifications = int(value)
		maxNotifLabel.SetText(fmt.Sprintf("Max simultaneous: %d notifications", int(value)))
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.updateNotificationFilters() // Update filters when recreating notifier
		aw.saveNotificationConfig()
	}

	maxNotifExplanation := widget.NewRichTextFromMarkdown(`**Prevents notification overload:**
- If 20 alerts fire at once → Only show first 5 (if max=5)
- Prevents screen flooding during major outages`)

	maxNotifContainer := container.NewVBox(
		widget.NewLabel("Maximum simultaneous notifications:"),
		maxNotifLabel,
		maxNotifSlider,
		maxNotifExplanation,
	)
	content.Add(maxNotifContainer)

	content.Add(widget.NewSeparator())

	// Sound file selection
	soundPathEntry := widget.NewEntry()
	soundPathEntry.SetText(aw.notificationConfig.SoundPath)
	soundPathEntry.SetPlaceHolder("Path to custom sound file (leave empty for system default)")

	browseBtn := widget.NewButton("Browse", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err == nil && reader != nil {
				soundPathEntry.SetText(reader.URI().Path())
				aw.notificationConfig.SoundPath = reader.URI().Path()
				aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
				aw.updateNotificationFilters() // Update filters when recreating notifier
				reader.Close()
			}
		}, aw.window)
	})

	testSoundBtn := widget.NewButton("Test Sound", func() {
		// Create a test alert to play sound
		testAlert := models.Alert{
			Labels: map[string]string{
				"alertname": "Test Alert",
				"severity":  "warning",
			},
			Annotations: map[string]string{
				"summary": "This is a test notification",
			},
			Status: models.AlertStatus{
				State: "active",
			},
		}

		go func() {
			aw.notifier.ProcessAlerts([]models.Alert{testAlert}, []models.Alert{})
		}()
	})

	soundContainer := container.NewVBox(
		widget.NewLabel("Custom sound file:"),
		soundPathEntry,
		container.NewHBox(browseBtn, testSoundBtn),
	)
	content.Add(soundContainer)

	content.Add(widget.NewSeparator())

	// Audio output device selection
	audioDeviceContainer := aw.createAudioDeviceSelection()
	content.Add(audioDeviceContainer)

	// Action buttons
	actionContainer := container.NewHBox(
		widget.NewButton("Reset to Defaults", func() {
			aw.notificationConfig = notifier.CreateDefaultNotificationConfig()
			aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
			aw.updateNotificationFilters() // Update filters when recreating notifier
			dialog.ShowInformation("Reset", "Notification settings reset to defaults", aw.window)
		}),
	)
	content.Add(actionContainer)

	return content
}

// createHiddenAlertsManagementTab creates the hidden alerts management tab content
func (aw *AlertsWindow) createHiddenAlertsManagementTab() *fyne.Container {
	content := container.NewVBox()

	// Header
	headerLabel := widget.NewLabelWithStyle("Hidden Alerts Management", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(headerLabel)

	// Current status
	hiddenCount := aw.hiddenAlertsCache.GetHiddenCount()
	statusLabel := widget.NewLabel(fmt.Sprintf("Currently hiding %d alert(s)", hiddenCount))
	content.Add(statusLabel)

	content.Add(widget.NewSeparator())

	// Explanation
	explanationText := widget.NewRichTextFromMarkdown(`**About Hidden Alerts:**

Hidden alerts are removed from the main view but continue to exist in Alertmanager. This feature helps you:

• **Reduce noise** - Hide known issues that are being worked on
• **Focus on important alerts** - Remove distractions from critical monitoring
• **Maintain clean dashboard** - Keep your alert view organized

**Key Features:**
• Hidden alerts persist between application restarts
• You can view and manage hidden alerts separately
• Bulk hide/unhide operations supported
• Hidden alerts can be cleared at any time`)
	explanationText.Wrapping = fyne.TextWrapWord
	content.Add(explanationText)

	content.Add(widget.NewSeparator())

	// Hidden alerts list
	hiddenAlertsLabel := widget.NewLabelWithStyle("Currently Hidden Alerts:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(hiddenAlertsLabel)

	// Create a list to show hidden alerts
	hiddenAlertsList := aw.createHiddenAlertsList()
	content.Add(hiddenAlertsList)

	content.Add(widget.NewSeparator())

	// Management actions
	actionsLabel := widget.NewLabelWithStyle("Management Actions:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(actionsLabel)

	// View hidden alerts button
	viewHiddenBtn := widget.NewButton("View Hidden Alerts in Main Window", func() {
		if !aw.showHiddenAlerts {
			aw.toggleShowHidden()
		}
		// Close the settings dialog
		dialog.ShowInformation("Switched View", "Now showing hidden alerts in the main window. Use the toolbar button to switch back to normal view.", aw.window)
	})
	content.Add(viewHiddenBtn)

	// Clear all hidden alerts button
	clearAllBtn := widget.NewButton("Clear All Hidden Alerts", func() {
		aw.confirmClearAllHiddenAlerts(statusLabel)
	})
	clearAllBtn.Importance = widget.WarningImportance
	content.Add(clearAllBtn)

	// Cleanup old entries button
	cleanupBtn := widget.NewButton("Cleanup Old Entries (30+ days)", func() {
		aw.cleanupOldHiddenAlerts(statusLabel)
	})
	content.Add(cleanupBtn)

	return content
}

// createHiddenAlertsList creates a list widget showing currently hidden alerts
func (aw *AlertsWindow) createHiddenAlertsList() *fyne.Container {
	hiddenAlerts := aw.hiddenAlertsCache.GetHiddenAlerts()

	if len(hiddenAlerts) == 0 {
		return container.NewVBox(
			widget.NewLabel("No alerts are currently hidden"),
		)
	}

	listContainer := container.NewVBox()

	// Create a card for each hidden alert
	for i, hiddenAlert := range hiddenAlerts {
		if i >= 10 { // Limit to 10 entries to avoid overwhelming the UI
			remainingCount := len(hiddenAlerts) - 10
			listContainer.Add(widget.NewLabel(fmt.Sprintf("... and %d more hidden alerts", remainingCount)))
			break
		}

		// Create alert info card
		alertCard := aw.createHiddenAlertCard(hiddenAlert)
		listContainer.Add(alertCard)
	}

	// Wrap in a scroll container with limited height
	scroll := container.NewScroll(listContainer)
	scroll.SetMinSize(fyne.NewSize(500, 200))

	return container.NewVBox(scroll)
}

// createHiddenAlertCard creates a card displaying information about a hidden alert
func (aw *AlertsWindow) createHiddenAlertCard(hiddenAlert HiddenAlertInfo) *widget.Card {
	// Alert details
	alertInfo := container.NewVBox(
		widget.NewLabelWithStyle(hiddenAlert.AlertName, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel(fmt.Sprintf("Instance: %s", hiddenAlert.Instance)),
		widget.NewLabel(fmt.Sprintf("Hidden: %s", hiddenAlert.HiddenAt.Format("2006-01-02 15:04"))),
	)

	if hiddenAlert.Reason != "" {
		alertInfo.Add(widget.NewLabel(fmt.Sprintf("Reason: %s", hiddenAlert.Reason)))
	}

	// Unhide button
	unhideBtn := widget.NewButton("Unhide", func() {
		// Find the alert in current alerts and unhide it
		for _, alert := range aw.alerts {
			if aw.hiddenAlertsCache.generateAlertKey(alert) == aw.generateKeyFromHiddenInfo(hiddenAlert) {
				if err := aw.hiddenAlertsCache.UnhideAlert(alert); err != nil {
					dialog.ShowError(fmt.Errorf("failed to unhide alert: %v", err), aw.window)
				} else {
					aw.safeApplyFilters()
					aw.updateHiddenCountDisplay()
					dialog.ShowInformation("Success", fmt.Sprintf("Alert '%s' has been unhidden", hiddenAlert.AlertName), aw.window)
				}
				return
			}
		}
		// If alert not found in current alerts, remove from hidden cache anyway
		dialog.ShowError(fmt.Errorf("alert not found in current alerts, but will be removed from hidden list"), aw.window)
	})
	unhideBtn.Importance = widget.LowImportance

	cardContent := container.NewBorder(
		nil, nil, // top, bottom
		alertInfo, unhideBtn, // left, right
		nil, // center
	)

	return widget.NewCard("", "", cardContent)
}

// generateKeyFromHiddenInfo generates an alert key from hidden alert info
func (aw *AlertsWindow) generateKeyFromHiddenInfo(hiddenAlert HiddenAlertInfo) string {
	if hiddenAlert.Instance == "unknown" || hiddenAlert.Instance == "" {
		job := ""
		if jobLabel, exists := hiddenAlert.Labels["job"]; exists {
			job = jobLabel
		}
		return fmt.Sprintf("%s::%s", hiddenAlert.AlertName, job)
	}
	return fmt.Sprintf("%s::%s", hiddenAlert.AlertName, hiddenAlert.Instance)
}

// confirmClearAllHiddenAlerts shows confirmation dialog and clears all hidden alerts
func (aw *AlertsWindow) confirmClearAllHiddenAlerts(statusLabel *widget.Label) {
	hiddenCount := aw.hiddenAlertsCache.GetHiddenCount()
	if hiddenCount == 0 {
		dialog.ShowInformation("No Hidden Alerts", "There are no hidden alerts to clear.", aw.window)
		return
	}

	content := widget.NewLabel(fmt.Sprintf("Are you sure you want to clear all %d hidden alerts?\n\nThis action cannot be undone.", hiddenCount))

	dialog := dialog.NewConfirm("Clear All Hidden Alerts", content.Text, func(confirmed bool) {
		if confirmed {
			if err := aw.hiddenAlertsCache.ClearAll(); err != nil {
				dialog.ShowError(fmt.Errorf("failed to clear hidden alerts: %v", err), aw.window)
			} else {
				statusLabel.SetText("Currently hiding 0 alert(s)")
				aw.updateHiddenCountDisplay()
				aw.safeApplyFilters()
				dialog.ShowInformation("Success", "All hidden alerts have been cleared.", aw.window)
			}
		}
	}, aw.window)

	dialog.Show()
}

// createAudioDeviceSelection creates the audio output device selection UI
func (aw *AlertsWindow) createAudioDeviceSelection() *fyne.Container {
	content := container.NewVBox()

	// Audio device selection title
	deviceTitle := widget.NewLabelWithStyle("Audio Output Device:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(deviceTitle)

	// Create audio device manager
	deviceManager := audio.NewAudioDeviceManager()

	// Get available devices
	devices, err := deviceManager.GetAvailableDevices()
	if err != nil {
		log.Printf("Failed to get audio devices: %v", err)
		// Fallback to default device only
		devices = []audio.AudioDevice{
			{ID: "default", Name: "System Default", Description: "Default system audio device", IsDefault: true},
		}
	}

	// Create device options for the select widget
	deviceOptions := make([]string, len(devices))
	deviceMap := make(map[string]audio.AudioDevice)

	for i, device := range devices {
		displayName := device.Name
		if device.IsDefault {
			displayName += " (Default)"
		}
		deviceOptions[i] = displayName
		deviceMap[displayName] = device
	}

	// Current device selection
	currentDeviceLabel := widget.NewLabel(fmt.Sprintf("Current: %s", aw.notificationConfig.AudioOutputDevice))
	content.Add(currentDeviceLabel)

	// Device selection dropdown
	deviceSelect := widget.NewSelect(deviceOptions, func(selected string) {
		if device, exists := deviceMap[selected]; exists {
			aw.notificationConfig.AudioOutputDevice = device.ID
			currentDeviceLabel.SetText(fmt.Sprintf("Current: %s", device.ID))

			// Update the notifier with device-aware sound player
			aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
			aw.updateNotificationFilters()
			aw.saveNotificationConfig()
		}
	})

	// Set current selection
	currentSelection := "System Default (Default)"
	for displayName, device := range deviceMap {
		if device.ID == aw.notificationConfig.AudioOutputDevice {
			currentSelection = displayName
			break
		}
	}
	deviceSelect.SetSelected(currentSelection)

	content.Add(widget.NewLabel("Select audio output device:"))
	content.Add(deviceSelect)

	// Refresh devices button
	refreshBtn := widget.NewButton("Refresh Devices", func() {
		// Re-enumerate devices
		newDevices, err := deviceManager.GetAvailableDevices()
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to refresh audio devices: %v", err), aw.window)
			return
		}

		// Update device options
		newDeviceOptions := make([]string, len(newDevices))
		newDeviceMap := make(map[string]audio.AudioDevice)

		for i, device := range newDevices {
			displayName := device.Name
			if device.IsDefault {
				displayName += " (Default)"
			}
			newDeviceOptions[i] = displayName
			newDeviceMap[displayName] = device
		}

		deviceSelect.Options = newDeviceOptions
		deviceSelect.Refresh()

		// Update the map reference
		deviceMap = newDeviceMap

		dialog.ShowInformation("Refreshed", fmt.Sprintf("Found %d audio devices", len(newDevices)), aw.window)
	})

	// Test device button
	testDeviceBtn := widget.NewButton("Test Selected Device", func() {
		// Create a test alert to play sound on the selected device
		testAlert := models.Alert{
			Labels: map[string]string{
				"alertname": "Audio Device Test",
				"severity":  "info",
			},
			Annotations: map[string]string{
				"summary": "Testing audio output device",
			},
			Status: models.AlertStatus{
				State: "active",
			},
		}

		go func() {
			aw.notifier.ProcessAlerts([]models.Alert{testAlert}, []models.Alert{})
		}()
	})

	buttonContainer := container.NewHBox(refreshBtn, testDeviceBtn)
	content.Add(buttonContainer)

	// Device information
	deviceInfo := widget.NewRichTextFromMarkdown(`**Audio Device Selection:**

Choose which audio output device to use for notification sounds:

• **System Default** - Uses the system's default audio device
• **Specific Devices** - Target a particular audio output (speakers, headphones, etc.)

**Note:** Device availability depends on your system configuration. If a selected device becomes unavailable, the system will fall back to the default device.`)
	deviceInfo.Wrapping = fyne.TextWrapWord
	content.Add(deviceInfo)

	return content
}

// formatCooldownTime formats seconds into a human-readable duration string
func formatCooldownTime(seconds int) string {
	duration := time.Duration(seconds) * time.Second
	if duration < time.Minute {
		return fmt.Sprintf("%ds", seconds)
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm", seconds/60)
	} else {
		return fmt.Sprintf("%dh", seconds/3600)
	}
}

// cleanupOldHiddenAlerts removes hidden alerts older than 30 days
func (aw *AlertsWindow) cleanupOldHiddenAlerts(statusLabel *widget.Label) {
	content := widget.NewLabel("Remove hidden alerts that are older than 30 days?\n\nThis helps clean up alerts that may no longer be relevant.")

	dialog := dialog.NewConfirm("Cleanup Old Entries", content.Text, func(confirmed bool) {
		if confirmed {
			// Cleanup alerts older than 30 days
			if err := aw.hiddenAlertsCache.CleanupExpired(30 * 24 * time.Hour); err != nil {
				dialog.ShowError(fmt.Errorf("failed to cleanup old hidden alerts: %v", err), aw.window)
			} else {
				newCount := aw.hiddenAlertsCache.GetHiddenCount()
				statusLabel.SetText(fmt.Sprintf("Currently hiding %d alert(s)", newCount))
				aw.updateHiddenCountDisplay()
				dialog.ShowInformation("Success", "Old hidden alert entries have been cleaned up.", aw.window)
			}
		}
	}, aw.window)

	dialog.Show()
}

// createResolvedAlertsSettingsTab creates the resolved alerts configuration tab
func (aw *AlertsWindow) createResolvedAlertsSettingsTab() *fyne.Container {
	content := container.NewVBox()

	// Header
	headerLabel := widget.NewLabelWithStyle("Resolved Alerts Configuration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(headerLabel)

	// Current status
	resolvedCount := aw.getResolvedAlertsCount()
	statusLabel := widget.NewLabel(fmt.Sprintf("Currently tracking %d resolved alert(s)", resolvedCount))
	content.Add(statusLabel)

	content.Add(widget.NewSeparator())

	// Explanation
	explanationText := widget.NewRichTextFromMarkdown(`**About Resolved Alerts:**

When alerts are resolved (no longer active in Alertmanager), they can be:

• **Tracked temporarily** - Store resolved alerts for a configurable time period
• **Notified about** - Send system notifications when alerts are resolved
• **Viewed separately** - Access resolved alerts in a dedicated panel

**Key Features:**
• Configurable retention period (how long to keep resolved alerts)
• Optional notifications when alerts are resolved
• Clean interface showing when alerts were resolved
• Automatic cleanup of expired resolved alerts`)
	explanationText.Wrapping = fyne.TextWrapWord
	content.Add(explanationText)

	content.Add(widget.NewSeparator())

	// Enable resolved alerts tracking
	enabledCheck := widget.NewCheck("Enable resolved alerts tracking", func(checked bool) {
		aw.resolvedAlertsConfig.Enabled = checked
		aw.saveResolvedAlertsConfig()
	})
	enabledCheck.SetChecked(aw.resolvedAlertsConfig.Enabled)
	content.Add(enabledCheck)

	// Enable notifications for resolved alerts
	notificationsCheck := widget.NewCheck("Send notifications when alerts are resolved", func(checked bool) {
		aw.resolvedAlertsConfig.NotificationsEnabled = checked
		aw.saveResolvedAlertsConfig()
	})
	notificationsCheck.SetChecked(aw.resolvedAlertsConfig.NotificationsEnabled)
	content.Add(notificationsCheck)

	content.Add(widget.NewSeparator())

	// Retention duration settings
	retentionLabel := widget.NewLabel(fmt.Sprintf("Retention period: %v", aw.resolvedAlertsConfig.RetentionDuration))
	content.Add(widget.NewLabel("How long to keep resolved alerts:"))
	content.Add(retentionLabel)

	// Retention duration options
	retentionOptions := []string{"15 minutes", "30 minutes", "1 hour", "2 hours", "4 hours", "8 hours", "24 hours"}
	retentionSelect := widget.NewSelect(retentionOptions, func(selected string) {
		var duration time.Duration
		switch selected {
		case "15 minutes":
			duration = 15 * time.Minute
		case "30 minutes":
			duration = 30 * time.Minute
		case "1 hour":
			duration = 1 * time.Hour
		case "2 hours":
			duration = 2 * time.Hour
		case "4 hours":
			duration = 4 * time.Hour
		case "8 hours":
			duration = 8 * time.Hour
		case "24 hours":
			duration = 24 * time.Hour
		default:
			duration = 1 * time.Hour
		}

		aw.resolvedAlertsConfig.RetentionDuration = duration
		retentionLabel.SetText(fmt.Sprintf("Retention period: %v", duration))
		aw.saveResolvedAlertsConfig()
	})

	// Set current selection based on current retention duration
	currentSelection := "1 hour"
	switch aw.resolvedAlertsConfig.RetentionDuration {
	case 15 * time.Minute:
		currentSelection = "15 minutes"
	case 30 * time.Minute:
		currentSelection = "30 minutes"
	case 1 * time.Hour:
		currentSelection = "1 hour"
	case 2 * time.Hour:
		currentSelection = "2 hours"
	case 4 * time.Hour:
		currentSelection = "4 hours"
	case 8 * time.Hour:
		currentSelection = "8 hours"
	case 24 * time.Hour:
		currentSelection = "24 hours"
	}
	retentionSelect.SetSelected(currentSelection)
	content.Add(retentionSelect)

	content.Add(widget.NewSeparator())

	// Management actions
	actionsLabel := widget.NewLabelWithStyle("Management Actions:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content.Add(actionsLabel)

	// Clear all resolved alerts button
	clearAllBtn := widget.NewButton("Clear All Resolved Alerts", func() {
		aw.confirmClearAllResolvedAlerts(statusLabel)
	})
	clearAllBtn.Importance = widget.WarningImportance
	content.Add(clearAllBtn)

	// View resolved alerts button
	viewResolvedBtn := widget.NewButton("View Resolved Alerts", func() {
		if !aw.showResolvedAlerts {
			aw.toggleShowResolved()
		}
	})
	content.Add(viewResolvedBtn)

	return content
}

// confirmClearAllResolvedAlerts shows confirmation dialog and clears all resolved alerts
func (aw *AlertsWindow) confirmClearAllResolvedAlerts(statusLabel *widget.Label) {
	resolvedCount := aw.getResolvedAlertsCount()
	if resolvedCount == 0 {
		dialog.ShowInformation("No Resolved Alerts", "There are no resolved alerts to clear.", aw.window)
		return
	}

	content := widget.NewLabel(fmt.Sprintf("Are you sure you want to clear all %d resolved alerts?\n\nThis action cannot be undone.", resolvedCount))

	dialog := dialog.NewConfirm("Clear All Resolved Alerts", content.Text, func(confirmed bool) {
		if confirmed {
			aw.resolvedAlertsCache.Clear()
			statusLabel.SetText("Currently tracking 0 resolved alert(s)")
			aw.updateResolvedCountDisplay()
			dialog.ShowInformation("Success", "All resolved alerts have been cleared.", aw.window)
		}
	}, aw.window)

	dialog.Show()
}

// saveResolvedAlertsConfig saves the resolved alerts configuration
func (aw *AlertsWindow) saveResolvedAlertsConfig() {
	if aw.originalConfig != nil {
		aw.originalConfig.ResolvedAlerts = aw.resolvedAlertsConfig
		if err := aw.originalConfig.SaveToFile(aw.configPath); err != nil {
			log.Printf("Failed to save resolved alerts config: %v", err)
		}
	}
}

// loadResolvedAlertsConfig loads the resolved alerts configuration
func (aw *AlertsWindow) loadResolvedAlertsConfig() {
	if aw.originalConfig != nil {
		aw.resolvedAlertsConfig = aw.originalConfig.ResolvedAlerts
		if aw.resolvedAlertsCache != nil {
			aw.resolvedAlertsCache.UpdateTTL(aw.resolvedAlertsConfig.RetentionDuration)
		}
	}
}
