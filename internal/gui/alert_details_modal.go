package gui

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	alertpb "notificator/internal/backend/proto/alert"
	authpb "notificator/internal/backend/proto/auth"
	"notificator/internal/models"
)

func (aw *AlertsWindow) showAlertDetails(alert models.Alert) {
	loadingContent := aw.createLoadingScreen(alert)

	dialogContent := container.NewBorder(nil, nil, nil, nil, loadingContent)
	alertDialog := dialog.NewCustom("Alert Details", "Close", dialogContent, aw.window)
	alertDialog.Resize(fyne.NewSize(1400, 1100))
	alertDialog.Show()

	// Start real-time subscription for this alert
	alertKey := alert.GetFingerprint()
	if aw.backendClient != nil && aw.backendClient.IsLoggedIn() {
		aw.startAlertSubscription(alertKey, alertDialog)
	}

	// Load actual content asynchronously
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in async content loading: %v", r)
				fyne.Do(func() {
					errorContent := container.NewVBox(
						widget.NewLabel("❌ Error loading alert details"),
						widget.NewLabel(fmt.Sprintf("Error: %v", r)),
						widget.NewLabel("Please try refreshing or reopening the alert."),
					)
					dialogContent.RemoveAll()
					dialogContent.Add(container.NewScroll(errorContent))
					dialogContent.Refresh()
				})
			}
		}()

		log.Printf("Starting async content loading...")

		// Add a small delay to ensure dialog is fully displayed
		time.Sleep(100 * time.Millisecond)
		log.Printf("Delay completed, creating simple content...")

		// Create simple content structure
		content := aw.createSimpleAlertContent(alert, alertDialog)
		log.Printf("Simple content created successfully")

		fyne.Do(func() {
			log.Printf("Setting content in UI thread...")
			// Hide the current dialog
			alertDialog.Hide()

			// Create new dialog with proper content
			newDialogContent := container.NewBorder(nil, nil, nil, nil, content)
			newDialog := dialog.NewCustom("Alert Details", "Close", newDialogContent, aw.window)
			newDialog.Resize(fyne.NewSize(1400, 1100))
			newDialog.Show()

			log.Printf("Content set successfully")
			log.Printf("showAlertDetails completed successfully")
		})

		log.Printf("Async content loading completed")
	}()
}

func (aw *AlertsWindow) createLoadingScreen(alert models.Alert) fyne.CanvasObject {
	// Create the most minimal loading screen possible to avoid any blocking operations
	loadingContainer := container.NewVBox()

	// Simple loading message without any complex operations
	loadingCard := widget.NewCard("📊 Loading Alert Details", "Please wait while we load the alert information", container.NewVBox())

	loadingContent := container.NewVBox()
	loadingContent.Add(widget.NewLabel("🔄 Loading alert details..."))
	loadingContent.Add(widget.NewLabel(""))
	loadingContent.Add(widget.NewLabel("This may take a moment to fetch all information."))
	loadingContent.Add(widget.NewLabel(""))
	loadingContent.Add(widget.NewLabel("The window will update automatically when ready."))

	loadingCard.SetContent(loadingContent)
	loadingContainer.Add(loadingCard)

	return container.NewScroll(loadingContainer)
}

func (aw *AlertsWindow) createAlertContent(alert models.Alert, detailsWindow fyne.Window) fyne.CanvasObject {
	log.Printf("createBasicAlertDetailsContent called")

	// Create the most minimal content possible
	mainContainer := container.NewVBox()

	// Create minimal toolbar without any potentially blocking operations
	toolbar := aw.createToolbar(detailsWindow)
	mainContainer.Add(toolbar)

	// Create a single tab with just a simple message
	tabs := container.NewAppTabs()

	// Create a simple welcome tab that loads immediately
	welcomeContent := container.NewVBox()
	welcomeContent.Add(widget.NewLabel("🎉 Alert Details Loaded Successfully"))
	welcomeContent.Add(widget.NewLabel(""))
	welcomeContent.Add(widget.NewLabel("Click on the tabs above to view different aspects of this alert:"))
	welcomeContent.Add(widget.NewLabel(""))
	welcomeContent.Add(widget.NewLabel("📊 Overview - General alert information"))
	welcomeContent.Add(widget.NewLabel("🔍 Details - Detailed alert data"))
	welcomeContent.Add(widget.NewLabel("🏷️ Labels - Alert labels"))
	welcomeContent.Add(widget.NewLabel("📝 Annotations - Alert annotations"))
	welcomeContent.Add(widget.NewLabel("🔇 Silence - Silence management"))
	welcomeContent.Add(widget.NewLabel("🤝 Collaboration - Team collaboration features"))

	tabs.Append(container.NewTabItem("🏠 Welcome", container.NewScroll(welcomeContent)))

	// Add placeholder tabs that will load on demand
	overviewPlaceholder := aw.createTabPlaceholder("📊 Overview", "Loading alert overview...")
	detailsPlaceholder := aw.createTabPlaceholder("🔍 Details", "Loading alert details...")
	labelsPlaceholder := aw.createTabPlaceholder("🏷️ Labels", "Loading labels...")
	annotationsPlaceholder := aw.createTabPlaceholder("📝 Annotations", "Loading annotations...")
	silencePlaceholder := aw.createSilencePlaceholder(alert)

	tabs.Append(container.NewTabItem("📊 Overview", overviewPlaceholder))
	tabs.Append(container.NewTabItem("🔍 Details", detailsPlaceholder))
	tabs.Append(container.NewTabItem("🏷️ Labels", labelsPlaceholder))
	tabs.Append(container.NewTabItem("📝 Annotations", annotationsPlaceholder))
	tabs.Append(container.NewTabItem("🔇 Silence", silencePlaceholder))

	// Add collaboration tab
	var collaborationPlaceholder fyne.CanvasObject
	if aw.isUserAuthenticated() {
		collaborationPlaceholder = aw.createCollaborationPlaceholder(alert)
		tabs.Append(container.NewTabItem("🤝 Collaboration", collaborationPlaceholder))
	} else {
		basicCollabContent := aw.createBasicCollaborationTab()
		tabs.Append(container.NewTabItem("🤝 Collaboration", basicCollabContent))
	}

	// Set up comprehensive lazy loading for all tabs
	tabs.OnChanged = func(tab *container.TabItem) {
		log.Printf("Tab changed to: %s", tab.Text)

		switch tab.Text {
		case "📊 Overview":
			if tab.Content == overviewPlaceholder {
				aw.loadTabContentAsync(tab, tabs, func() fyne.CanvasObject {
					return aw.createOverviewTab(alert)
				})
			}
		case "🔍 Details":
			if tab.Content == detailsPlaceholder {
				aw.loadTabContentAsync(tab, tabs, func() fyne.CanvasObject {
					return aw.createDetailsTab(alert)
				})
			}
		case "🏷️ Labels":
			if tab.Content == labelsPlaceholder {
				aw.loadTabContentAsync(tab, tabs, func() fyne.CanvasObject {
					return aw.createLabelsTab(alert)
				})
			}
		case "📝 Annotations":
			if tab.Content == annotationsPlaceholder {
				aw.loadTabContentAsync(tab, tabs, func() fyne.CanvasObject {
					return aw.createAnnotationsTab(alert)
				})
			}
		case "🔇 Silence":
			if tab.Content == silencePlaceholder {
				aw.loadSilenceContentAsync(alert, tab, tabs)
			}
		case "🤝 Collaboration":
			if aw.isUserAuthenticated() && collaborationPlaceholder != nil && tab.Content == collaborationPlaceholder {
				aw.loadCollaborationContentAsync(alert, tab, tabs)
			}
		}
	}

	mainContainer.Add(tabs)
	return mainContainer
}

// createSimpleAlertContent creates the complete alert details content
func (aw *AlertsWindow) createSimpleAlertContent(alert models.Alert, alertDialog interface{}) fyne.CanvasObject {
	log.Printf("createSimpleAlertContent called")

	// Create the main container with proper sizing and padding
	mainContainer := container.NewBorder(nil, nil, nil, nil, nil)

	// Create enhanced toolbar with alert info
	toolbar := aw.createAlertToolbar(alert)

	// Create tabbed interface with lazy loading for ALL tabs
	tabs := container.NewAppTabs()

	// Store placeholders for lazy loading
	placeholders := make(map[string]fyne.CanvasObject)

	// Overview tab - loads immediately as default
	overviewContent := aw.createOverviewTab(alert)
	tabs.Append(container.NewTabItem("📊 Overview", overviewContent))

	// Create placeholder tabs
	placeholders["details"] = aw.createTabPlaceholder("🔍 Details", "Loading alert details...")
	placeholders["labels"] = aw.createTabPlaceholder("🏷️ Labels", "Loading labels...")
	placeholders["annotations"] = aw.createTabPlaceholder("📝 Annotations", "Loading annotations...")
	placeholders["silence"] = aw.createSilencePlaceholder(alert)

	// Add placeholder tabs
	detailsTab := container.NewTabItem("🔍 Details", placeholders["details"])
	labelsTab := container.NewTabItem("🏷️ Labels", placeholders["labels"])
	annotationsTab := container.NewTabItem("📝 Annotations", placeholders["annotations"])
	silenceTab := container.NewTabItem("🔇 Silence", placeholders["silence"])

	tabs.Append(detailsTab)
	tabs.Append(labelsTab)
	tabs.Append(annotationsTab)
	tabs.Append(silenceTab)

	// Add collaboration tab
	var collaborationTab *container.TabItem
	if aw.isUserAuthenticated() {
		placeholders["collaboration"] = aw.createCollaborationPlaceholder(alert)
		collaborationTab = container.NewTabItem("🤝 Collaboration", placeholders["collaboration"])
		tabs.Append(collaborationTab)
	} else {
		basicCollabContent := aw.createBasicCollaborationTab()
		tabs.Append(container.NewTabItem("🤝 Collaboration", basicCollabContent))
	}

	// Set up lazy loading for tabs
	tabs.OnChanged = func(tab *container.TabItem) {
		log.Printf("Tab changed to: %s", tab.Text)

		switch tab.Text {
		case "🔍 Details":
			if tab.Content == placeholders["details"] {
				aw.loadTabContentAsync(tab, tabs, func() fyne.CanvasObject {
					return aw.createDetailsTab(alert)
				})
			}
		case "🏷️ Labels":
			if tab.Content == placeholders["labels"] {
				aw.loadTabContentAsync(tab, tabs, func() fyne.CanvasObject {
					return aw.createLabelsTab(alert)
				})
			}
		case "📝 Annotations":
			if tab.Content == placeholders["annotations"] {
				aw.loadTabContentAsync(tab, tabs, func() fyne.CanvasObject {
					return aw.createAnnotationsTab(alert)
				})
			}
		case "🔇 Silence":
			if tab.Content == placeholders["silence"] {
				aw.loadSilenceContentAsync(alert, tab, tabs)
			}
		case "🤝 Collaboration":
			if aw.isUserAuthenticated() && collaborationTab != nil && tab.Content == placeholders["collaboration"] {
				aw.loadCollaborationContentAsync(alert, tab, tabs)
			}
		}
	}

	// Create a container that will force tabs to use all available height
	topSection := container.NewVBox(toolbar, widget.NewSeparator())

	// Use border layout to make tabs fill all available space
	mainContainer = container.NewBorder(
		topSection,
		nil,
		nil,
		nil,
		tabs,
	)

	// Force the main container to use all available space
	mainContainer.Resize(fyne.NewSize(1380, 1050))

	log.Printf("createSimpleAlertContent completed")
	return mainContainer
}

func (aw *AlertsWindow) createAlertToolbar(alert models.Alert) fyne.CanvasObject {
	toolbar := container.NewHBox()

	// Alert status and severity icons
	statusIcon := aw.getStatusIcon(alert.Status.State)
	severityIcon := aw.getSeverityIcon(alert.GetSeverity())

	// Alert name with icons
	alertLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("%s %s %s", statusIcon, severityIcon, alert.GetAlertName()),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)
	toolbar.Add(alertLabel)

	// Add some spacing
	toolbar.Add(layout.NewSpacer())

	// Duration badge
	duration := alert.Duration()
	durationText := formatDuration(duration)
	durationLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("⏱️ %s", durationText),
		fyne.TextAlignCenter,
		fyne.TextStyle{},
	)
	toolbar.Add(durationLabel)

	// Refresh button
	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		aw.refreshAlertData(alert)
	})
	toolbar.Add(refreshBtn)

	// Copy button
	copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		aw.copyAlertDetailsToClipboard(alert)
	})
	toolbar.Add(copyBtn)

	// External link button (if generator URL exists)
	if alert.GeneratorURL != "" {
		linkBtn := widget.NewButtonWithIcon("", theme.ComputerIcon(), func() {
			aw.openURLInBrowser(alert.GeneratorURL)
		})
		toolbar.Add(linkBtn)
	}

	return toolbar
}

// copyAlertDetailsToClipboard copies alert details to clipboard
func (aw *AlertsWindow) copyAlertDetailsToClipboard(alert models.Alert) {
	// Format alert details for clipboard
	var details strings.Builder
	details.WriteString(fmt.Sprintf("Alert: %s\n", alert.GetAlertName()))
	details.WriteString(fmt.Sprintf("Status: %s\n", alert.Status.State))
	details.WriteString(fmt.Sprintf("Severity: %s\n", alert.GetSeverity()))
	details.WriteString(fmt.Sprintf("Duration: %s\n", formatDuration(alert.Duration())))
	details.WriteString(fmt.Sprintf("Started: %s\n", alert.StartsAt.Format(time.RFC3339)))

	if !alert.EndsAt.IsZero() {
		details.WriteString(fmt.Sprintf("Ended: %s\n", alert.EndsAt.Format(time.RFC3339)))
	}

	details.WriteString("\nLabels:\n")
	for k, v := range alert.Labels {
		details.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
	}

	details.WriteString("\nAnnotations:\n")
	for k, v := range alert.Annotations {
		details.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
	}

	// Copy to clipboard
	aw.window.Clipboard().SetContent(details.String())

	// Show confirmation
	dialog.ShowInformation("Copied", "Alert details copied to clipboard", aw.window)
}

