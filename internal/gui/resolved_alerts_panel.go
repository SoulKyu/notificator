package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (aw *AlertsWindow) showResolvedAlertsDialog() {
	resolvedAlerts := aw.resolvedAlertsCache.GetResolvedAlerts()
	
	// Create main container
	content := container.NewVBox()
	
	// Header with count
	count := len(resolvedAlerts)
	headerText := fmt.Sprintf("🟢 Resolved Alerts (%d)", count)
	header := widget.NewRichTextFromMarkdown(fmt.Sprintf("## %s", headerText))
	content.Add(header)
	content.Add(widget.NewSeparator())
	
	if count == 0 {
		// No resolved alerts
		emptyCard := widget.NewCard("", "No resolved alerts", container.NewVBox(
			widget.NewLabel("No alerts have been resolved recently."),
			widget.NewLabel("Resolved alerts are kept for a limited time and then automatically removed."),
		))
		content.Add(emptyCard)
	} else {
		// Create scroll container for alerts
		alertsList := container.NewVBox()
		
		// Add each resolved alert
		for _, resolvedAlert := range resolvedAlerts {
			alertCard := aw.createResolvedAlertCard(resolvedAlert)
			alertsList.Add(alertCard)
		}
		
		scroll := container.NewScroll(alertsList)
		scroll.SetMinSize(fyne.NewSize(800, 400))
		content.Add(scroll)
		
		// Add clear all button
		clearBtn := widget.NewButtonWithIcon("Clear All", theme.DeleteIcon(), func() {
			aw.resolvedAlertsCache.Clear()
			aw.refreshResolvedAlertsDialog()
		})
		clearBtn.Importance = widget.DangerImportance
		
		buttonContainer := container.NewHBox(layout.NewSpacer(), clearBtn)
		content.Add(buttonContainer)
	}
	
	// Create and show dialog
	dialog := container.NewBorder(nil, nil, nil, nil, content)
	
	aw.resolvedDialog = fyne.CurrentApp().NewWindow("Resolved Alerts")
	aw.resolvedDialog.SetContent(dialog)
	aw.resolvedDialog.Resize(fyne.NewSize(900, 600))
	aw.resolvedDialog.CenterOnScreen()
	aw.resolvedDialog.Show()
}

func (aw *AlertsWindow) createResolvedAlertCard(resolvedAlert ResolvedAlert) *widget.Card {
	alert := resolvedAlert.Alert
	
	// Card title with alert name and resolved time
	resolvedTime := resolvedAlert.ResolvedAt.Format("15:04:05")
	expiresTime := resolvedAlert.ExpiresAt.Format("15:04:05")
	title := fmt.Sprintf("🟢 %s (resolved at %s)", alert.GetAlertName(), resolvedTime)
	
	// Card content
	content := container.NewVBox()
	
	// Alert details
	detailsContainer := container.NewVBox()
	detailsContainer.Add(widget.NewLabel(fmt.Sprintf("Summary: %s", alert.GetSummary())))
	detailsContainer.Add(widget.NewLabel(fmt.Sprintf("Instance: %s", alert.GetInstance())))
	detailsContainer.Add(widget.NewLabel(fmt.Sprintf("Team: %s", alert.GetTeam())))
	detailsContainer.Add(widget.NewLabel(fmt.Sprintf("Original Severity: %s", alert.GetSeverity())))
	detailsContainer.Add(widget.NewLabel(fmt.Sprintf("Expires at: %s", expiresTime)))
	
	content.Add(detailsContainer)
	
	// Action buttons
	buttonsContainer := container.NewHBox()
	
	// View details button
	detailsBtn := widget.NewButtonWithIcon("View Details", theme.InfoIcon(), func() {
		// Use the same alert details modal but for resolved alert
		aw.showAlertDetails(alert)
	})
	detailsBtn.Importance = widget.MediumImportance
	
	// Remove from resolved button
	removeBtn := widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() {
		aw.resolvedAlertsCache.Remove(alert.GetFingerprint())
		aw.refreshResolvedAlertsDialog()
	})
	removeBtn.Importance = widget.LowImportance
	
	buttonsContainer.Add(detailsBtn)
	buttonsContainer.Add(layout.NewSpacer())
	buttonsContainer.Add(removeBtn)
	
	content.Add(widget.NewSeparator())
	content.Add(buttonsContainer)
	
	// Create card with green theme
	card := widget.NewCard(title, "", content)
	return card
}

func (aw *AlertsWindow) refreshResolvedAlertsDialog() {
	if aw.resolvedDialog != nil {
		aw.resolvedDialog.Close()
		aw.showResolvedAlertsDialog()
	}
}

func (aw *AlertsWindow) getResolvedAlertsCount() int {
	return aw.resolvedAlertsCache.GetCount()
}