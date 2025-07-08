package gui

import (
	"fmt"
	"log"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"notificator/internal/models"
)

// showAlertDetails displays detailed information about an alert with enhanced formatting
func (aw *AlertsWindow) showAlertDetails(alert models.Alert) {
	// Create tabbed interface for better organization
	tabs := container.NewAppTabs()

	// Overview Tab
	overviewContent := aw.createOverviewTab(alert)
	tabs.Append(container.NewTabItem("Overview", overviewContent))

	// Details Tab
	detailsContent := aw.createDetailsTab(alert)
	tabs.Append(container.NewTabItem("Details", detailsContent))

	// Labels Tab
	labelsContent := aw.createLabelsTab(alert)
	tabs.Append(container.NewTabItem("Labels", labelsContent))

	// Annotations Tab
	annotationsContent := aw.createAnnotationsTab(alert)
	tabs.Append(container.NewTabItem("Annotations", annotationsContent))

	// Silence Tab (replaces Status tab)
	silenceContent := aw.createSilenceTab(alert)
	tabs.Append(container.NewTabItem("Silence", silenceContent))

	// Create dialog with enhanced size
	dialog := dialog.NewCustom("Alert Details", "Close", tabs, aw.window)
	dialog.Resize(fyne.NewSize(800, 700))
	dialog.Show()
}

// createOverviewTab creates the overview tab content
func (aw *AlertsWindow) createOverviewTab(alert models.Alert) fyne.CanvasObject {
	// Check if alert is silenced
	silenceInfo := ""
	if alert.Status.State == "suppressed" || len(alert.Status.SilencedBy) > 0 {
		if len(alert.Status.SilencedBy) > 0 {
			silenceInfo = fmt.Sprintf("\n**🔇 Silenced by:** %s\n", strings.Join(alert.Status.SilencedBy, ", "))
		} else {
			silenceInfo = "\n**🔇 Alert is suppressed/silenced**\n"
		}
	}

	// Check if alert is inhibited
	inhibitInfo := ""
	if len(alert.Status.InhibitedBy) > 0 {
		inhibitInfo = fmt.Sprintf("\n**⏸️ Inhibited by:** %s\n", strings.Join(alert.Status.InhibitedBy, ", "))
	}

	// Enhanced end time display
	endTimeInfo := ""
	if !alert.EndsAt.IsZero() {
		endTimeInfo = fmt.Sprintf("\n**Ended:** %s", alert.EndsAt.Format("2006-01-02 15:04:05"))
	}

	content := widget.NewRichTextFromMarkdown(fmt.Sprintf(`
# 🚨 %s

## 📊 Quick Info
- **Severity:** %s %s
- **Status:** %s %s  
- **Team:** %s
- **Instance:** %s
- **Started:** %s%s
- **Duration:** %s
%s%s

## 📝 Summary
%s

## 🔗 Generator URL
%s
`,
		alert.GetAlertName(),
		aw.getSeverityIcon(alert.GetSeverity()), alert.GetSeverity(),
		aw.getStatusIcon(alert.Status.State), alert.Status.State,
		alert.GetTeam(),
		alert.GetInstance(),
		alert.StartsAt.Format("2006-01-02 15:04:05"),
		endTimeInfo,
		formatDuration(alert.Duration()),
		silenceInfo,
		inhibitInfo,
		alert.GetSummary(),
		alert.GeneratorURL,
	))

	content.Wrapping = fyne.TextWrapWord
	scroll := container.NewScroll(content)
	return scroll
}