// createTabPlaceholder creates a generic loading placeholder for tabs
func (aw *AlertsWindow) createTabPlaceholder(title string, message string) fyne.CanvasObject {
	loadingContainer := container.NewVBox()

	loadingCard := widget.NewCard(title, message, container.NewVBox())
	loadingContent := container.NewVBox()
	loadingContent.Add(widget.NewLabel("🔄 Loading..."))
	loadingContent.Add(widget.NewLabel(""))
	loadingContent.Add(widget.NewLabel("Content will appear when you select this tab."))

	loadingCard.SetContent(loadingContent)
	loadingContainer.Add(loadingCard)

	return container.NewScroll(loadingContainer)
}

// loadTabContentAsync loads tab content asynchronously
func (aw *AlertsWindow) loadTabContentAsync(tab *container.TabItem, tabs *container.AppTabs, contentLoader func() fyne.CanvasObject) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Tab %s loading panic: %v", tab.Text, r)
				fyne.Do(func() {
					errorContent := container.NewVBox(
						widget.NewLabel("❌ Error loading tab content"),
						widget.NewLabel(fmt.Sprintf("Error: %v", r)),
						widget.NewLabel("Please try refreshing the tab."),
					)
					tab.Content = container.NewScroll(errorContent)
					tabs.Refresh()
				})
			}
		}()

		content := contentLoader()
		fyne.Do(func() {
			tab.Content = content
			tabs.Refresh()
		})
	}()
}

// createAlertDetailsContent creates the content for the alert details window
func (aw *AlertsWindow) createAlertDetailsContent(alert models.Alert, detailsWindow fyne.Window) fyne.CanvasObject {
	log.Printf("createAlertDetailsContent called")

	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in createAlertDetailsContent: %v", r)
		}
	}()

	// Create main container with toolbar
	log.Printf("Creating main container...")
	mainContainer := container.NewVBox()

	// Create toolbar for window controls
	log.Printf("Creating toolbar...")
	toolbar := aw.createAlertDetailsToolbar(alert, detailsWindow)
	mainContainer.Add(toolbar)

	// Create tabbed interface for better organization
	log.Printf("Creating tabs...")
	tabs := container.NewAppTabs()

	// Overview Tab - Always load first
	log.Printf("Creating overview tab...")
	overviewContent := aw.createOverviewTab(alert)
	tabs.Append(container.NewTabItem("📊 Overview", overviewContent))

	// Details Tab
	log.Printf("Creating details tab...")
	detailsContent := aw.createDetailsTab(alert)
	tabs.Append(container.NewTabItem("🔍 Details", detailsContent))

	// Labels Tab
	log.Printf("Creating labels tab...")
	labelsContent := aw.createLabelsTab(alert)
	tabs.Append(container.NewTabItem("🏷️ Labels", labelsContent))

	// Annotations Tab
	log.Printf("Creating annotations tab...")
	annotationsContent := aw.createAnnotationsTab(alert)
	tabs.Append(container.NewTabItem("📝 Annotations", annotationsContent))

	// Collaboration Tab - Load lazily to avoid blocking
	log.Printf("Setting up collaboration tab...")
	var collaborationPlaceholder fyne.CanvasObject
	if aw.isUserAuthenticated() {
		log.Printf("User is authenticated, creating collaboration placeholder...")
		collaborationPlaceholder = aw.createCollaborationPlaceholder(alert)
		collaborationTab := container.NewTabItem("🤝 Collaboration", collaborationPlaceholder)
		tabs.Append(collaborationTab)
	} else {
		log.Printf("User not authenticated, creating basic collaboration tab...")
		// Show basic collaboration info for non-authenticated users
		basicCollabContent := aw.createBasicCollaborationTab()
		tabs.Append(container.NewTabItem("🤝 Collaboration", basicCollabContent))
	}

	// Silence Tab - Load lazily to avoid blocking
	log.Printf("Setting up silence tab...")
	silencePlaceholder := aw.createSilencePlaceholder(alert)
	silenceTab := container.NewTabItem("🔇 Silence", silencePlaceholder)
	tabs.Append(silenceTab)

	// Enhance lazy loading for both collaboration and silence tabs
	originalOnChanged := tabs.OnChanged
	tabs.OnChanged = func(tab *container.TabItem) {
		log.Printf("Tab changed to: %s", tab.Text)

		// Handle collaboration tab lazy loading
		if tab.Text == "🤝 Collaboration" && collaborationPlaceholder != nil && tab.Content == collaborationPlaceholder {
			log.Printf("Loading collaboration content...")
			aw.loadCollaborationContentAsync(alert, tab, tabs)
		}

		// Handle silence tab lazy loading
		if tab.Text == "🔇 Silence" && tab.Content == silencePlaceholder {
			log.Printf("Loading silence content...")
			aw.loadSilenceContentAsync(alert, tab, tabs)
		}

		// Call original handler if it exists
		if originalOnChanged != nil {
			originalOnChanged(tab)
		}
	}

	log.Printf("Adding tabs to main container...")
	mainContainer.Add(tabs)

	log.Printf("createAlertDetailsContent completed successfully")
	return mainContainer
}

// createCollaborationPlaceholder creates a loading placeholder for the collaboration tab
func (aw *AlertsWindow) createCollaborationPlaceholder(alert models.Alert) fyne.CanvasObject {
	loadingContainer := container.NewVBox()

	// Loading indicator
	loadingCard := widget.NewCard("🤝 Collaboration", "Loading collaboration features...", container.NewVBox())

	loadingContent := container.NewVBox()
	loadingContent.Add(widget.NewLabel("🔄 Loading collaboration interface..."))
	loadingContent.Add(widget.NewLabel(""))
	loadingContent.Add(widget.NewLabel("Please wait while we set up the collaboration features."))

	loadingCard.SetContent(loadingContent)
	loadingContainer.Add(loadingCard)

	return container.NewScroll(loadingContainer)
}

// createBasicCollaborationTab creates a basic collaboration tab for non-authenticated users
func (aw *AlertsWindow) createBasicCollaborationTab() fyne.CanvasObject {
	basicContainer := container.NewVBox()

	// Authentication prompt
	authCard := widget.NewCard("🔐 Authentication Required", "Login to access collaboration features", container.NewVBox())

	authContent := container.NewVBox()
	authContent.Add(widget.NewLabel("Collaboration features require backend authentication."))
	authContent.Add(widget.NewLabel(""))
	authContent.Add(widget.NewLabel("Available collaboration features:"))
	authContent.Add(widget.NewLabel("• 🤝 Acknowledge alerts"))
	authContent.Add(widget.NewLabel("• 💬 Add comments and discussions"))
	authContent.Add(widget.NewLabel("• 📈 View activity timeline"))
	authContent.Add(widget.NewLabel("• ⚡ Quick actions"))
	authContent.Add(widget.NewLabel("• 👥 Live presence indicators"))
	authContent.Add(widget.NewLabel(""))

	loginBtn := widget.NewButtonWithIcon("🔑 Login to Backend", theme.LoginIcon(), func() {
		aw.showAuthDialog()
	})
	loginBtn.Importance = widget.HighImportance
	authContent.Add(loginBtn)

	authCard.SetContent(authContent)
	basicContainer.Add(authCard)

	return container.NewScroll(basicContainer)
}

// createSilencePlaceholder creates a loading placeholder for the silence tab
func (aw *AlertsWindow) createSilencePlaceholder(alert models.Alert) fyne.CanvasObject {
	loadingContainer := container.NewVBox()

	// Loading indicator
	loadingCard := widget.NewCard("🔇 Silence", "Loading silence information...", container.NewVBox())

	loadingContent := container.NewVBox()
	loadingContent.Add(widget.NewLabel("🔄 Loading silence details..."))
	loadingContent.Add(widget.NewLabel(""))
	loadingContent.Add(widget.NewLabel("Please wait while we check silence status from Alertmanager."))

	loadingCard.SetContent(loadingContent)
	loadingContainer.Add(loadingCard)

	return container.NewScroll(loadingContainer)
}

// loadCollaborationContentAsync loads collaboration content asynchronously
func (aw *AlertsWindow) loadCollaborationContentAsync(alert models.Alert, tab *container.TabItem, tabs *container.AppTabs) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Collaboration tab panic: %v", r)
				fyne.Do(func() {
					errorContent := container.NewVBox(
						widget.NewLabel("❌ Error loading collaboration features"),
						widget.NewLabel(fmt.Sprintf("Error: %v", r)),
						widget.NewLabel("Please try refreshing the tab."),
					)
					tab.Content = container.NewScroll(errorContent)
					tabs.Refresh()
				})
			}
		}()

		collaborationContent := aw.createCollaborationTab(alert)
		fyne.Do(func() {
			tab.Content = collaborationContent
			tabs.Refresh()
		})
	}()
}

// loadSilenceContentAsync loads silence content asynchronously
func (aw *AlertsWindow) loadSilenceContentAsync(alert models.Alert, tab *container.TabItem, tabs *container.AppTabs) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Silence tab panic: %v", r)
				fyne.Do(func() {
					errorContent := container.NewVBox(
						widget.NewLabel("❌ Error loading silence information"),
						widget.NewLabel(fmt.Sprintf("Error: %v", r)),
						widget.NewLabel("Please try refreshing the tab."),
					)
					tab.Content = container.NewScroll(errorContent)
					tabs.Refresh()
				})
			}
		}()

		silenceContent := aw.createSilenceTab(alert)
		fyne.Do(func() {
			tab.Content = silenceContent
			tabs.Refresh()
		})
	}()
}

// createAlertDetailsToolbar creates a toolbar for the alert details window
func (aw *AlertsWindow) createAlertDetailsToolbar(alert models.Alert, detailsWindow fyne.Window) fyne.CanvasObject {
	toolbar := container.NewHBox()

	// Alert status indicator
	statusIcon := aw.getStatusIcon(alert.Status.State)
	severityIcon := aw.getSeverityIcon(alert.GetSeverity())

	statusLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("%s %s %s", statusIcon, severityIcon, alert.GetAlertName()),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)
	toolbar.Add(statusLabel)

	// Spacer
	toolbar.Add(widget.NewLabel(""))

	// Duration display
	durationLabel := widget.NewLabel(formatDuration(alert.Duration()))
	durationLabel.TextStyle = fyne.TextStyle{Italic: true}
	toolbar.Add(durationLabel)

	// Spacer
	toolbar.Add(widget.NewLabel(""))

	// Refresh button
	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		aw.refreshAlertData(alert)
	})
	refreshBtn.Importance = widget.MediumImportance
	toolbar.Add(refreshBtn)

	// Pin/Unpin button (keeps window on top)
	pinBtn := widget.NewButtonWithIcon("", theme.ViewFullScreenIcon(), func() {
		// Toggle always on top (this is a UI placeholder - actual implementation depends on OS)
		dialog.ShowInformation("Pin Window", "Window pinning feature coming soon", detailsWindow)
	})
	toolbar.Add(pinBtn)

	// Close button
	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		detailsWindow.Close()
	})
	closeBtn.Importance = widget.DangerImportance
	toolbar.Add(closeBtn)

	return toolbar
}

func (aw *AlertsWindow) createToolbar(detailsWindow fyne.Window) fyne.CanvasObject {
	toolbar := container.NewHBox()

	// Simple title label
	titleLabel := widget.NewLabelWithStyle(
		"Alert Details",
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)
	toolbar.Add(titleLabel)

	// Spacer
	toolbar.Add(widget.NewLabel(""))

	// Close button
	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		detailsWindow.Close()
	})
	closeBtn.Importance = widget.DangerImportance
	toolbar.Add(closeBtn)

	return toolbar
}

// refreshAlertData refreshes the alert data and collaboration information
func (aw *AlertsWindow) refreshAlertData(alert models.Alert) {
	// Show loading indicator
	aw.setStatus("Refreshing alert data...")

	go func() {
		// Refresh the main alerts data first
		aw.loadAlertsWithCaching()

		// Also refresh backend collaboration data if available
		if aw.backendClient != nil && aw.backendClient.IsLoggedIn() {
			alertKey := alert.GetFingerprint()

			// Try to refresh comments and acknowledgments
			if _, err := aw.backendClient.GetComments(alertKey); err == nil {
				log.Printf("Refreshed comments for alert %s", alertKey)
			}

			if _, err := aw.getAlertAcknowledgments(alertKey); err == nil {
				log.Printf("Refreshed acknowledgments for alert %s", alertKey)
			}
		}

		// Add a small delay to ensure data is loaded
		time.Sleep(500 * time.Millisecond)

		// Find the updated alert in the current data
		fingerprint := alert.GetFingerprint()
		var updatedAlert *models.Alert

		aw.alertsMutex.RLock()
		for _, a := range aw.alerts {
			if a.GetFingerprint() == fingerprint {
				updatedAlert = &a
				break
			}
		}
		aw.alertsMutex.RUnlock()

		fyne.Do(func() {
			if updatedAlert != nil {
				aw.setStatus("Alert data refreshed successfully")
				dialog.ShowInformation("Refresh", "Alert data refreshed successfully!\n\nThe main alerts table has been updated. For collaboration data updates, switch between tabs to see the latest comments and acknowledgments.", aw.window)
			} else {
				aw.setStatus("Alert may have been resolved or removed")
				dialog.ShowInformation("Refresh", "Alert may have been resolved or is no longer active.\n\nThe alert might have been resolved or removed from the alertmanager.", aw.window)
			}
		})
	}()
}

