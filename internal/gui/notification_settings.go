package gui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

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

	// Hidden Alerts Management Tab
	hiddenAlertsTab := aw.createHiddenAlertsManagementTab()
	tabs.Append(container.NewTabItem("Hidden Alerts", hiddenAlertsTab))

	content.Add(tabs)

	// Create scrollable container
	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(600, 700))

	// Show dialog
	settingsDialog := dialog.NewCustom("Settings", "Close", scroll, aw.window)
	settingsDialog.Resize(fyne.NewSize(650, 750))
	settingsDialog.Show()
}

// createNotificationSettingsTab creates the notification settings tab content
func (aw *AlertsWindow) createNotificationSettingsTab() *fyne.Container {
	content := container.NewVBox()

	// Enable notifications
	enabledCheck := widget.NewCheck("Enable notifications", func(checked bool) {
		aw.notificationConfig.Enabled = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.saveNotificationConfig()
	})
	enabledCheck.SetChecked(aw.notificationConfig.Enabled)
	content.Add(enabledCheck)

	// Enable system notifications
	systemCheck := widget.NewCheck("Show system notifications", func(checked bool) {
		aw.notificationConfig.ShowSystem = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.saveNotificationConfig()
	})
	systemCheck.SetChecked(aw.notificationConfig.ShowSystem)
	content.Add(systemCheck)

	// Enable sound
	soundCheck := widget.NewCheck("Enable sound notifications", func(checked bool) {
		aw.notificationConfig.SoundEnabled = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
		aw.saveNotificationConfig()
	})
	soundCheck.SetChecked(aw.notificationConfig.SoundEnabled)
	content.Add(soundCheck)

	// Critical only mode
	criticalOnlyCheck := widget.NewCheck("Critical alerts only", func(checked bool) {
		aw.notificationConfig.CriticalOnly = checked
		aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
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
				State: "firing",
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

	// Action buttons
	actionContainer := container.NewHBox(
		widget.NewButton("Reset to Defaults", func() {
			aw.notificationConfig = notifier.CreateDefaultNotificationConfig()
			aw.notifier = notifier.NewNotifier(aw.notificationConfig, aw.app)
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
					dialog.ShowError(fmt.Errorf("Failed to unhide alert: %v", err), aw.window)
				} else {
					aw.applyFilters()
					aw.updateHiddenCountDisplay()
					dialog.ShowInformation("Success", fmt.Sprintf("Alert '%s' has been unhidden", hiddenAlert.AlertName), aw.window)
				}
				return
			}
		}
		// If alert not found in current alerts, remove from hidden cache anyway
		dialog.ShowError(fmt.Errorf("Alert not found in current alerts, but will be removed from hidden list"), aw.window)
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
				dialog.ShowError(fmt.Errorf("Failed to clear hidden alerts: %v", err), aw.window)
			} else {
				statusLabel.SetText("Currently hiding 0 alert(s)")
				aw.updateHiddenCountDisplay()
				aw.applyFilters()
				dialog.ShowInformation("Success", "All hidden alerts have been cleared.", aw.window)
			}
		}
	}, aw.window)

	dialog.Show()
}

// cleanupOldHiddenAlerts removes hidden alerts older than 30 days
func (aw *AlertsWindow) cleanupOldHiddenAlerts(statusLabel *widget.Label) {
	content := widget.NewLabel("Remove hidden alerts that are older than 30 days?\n\nThis helps clean up alerts that may no longer be relevant.")

	dialog := dialog.NewConfirm("Cleanup Old Entries", content.Text, func(confirmed bool) {
		if confirmed {
			// Cleanup alerts older than 30 days
			if err := aw.hiddenAlertsCache.CleanupExpired(30 * 24 * time.Hour); err != nil {
				dialog.ShowError(fmt.Errorf("Failed to cleanup old hidden alerts: %v", err), aw.window)
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