// createDetailsTab creates the details tab content
func (aw *AlertsWindow) createDetailsTab(alert models.Alert) fyne.CanvasObject {
	content := container.NewVBox()

	// Basic Information Card
	basicCard := widget.NewCard("Basic Information", "", container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Alert Name: %s", alert.GetAlertName())),
		widget.NewLabel(fmt.Sprintf("Severity: %s", alert.GetSeverity())),
		widget.NewLabel(fmt.Sprintf("Status: %s", alert.Status.State)),
		widget.NewLabel(fmt.Sprintf("Team: %s", alert.GetTeam())),
		widget.NewLabel(fmt.Sprintf("Instance: %s", alert.GetInstance())),
	))

	// Timing Information Card
	timingCard := widget.NewCard("Timing Information", "", container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Started: %s", alert.StartsAt.Format("2006-01-02 15:04:05 MST"))),
		widget.NewLabel(fmt.Sprintf("Duration: %s", formatDuration(alert.Duration()))),
	))

	if !alert.EndsAt.IsZero() {
		timingCard.SetContent(container.NewVBox(
			timingCard.Content,
			widget.NewLabel(fmt.Sprintf("Ended: %s", alert.EndsAt.Format("2006-01-02 15:04:05 MST"))),
		))
	}

	// Summary Card
	summaryEntry := widget.NewMultiLineEntry()
	summaryEntry.SetText(alert.GetSummary())
	summaryEntry.Wrapping = fyne.TextWrapWord
	summaryEntry.Disable()
	summaryCard := widget.NewCard("Summary", "", summaryEntry)

	content.Add(basicCard)
	content.Add(timingCard)
	content.Add(summaryCard)

	return container.NewScroll(content)
}