// createOverviewTab creates the enhanced overview tab content
func (aw *AlertsWindow) createOverviewTab(alert models.Alert) fyne.CanvasObject {
	log.Printf("createOverviewTab called")
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in createOverviewTab: %v", r)
		}
	}()

	// Create main container that will expand to fill available space
	mainContainer := container.NewVBox()

	// Add spacer at the top to push content down
	mainContainer.Add(layout.NewSpacer())

	// Create two-column layout with equal width distribution
	leftColumn := container.NewVBox()
	rightColumn := container.NewVBox()

	// Status Card - left column
	log.Printf("Creating status card...")
	statusCard := aw.createStatusCard(alert)
	log.Printf("Status card created, adding to left column")
	leftColumn.Add(statusCard)
	leftColumn.Add(widget.NewLabel("")) // Spacing
	leftColumn.Add(widget.NewLabel("")) // More spacing

	// Timeline Card - right column
	log.Printf("Creating timeline card...")
	timelineCard := aw.createTimelineCard(alert)
	log.Printf("Timeline card created, adding to right column")
	rightColumn.Add(timelineCard)
	rightColumn.Add(widget.NewLabel("")) // Spacing
	rightColumn.Add(widget.NewLabel("")) // More spacing

	// Metadata Card - left column
	metadataCard := aw.createMetadataCard(alert)
	leftColumn.Add(metadataCard)
	leftColumn.Add(widget.NewLabel("")) // Spacing
	leftColumn.Add(widget.NewLabel("")) // More spacing

	// Generator URL Card - right column (if exists)
	if alert.GeneratorURL != "" {
		generatorCard := aw.createGeneratorURLCard(alert)
		rightColumn.Add(generatorCard)
		rightColumn.Add(widget.NewLabel("")) // Spacing
		rightColumn.Add(widget.NewLabel("")) // More spacing
	}

	// Create horizontal layout for columns with equal spacing
	log.Printf("Creating columns container...")
	columnsContainer := container.NewGridWithColumns(2, leftColumn, rightColumn)

	// Summary Card - full width at the bottom
	log.Printf("Creating summary card...")
	summaryCard := aw.createSummaryCard(alert)

	// Add to main container with better spacing
	log.Printf("Adding components to main container...")
	mainContainer.Add(columnsContainer)
	mainContainer.Add(widget.NewLabel("")) // More spacing
	mainContainer.Add(widget.NewSeparator())
	mainContainer.Add(widget.NewLabel("")) // More spacing
	mainContainer.Add(summaryCard)

	// Add spacer at the bottom to fill remaining space
	mainContainer.Add(layout.NewSpacer())

	// Create scroll container that properly fills the dialog
	log.Printf("Creating scroll container...")
	scrollContainer := container.NewScroll(mainContainer)

	log.Printf("Overview tab content created successfully")
	return scrollContainer
}

// createStatusCard creates a card showing alert status information
func (aw *AlertsWindow) createStatusCard(alert models.Alert) *widget.Card {
	content := container.NewVBox()

	// Main status row
	statusRow := container.NewHBox()

	// Status icon and label
	statusIcon := aw.getStatusIcon(alert.Status.State)
	statusText := strings.ToUpper(string(alert.Status.State[0])) + alert.Status.State[1:]
	statusLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("%s %s", statusIcon, statusText),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)
	statusRow.Add(statusLabel)

	// Add spacing
	statusRow.Add(layout.NewSpacer())

	// Severity badge
	severityIcon := aw.getSeverityIcon(alert.GetSeverity())
	severityText := strings.ToUpper(string(alert.GetSeverity()[0])) + alert.GetSeverity()[1:]
	severityLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("%s %s", severityIcon, severityText),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	// Color code severity
	switch alert.GetSeverity() {
	case "critical":
		severityLabel.Importance = widget.DangerImportance
	case "warning":
		severityLabel.Importance = widget.WarningImportance
	default:
		severityLabel.Importance = widget.MediumImportance
	}

	statusRow.Add(severityLabel)
	content.Add(statusRow)

	// Add some spacing
	content.Add(widget.NewLabel("")) // Empty label for spacing

	// Additional info rows
	infoContainer := container.NewVBox()

	// Team info
	if team := alert.GetTeam(); team != "" {
		teamLabel := widget.NewLabel(fmt.Sprintf("👥 Team: %s", team))
		infoContainer.Add(teamLabel)
	}

	// Instance info
	if instance := alert.GetInstance(); instance != "" {
		instanceLabel := widget.NewLabel(fmt.Sprintf("🖥️ Instance: %s", instance))
		infoContainer.Add(instanceLabel)
	}

	content.Add(infoContainer)

	// Silence/Inhibition status
	if alert.Status.State == "suppressed" || len(alert.Status.SilencedBy) > 0 {
		content.Add(widget.NewLabel("")) // Spacing
		silenceLabel := widget.NewLabel(fmt.Sprintf("🔇 Silenced by: %s", strings.Join(alert.Status.SilencedBy, ", ")))
		silenceLabel.Importance = widget.LowImportance
		content.Add(silenceLabel)
	}

	if len(alert.Status.InhibitedBy) > 0 {
		if alert.Status.State != "suppressed" && len(alert.Status.SilencedBy) == 0 {
			content.Add(widget.NewLabel("")) // Spacing only if no silence info above
		}
		inhibitLabel := widget.NewLabel(fmt.Sprintf("⏸️ Inhibited by: %s", strings.Join(alert.Status.InhibitedBy, ", ")))
		inhibitLabel.Importance = widget.LowImportance
		content.Add(inhibitLabel)
	}

	card := widget.NewCard("🚨 Alert Status", "", content)
	return card
}

// createTimelineCard creates a card showing alert timeline
func (aw *AlertsWindow) createTimelineCard(alert models.Alert) *widget.Card {
	content := container.NewVBox()

	// Started time
	startedRow := container.NewHBox()
	startedRow.Add(widget.NewLabel("🕐 Started:"))
	startedRow.Add(layout.NewSpacer())
	startedLabel := widget.NewLabelWithStyle(
		alert.StartsAt.Format("2006-01-02 15:04:05"),
		fyne.TextAlignTrailing,
		fyne.TextStyle{Monospace: true},
	)
	startedRow.Add(startedLabel)
	content.Add(startedRow)

	// Duration
	durationRow := container.NewHBox()
	durationRow.Add(widget.NewLabel("⏱️ Duration:"))
	durationRow.Add(layout.NewSpacer())
	durationLabel := widget.NewLabelWithStyle(
		formatDuration(alert.Duration()),
		fyne.TextAlignTrailing,
		fyne.TextStyle{Bold: true},
	)
	durationRow.Add(durationLabel)
	content.Add(durationRow)

	// Ended time (if applicable)
	if !alert.EndsAt.IsZero() {
		endedRow := container.NewHBox()
		endedRow.Add(widget.NewLabel("🏁 Ended:"))
		endedRow.Add(layout.NewSpacer())
		endedLabel := widget.NewLabelWithStyle(
			alert.EndsAt.Format("2006-01-02 15:04:05"),
			fyne.TextAlignTrailing,
			fyne.TextStyle{Monospace: true},
		)
		endedRow.Add(endedLabel)
		content.Add(endedRow)
	} else {
		// Show that it's still active
		activeRow := container.NewHBox()
		activeRow.Add(widget.NewLabel("🔴 Status:"))
		activeRow.Add(layout.NewSpacer())
		activeLabel := widget.NewLabelWithStyle(
			"Still Active",
			fyne.TextAlignTrailing,
			fyne.TextStyle{Bold: true},
		)
		activeLabel.Importance = widget.DangerImportance
		activeRow.Add(activeLabel)
		content.Add(activeRow)
	}

	return widget.NewCard("📅 Timeline", "", content)
}

// createSummaryCard creates a card showing alert summary
func (aw *AlertsWindow) createSummaryCard(alert models.Alert) *widget.Card {
	content := container.NewVBox()

	summaryText := alert.GetSummary()
	if summaryText == "" {
		summaryText = "No summary available for this alert"
	}

	summaryLabel := widget.NewLabel(summaryText)
	summaryLabel.Wrapping = fyne.TextWrapWord
	content.Add(summaryLabel)

	// Add description if available
	if description := alert.Annotations["description"]; description != "" && description != summaryText {
		content.Add(widget.NewLabel("")) // Spacing
		descLabel := widget.NewLabelWithStyle("Description:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		content.Add(descLabel)

		descText := widget.NewLabel(description)
		descText.Wrapping = fyne.TextWrapWord
		content.Add(descText)
	}

	return widget.NewCard("📝 Summary", "", content)
}

// createMetadataCard creates a card showing alert metadata
func (aw *AlertsWindow) createMetadataCard(alert models.Alert) *widget.Card {
	content := container.NewVBox()

	// Alert name
	nameRow := container.NewHBox()
	nameRow.Add(widget.NewLabel("🏷️ Alert:"))
	nameRow.Add(layout.NewSpacer())
	nameLabel := widget.NewLabelWithStyle(alert.GetAlertName(), fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	nameRow.Add(nameLabel)
	content.Add(nameRow)

	// Team
	teamRow := container.NewHBox()
	teamRow.Add(widget.NewLabel("👥 Team:"))
	teamRow.Add(layout.NewSpacer())
	teamLabel := widget.NewLabelWithStyle(alert.GetTeam(), fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	teamRow.Add(teamLabel)
	content.Add(teamRow)

	// Instance
	instanceRow := container.NewHBox()
	instanceRow.Add(widget.NewLabel("🖥️ Instance:"))
	instanceRow.Add(layout.NewSpacer())
	instanceLabel := widget.NewLabelWithStyle(alert.GetInstance(), fyne.TextAlignTrailing, fyne.TextStyle{Monospace: true})
	instanceRow.Add(instanceLabel)
	content.Add(instanceRow)

	// Fingerprint (get from labels if available)
	fingerprint := alert.GetFingerprint()
	if fingerprint != "" {
		fingerprintRow := container.NewHBox()
		fingerprintRow.Add(widget.NewLabel("🔑 ID:"))
		fingerprintRow.Add(layout.NewSpacer())
		fingerprintLabel := widget.NewLabel(fingerprint[:8] + "...")
		fingerprintLabel.TextStyle = fyne.TextStyle{Monospace: true}
		fingerprintRow.Add(fingerprintLabel)
		content.Add(fingerprintRow)
	}

	return widget.NewCard("ℹ️ Metadata", "", content)
}

// createGeneratorURLCard creates a card for generator URL
func (aw *AlertsWindow) createGeneratorURLCard(alert models.Alert) *widget.Card {
	content := container.NewVBox()

	// URL preview
	urlLabel := widget.NewLabel(alert.GeneratorURL)
	urlLabel.TextStyle = fyne.TextStyle{Monospace: true}
	urlLabel.Wrapping = fyne.TextWrapWord
	urlLabel.Truncation = fyne.TextTruncateEllipsis
	content.Add(urlLabel)

	// Open button
	openBtn := widget.NewButtonWithIcon("Open in Browser", theme.ComputerIcon(), func() {
		go func() {
			if err := aw.openURLInBrowser(alert.GeneratorURL); err != nil {
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("failed to open URL: %v", err), aw.window)
				})
			}
		}()
	})
	openBtn.Importance = widget.HighImportance
	content.Add(openBtn)

	return widget.NewCard("🔗 Generator URL", "", content)
}

// createQuickActionsCard creates a card with quick action buttons
func (aw *AlertsWindow) createQuickActionsCard(alert models.Alert) *widget.Card {
	content := container.NewVBox()

	// First row of actions
	row1 := container.NewGridWithColumns(3)

	// Acknowledge button (if authenticated)
	if aw.isUserAuthenticated() && alert.Status.State == "firing" {
		ackBtn := widget.NewButtonWithIcon("Acknowledge", theme.ConfirmIcon(), func() {
			aw.acknowledgeAlertFromDetails(alert)
		})
		ackBtn.Importance = widget.HighImportance
		row1.Add(ackBtn)
	} else {
		// Add spacer if not authenticated
		row1.Add(widget.NewLabel(""))
	}

	// Silence button
	if alert.Status.State != "suppressed" {
		silenceBtn := widget.NewButtonWithIcon("Silence", theme.VolumeDownIcon(), func() {
			aw.showSilenceDialog(alert)
		})
		row1.Add(silenceBtn)
	} else {
		// Add spacer if suppressed
		row1.Add(widget.NewLabel(""))
	}

	// Hide button
	hideBtn := widget.NewButtonWithIcon("Hide Alert", theme.VisibilityOffIcon(), func() {
		aw.hideAlert(alert)
	})
	row1.Add(hideBtn)

	content.Add(row1)

	// Add spacing
	content.Add(widget.NewLabel(""))

	// Second row of actions
	row2 := container.NewGridWithColumns(2)

	// Copy details button
	copyBtn := widget.NewButtonWithIcon("Copy Details", theme.ContentCopyIcon(), func() {
		aw.copyAlertDetailsToClipboard(alert)
	})
	row2.Add(copyBtn)

	// Share button
	shareBtn := widget.NewButtonWithIcon("Share", theme.MailSendIcon(), func() {
		aw.shareAlertFromDetails(alert)
	})
	row2.Add(shareBtn)

	content.Add(row2)

	return widget.NewCard("⚡ Quick Actions", "", content)
}

// acknowledgeAlertFromDetails acknowledges an alert from the details modal
func (aw *AlertsWindow) acknowledgeAlertFromDetails(alert models.Alert) {
	// Use the existing acknowledgment system
	alertKey := alert.GetFingerprint()
	aw.acknowledgeAlertWithUIRefresh(alertKey, "Acknowledged from alert details", nil)
}

// showSilenceDialog shows the silence creation dialog
func (aw *AlertsWindow) showSilenceDialog(alert models.Alert) {
	// TODO: Implement silence dialog
	dialog.ShowInformation("Silence", "Silence creation dialog coming soon!", aw.window)
}

// hideAlert hides an alert from the view
func (aw *AlertsWindow) hideAlert(alert models.Alert) {
	reason := "Hidden from alert details"
	if err := aw.hiddenAlertsCache.HideAlert(alert, reason); err != nil {
		dialog.ShowError(fmt.Errorf("failed to hide alert: %v", err), aw.window)
		return
	}

	aw.safeApplyFilters()
	aw.updateHiddenCountDisplay()
	dialog.ShowInformation("Hidden", "Alert has been hidden from view", aw.window)
}

