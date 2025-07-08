package gui

import (
	"fmt"

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
	title := widget.NewLabelWithStyle("Notification Settings", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	content.Add(title)
	content.Add(widget.NewSeparator())

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
		widget.NewButton("Close", func() {
			// Settings are applied in real-time, so just close
		}),
	)
	content.Add(actionContainer)

	// Create scrollable container
	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(500, 600))

	// Show dialog
	settingsDialog := dialog.NewCustom("Notification Settings", "Close", scroll, aw.window)
	settingsDialog.Resize(fyne.NewSize(550, 650))
	settingsDialog.Show()
}