// createLabelsTab creates the labels tab content
func (aw *AlertsWindow) createLabelsTab(alert models.Alert) fyne.CanvasObject {
	content := container.NewVBox()

	if len(alert.Labels) == 0 {
		content.Add(widget.NewLabel("No labels available"))
		return content
	}

	// Create a table for labels
	labelData := make([][]string, 0, len(alert.Labels))
	for key, value := range alert.Labels {
		labelData = append(labelData, []string{key, value})
	}

	table := widget.NewTable(
		func() (int, int) { return len(labelData), 2 },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.TableCellID, o fyne.CanvasObject) {
			label := o.(*widget.Label)
			if i.Col == 0 {
				label.SetText(labelData[i.Row][0])
				label.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				label.SetText(labelData[i.Row][1])
				label.TextStyle = fyne.TextStyle{}
			}
		},
	)

	table.SetColumnWidth(0, 200)
	table.SetColumnWidth(1, 400)

	// Add headers
	headerContainer := container.NewHBox(
		widget.NewLabelWithStyle("Key", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Value", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	content.Add(headerContainer)
	content.Add(table)

	return container.NewScroll(content)
}

// createAnnotationsTab creates the annotations tab content
func (aw *AlertsWindow) createAnnotationsTab(alert models.Alert) fyne.CanvasObject {
	content := container.NewVBox()

	if len(alert.Annotations) == 0 {
		content.Add(widget.NewLabel("No annotations available"))
		return content
	}

	// Create cards for each annotation
	for key, value := range alert.Annotations {
		annotationEntry := widget.NewMultiLineEntry()
		annotationEntry.SetText(value)
		annotationEntry.Wrapping = fyne.TextWrapWord
		annotationEntry.Disable()

		card := widget.NewCard(key, "", annotationEntry)
		content.Add(card)
	}

	return container.NewScroll(content)
}

// createStatusTab creates the status tab content
func (aw *AlertsWindow) createStatusTab(alert models.Alert) fyne.CanvasObject {
	content := container.NewVBox()

	// Status Information Card
	statusInfo := container.NewVBox(
		widget.NewLabelWithStyle(fmt.Sprintf("Current State: %s", strings.ToUpper(alert.Status.State)), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	// Add status description
	statusDescription := aw.getStatusDescription(alert.Status.State)
	if statusDescription != "" {
		statusInfo.Add(widget.NewLabel(statusDescription))
	}

	// Silenced Information with enhanced details
	if alert.Status.State == "suppressed" || len(alert.Status.SilencedBy) > 0 {
		statusInfo.Add(widget.NewSeparator())
		statusInfo.Add(widget.NewLabelWithStyle("🔇 Silencing Information", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

		if len(alert.Status.SilencedBy) > 0 {
			statusInfo.Add(widget.NewLabel("This alert is currently silenced by the following silence(s):"))

			// Fetch detailed silence information for each silence ID
			for i, silenceID := range alert.Status.SilencedBy {
				statusInfo.Add(widget.NewLabel(fmt.Sprintf("  %d. Silence ID: %s", i+1, silenceID)))

				// Try to fetch silence details
				aw.addSilenceDetails(statusInfo, silenceID)
			}

			// Add helpful information about silences
			statusInfo.Add(widget.NewSeparator())
			silenceHelpText := widget.NewRichTextFromMarkdown(`**About Silences:**
• Silences prevent notifications from being sent for matching alerts
• Silenced alerts continue to be evaluated and can still be viewed
• Silences have expiration times and will automatically lift when expired
• You can manage silences through the Alertmanager web interface`)
			silenceHelpText.Wrapping = fyne.TextWrapWord
			statusInfo.Add(silenceHelpText)
		} else {
			statusInfo.Add(widget.NewLabel("• Alert is suppressed/silenced (no specific silence ID available)"))
		}
	}

	// Inhibited Information with enhanced details
	if len(alert.Status.InhibitedBy) > 0 {
		statusInfo.Add(widget.NewSeparator())
		statusInfo.Add(widget.NewLabelWithStyle("⏸️ Inhibition Information", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

		statusInfo.Add(widget.NewLabel("This alert is currently inhibited by the following alert(s):"))
		for i, inhibitID := range alert.Status.InhibitedBy {
			statusInfo.Add(widget.NewLabel(fmt.Sprintf("  %d. Alert: %s", i+1, inhibitID)))
		}

		// Add helpful information about inhibitions
		statusInfo.Add(widget.NewSeparator())
		inhibitHelpText := widget.NewRichTextFromMarkdown(`**About Inhibitions:**
• Inhibitions prevent notifications for less critical alerts when more critical ones are firing
• This helps reduce alert noise during incidents
• Inhibited alerts are still active but notifications are suppressed
• Inhibitions are automatically lifted when the inhibiting alert resolves`)
		inhibitHelpText.Wrapping = fyne.TextWrapWord
		statusInfo.Add(inhibitHelpText)
	}

	// Show positive status for active alerts
	if alert.Status.State == "firing" && len(alert.Status.SilencedBy) == 0 && len(alert.Status.InhibitedBy) == 0 {
		statusInfo.Add(widget.NewSeparator())
		statusInfo.Add(widget.NewLabelWithStyle("✅ Alert Status", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		statusInfo.Add(widget.NewLabel("• This alert is actively firing"))
		statusInfo.Add(widget.NewLabel("• Notifications are being sent"))
		statusInfo.Add(widget.NewLabel("• No silences or inhibitions are active"))
	}

	statusCard := widget.NewCard("Alert Status Details", "", statusInfo)
	content.Add(statusCard)

	// Generator URL Information
	if alert.GeneratorURL != "" {
		generatorCard := widget.NewCard("Generator URL", "", widget.NewLabel(alert.GeneratorURL))
		content.Add(generatorCard)
	}

	return container.NewScroll(content)
}

// getStatusDescription returns a human-readable description of the alert status
func (aw *AlertsWindow) getStatusDescription(state string) string {
	switch state {
	case "firing":
		return "The alert condition is currently active and notifications may be sent."
	case "resolved":
		return "The alert condition is no longer active and has been resolved."
	case "suppressed":
		return "The alert is active but notifications are suppressed due to silencing rules."
	default:
		return ""
	}
}

// Helper functions for icons
func (aw *AlertsWindow) getSeverityIcon(severity string) string {
	switch severity {
	case "critical":
		return "🔴"
	case "warning":
		return "🟡"
	case "info":
		return "🔵"
	default:
		return "⚪"
	}
}

func (aw *AlertsWindow) getStatusIcon(status string) string {
	switch status {
	case "firing":
		return "🔥"
	case "resolved":
		return "✅"
	case "suppressed":
		return "🔇"
	default:
		return "❓"
	}
}

// createSilenceTab creates the silence tab content
func (aw *AlertsWindow) createSilenceTab(alert models.Alert) fyne.CanvasObject {
	content := container.NewVBox()

	// Check if alert is currently silenced
	if alert.Status.State == "suppressed" || len(alert.Status.SilencedBy) > 0 {
		// Show existing silence information
		content.Add(widget.NewLabelWithStyle("🔇 Alert is Currently Silenced", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}))
		content.Add(widget.NewSeparator())

		if len(alert.Status.SilencedBy) > 0 {
			content.Add(widget.NewLabel("This alert is silenced by the following silence(s):"))
			content.Add(widget.NewLabel(""))

			// Show details for each silence
			for i, silenceID := range alert.Status.SilencedBy {
				silenceCard := aw.createSilenceInfoCard(silenceID, i+1)
				content.Add(silenceCard)
			}
		} else {
			content.Add(widget.NewLabel("Alert is suppressed/silenced (no specific silence ID available)"))
		}

		// Add helpful information about silences
		content.Add(widget.NewSeparator())
		helpText := widget.NewRichTextFromMarkdown(`**About Silences:**
• Silences prevent notifications from being sent for matching alerts
• Silenced alerts continue to be evaluated and can still be viewed
• Silences have expiration times and will automatically lift when expired
• You can manage silences through the Alertmanager web interface`)
		helpText.Wrapping = fyne.TextWrapWord
		content.Add(helpText)

	} else {
		// Show silence creation interface
		content.Add(widget.NewLabelWithStyle("🔕 Create New Silence", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}))
		content.Add(widget.NewSeparator())

		// Show status-specific message
		switch alert.Status.State {
		case "firing":
			content.Add(widget.NewLabel("This alert is currently firing. You can create a silence to suppress notifications."))
		case "resolved":
			content.Add(widget.NewLabel("This alert is resolved. You can still create a silence to prevent future notifications if it fires again."))
		default:
			content.Add(widget.NewLabel(fmt.Sprintf("This alert has status '%s'. You can create a silence to suppress notifications.", alert.Status.State)))
		}
		content.Add(widget.NewLabel(""))

		// Always show the silence form - users should be able to silence any alert
		silenceForm := aw.createSilenceForm(alert)
		content.Add(silenceForm)
	}

	return container.NewScroll(content)
}

// createSilenceInfoCard creates a card showing detailed silence information
func (aw *AlertsWindow) createSilenceInfoCard(silenceID string, index int) *widget.Card {
	// Try to fetch silence details
	silence, err := aw.client.FetchSilence(silenceID)
	if err != nil {
		// If we can't fetch details, show basic info
		errorContent := container.NewVBox(
			widget.NewLabel(fmt.Sprintf("Silence ID: %s", silenceID)),
			widget.NewLabel(fmt.Sprintf("⚠️ Could not fetch details: %v", err)),
		)
		return widget.NewCard(fmt.Sprintf("Silence %d", index), "", errorContent)
	}

	// Create detailed silence information
	silenceInfo := container.NewVBox()

	// Basic info
	silenceInfo.Add(widget.NewLabelWithStyle(fmt.Sprintf("ID: %s", silence.ID), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}))
	silenceInfo.Add(widget.NewLabel(fmt.Sprintf("Created by: %s", silence.CreatedBy)))

	// Comment
	if silence.Comment != "" {
		commentEntry := widget.NewMultiLineEntry()
		commentEntry.SetText(silence.Comment)
		commentEntry.Wrapping = fyne.TextWrapWord
		commentEntry.Disable()
		commentEntry.Resize(fyne.NewSize(400, 60))
		silenceInfo.Add(widget.NewLabel("Comment:"))
		silenceInfo.Add(commentEntry)
	}

	// Timing information
	silenceInfo.Add(widget.NewSeparator())
	silenceInfo.Add(widget.NewLabel(fmt.Sprintf("Started: %s", silence.StartsAt.Format("2006-01-02 15:04:05"))))
	silenceInfo.Add(widget.NewLabel(fmt.Sprintf("Expires: %s", silence.EndsAt.Format("2006-01-02 15:04:05"))))

	// Status and time remaining
	statusText := fmt.Sprintf("Status: %s", strings.ToUpper(silence.Status.State))
	if silence.IsActive() {
		remaining := silence.TimeRemaining()
		if remaining > 0 {
			statusText += fmt.Sprintf(" (expires in %s)", formatDuration(remaining))
		}
	}
	statusLabel := widget.NewLabel(statusText)
	if silence.IsActive() {
		statusLabel.Importance = widget.WarningImportance
	} else if silence.IsExpired() {
		statusLabel.Importance = widget.LowImportance
	}
	silenceInfo.Add(statusLabel)

	// Matchers information
	if len(silence.Matchers) > 0 {
		silenceInfo.Add(widget.NewSeparator())
		silenceInfo.Add(widget.NewLabelWithStyle("Matchers:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		for _, matcher := range silence.Matchers {
			operator := "="
			if !matcher.IsEqual {
				operator = "!="
			}
			if matcher.IsRegex {
				operator += "~"
			}
			matcherText := fmt.Sprintf("• %s %s %s", matcher.Name, operator, matcher.Value)
			matcherLabel := widget.NewLabelWithStyle(matcherText, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
			silenceInfo.Add(matcherLabel)
		}
	}

	return widget.NewCard(fmt.Sprintf("Silence %d", index), "", silenceInfo)
}

// createSilenceForm creates a form for creating a new silence
func (aw *AlertsWindow) createSilenceForm(alert models.Alert) fyne.CanvasObject {
	form := container.NewVBox()

	// Creator field
	form.Add(widget.NewLabelWithStyle("Created by:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	creatorEntry := widget.NewEntry()
	creatorEntry.SetPlaceHolder("Your name or identifier...")
	creatorEntry.SetText("notificator-gui") // Default value
	form.Add(creatorEntry)

	// Duration selection
	form.Add(widget.NewLabel(""))
	form.Add(widget.NewLabelWithStyle("Duration:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	durationOptions := []string{"1 hour", "4 hours", "8 hours", "24 hours", "3 days", "1 week", "Custom"}
	durationSelect := widget.NewSelect(durationOptions, nil)
	durationSelect.SetSelected("1 hour")
	form.Add(durationSelect)

	// Custom duration field (initially hidden)
	customDurationContainer := container.NewVBox()
	customDurationContainer.Hide()

	customDurationEntry := widget.NewEntry()
	customDurationEntry.SetPlaceHolder("Examples: 2h30m, 5d, 1w2d, 30m")

	durationHelpText := widget.NewRichTextFromMarkdown(`**Duration Format Examples:**
• **Minutes:** 30m, 45m
• **Hours:** 2h, 12h, 2h30m
• **Days:** 1d, 5d, 2d12h
• **Weeks:** 1w, 2w, 1w3d
• **Combined:** 1w2d3h30m`)
	durationHelpText.Wrapping = fyne.TextWrapWord

	customDurationContainer.Add(widget.NewLabel("Enter custom duration:"))
	customDurationContainer.Add(customDurationEntry)
	customDurationContainer.Add(durationHelpText)

	form.Add(customDurationContainer)

	// Handle duration selection change
	durationSelect.OnChanged = func(selected string) {
		if selected == "Custom" {
			customDurationContainer.Show()
		} else {
			customDurationContainer.Hide()
		}
	}

	// Comment field
	form.Add(widget.NewLabel(""))
	form.Add(widget.NewLabelWithStyle("Comment (optional):", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	commentEntry := widget.NewMultiLineEntry()
	commentEntry.SetPlaceHolder("Reason for silencing this alert...")
	commentEntry.Wrapping = fyne.TextWrapWord
	commentEntry.Resize(fyne.NewSize(400, 80))
	form.Add(commentEntry)

	// Matchers preview
	form.Add(widget.NewLabel(""))
	form.Add(widget.NewLabelWithStyle("Silence will match:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	matchersInfo := container.NewVBox()
	matchersInfo.Add(widget.NewLabelWithStyle(fmt.Sprintf("• alertname = %s", alert.GetAlertName()), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}))

	if instance := alert.GetInstance(); instance != "unknown" && instance != "" {
		matchersInfo.Add(widget.NewLabelWithStyle(fmt.Sprintf("• instance = %s", instance), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}))
	}

	form.Add(matchersInfo)

	// Advanced options (collapsed by default)
	form.Add(widget.NewLabel(""))
	advancedCheck := widget.NewCheck("Show advanced options", nil)
	form.Add(advancedCheck)

	advancedContainer := container.NewVBox()
	advancedContainer.Hide()

	// Custom matchers
	advancedContainer.Add(widget.NewLabelWithStyle("Additional Matchers:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	advancedContainer.Add(widget.NewLabel("Add custom label matchers (one per line, format: label=value)"))

	customMatchersEntry := widget.NewMultiLineEntry()
	customMatchersEntry.SetPlaceHolder("job=prometheus\nenv=production")
	customMatchersEntry.Resize(fyne.NewSize(400, 60))
	advancedContainer.Add(customMatchersEntry)

	form.Add(advancedContainer)

	// Toggle advanced options
	advancedCheck.OnChanged = func(checked bool) {
		if checked {
			advancedContainer.Show()
		} else {
			advancedContainer.Hide()
		}
	}

	// Create silence button
	form.Add(widget.NewLabel(""))
	createBtn := widget.NewButtonWithIcon("Create Silence", theme.VolumeDownIcon(), func() {
		aw.createSilenceFromForm(alert, creatorEntry.Text, durationSelect.Selected, customDurationEntry.Text, commentEntry.Text, customMatchersEntry.Text)
	})
	createBtn.Importance = widget.WarningImportance
	form.Add(createBtn)

	return form
}

// parseDuration parses a custom duration string and returns a time.Duration
func (aw *AlertsWindow) parseDuration(durationStr string) (time.Duration, error) {
	// Clean the input
	durationStr = strings.TrimSpace(strings.ToLower(durationStr))
	if durationStr == "" {
		return 0, fmt.Errorf("duration cannot be empty")
	}

	// Try to parse using Go's built-in parser first
	if duration, err := time.ParseDuration(durationStr); err == nil {
		return duration, nil
	}

	// Custom parsing for more flexible formats
	var totalDuration time.Duration

	// Split by common separators and parse each part
	parts := strings.FieldsFunc(durationStr, func(r rune) bool {
		return r == ' ' || r == ','
	})

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Try standard Go duration parsing
		if d, err := time.ParseDuration(part); err == nil {
			totalDuration += d
			continue
		}

		// Custom parsing for week format
		if strings.HasSuffix(part, "w") {
			weekStr := strings.TrimSuffix(part, "w")
			if weeks, err := time.ParseDuration(weekStr + "h"); err == nil {
				totalDuration += weeks * 24 * 7 // Convert to weeks
				continue
			}
		}

		return 0, fmt.Errorf("invalid duration format: %s", part)
	}

	if totalDuration == 0 {
		return 0, fmt.Errorf("invalid duration format: %s", durationStr)
	}

	return totalDuration, nil
}

// createSilenceFromForm creates a silence based on the form inputs
func (aw *AlertsWindow) createSilenceFromForm(alert models.Alert, creator, duration, customDuration, comment, customMatchers string) {
	// Parse duration
	var silenceDuration time.Duration
	var err error

	if duration == "Custom" {
		// Parse custom duration
		silenceDuration, err = aw.parseDuration(customDuration)
		if err != nil {
			dialog.ShowError(fmt.Errorf("Invalid duration format: %v\n\nExamples of valid formats:\n• 30m (30 minutes)\n• 2h30m (2 hours 30 minutes)\n• 1d (1 day)\n• 1w2d (1 week 2 days)", err), aw.window)
			return
		}
	} else {
		// Use predefined duration
		switch duration {
		case "1 hour":
			silenceDuration = time.Hour
		case "4 hours":
			silenceDuration = 4 * time.Hour
		case "8 hours":
			silenceDuration = 8 * time.Hour
		case "24 hours":
			silenceDuration = 24 * time.Hour
		case "3 days":
			silenceDuration = 72 * time.Hour
		case "1 week":
			silenceDuration = 168 * time.Hour
		default:
			silenceDuration = time.Hour // Default to 1 hour
		}
	}

	// Validate duration (minimum 1 minute, maximum 1 year)
	if silenceDuration < time.Minute {
		dialog.ShowError(fmt.Errorf("Duration too short: minimum is 1 minute"), aw.window)
		return
	}
	if silenceDuration > 365*24*time.Hour {
		dialog.ShowError(fmt.Errorf("Duration too long: maximum is 1 year"), aw.window)
		return
	}

	// Validate creator
	if strings.TrimSpace(creator) == "" {
		dialog.ShowError(fmt.Errorf("Creator field cannot be empty"), aw.window)
		return
	}

	// Create silence matchers
	var matchers []models.SilenceMatcher

	// Always match on alertname
	matchers = append(matchers, models.SilenceMatcher{
		Name:    "alertname",
		Value:   alert.GetAlertName(),
		IsRegex: false,
		IsEqual: true,
	})

	// Add instance matcher if available
	if instance := alert.GetInstance(); instance != "unknown" && instance != "" {
		matchers = append(matchers, models.SilenceMatcher{
			Name:    "instance",
			Value:   instance,
			IsRegex: false,
			IsEqual: true,
		})
	}

	// Parse custom matchers
	if customMatchers != "" {
		lines := strings.Split(customMatchers, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Simple parsing: label=value
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				matchers = append(matchers, models.SilenceMatcher{
					Name:    strings.TrimSpace(parts[0]),
					Value:   strings.TrimSpace(parts[1]),
					IsRegex: false,
					IsEqual: true,
				})
			}
		}
	}

	// Create the silence
	now := time.Now()
	silence := models.Silence{
		Matchers:  matchers,
		StartsAt:  now,
		EndsAt:    now.Add(silenceDuration),
		CreatedBy: strings.TrimSpace(creator),
		Comment:   comment,
	}

	// Show progress
	aw.setStatus("Creating silence...")

	go func() {
		createdSilence, err := aw.client.CreateSilence(silence)

		aw.scheduleUpdate(func() {
			if err != nil {
				log.Printf("Failed to create silence: %v", err)
				aw.setStatus("Failed to create silence")
				dialog.ShowError(fmt.Errorf("Failed to create silence: %v", err), aw.window)
			} else {
				aw.setStatus(fmt.Sprintf("Silence created: %s", createdSilence.ID))
				dialog.ShowInformation("Success",
					fmt.Sprintf("Silence created successfully!\nID: %s\nDuration: %s",
						createdSilence.ID, duration), aw.window)

				// Refresh alerts to show updated status
				aw.loadAlerts()
			}
		})
	}()
}

// addSilenceDetails fetches and adds detailed silence information to the status info
func (aw *AlertsWindow) addSilenceDetails(statusInfo *fyne.Container, silenceID string) {
	// Try to fetch silence details from Alertmanager
	silence, err := aw.client.FetchSilence(silenceID)
	if err != nil {
		// If we can't fetch details, show basic info
		statusInfo.Add(widget.NewLabel(fmt.Sprintf("     ⚠️ Could not fetch silence details: %v", err)))
		return
	}

	// Create a card with silence details
	silenceDetails := container.NewVBox()

	// Basic silence info
	silenceDetails.Add(widget.NewLabelWithStyle(fmt.Sprintf("     📝 Comment: %s", silence.Comment), fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	silenceDetails.Add(widget.NewLabel(fmt.Sprintf("     👤 Created by: %s", silence.CreatedBy)))

	// Timing information
	silenceDetails.Add(widget.NewLabel(fmt.Sprintf("     ⏰ Started: %s", silence.StartsAt.Format("2006-01-02 15:04:05"))))
	silenceDetails.Add(widget.NewLabel(fmt.Sprintf("     ⏰ Expires: %s", silence.EndsAt.Format("2006-01-02 15:04:05"))))

	// Status and time remaining
	statusText := fmt.Sprintf("     📊 Status: %s", strings.ToUpper(silence.Status.State))
	if silence.IsActive() {
		remaining := silence.TimeRemaining()
		if remaining > 0 {
			statusText += fmt.Sprintf(" (expires in %s)", formatDuration(remaining))
		}
	}
	silenceDetails.Add(widget.NewLabel(statusText))

	// Matchers information
	if len(silence.Matchers) > 0 {
		silenceDetails.Add(widget.NewLabel("     🎯 Matchers:"))
		for _, matcher := range silence.Matchers {
			operator := "="
			if !matcher.IsEqual {
				operator = "!="
			}
			if matcher.IsRegex {
				operator += "~"
			}
			matcherText := fmt.Sprintf("       • %s %s %s", matcher.Name, operator, matcher.Value)
			silenceDetails.Add(widget.NewLabel(matcherText))
		}
	}

	// Add all silence details to the status info
	for _, obj := range silenceDetails.Objects {
		statusInfo.Add(obj)
	}

	// Add a small separator after each silence
	statusInfo.Add(widget.NewLabel(""))
}