// shareAlertFromDetails opens sharing options for an alert from the details modal
func (aw *AlertsWindow) shareAlertFromDetails(alert models.Alert) {
	// Create sharing options
	shareText := fmt.Sprintf("🚨 Alert: %s\nSeverity: %s\nStatus: %s\nSummary: %s",
		alert.GetAlertName(),
		alert.GetSeverity(),
		alert.Status.State,
		alert.GetSummary(),
	)

	// Copy to clipboard for now
	aw.window.Clipboard().SetContent(shareText)
	dialog.ShowInformation("Shared", "Alert details copied to clipboard for sharing", aw.window)
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

	// Sort labels by key for consistent display
	keys := make([]string, 0, len(alert.Labels))
	for key := range alert.Labels {
		keys = append(keys, key)
	}

	// Sort labels efficiently using Go's built-in sort
	sort.Strings(keys)

	// Create cards for each label for better readability
	for _, key := range keys {
		value := alert.Labels[key]

		// Create a card for each label
		labelContent := container.NewVBox()

		// Key with bold styling
		keyLabel := widget.NewLabelWithStyle(key, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		labelContent.Add(keyLabel)

		// Value with monospace font for better readability
		valueLabel := widget.NewLabel(value)
		valueLabel.TextStyle = fyne.TextStyle{Monospace: true}
		valueLabel.Wrapping = fyne.TextWrapWord
		labelContent.Add(valueLabel)

		// Create card with key as title
		card := widget.NewCard("", "", labelContent)
		content.Add(card)
	}

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
	case "active":
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
	log.Printf("createSilenceTab called")
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in createSilenceTab: %v", r)
		}
	}()

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
				silenceCard := aw.createSilenceInfoCardAsync(silenceID, i+1)
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
		case "active":
			content.Add(widget.NewLabel("This alert is currently active. You can create a silence to suppress notifications."))
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

// createSilenceInfoCardAsync creates a card showing detailed silence information (async version)
func (aw *AlertsWindow) createSilenceInfoCardAsync(silenceID string, index int) *widget.Card {
	log.Printf("createSilenceInfoCardAsync called for silence ID: %s", silenceID)

	// Create placeholder card
	card := widget.NewCard(fmt.Sprintf("Silence %d", index), "", container.NewVBox())

	// Show loading state initially
	loadingContent := container.NewVBox()
	loadingContent.Add(widget.NewLabel(fmt.Sprintf("Silence ID: %s", silenceID)))
	loadingContent.Add(widget.NewLabel("🔄 Loading silence details..."))
	card.SetContent(loadingContent)

	// Load silence details asynchronously
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in createSilenceInfoCardAsync: %v", r)
			}
		}()

		// Add timeout to prevent hanging
		done := make(chan bool, 1)
		var silence *models.Silence
		var err error

		// Fetch silence details with timeout
		go func() {
			log.Printf("Fetching silence details from alertmanager...")
			silence, err = aw.client.FetchSilence(silenceID)
			done <- true
		}()

		// Wait for completion or timeout
		select {
		case <-done:
			// Continue with normal processing
		case <-time.After(5 * time.Second):
			// Timeout occurred
			fyne.Do(func() {
				errorContent := container.NewVBox(
					widget.NewLabel(fmt.Sprintf("Silence ID: %s", silenceID)),
					widget.NewLabel("⏰ Loading timed out"),
					widget.NewLabel("Check your alertmanager connection"),
				)
				card.SetContent(errorContent)
			})
			return
		}

		fyne.Do(func() {
			if err != nil {
				// If we can't fetch details, show basic info
				errorContent := container.NewVBox(
					widget.NewLabel(fmt.Sprintf("Silence ID: %s", silenceID)),
					widget.NewLabel(fmt.Sprintf("⚠️ Could not fetch details: %v", err)),
				)
				card.SetContent(errorContent)
				return
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

			card.SetContent(silenceInfo)
		})
	}()

	return card
}

// createSilenceInfoCard creates a card showing detailed silence information
func (aw *AlertsWindow) createSilenceInfoCard(silenceID string, index int) *widget.Card {
	log.Printf("createSilenceInfoCard called for silence ID: %s", silenceID)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in createSilenceInfoCard: %v", r)
		}
	}()

	// Try to fetch silence details
	log.Printf("Fetching silence details from alertmanager...")
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
			dialog.ShowError(fmt.Errorf("invalid duration format: %v\n\nExamples of valid formats:\n• 30m (30 minutes)\n• 2h30m (2 hours 30 minutes)\n• 1d (1 day)\n• 1w2d (1 week 2 days)", err), aw.window)
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
		dialog.ShowError(fmt.Errorf("duration too short: minimum is 1 minute"), aw.window)
		return
	}
	if silenceDuration > 365*24*time.Hour {
		dialog.ShowError(fmt.Errorf("duration too long: maximum is 1 year"), aw.window)
		return
	}

	// Validate creator
	if strings.TrimSpace(creator) == "" {
		dialog.ShowError(fmt.Errorf("creator field cannot be empty"), aw.window)
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

		fyne.Do(func() {
			if err != nil {
				log.Printf("Failed to create silence: %v", err)
				aw.setStatus("Failed to create silence")
				dialog.ShowError(fmt.Errorf("failed to create silence: %v", err), aw.window)
			} else {
				aw.setStatus(fmt.Sprintf("Silence created: %s", createdSilence.ID))
				dialog.ShowInformation("Success",
					fmt.Sprintf("Silence created successfully!\nID: %s\nDuration: %s",
						createdSilence.ID, duration), aw.window)

				// Refresh alerts to show updated status
				aw.loadAlertsWithCaching()
			}
		})
	}()
}

// openURLInBrowser opens a URL in the default browser using system commands
func (aw *AlertsWindow) openURLInBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}

func (aw *AlertsWindow) createCollaborationTab(alert models.Alert) fyne.CanvasObject {
	// Add error handling wrapper
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Error in createCollaborationTab: %v", r)
		}
	}()

	// Generate alert key for backend operations
	alertKey := alert.GetFingerprint()
	if alertKey == "" {
		// Fallback to a combination of alertname and instance
		alertKey = fmt.Sprintf("%s_%s", alert.GetAlertName(), alert.GetInstance())
	}

	// Check if backend is available
	if aw.backendClient == nil || !aw.backendClient.IsLoggedIn() {
		// Return a simple error message
		errorContainer := container.NewVBox()
		errorCard := widget.NewCard("⚠️ Backend Not Available", "Collaboration features require backend connection", container.NewVBox())
		errorContent := container.NewVBox()
		errorContent.Add(widget.NewLabel("The collaboration features require a backend connection."))
		errorContent.Add(widget.NewLabel("Please ensure the backend is running and you are logged in."))
		errorCard.SetContent(errorContent)
		errorContainer.Add(errorCard)
		return container.NewScroll(errorContainer)
	}

	// Create main container with modern acknowledge UX
	mainContainer := container.NewVBox()

	// Store reference to this container for real-time updates
	aw.activeCollaborationContainers[alertKey] = mainContainer

	// Add spacer for padding
	mainContainer.Add(layout.NewSpacer())

	// Create modern acknowledge interface
	acknowledgeCard := aw.createAcknowledgeCard(alert, alertKey)
	mainContainer.Add(acknowledgeCard)

	// Add some spacing
	mainContainer.Add(widget.NewLabel(""))

	// Create comments section
	commentsCard := aw.createCommentsSection(alertKey)
	mainContainer.Add(commentsCard)

	// Add spacer for bottom padding
	mainContainer.Add(layout.NewSpacer())

	return container.NewScroll(mainContainer)
}

func (aw *AlertsWindow) createAcknowledgeCard(alert models.Alert, alertKey string) fyne.CanvasObject {
	// Create a container that can be refreshed
	cardContainer := container.NewVBox()

	// Load and display the acknowledge card content
	aw.refreshAcknowledgeCard(cardContainer, alert, alertKey)

	return cardContainer
}

// refreshAcknowledgeCard refreshes the acknowledge card content
func (aw *AlertsWindow) refreshAcknowledgeCard(cardContainer *fyne.Container, alert models.Alert, alertKey string) {
	// Clear existing content
	cardContainer.RemoveAll()

	card := widget.NewCard("👋 Acknowledge Alert", "Take ownership and manage this alert", container.NewVBox())

	content := container.NewVBox()

	// Get existing acknowledgments
	acknowledgments, err := aw.getAlertAcknowledgments(alertKey)
	if err != nil {
		log.Printf("Error getting acknowledgments: %v", err)
		acknowledgments = []*alertpb.Acknowledgment{}
	}

	// Current acknowledgment status section
	statusSection := container.NewVBox()
	statusTitle := widget.NewLabelWithStyle("Current Status", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	statusSection.Add(statusTitle)

	isAcknowledged := len(acknowledgments) > 0

	var statusIndicator *widget.Label
	var actionButton *widget.Button

	if isAcknowledged {
		statusIndicator = widget.NewLabelWithStyle(fmt.Sprintf("✅ Alert Acknowledged (%d acknowledgments)", len(acknowledgments)), fyne.TextAlignLeading, fyne.TextStyle{})
		statusIndicator.Importance = widget.SuccessImportance

		actionButton = widget.NewButtonWithIcon("🔄 Add Update", theme.DocumentSaveIcon(), func() {
			aw.showUpdateAcknowledgeDialog(alert, alertKey, cardContainer)
		})
		actionButton.Importance = widget.MediumImportance
	} else {
		statusIndicator = widget.NewLabelWithStyle("⏳ Awaiting Acknowledgment", fyne.TextAlignLeading, fyne.TextStyle{})
		statusIndicator.Importance = widget.WarningImportance

		actionButton = widget.NewButtonWithIcon("✋ Acknowledge Alert", theme.ConfirmIcon(), func() {
			aw.showAcknowledgeDialog(alert, alertKey, cardContainer)
		})
		actionButton.Importance = widget.HighImportance
	}

	statusSection.Add(statusIndicator)
	statusSection.Add(widget.NewLabel("")) // Spacing

	// Display existing acknowledgments
	if len(acknowledgments) > 0 {
		acknowledgementsSection := container.NewVBox()
		acknowledgementsTitle := widget.NewLabelWithStyle("Acknowledgment History", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		acknowledgementsSection.Add(acknowledgementsTitle)

		for i, ack := range acknowledgments {
			// Create acknowledgment item
			ackContainer := container.NewVBox()

			// Header with user and timestamp
			headerContainer := container.NewHBox()
			userLabel := widget.NewLabelWithStyle(fmt.Sprintf("👤 %s", ack.Username), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			headerContainer.Add(userLabel)
			headerContainer.Add(layout.NewSpacer())

			// Format timestamp
			timestamp := "Unknown time"
			if ack.CreatedAt != nil {
				// Convert protobuf timestamp to Go time
				t := ack.CreatedAt.AsTime()
				timestamp = t.Format("Jan 2, 2006 15:04")
			}
			timeLabel := widget.NewLabelWithStyle(timestamp, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true})
			headerContainer.Add(timeLabel)

			ackContainer.Add(headerContainer)

			// Reason
			reasonLabel := widget.NewLabel(fmt.Sprintf("💬 %s", ack.Reason))
			reasonLabel.Wrapping = fyne.TextWrapWord
			ackContainer.Add(reasonLabel)

			// Add action buttons for this acknowledgment
			ackActionsContainer := container.NewHBox()
			ackActionsContainer.Add(layout.NewSpacer())

			// Remove button (only show if user can delete)
			removeBtn := widget.NewButtonWithIcon("🗑️ Remove All", theme.DeleteIcon(), func() {
				aw.showRemoveAcknowledgmentDialog(alert, alertKey, ack, cardContainer)
			})
			removeBtn.Importance = widget.DangerImportance
			ackActionsContainer.Add(removeBtn)

			ackContainer.Add(widget.NewLabel("")) // Spacing
			ackContainer.Add(ackActionsContainer)

			// Add border for each acknowledgment
			ackCard := widget.NewCard("", "", ackContainer)
			acknowledgementsSection.Add(ackCard)

			// Add spacing between acknowledgments (except for the last one)
			if i < len(acknowledgments)-1 {
				acknowledgementsSection.Add(widget.NewLabel(""))
			}
		}

		content.Add(acknowledgementsSection)
		content.Add(widget.NewSeparator())
	}

	// Action section
	actionSection := container.NewVBox()
	actionTitle := widget.NewLabelWithStyle("Take Action", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	actionSection.Add(actionTitle)

	if isAcknowledged {
		actionSection.Add(widget.NewLabel("Add updates or additional context to the acknowledgment"))
	} else {
		actionSection.Add(widget.NewLabel("Acknowledge this alert to indicate you're working on it"))
	}
	actionSection.Add(widget.NewLabel("")) // Spacing

	// Create action button container
	buttonContainer := container.NewHBox()
	buttonContainer.Add(actionButton)
	buttonContainer.Add(layout.NewSpacer())

	// Add refresh button
	refreshButton := widget.NewButtonWithIcon("🔄 Refresh", theme.ViewRefreshIcon(), func() {
		aw.refreshAcknowledgeCard(cardContainer, alert, alertKey)
	})
	refreshButton.Importance = widget.LowImportance
	buttonContainer.Add(refreshButton)

	// Add info button
	infoButton := widget.NewButtonWithIcon("ℹ️ Info", theme.InfoIcon(), func() {
		aw.showAcknowledgeInfoDialog()
	})
	infoButton.Importance = widget.LowImportance
	buttonContainer.Add(infoButton)

	actionSection.Add(buttonContainer)

	// Add sections to content
	content.Add(statusSection)
	content.Add(widget.NewSeparator())
	content.Add(actionSection)

	card.SetContent(content)
	cardContainer.Add(card)
	cardContainer.Refresh()
}

// showAcknowledgeDialog shows a modern acknowledge dialog
func (aw *AlertsWindow) showAcknowledgeDialog(alert models.Alert, alertKey string, cardContainer *fyne.Container) {
	// Create acknowledge form
	acknowledgeForm := container.NewVBox()

	// Title section
	titleLabel := widget.NewLabelWithStyle("Acknowledge Alert", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	acknowledgeForm.Add(titleLabel)
	acknowledgeForm.Add(widget.NewLabel("")) // Spacing

	// Alert info section
	alertInfo := container.NewVBox()
	alertInfo.Add(widget.NewLabelWithStyle("Alert Details:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	alertInfo.Add(widget.NewLabel(fmt.Sprintf("🚨 %s", alert.GetAlertName())))
	alertInfo.Add(widget.NewLabel(fmt.Sprintf("🖥️ %s", alert.GetInstance())))
	alertInfo.Add(widget.NewLabel(fmt.Sprintf("⚠️ %s", alert.GetSeverity())))
	acknowledgeForm.Add(alertInfo)
	acknowledgeForm.Add(widget.NewLabel("")) // Spacing

	// Reason section
	reasonLabel := widget.NewLabelWithStyle("Acknowledgment Reason:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	acknowledgeForm.Add(reasonLabel)

	reasonEntry := widget.NewMultiLineEntry()
	reasonEntry.SetPlaceHolder("Explain why you're acknowledging this alert and what actions you plan to take...")
	reasonEntry.Resize(fyne.NewSize(500, 120))
	acknowledgeForm.Add(reasonEntry)
	acknowledgeForm.Add(widget.NewLabel("")) // Spacing

	// Create and show dialog first
	acknowledgeDialog := dialog.NewCustom("Acknowledge Alert", "", acknowledgeForm, aw.window)

	// Buttons
	buttonContainer := container.NewHBox()

	cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		acknowledgeDialog.Hide()
	})
	cancelBtn.Importance = widget.LowImportance

	acknowledgeBtn := widget.NewButtonWithIcon("✋ Acknowledge", theme.ConfirmIcon(), func() {
		reason := reasonEntry.Text
		if reason == "" {
			// Show validation error
			dialog.ShowInformation("Validation Error", "Please provide a reason for acknowledging this alert.", aw.window)
			return
		}

		// Call backend to acknowledge alert
		err := aw.acknowledgeAlert(alertKey, reason)

		if err != nil {
			log.Printf("Error acknowledging alert %s: %v", alertKey, err)
			dialog.ShowError(fmt.Errorf("Failed to acknowledge alert: %v", err), aw.window)
			return
		}

		// Close the dialog, refresh the card, and show success
		acknowledgeDialog.Hide()
		aw.refreshAcknowledgeCard(cardContainer, alert, alertKey)
		dialog.ShowInformation("Success", "Alert has been acknowledged successfully!", aw.window)
	})
	acknowledgeBtn.Importance = widget.HighImportance

	buttonContainer.Add(layout.NewSpacer())
	buttonContainer.Add(cancelBtn)
	buttonContainer.Add(acknowledgeBtn)

	acknowledgeForm.Add(buttonContainer)

	acknowledgeDialog.Resize(fyne.NewSize(600, 450))
	acknowledgeDialog.Show()
}

// showUpdateAcknowledgeDialog shows dialog to update existing acknowledgment
func (aw *AlertsWindow) showUpdateAcknowledgeDialog(alert models.Alert, alertKey string, cardContainer *fyne.Container) {
	// Similar to acknowledge dialog but for updating
	updateForm := container.NewVBox()

	titleLabel := widget.NewLabelWithStyle("Update Acknowledgment", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	updateForm.Add(titleLabel)
	updateForm.Add(widget.NewLabel("")) // Spacing

	// Current status
	statusLabel := widget.NewLabelWithStyle("Current Status: ✅ Acknowledged", fyne.TextAlignLeading, fyne.TextStyle{})
	statusLabel.Importance = widget.SuccessImportance
	updateForm.Add(statusLabel)
	updateForm.Add(widget.NewLabel("")) // Spacing

	// New reason section
	reasonLabel := widget.NewLabelWithStyle("Update Reason:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	updateForm.Add(reasonLabel)

	reasonEntry := widget.NewMultiLineEntry()
	reasonEntry.SetPlaceHolder("Provide an update on your progress or changes to the acknowledgment...")
	reasonEntry.Resize(fyne.NewSize(400, 100))
	updateForm.Add(reasonEntry)
	updateForm.Add(widget.NewLabel("")) // Spacing

	// Create and show dialog first
	updateDialog := dialog.NewCustom("Update Acknowledgment", "", updateForm, aw.window)

	// Buttons
	buttonContainer := container.NewHBox()

	cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		updateDialog.Hide()
	})
	cancelBtn.Importance = widget.LowImportance

	updateBtn := widget.NewButtonWithIcon("🔄 Update", theme.DocumentSaveIcon(), func() {
		reason := reasonEntry.Text
		if reason == "" {
			dialog.ShowInformation("Validation Error", "Please provide an update reason.", aw.window)
			return
		}

		// Call backend to update acknowledgment (add new acknowledgment)
		err := aw.acknowledgeAlert(alertKey, reason)

		if err != nil {
			log.Printf("Error updating acknowledgment for alert %s: %v", alertKey, err)
			dialog.ShowError(fmt.Errorf("Failed to update acknowledgment: %v", err), aw.window)
			return
		}

		// Close the dialog, refresh the card, and show success
		updateDialog.Hide()
		aw.refreshAcknowledgeCard(cardContainer, alert, alertKey)
		dialog.ShowInformation("Success", "Acknowledgment has been updated successfully!", aw.window)
	})
	updateBtn.Importance = widget.MediumImportance

	buttonContainer.Add(layout.NewSpacer())
	buttonContainer.Add(cancelBtn)
	buttonContainer.Add(updateBtn)

	updateForm.Add(buttonContainer)

	updateDialog.Resize(fyne.NewSize(500, 350))
	updateDialog.Show()
}

// showAcknowledgeInfoDialog shows information about acknowledgments
func (aw *AlertsWindow) showAcknowledgeInfoDialog() {
	infoContent := container.NewVBox()

	titleLabel := widget.NewLabelWithStyle("About Alert Acknowledgments", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	infoContent.Add(titleLabel)
	infoContent.Add(widget.NewLabel("")) // Spacing

	infoText := container.NewVBox()
	infoText.Add(widget.NewLabel("📋 What is an acknowledgment?"))
	infoText.Add(widget.NewLabel("An acknowledgment indicates that someone is aware of and working on the alert."))
	infoText.Add(widget.NewLabel(""))
	infoText.Add(widget.NewLabel("✋ When to acknowledge:"))
	infoText.Add(widget.NewLabel("• You understand the alert and are investigating"))
	infoText.Add(widget.NewLabel("• You're taking action to resolve the issue"))
	infoText.Add(widget.NewLabel("• You want to prevent others from duplicating work"))
	infoText.Add(widget.NewLabel(""))
	infoText.Add(widget.NewLabel("🔄 Updating acknowledgments:"))
	infoText.Add(widget.NewLabel("• Provide progress updates to keep team informed"))
	infoText.Add(widget.NewLabel("• Share findings or resolution steps"))
	infoText.Add(widget.NewLabel("• Notify when the issue is resolved"))

	infoContent.Add(infoText)

	dialog.ShowCustom("Acknowledgment Info", "Close", infoContent, aw.window)
}

// checkAlertAcknowledgmentStatus checks if an alert has been acknowledged
func (aw *AlertsWindow) checkAlertAcknowledgmentStatus(alertKey string) bool {
	acknowledgments, err := aw.getAlertAcknowledgments(alertKey)
	if err != nil {
		log.Printf("Error checking acknowledgment status for alert %s: %v", alertKey, err)
		return false
	}

	return len(acknowledgments) > 0
}

// showRemoveAcknowledgmentDialog shows a confirmation dialog to remove an acknowledgment
func (aw *AlertsWindow) showRemoveAcknowledgmentDialog(alert models.Alert, alertKey string, ack *alertpb.Acknowledgment, cardContainer *fyne.Container) {
	// Create confirmation dialog content
	confirmForm := container.NewVBox()

	// Title
	titleLabel := widget.NewLabelWithStyle("Remove Acknowledgment", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	confirmForm.Add(titleLabel)
	confirmForm.Add(widget.NewLabel("")) // Spacing

	// Warning message
	warningLabel := widget.NewLabelWithStyle("⚠️ Are you sure you want to remove ALL acknowledgments for this alert?", fyne.TextAlignCenter, fyne.TextStyle{})
	warningLabel.Importance = widget.WarningImportance
	confirmForm.Add(warningLabel)
	confirmForm.Add(widget.NewLabel("")) // Spacing

	// Additional warning
	noteWarning := widget.NewLabel("Note: This will remove ALL acknowledgments for this alert, not just this specific one.")
	noteWarning.Importance = widget.WarningImportance
	confirmForm.Add(noteWarning)
	confirmForm.Add(widget.NewLabel("")) // Spacing

	// Show acknowledgment details
	detailsContainer := container.NewVBox()
	detailsContainer.Add(widget.NewLabelWithStyle("Acknowledgment Details:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	detailsContainer.Add(widget.NewLabel(fmt.Sprintf("👤 User: %s", ack.Username)))

	timestamp := "Unknown time"
	if ack.CreatedAt != nil {
		t := ack.CreatedAt.AsTime()
		timestamp = t.Format("Jan 2, 2006 15:04")
	}
	detailsContainer.Add(widget.NewLabel(fmt.Sprintf("🕐 Created: %s", timestamp)))
	detailsContainer.Add(widget.NewLabel(fmt.Sprintf("💬 Reason: %s", ack.Reason)))

	confirmForm.Add(detailsContainer)
	confirmForm.Add(widget.NewLabel("")) // Spacing

	// Warning note
	noteLabel := widget.NewLabel("This action cannot be undone.")
	noteLabel.Importance = widget.LowImportance
	confirmForm.Add(noteLabel)
	confirmForm.Add(widget.NewLabel("")) // Spacing

	// Create dialog first
	removeDialog := dialog.NewCustom("Remove Acknowledgment", "", confirmForm, aw.window)

	// Buttons
	buttonContainer := container.NewHBox()

	cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		removeDialog.Hide()
	})
	cancelBtn.Importance = widget.LowImportance

	removeBtn := widget.NewButtonWithIcon("🗑️ Remove All", theme.DeleteIcon(), func() {
		// Call backend to remove acknowledgment
		err := aw.removeAcknowledgment(alertKey, ack.Id)

		if err != nil {
			log.Printf("Error removing acknowledgment %s: %v", ack.Id, err)
			dialog.ShowError(fmt.Errorf("Failed to remove acknowledgment: %v", err), aw.window)
			return
		}

		// Close the dialog, refresh the card, and show success
		removeDialog.Hide()
		aw.refreshAcknowledgeCard(cardContainer, alert, alertKey)
		dialog.ShowInformation("Success", "All acknowledgments have been removed successfully!", aw.window)
	})
	removeBtn.Importance = widget.DangerImportance

	buttonContainer.Add(layout.NewSpacer())
	buttonContainer.Add(cancelBtn)
	buttonContainer.Add(removeBtn)

	confirmForm.Add(buttonContainer)

	removeDialog.Resize(fyne.NewSize(500, 400))
	removeDialog.Show()
}

// removeAcknowledgment calls backend to remove acknowledgments for an alert
func (aw *AlertsWindow) removeAcknowledgment(alertKey, acknowledgmentId string) error {
	// Note: The current backend API removes ALL acknowledgments for the alert
	// The acknowledgmentId parameter is kept for future individual deletion support
	return aw.withBackendOperation(func() error {
		_, err := aw.backendClient.DeleteAcknowledgment(alertKey)
		return err
	})
}

// createCollaborationDashboard creates a visual dashboard of collaboration status
func (aw *AlertsWindow) createCollaborationDashboard(alertKey string) fyne.CanvasObject {
	dashboardCard := widget.NewCard("🎯 Collaboration Status", "Real-time collaboration overview", container.NewVBox())

	contentContainer := container.NewVBox()

	// Load collaboration status
	aw.loadCollaborationDashboard(alertKey, contentContainer)

	dashboardCard.SetContent(contentContainer)
	return dashboardCard
}

// loadCollaborationDashboard loads the collaboration dashboard content
func (aw *AlertsWindow) loadCollaborationDashboard(alertKey string, containerObj *fyne.Container) {
	if aw.backendClient == nil || !aw.backendClient.IsLoggedIn() {
		containerObj.Add(widget.NewLabel("🔒 Login required for collaboration"))
		return
	}

	// Show loading state
	containerObj.Add(widget.NewLabel("🔄 Loading collaboration status..."))

	go func() {
		// Add timeout to prevent hanging
		done := make(chan bool, 1)
		var acknowledgments []*alertpb.Acknowledgment
		var comments []*alertpb.Comment
		var ackErr, commErr error

		// Load data with timeout
		go func() {
			acknowledgments, ackErr = aw.getAlertAcknowledgments(alertKey)
			comments, commErr = aw.getAlertComments(alertKey)
			done <- true
		}()

		// Wait for completion or timeout
		select {
		case <-done:
			// Continue with normal processing
		case <-time.After(10 * time.Second):
			// Timeout occurred
			fyne.Do(func() {
				containerObj.Objects = nil
				containerObj.Add(widget.NewLabel("⏰ Loading timed out"))
				containerObj.Add(widget.NewLabel("Please check your backend connection"))
			})
			return
		}

		fyne.Do(func() {
			containerObj.Objects = nil

			if ackErr != nil || commErr != nil {
				containerObj.Add(widget.NewLabel("❌ Failed to load collaboration data"))
				if ackErr != nil {
					containerObj.Add(widget.NewLabel(fmt.Sprintf("Acknowledgment error: %v", ackErr)))
				}
				if commErr != nil {
					containerObj.Add(widget.NewLabel(fmt.Sprintf("Comment error: %v", commErr)))
				}
				return
			}

			// Collaboration metrics
			metricsContainer := container.NewVBox()

			// Acknowledgment status
			ackStatus := "🟥 Unacknowledged"
			if len(acknowledgments) > 0 {
				ackStatus = "🟢 Acknowledged"
			}
			ackLabel := widget.NewLabelWithStyle(ackStatus, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			metricsContainer.Add(ackLabel)

			// Comments count
			commentsLabel := widget.NewLabel(fmt.Sprintf("💬 %d comments", len(comments)))
			metricsContainer.Add(commentsLabel)

			// Active collaborators (simulated)
			collaboratorsLabel := widget.NewLabel("👥 Active: 2 users")
			metricsContainer.Add(collaboratorsLabel)

			// Recent activity indicator
			if len(acknowledgments) > 0 || len(comments) > 0 {
				recentActivity := widget.NewLabel("⚡ Recent activity")
				recentActivity.TextStyle = fyne.TextStyle{Italic: true}
				metricsContainer.Add(recentActivity)
			}

			containerObj.Add(metricsContainer)

			// Live presence indicators (simulated)
			presenceContainer := container.NewVBox()
			presenceContainer.Add(widget.NewSeparator())
			presenceContainer.Add(widget.NewLabelWithStyle("👁️ Currently Viewing", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

			// Simulate active users
			activeUsers := []string{"👤 You", "👤 DevOps Team", "👤 SRE Lead"}
			for _, user := range activeUsers {
				userLabel := widget.NewLabel(user)
				userLabel.TextStyle = fyne.TextStyle{Italic: true}
				presenceContainer.Add(userLabel)
			}

			containerObj.Add(presenceContainer)
		})
	}()
}

// createQuickActionsPanel creates a panel with quick action buttons
func (aw *AlertsWindow) createQuickActionsPanel(alertKey string) fyne.CanvasObject {
	actionsCard := widget.NewCard("⚡ Quick Actions", "One-click common actions", container.NewVBox())

	actionsContainer := container.NewVBox()

	// Quick acknowledge button
	quickAckBtn := widget.NewButtonWithIcon("🚀 Quick Acknowledge", theme.ConfirmIcon(), func() {
		aw.quickAcknowledge(alertKey)
	})
	quickAckBtn.Importance = widget.HighImportance
	actionsContainer.Add(quickAckBtn)

	// Add quick comment button
	quickCommentBtn := widget.NewButtonWithIcon("💭 Quick Comment", theme.MailSendIcon(), func() {
		aw.showQuickCommentDialog(alertKey)
	})
	actionsContainer.Add(quickCommentBtn)

	// Emergency escalate button
	escalateBtn := widget.NewButtonWithIcon("🚨 Escalate", theme.WarningIcon(), func() {
		aw.escalateAlert(alertKey)
	})
	escalateBtn.Importance = widget.DangerImportance
	actionsContainer.Add(escalateBtn)

	// Share alert button
	shareBtn := widget.NewButtonWithIcon("📤 Share", theme.MailSendIcon(), func() {
		aw.shareAlert(alertKey)
	})
	actionsContainer.Add(shareBtn)

	actionsCard.SetContent(actionsContainer)
	return actionsCard
}

// createActivityTimeline creates an activity timeline view
func (aw *AlertsWindow) createActivityTimeline(alertKey string) fyne.CanvasObject {
	timelineCard := widget.NewCard("📈 Activity Timeline", "Chronological view of all activities", container.NewVBox())

	contentContainer := container.NewVBox()

	// Load timeline data
	aw.loadActivityTimeline(alertKey, contentContainer)

	timelineCard.SetContent(contentContainer)
	return timelineCard
}

// loadActivityTimeline loads the activity timeline content
func (aw *AlertsWindow) loadActivityTimeline(alertKey string, containerObj *fyne.Container) {
	if aw.backendClient == nil || !aw.backendClient.IsLoggedIn() {
		containerObj.Add(widget.NewLabel("🔒 Login required for timeline"))
		return
	}

	// Show loading state
	containerObj.Add(widget.NewLabel("🔄 Loading activity timeline..."))

	go func() {
		// Add timeout to prevent hanging
		done := make(chan bool, 1)
		var acknowledgments []*alertpb.Acknowledgment
		var comments []*alertpb.Comment
		var ackErr, commErr error

		// Load data with timeout
		go func() {
			acknowledgments, ackErr = aw.getAlertAcknowledgments(alertKey)
			comments, commErr = aw.getAlertComments(alertKey)
			done <- true
		}()

		// Wait for completion or timeout
		select {
		case <-done:
			// Continue with normal processing
		case <-time.After(10 * time.Second):
			// Timeout occurred
			fyne.Do(func() {
				containerObj.Objects = nil
				containerObj.Add(widget.NewLabel("⏰ Timeline loading timed out"))
				containerObj.Add(widget.NewLabel("Please check your backend connection"))
			})
			return
		}

		fyne.Do(func() {
			containerObj.Objects = nil

			if ackErr != nil || commErr != nil {
				containerObj.Add(widget.NewLabel("❌ Failed to load timeline"))
				if ackErr != nil {
					containerObj.Add(widget.NewLabel(fmt.Sprintf("Acknowledgment error: %v", ackErr)))
				}
				if commErr != nil {
					containerObj.Add(widget.NewLabel(fmt.Sprintf("Comment error: %v", commErr)))
				}
				return
			}

			// Combine and sort activities by timestamp
			activities := aw.mergeActivities(acknowledgments, comments)

			if len(activities) == 0 {
				emptyCard := widget.NewCard("", "", container.NewVBox(
					widget.NewLabel("📭 No activities yet"),
					widget.NewLabel("Activities will appear here as team members interact with this alert."),
				))
				containerObj.Add(emptyCard)
				return
			}

			// Create timeline entries
			for _, activity := range activities {
				timelineEntry := aw.createTimelineEntry(activity)
				containerObj.Add(timelineEntry)
			}
		})
	}()
}

// Activity represents a timeline activity
type Activity struct {
	Type      string // "acknowledgment" or "comment"
	User      string
	Content   string
	Timestamp time.Time
	Icon      string
}

// mergeActivities combines acknowledgments and comments into a sorted timeline
func (aw *AlertsWindow) mergeActivities(acknowledgments []*alertpb.Acknowledgment, comments []*alertpb.Comment) []Activity {
	var activities []Activity

	// Add acknowledgments
	for _, ack := range acknowledgments {
		activity := Activity{
			Type:    "acknowledgment",
			User:    ack.Username,
			Content: ack.Reason,
			Icon:    "🤝",
		}
		if ack.CreatedAt != nil {
			activity.Timestamp = ack.CreatedAt.AsTime()
		}
		activities = append(activities, activity)
	}

	// Add comments
	for _, comment := range comments {
		activity := Activity{
			Type:    "comment",
			User:    comment.Username,
			Content: comment.Content,
			Icon:    "💬",
		}
		if comment.CreatedAt != nil {
			activity.Timestamp = comment.CreatedAt.AsTime()
		}
		activities = append(activities, activity)
	}

	// Sort by timestamp (newest first)
	for i := 0; i < len(activities); i++ {
		for j := i + 1; j < len(activities); j++ {
			if activities[i].Timestamp.Before(activities[j].Timestamp) {
				activities[i], activities[j] = activities[j], activities[i]
			}
		}
	}

	return activities
}

// createTimelineEntry creates a timeline entry widget
func (aw *AlertsWindow) createTimelineEntry(activity Activity) fyne.CanvasObject {
	entryContainer := container.NewVBox()

	// Timeline connector (visual line)
	connectorContainer := container.NewHBox()
	connectorContainer.Add(widget.NewLabel("│"))

	// Activity content
	activityContainer := container.NewVBox()

	// Header with icon, user, and timestamp
	headerContainer := container.NewHBox()
	headerContainer.Add(widget.NewLabel(activity.Icon))
	headerContainer.Add(widget.NewLabelWithStyle(activity.User, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	// Relative timestamp
	if !activity.Timestamp.IsZero() {
		relativeTime := aw.getRelativeTime(activity.Timestamp)
		timeLabel := widget.NewLabel(relativeTime)
		timeLabel.TextStyle = fyne.TextStyle{Italic: true}
		headerContainer.Add(timeLabel)
	}

	activityContainer.Add(headerContainer)

	// Activity content
	contentLabel := widget.NewLabel(activity.Content)
	contentLabel.Wrapping = fyne.TextWrapWord
	activityContainer.Add(contentLabel)

	// Combine connector and content
	fullContainer := container.NewHBox()
	fullContainer.Add(connectorContainer)
	fullContainer.Add(activityContainer)

	entryContainer.Add(fullContainer)
	entryContainer.Add(widget.NewSeparator())

	return entryContainer
}

// getRelativeTime returns a human-readable relative time
func (aw *AlertsWindow) getRelativeTime(timestamp time.Time) string {
	duration := time.Since(timestamp)

	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		return fmt.Sprintf("%d min ago", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(duration.Hours()))
	} else {
		return fmt.Sprintf("%d days ago", int(duration.Hours()/24))
	}
}

func (aw *AlertsWindow) createCommentsSection(alertKey string) fyne.CanvasObject {
	commentsCard := widget.NewCard("💬 Comments", "Collaborate with your team", container.NewVBox())

	contentContainer := container.NewVBox()

	// Load comments
	aw.loadAdvancedComments(alertKey, contentContainer)

	commentsCard.SetContent(contentContainer)
	return commentsCard
}

// loadAdvancedComments loads comments with advanced features
func (aw *AlertsWindow) loadAdvancedComments(alertKey string, containerObj *fyne.Container) {
	if aw.backendClient == nil || !aw.backendClient.IsLoggedIn() {
		aw.showNotLoggedInMessage(containerObj, "comments")
		return
	}

	// Show loading state
	containerObj.Add(widget.NewLabel("🔄 Loading comments..."))

	go func() {
		// Add timeout to prevent hanging
		done := make(chan bool, 1)
		var comments []*alertpb.Comment
		var err error

		// Load data with timeout
		go func() {
			comments, err = aw.getAlertComments(alertKey)
			done <- true
		}()

		// Wait for completion or timeout
		select {
		case <-done:
			// Continue with normal processing
		case <-time.After(10 * time.Second):
			// Timeout occurred
			fyne.Do(func() {
				containerObj.Objects = nil
				containerObj.Add(widget.NewLabel("⏰ Comments loading timed out"))
				containerObj.Add(widget.NewLabel("Please check your backend connection"))
			})
			return
		}

		fyne.Do(func() {
			containerObj.Objects = nil

			if err != nil {
				containerObj.Add(widget.NewLabel("❌ Failed to load comments"))
				containerObj.Add(widget.NewLabel(fmt.Sprintf("Error: %v", err)))
				return
			}

			// Advanced comment input form
			commentForm := aw.createCommentForm(alertKey, containerObj)
			containerObj.Add(commentForm)

			containerObj.Add(widget.NewSeparator())

			// Show comments
			if len(comments) == 0 {
				emptyCard := widget.NewCard("", "", container.NewVBox(
					widget.NewLabel("💭 No comments yet"),
					widget.NewLabel("Start a conversation with your team!"),
				))
				containerObj.Add(emptyCard)
			} else {
				// Comments count
				countLabel := widget.NewLabelWithStyle(
					fmt.Sprintf("💬 %d comments", len(comments)),
					fyne.TextAlignLeading,
					fyne.TextStyle{Bold: true},
				)
				containerObj.Add(countLabel)

				// Display each comment
				for _, comment := range comments {
					commentCard := aw.createCommentCard(comment, alertKey, containerObj)
					containerObj.Add(commentCard)
				}
			}
		})
	}()
}

func (aw *AlertsWindow) createCommentForm(alertKey string, parentContainer *fyne.Container) fyne.CanvasObject {
	formCard := widget.NewCard("✍️ Add Comment", "Share insights with your team", container.NewVBox())

	// Comment templates for quick responses
	templatesContainer := container.NewHBox()

	templates := []struct {
		Label string
		Text  string
	}{
		{"🔍 Investigating", "I'm currently investigating this issue..."},
		{"✅ Resolved", "This issue has been resolved. Root cause was..."},
		{"🚧 Working on it", "I'm working on this. ETA: ..."},
		{"❓ Need info", "I need more information about..."},
	}

	var commentEntry *MentionEntry
	if aw.backendClient != nil && aw.backendClient.IsLoggedIn() {
		commentEntry = NewMentionEntry(aw.backendClient, aw.window)
	} else {
		// Fallback to regular entry if not logged in
		regularEntry := widget.NewMultiLineEntry()
		regularEntry.SetPlaceHolder("What's happening with this alert? Share updates, ask questions, or provide insights...")
		regularEntry.Wrapping = fyne.TextWrapWord
		regularEntry.Resize(fyne.NewSize(500, 120))
		// Create a wrapper to match the interface
		commentEntry = &MentionEntry{Entry: *regularEntry}
	}
	commentEntry.SetPlaceHolder("What's happening with this alert? Share updates, ask questions, or provide insights... (Try @username to mention someone)")
	commentEntry.Resize(fyne.NewSize(500, 120))

	for _, template := range templates {
		template := template // Capture for closure
		templateBtn := widget.NewButton(template.Label, func() {
			commentEntry.SetText(template.Text)
		})
		templateBtn.Importance = widget.LowImportance
		templatesContainer.Add(templateBtn)
	}

	// Character counter with smart suggestions
	charCountLabel := widget.NewLabel("0 / 1000")
	charCountLabel.TextStyle = fyne.TextStyle{Italic: true}

	// Smart suggestions (simulated)
	suggestionsLabel := widget.NewLabel("💡 Suggestions: Add @mention, #tag, or /command")
	suggestionsLabel.TextStyle = fyne.TextStyle{Italic: true}

	// Enhanced button container
	buttonContainer := container.NewHBox()

	// Submit button with enhanced styling
	submitButton := widget.NewButtonWithIcon("🚀 Post Comment", theme.MailSendIcon(), func() {
		content := strings.TrimSpace(commentEntry.Text)
		if content == "" {
			dialog.ShowError(fmt.Errorf("comment cannot be empty"), aw.window)
			return
		}
		if len(content) > 1000 {
			dialog.ShowError(fmt.Errorf("comment too long (max 1000 characters)"), aw.window)
			return
		}
		aw.addAdvancedComment(alertKey, content, &commentEntry.Entry, parentContainer)
	})
	submitButton.Importance = widget.HighImportance
	submitButton.Disable()

	// Draft save button
	draftBtn := widget.NewButtonWithIcon("💾 Save Draft", theme.DocumentSaveIcon(), func() {
		// Save draft functionality
		dialog.ShowInformation("Draft Saved", "Your comment has been saved as a draft", aw.window)
	})

	buttonContainer.Add(submitButton)
	buttonContainer.Add(draftBtn)

	// Update character count and suggestions
	updateUI := func(text string) {
		charCount := len(text)
		charCountLabel.SetText(fmt.Sprintf("%d / 1000", charCount))

		// Color coding for character count
		if charCount > 1000 {
			charCountLabel.Importance = widget.DangerImportance
		} else if charCount > 900 {
			charCountLabel.Importance = widget.WarningImportance
		} else {
			charCountLabel.Importance = widget.LowImportance
		}

		// Enable/disable submit button
		if strings.TrimSpace(text) == "" || charCount > 1000 {
			submitButton.Disable()
		} else {
			submitButton.Enable()
		}

		// Update suggestions based on content
		if strings.Contains(text, "@") {
			suggestionsLabel.SetText("💡 @mention detected - team members will be notified")
		} else if strings.Contains(text, "#") {
			suggestionsLabel.SetText("💡 #tag detected - helps with categorization")
		} else if len(text) > 0 {
			suggestionsLabel.SetText("💡 Tip: Use @mention to notify specific team members")
		} else {
			suggestionsLabel.SetText("💡 Suggestions: Add @mention, #tag, or /command")
		}
	}

	// Set up the callbacks
	if aw.backendClient != nil && aw.backendClient.IsLoggedIn() {
		// For MentionEntry, chain the original OnChanged with our UI updates
		originalOnChanged := commentEntry.handleTextChange
		commentEntry.Entry.OnChanged = func(text string) {
			originalOnChanged(text)
			updateUI(text)
		}

		// Set up mention callback
		commentEntry.SetOnMentionSelected(func(user *authpb.User) {
			// Could show a notification or update UI
			suggestionsLabel.SetText(fmt.Sprintf("💡 Mentioned @%s", user.Username))
		})
	} else {
		// For regular entry, just use the UI update
		commentEntry.Entry.OnChanged = updateUI
	}

	formContent := container.NewVBox(
		widget.NewLabel("Quick Templates:"),
		templatesContainer,
		widget.NewSeparator(),
		commentEntry,
		container.NewHBox(
			suggestionsLabel,
			widget.NewLabel(""), // Spacer
			charCountLabel,
		),
		buttonContainer,
	)

	formCard.SetContent(formContent)
	return formCard
}

func (aw *AlertsWindow) createCommentCard(comment *alertpb.Comment, alertKey string, parentContainer *fyne.Container) *widget.Card {
	// Enhanced timestamp with more context
	timeStr := "Unknown time"
	if comment.CreatedAt != nil {
		createdTime := comment.CreatedAt.AsTime()
		timeStr = createdTime.Format("Jan 2, 15:04")

		// Add relative time with more granularity
		duration := time.Since(createdTime)
		if duration < time.Minute {
			timeStr += " (just now)"
		} else if duration < time.Hour {
			timeStr += fmt.Sprintf(" (%d min ago)", int(duration.Minutes()))
		} else if duration < 24*time.Hour {
			timeStr += fmt.Sprintf(" (%d hours ago)", int(duration.Hours()))
		} else {
			timeStr += fmt.Sprintf(" (%d days ago)", int(duration.Hours()/24))
		}
	}

	// Enhanced header with user avatar placeholder and status
	headerContainer := container.NewHBox()

	// User avatar (placeholder)
	avatarLabel := widget.NewLabel("👤")
	headerContainer.Add(avatarLabel)

	// User name with role/status
	userLabel := widget.NewLabelWithStyle(comment.Username, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	headerContainer.Add(userLabel)

	// User status (simulated)
	statusLabel := widget.NewLabel("🟢 Online")
	statusLabel.TextStyle = fyne.TextStyle{Italic: true}
	headerContainer.Add(statusLabel)

	headerContainer.Add(widget.NewLabel("•"))
	headerContainer.Add(widget.NewLabel(timeStr))

	// Enhanced comment content with better formatting
	contentContainer := container.NewVBox()

	// Process comment for mentions and tags
	processedContent := aw.processCommentContent(comment.Content)
	commentText := widget.NewRichTextFromMarkdown(processedContent)
	commentText.Wrapping = fyne.TextWrapWord
	contentContainer.Add(commentText)

	// Enhanced actions section
	currentUser := aw.getCurrentUser()
	if currentUser != nil && currentUser.ID == comment.UserId {
		actionsContainer := container.NewHBox()

		// Edit button
		editBtn := widget.NewButtonWithIcon("✏️ Edit", theme.DocumentIcon(), func() {
			// Edit functionality not implemented yet
		})
		editBtn.Importance = widget.MediumImportance
		actionsContainer.Add(editBtn)

		// Delete button
		deleteBtn := widget.NewButtonWithIcon("🗑️ Delete", theme.DeleteIcon(), func() {
			// Delete functionality not implemented yet
		})
		deleteBtn.Importance = widget.DangerImportance
		actionsContainer.Add(deleteBtn)

		contentContainer.Add(widget.NewSeparator())
		contentContainer.Add(actionsContainer)
	} else {
		// Actions for other users' comments
		otherActionsContainer := container.NewHBox()

		// Reply button
		replyBtn := widget.NewButtonWithIcon("💬 Reply", theme.MailReplyIcon(), func() {
			aw.replyToComment(comment, alertKey, parentContainer)
		})
		replyBtn.Importance = widget.MediumImportance
		otherActionsContainer.Add(replyBtn)

		// Quote button
		quoteBtn := widget.NewButtonWithIcon("📝 Quote", theme.DocumentIcon(), func() {
			aw.quoteComment(comment, alertKey, parentContainer)
		})
		otherActionsContainer.Add(quoteBtn)

		if len(otherActionsContainer.Objects) > 0 {
			contentContainer.Add(widget.NewSeparator())
			contentContainer.Add(otherActionsContainer)
		}
	}

	mainContent := container.NewVBox(
		headerContainer,
		contentContainer,
	)

	return widget.NewCard("", "", mainContent)
}

// processCommentContent processes comment content for mentions and tags
func (aw *AlertsWindow) processCommentContent(content string) string {
	// Process @mentions with better highlighting
	// Find @username patterns and highlight them
	words := strings.Fields(content)
	for i, word := range words {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			// Extract username (remove punctuation at the end)
			username := word[1:]
			// Remove trailing punctuation
			for len(username) > 0 && !isAlphanumeric(username[len(username)-1]) {
				username = username[:len(username)-1]
			}

			if len(username) > 0 {
				// Highlight the mention
				highlighted := fmt.Sprintf("**@%s**", username)
				words[i] = strings.Replace(word, "@"+username, highlighted, 1)
			}
		}

		// Process #tags similarly
		if strings.HasPrefix(word, "#") && len(word) > 1 {
			tag := word[1:]
			// Remove trailing punctuation
			for len(tag) > 0 && !isAlphanumeric(tag[len(tag)-1]) {
				tag = tag[:len(tag)-1]
			}

			if len(tag) > 0 {
				// Highlight the tag
				highlighted := fmt.Sprintf("**#%s**", tag)
				words[i] = strings.Replace(word, "#"+tag, highlighted, 1)
			}
		}
	}

	return strings.Join(words, " ")
}

// Helper function to check if a character is alphanumeric or underscore
func isAlphanumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// startAlertSubscription starts a real-time subscription for alert updates
func (aw *AlertsWindow) startAlertSubscription(alertKey string, alertDialog *dialog.CustomDialog) {
	log.Printf("Starting real-time subscription for alert: %s", alertKey)

	err := aw.backendClient.SubscribeToAlertUpdates(alertKey, func(update *alertpb.AlertUpdate) {
		log.Printf("Received real-time update: %v for alert %s", update.UpdateType, alertKey)

		switch update.UpdateType {
		case alertpb.UpdateType_COMMENT_ADDED:
			if comment := update.GetComment(); comment != nil {
				aw.handleNewComment(comment, alertKey)
			}

		case alertpb.UpdateType_COMMENT_DELETED:
			if commentId := update.GetDeletedCommentId(); commentId != "" {
				aw.handleDeletedComment(commentId, alertKey)
			}

		case alertpb.UpdateType_ACKNOWLEDGMENT_ADDED:
			if ack := update.GetAcknowledgment(); ack != nil {
				aw.handleNewAcknowledgment(ack, alertKey)
			}

		case alertpb.UpdateType_ACKNOWLEDGMENT_DELETED:
			if ackId := update.GetDeletedAcknowledgmentId(); ackId != "" {
				aw.handleDeletedAcknowledgment(ackId, alertKey)
			}
		}
	})

	if err != nil {
		log.Printf("Failed to start alert subscription: %v", err)
		// Show a non-intrusive notification
		aw.setStatus("Real-time updates unavailable")
	} else {
		log.Printf("Real-time subscription started for alert: %s", alertKey)
		aw.setStatus("🔴 Live - Real-time updates active")

		// Show a welcome notification to let user know real-time updates are working
		go func() {
			time.Sleep(2 * time.Second)
			fyne.Do(func() {
				aw.setStatus("💬 You'll see new comments and acknowledgments instantly!")
			})
		}()
	}
}

// handleNewComment handles real-time comment additions
func (aw *AlertsWindow) handleNewComment(comment *alertpb.Comment, alertKey string) {
	log.Printf("New comment added: %s by %s", comment.Content, comment.Username)

	// Show a brief notification
	aw.setStatus(fmt.Sprintf("💬 New comment from %s", comment.Username))

	// Update comment count cache
	aw.cacheMutex.Lock()
	if count, exists := aw.commentCountCache[alertKey]; exists {
		aw.commentCountCache[alertKey] = count + 1
	}
	aw.cacheMutex.Unlock()

	// Refresh the collaboration tab if it's currently loaded
	aw.refreshActiveCollaborationContent(alertKey)
}

// handleDeletedComment handles real-time comment deletions
func (aw *AlertsWindow) handleDeletedComment(commentId string, alertKey string) {
	log.Printf("Comment deleted: %s", commentId)
	aw.setStatus("🗑️ Comment deleted")
}

// handleNewAcknowledgment handles real-time acknowledgment additions
func (aw *AlertsWindow) handleNewAcknowledgment(ack *alertpb.Acknowledgment, alertKey string) {
	log.Printf("New acknowledgment: %s by %s", ack.Reason, ack.Username)
	aw.setStatus(fmt.Sprintf("✅ %s acknowledged the alert", ack.Username))

	// Update acknowledgment count cache
	aw.cacheMutex.Lock()
	if count, exists := aw.ackCountCache[alertKey]; exists {
		aw.ackCountCache[alertKey] = count + 1
	}
	aw.cacheMutex.Unlock()

	// Refresh the collaboration tab if it's currently loaded
	aw.refreshActiveCollaborationContent(alertKey)
}

// handleDeletedAcknowledgment handles real-time acknowledgment deletions
func (aw *AlertsWindow) handleDeletedAcknowledgment(ackId string, alertKey string) {
	log.Printf("Acknowledgment deleted: %s", ackId)
	aw.setStatus("❌ Acknowledgment removed")
}

// refreshActiveCollaborationContent refreshes collaboration content for real-time updates
func (aw *AlertsWindow) refreshActiveCollaborationContent(alertKey string) {
	// Check if we have an active collaboration container for this alert
	if container, exists := aw.activeCollaborationContainers[alertKey]; exists && container != nil {
		log.Printf("Refreshing collaboration content for alert %s", alertKey)

		// Refresh the container content in the UI thread
		fyne.Do(func() {
			// Find the comments card by iterating through container objects
			for _, obj := range container.Objects {
				if card, ok := obj.(*widget.Card); ok {
					// Check if this is the comments card by its title
					if strings.Contains(card.Title, "💬") || strings.Contains(card.Title, "Comments") {
						aw.refreshCommentsCard(card, alertKey)
						break
					}
				}
			}
		})
	}
}

// refreshCommentsCard refreshes the comments card content with latest data
func (aw *AlertsWindow) refreshCommentsCard(commentsCard *widget.Card, alertKey string) {
	log.Printf("Refreshing comments card for alert %s", alertKey)

	// Create new content container
	contentContainer := container.NewVBox()

	// Reload comments from backend
	aw.loadAdvancedComments(alertKey, contentContainer)

	// Update the card content
	commentsCard.SetContent(contentContainer)
	commentsCard.Refresh()
}

// cleanupActiveCollaborationContainer removes the container reference when dialog is closed
func (aw *AlertsWindow) cleanupActiveCollaborationContainer(alertKey string) {
	if aw.activeCollaborationContainers != nil {
		delete(aw.activeCollaborationContainers, alertKey)
		log.Printf("Cleaned up collaboration container for alert %s", alertKey)
	}
}

// Quick action implementations
func (aw *AlertsWindow) quickAcknowledge(alertKey string) {
	aw.acknowledgeAlertWithUIRefresh(alertKey, "Quick acknowledge - investigating", nil)
}

func (aw *AlertsWindow) showQuickCommentDialog(alertKey string) {
	entry := widget.NewMultiLineEntry()
	entry.SetPlaceHolder("Quick comment...")
	entry.Resize(fyne.NewSize(400, 100))

	dialog.ShowCustomConfirm("Quick Comment", "Post", "Cancel", entry, func(confirmed bool) {
		if confirmed && strings.TrimSpace(entry.Text) != "" {
			aw.addAdvancedComment(alertKey, entry.Text, entry, nil)
		}
	}, aw.window)
}

func (aw *AlertsWindow) escalateAlert(alertKey string) {
	dialog.ShowInformation("Escalate Alert", "Escalation feature coming soon!", aw.window)
}

func (aw *AlertsWindow) shareAlert(alertKey string) {
	dialog.ShowInformation("Share Alert", "Share feature coming soon!", aw.window)
}

func (aw *AlertsWindow) addAdvancedComment(alertKey, content string, entry *widget.Entry, parentContainer *fyne.Container) {
	err := aw.addAlertComment(alertKey, content)
	if err != nil {
		log.Printf("Failed to add comment: %v", err)
		return
	}
	entry.SetText("")
}

func (aw *AlertsWindow) replyToComment(comment *alertpb.Comment, alertKey string, parentContainer *fyne.Container) {
	dialog.ShowInformation("Reply", "Reply feature coming soon!", aw.window)
}

func (aw *AlertsWindow) quoteComment(comment *alertpb.Comment, alertKey string, parentContainer *fyne.Container) {
	dialog.ShowInformation("Quote", "Quote feature coming soon!", aw.window)
}

func (aw *AlertsWindow) createAcknowledgmentSection(alertKey string) fyne.CanvasObject {
	// Main card container
	mainCard := widget.NewCard("🤝 Acknowledgments", "Collaborate on alert resolution", container.NewVBox())

	// Create dynamic content container
	contentContainer := container.NewVBox()

	// Load acknowledgment status and content
	aw.loadAcknowledgmentStatus(alertKey, contentContainer)

	mainCard.SetContent(contentContainer)
	return mainCard
}

// loadAcknowledgmentStatus loads the acknowledgment status and creates appropriate UI
func (aw *AlertsWindow) loadAcknowledgmentStatus(alertKey string, containerObj *fyne.Container) {
	if aw.backendClient == nil || !aw.backendClient.IsLoggedIn() {
		aw.showNotLoggedInMessage(containerObj, "acknowledgments")
		return
	}

	// Show loading state
	loadingLabel := widget.NewLabel("Loading acknowledgment status...")
	containerObj.Add(loadingLabel)

	go func() {
		acknowledgments, err := aw.getAlertAcknowledgments(alertKey)

		fyne.Do(func() {
			// Clear loading state
			containerObj.Objects = nil

			if err != nil {
				log.Printf("Failed to load acknowledgments: %v", err)
				containerObj.Add(widget.NewLabel("❌ Failed to load acknowledgment status"))
				return
			}

			// Check if alert is already acknowledged
			isAcknowledged := len(acknowledgments) > 0
			currentUser := aw.getCurrentUser()

			if isAcknowledged {
				// Show acknowledged state
				aw.showAcknowledgedState(alertKey, acknowledgments, currentUser, containerObj)
			} else {
				// Show acknowledgment form
				aw.showAcknowledgmentForm(alertKey, containerObj)
			}
		})
	}()
}

// showAcknowledgedState shows the UI when alert is already acknowledged
func (aw *AlertsWindow) showAcknowledgedState(alertKey string, acknowledgments []*alertpb.Acknowledgment, currentUser *User, containerObj *fyne.Container) {
	// Status indicator
	statusCard := widget.NewCard("", "", container.NewVBox(
		widget.NewLabelWithStyle("✅ Alert Acknowledged", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("This alert has been acknowledged and is being worked on"),
	))
	statusCard.SetContent(container.NewBorder(nil, nil,
		widget.NewIcon(theme.ConfirmIcon()), nil,
		statusCard.Content,
	))
	containerObj.Add(statusCard)

	// Show acknowledgments
	for _, ack := range acknowledgments {
		ackCard := aw.createAcknowledgmentCard(ack, alertKey, currentUser, containerObj)
		containerObj.Add(ackCard)
	}

	// Add acknowledgment button for additional users
	if currentUser != nil {
		hasCurrentUserAck := false
		for _, ack := range acknowledgments {
			if ack.UserId == currentUser.ID {
				hasCurrentUserAck = true
				break
			}
		}

		if !hasCurrentUserAck {
			containerObj.Add(widget.NewSeparator())
			addAckBtn := widget.NewButtonWithIcon("Add My Acknowledgment", theme.ContentAddIcon(), func() {
				aw.showAcknowledgmentForm(alertKey, containerObj)
			})
			addAckBtn.Importance = widget.MediumImportance
			containerObj.Add(addAckBtn)
		}
	}
}

// showAcknowledgmentForm shows the acknowledgment form
func (aw *AlertsWindow) showAcknowledgmentForm(alertKey string, containerObj *fyne.Container) {
	// Clear existing content
	containerObj.Objects = nil

	// Create modern form
	formCard := widget.NewCard("Acknowledge Alert", "Indicate that you're working on this alert", container.NewVBox())

	// Reason selection with icons
	reasonOptions := []struct {
		Text        string
		Icon        string
		Description string
	}{
		{"🔍 Investigating", "🔍", "Currently investigating the issue"},
		{"🔧 Known Issue", "🔧", "This is a known issue being worked on"},
		{"⚠️ Planned Maintenance", "⚠️", "Alert is due to planned maintenance"},
		{"❌ False Positive", "❌", "Alert appears to be a false alarm"},
		{"📝 Other", "📝", "Custom reason"},
	}

	// Create reason buttons instead of dropdown for better UX
	reasonContainer := container.NewVBox()
	reasonContainer.Add(widget.NewLabelWithStyle("Select Reason:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	var selectedReason string
	var selectedButton *widget.Button
	var customReasonEntry *widget.Entry

	// Create custom reason entry (initially hidden)
	customReasonEntry = widget.NewEntry()
	customReasonEntry.SetPlaceHolder("Enter custom reason...")
	customReasonEntry.Hide()

	// Acknowledge button (declare here so it's available in closures)
	ackButton := widget.NewButtonWithIcon("✅ Acknowledge Alert", theme.ConfirmIcon(), func() {
		finalReason := selectedReason
		if strings.Contains(selectedReason, "Other") && customReasonEntry.Text != "" {
			finalReason = customReasonEntry.Text
		}

		if finalReason == "" {
			dialog.ShowError(fmt.Errorf("please select a reason"), aw.window)
			return
		}

		// Clean up the reason text (remove emoji prefix)
		if strings.Contains(finalReason, " ") {
			parts := strings.SplitN(finalReason, " ", 2)
			if len(parts) == 2 {
				finalReason = parts[1]
			}
		}

		aw.acknowledgeAlertWithUIRefresh(alertKey, finalReason, containerObj)
	})
	ackButton.Importance = widget.HighImportance
	ackButton.Disable() // Initially disabled

	// Create reason buttons with proper callbacks
	var reasonButtons []*widget.Button
	for i, option := range reasonOptions {
		reason := option.Text
		btn := widget.NewButton(reason, nil) // Create without callback first
		btn.Alignment = widget.ButtonAlignLeading
		reasonContainer.Add(btn)
		reasonButtons = append(reasonButtons, btn)

		// Set up the callback with closure
		func(btnRef *widget.Button, reasonRef string, index int) {
			btnRef.OnTapped = func() {
				// Update selection
				if selectedButton != nil {
					selectedButton.Importance = widget.LowImportance
				}
				selectedReason = reasonRef
				selectedButton = btnRef
				btnRef.Importance = widget.HighImportance
				ackButton.Enable()

				// Show/hide custom reason entry
				if strings.Contains(reasonRef, "Other") {
					customReasonEntry.Show()
				} else {
					customReasonEntry.Hide()
				}
			}
		}(btn, reason, i)
	}

	reasonContainer.Add(customReasonEntry)

	formContent := container.NewVBox(
		reasonContainer,
		widget.NewSeparator(),
		ackButton,
	)

	formCard.SetContent(formContent)
	containerObj.Add(formCard)
}

// Helper function to find index of option
func indexOf(options []struct {
	Text        string
	Icon        string
	Description string
}, target struct {
	Text        string
	Icon        string
	Description string
}) int {
	for i, option := range options {
		if option.Text == target.Text {
			return i
		}
	}
	return -1
}

func (aw *AlertsWindow) createAcknowledgmentCard(ack *alertpb.Acknowledgment, alertKey string, currentUser *User, parentContainer *fyne.Container) *widget.Card {
	// Get timestamp
	timeStr := "Unknown time"
	if ack.CreatedAt != nil {
		createdTime := ack.CreatedAt.AsTime()
		timeStr = createdTime.Format("Jan 2, 15:04")
		// Add relative time
		duration := time.Since(createdTime)
		if duration < 24*time.Hour {
			timeStr += fmt.Sprintf(" (%s ago)", formatDuration(duration))
		}
	}

	// Create header with user info
	headerContainer := container.NewHBox(
		widget.NewIcon(theme.AccountIcon()),
		widget.NewLabelWithStyle(ack.Username, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("•"),
		widget.NewLabel(timeStr),
	)

	// Reason with icon
	reasonContainer := container.NewHBox()
	reasonIcon := aw.getReasonIcon(ack.Reason)
	reasonContainer.Add(widget.NewLabel(reasonIcon))
	reasonContainer.Add(widget.NewLabel(ack.Reason))

	content := container.NewVBox(
		headerContainer,
		reasonContainer,
	)

	// Add actions if it's current user's acknowledgment
	if currentUser != nil && currentUser.ID == ack.UserId {
		actionsContainer := container.NewHBox()

		removeBtn := widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() {
			aw.deleteAcknowledgmentWithUIRefresh(alertKey, parentContainer)
		})
		removeBtn.Importance = widget.DangerImportance
		actionsContainer.Add(removeBtn)

		content.Add(widget.NewSeparator())
		content.Add(actionsContainer)
	}

	return widget.NewCard("", "", content)
}

// acknowledgeAlertWithUIRefresh acknowledges an alert and refreshes the UI
func (aw *AlertsWindow) acknowledgeAlertWithUIRefresh(alertKey, reason string, containerObj *fyne.Container) {
	if aw.backendClient == nil || !aw.backendClient.IsLoggedIn() {
		dialog.ShowError(fmt.Errorf("not logged in to backend"), aw.window)
		return
	}

	aw.setStatus("Acknowledging alert...")

	go func() {
		resp, err := aw.backendClient.AddAcknowledgment(alertKey, reason)

		fyne.Do(func() {
			if err != nil {
				log.Printf("Failed to acknowledge alert: %v", err)
				aw.setStatus("Failed to acknowledge alert")
				dialog.ShowError(fmt.Errorf("failed to acknowledge alert: %v", err), aw.window)
				return
			}

			if !resp.Success {
				aw.setStatus("Failed to acknowledge alert")
				dialog.ShowError(fmt.Errorf("acknowledgment failed: %s", resp.Message), aw.window)
				return
			}

			aw.setStatus("Alert acknowledged successfully")

			// Show success message
			dialog.ShowInformation("✅ Success",
				fmt.Sprintf("Alert acknowledged successfully!\nReason: %s", reason),
				aw.window)

			// Refresh the acknowledgment section
			aw.loadAcknowledgmentStatus(alertKey, containerObj)
		})
	}()
}

// deleteAcknowledgmentWithUIRefresh removes an acknowledgment and refreshes UI
func (aw *AlertsWindow) deleteAcknowledgmentWithUIRefresh(alertKey string, containerObj *fyne.Container) {
	if aw.backendClient == nil || !aw.backendClient.IsLoggedIn() {
		dialog.ShowError(fmt.Errorf("not logged in to backend"), aw.window)
		return
	}

	// Confirm deletion
	dialog.ShowConfirm("Remove Acknowledgment",
		"Are you sure you want to remove your acknowledgment for this alert?",
		func(confirmed bool) {
			if !confirmed {
				return
			}

			aw.setStatus("Removing acknowledgment...")

			go func() {
				resp, err := aw.backendClient.DeleteAcknowledgment(alertKey)

				fyne.Do(func() {
					if err != nil {
						log.Printf("Failed to delete acknowledgment: %v", err)
						aw.setStatus("Failed to remove acknowledgment")
						dialog.ShowError(fmt.Errorf("failed to remove acknowledgment: %v", err), aw.window)
						return
					}

					if !resp.Success {
						aw.setStatus("Failed to remove acknowledgment")
						dialog.ShowError(fmt.Errorf("failed to remove acknowledgment: %s", resp.Message), aw.window)
						return
					}

					aw.setStatus("Acknowledgment removed successfully")

					// Refresh the acknowledgment section
					aw.loadAcknowledgmentStatus(alertKey, containerObj)
				})
			}()
		}, aw.window)
}

// getReasonIcon returns an appropriate icon for the acknowledgment reason
func (aw *AlertsWindow) getReasonIcon(reason string) string {
	reasonLower := strings.ToLower(reason)
	switch {
	case strings.Contains(reasonLower, "investigating"):
		return "🔍"
	case strings.Contains(reasonLower, "known"):
		return "🔧"
	case strings.Contains(reasonLower, "maintenance"):
		return "⚠️"
	case strings.Contains(reasonLower, "false"):
		return "❌"
	default:
		return "📝"
	}
}

// showNotLoggedInMessage shows a message when user is not logged in
func (aw *AlertsWindow) showNotLoggedInMessage(containerObj *fyne.Container, feature string) {
	loginCard := widget.NewCard("🔐 Login Required",
		fmt.Sprintf("Please log in to backend to use %s", feature),
		container.NewVBox(
			widget.NewLabel("Backend authentication is required for collaboration features."),
			widget.NewLabel(""),
			widget.NewButtonWithIcon("Login", theme.LoginIcon(), func() {
				aw.showAuthDialog()
			}),
		),
	)
	containerObj.Add(loginCard)
}
