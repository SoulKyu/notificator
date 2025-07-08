package gui

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"notificator/internal/alertmanager"
	"notificator/internal/models"
	"notificator/internal/notifier"
)

// ColumnConfig represents configuration for table columns
type ColumnConfig struct {
	Name     string
	Width    float32
	MinWidth float32
	MaxWidth float32
}

// AlertsWindow represents the main GUI window
type AlertsWindow struct {
	app            fyne.App
	window         fyne.Window
	client         *alertmanager.Client
	alerts         []models.Alert
	previousAlerts []models.Alert // Track previous alerts for notification comparison
	table          *widget.Table
	data           binding.UntypedList
	filteredData   []models.Alert

	// UI components
	refreshBtn        *widget.Button
	searchEntry       *widget.Entry
	autocompleteEntry *AutocompleteEntry // Store reference to the autocomplete entry
	severitySelect    *widget.Select
	statusSelect      *widget.Select
	teamSelect        *widget.Select
	statusLabel       *widget.Label
	lastUpdate        *widget.Label

	// Auto-refresh
	autoRefresh   bool
	refreshTicker *time.Ticker

	// Channel for thread-safe updates
	updateChan chan func()

	// Column configuration
	columns      []ColumnConfig
	columnDialog *dialog.CustomDialog

	// Notification system
	notifier           *notifier.Notifier
	notificationConfig notifier.NotificationConfig

	// Configuration saving
	configPath string
	fullConfig interface{} // Store reference to full config for saving

	// Visual enhancements
	dashboardCards *fyne.Container
	themeVariant   string // "light" or "dark"
	themeBtn       *widget.Button

	// Sorting
	sortColumn    int
	sortAscending bool

	// Search suggestions
	searchSuggestions []string

	// Status bar metrics
	statusBarMetrics *StatusBarMetrics
}

// StatusBarMetrics holds references to status bar metric labels
type StatusBarMetrics struct {
	criticalLabel *widget.Label
	warningLabel  *widget.Label
	infoLabel     *widget.Label
	activeLabel   *widget.Label
	totalLabel    *widget.Label
}

// getDefaultColumns returns the default column configuration
func getDefaultColumns() []ColumnConfig {
	return []ColumnConfig{
		{Name: "Alert", Width: 200, MinWidth: 100, MaxWidth: 400},
		{Name: "Severity", Width: 120, MinWidth: 100, MaxWidth: 150},
		{Name: "Status", Width: 120, MinWidth: 100, MaxWidth: 150},
		{Name: "Team", Width: 120, MinWidth: 80, MaxWidth: 200},
		{Name: "Summary", Width: 400, MinWidth: 200, MaxWidth: 800},
		{Name: "Duration", Width: 120, MinWidth: 80, MaxWidth: 200},
		{Name: "Instance", Width: 200, MinWidth: 100, MaxWidth: 400},
	}
}

// NewAlertsWindow creates a new alerts window
func NewAlertsWindow(client *alertmanager.Client, configPath string, initialConfig interface{}) *AlertsWindow {
	myApp := app.NewWithID("com.notificator.alerts")
	myApp.SetIcon(theme.InfoIcon())

	window := myApp.NewWindow("Notificator - Alert Dashboard")
	window.Resize(fyne.NewSize(1400, 800))
	window.CenterOnScreen()

	var notifConfig notifier.NotificationConfig
	if initialConfig != nil {
		if cfg, ok := initialConfig.(*ConfigStruct); ok {
			notifConfig = notifier.NotificationConfig{
				Enabled:          cfg.Notifications.Enabled,
				SoundEnabled:     cfg.Notifications.SoundEnabled,
				SoundPath:        cfg.Notifications.SoundPath,
				ShowSystem:       cfg.Notifications.ShowSystem,
				CriticalOnly:     cfg.Notifications.CriticalOnly,
				MaxNotifications: cfg.Notifications.MaxNotifications,
				CooldownSeconds:  cfg.Notifications.CooldownSeconds,
				SeverityRules:    cfg.Notifications.SeverityRules,
			}
		} else {
			notifConfig = notifier.CreateDefaultNotificationConfig()
		}
	} else {
		notifConfig = notifier.CreateDefaultNotificationConfig()
	}

	aw := &AlertsWindow{
		app:                myApp,
		window:             window,
		client:             client,
		alerts:             []models.Alert{},
		previousAlerts:     []models.Alert{},
		filteredData:       []models.Alert{},
		data:               binding.NewUntypedList(),
		autoRefresh:        true,
		updateChan:         make(chan func(), 100),
		columns:            getDefaultColumns(),
		notificationConfig: notifConfig,
		configPath:         configPath,
		fullConfig:         initialConfig,
		themeVariant:       "light",
	}

	aw.loadColumnConfig()
	aw.loadThemePreference()
	aw.notifier = notifier.NewNotifier(notifConfig, myApp)

	aw.setupUI()
	aw.setupKeyboardShortcuts()
	aw.startUpdateHandler()
	aw.loadAlerts()
	aw.startAutoRefresh()

	return aw
}

// ConfigStruct for type assertion (simplified)
type ConfigStruct struct {
	Notifications NotificationConfigStruct `json:"notifications"`
	ColumnWidths  map[string]float32       `json:"column_widths"`
}

// NotificationConfigStruct for type assertion (simplified)
type NotificationConfigStruct struct {
	Enabled          bool            `json:"enabled"`
	SoundEnabled     bool            `json:"sound_enabled"`
	SoundPath        string          `json:"sound_path"`
	ShowSystem       bool            `json:"show_system"`
	CriticalOnly     bool            `json:"critical_only"`
	MaxNotifications int             `json:"max_notifications"`
	CooldownSeconds  int             `json:"cooldown_seconds"`
	SeverityRules    map[string]bool `json:"severity_rules"`
}

// startUpdateHandler starts a goroutine to handle UI updates safely
func (aw *AlertsWindow) startUpdateHandler() {
	go func() {
		for updateFunc := range aw.updateChan {
			updateFunc()
		}
	}()
}

// scheduleUpdate schedules a function to run on the main UI thread
func (aw *AlertsWindow) scheduleUpdate(updateFunc func()) {
	select {
	case aw.updateChan <- updateFunc:
		// Update scheduled successfully
	default:
		// Channel is full, skip this update
		log.Printf("Update channel full, skipping update")
	}
}

// loadAlerts fetches alerts from Alertmanager
func (aw *AlertsWindow) loadAlerts() {
	aw.setStatus("Loading alerts...")
	aw.refreshBtn.SetText("Loading...")
	aw.refreshBtn.Disable()

	go func() {
		alerts, err := aw.client.FetchAlerts()

		// Schedule UI update on main thread
		aw.scheduleUpdate(func() {
			if err != nil {
				log.Printf("Failed to fetch alerts: %v", err)
				aw.setStatus(fmt.Sprintf("Error: %v", err))

				// Show error dialog
				dialog.ShowError(err, aw.window)
			} else {
				// Process notifications before updating UI
				aw.notifier.ProcessAlerts(alerts, aw.previousAlerts)

				// Update alerts
				aw.previousAlerts = aw.alerts // Store previous alerts
				aw.alerts = alerts
				aw.updateTeamFilter()
				aw.applyFilters()
				aw.updateDashboard() // Update dashboard cards

				activeCount := 0
				for _, alert := range alerts {
					if alert.IsActive() {
						activeCount++
					}
				}

				aw.setStatus(fmt.Sprintf("Loaded %d alerts (%d active)", len(alerts), activeCount))
				aw.lastUpdate.SetText(time.Now().Format("15:04:05"))
			}

			aw.refreshBtn.SetText("Refresh")
			aw.refreshBtn.Enable()
		})
	}()
}

// safeApplyFilters applies filters only if table is initialized
func (aw *AlertsWindow) safeApplyFilters() {
	if aw.table != nil {
		aw.applyFilters()
	}
}

// applyFilters applies current filter settings to the alerts
func (aw *AlertsWindow) applyFilters() {
	filtered := []models.Alert{}

	searchText := strings.ToLower(aw.searchEntry.Text)
	severityFilter := aw.severitySelect.Selected
	statusFilter := aw.statusSelect.Selected
	teamFilter := aw.teamSelect.Selected

	for _, alert := range aw.alerts {
		// Apply search filter
		if searchText != "" {
			searchMatch := strings.Contains(strings.ToLower(alert.GetAlertName()), searchText) ||
				strings.Contains(strings.ToLower(alert.GetSummary()), searchText) ||
				strings.Contains(strings.ToLower(alert.GetTeam()), searchText) ||
				strings.Contains(strings.ToLower(alert.GetInstance()), searchText)

			if !searchMatch {
				continue
			}
		}

		// Apply severity filter
		if severityFilter != "All" && alert.GetSeverity() != severityFilter {
			continue
		}

		// Apply status filter
		if statusFilter != "All" && alert.Status.State != statusFilter {
			continue
		}

		// Apply team filter
		if teamFilter != "All" && alert.GetTeam() != teamFilter {
			continue
		}

		filtered = append(filtered, alert)
	}

	aw.filteredData = filtered

	// Apply custom sorting
	aw.sortFilteredData()

	// Update dashboard with filtered data
	aw.updateDashboard()

	if aw.table != nil {
		aw.table.Refresh()
	}
}

// updateTeamFilter populates the team filter with unique teams from alerts
func (aw *AlertsWindow) updateTeamFilter() {
	teams := make(map[string]bool)
	for _, alert := range aw.alerts {
		team := alert.GetTeam()
		if team != "unknown" && team != "" {
			teams[team] = true
		}
	}

	teamOptions := []string{"All"}
	for team := range teams {
		teamOptions = append(teamOptions, team)
	}
	sort.Strings(teamOptions[1:]) // Sort all except "All"

	// Safely update the UI component
	if aw.teamSelect != nil {
		aw.teamSelect.Options = teamOptions
		aw.teamSelect.Refresh()
	}
}

// Event handlers
func (aw *AlertsWindow) handleRefresh() {
	aw.loadAlerts()
}

func (aw *AlertsWindow) handleExport() {
	dialog.ShowInformation("Export", "Export functionality coming soon!", aw.window)
}

func (aw *AlertsWindow) handleSettings() {
	aw.showNotificationSettings()
}

func (aw *AlertsWindow) clearFilters() {
	aw.searchEntry.SetText("")
	aw.severitySelect.SetSelected("All")
	aw.statusSelect.SetSelected("All")
	aw.teamSelect.SetSelected("All")
	aw.focusSearchEntry()
}

func (aw *AlertsWindow) focusSearchEntry() {
	if aw.searchEntry != nil {
		aw.window.Canvas().Focus(aw.searchEntry)
	}
}

func (aw *AlertsWindow) setupKeyboardShortcuts() {
	aw.window.Canvas().SetOnTypedKey(func(key *fyne.KeyEvent) {
		if key.Name == fyne.KeyF && (key.Physical.ScanCode == 0 || key.Physical.ScanCode == 33) {
			aw.focusSearchEntry()
		}
	})
}

// handleColumnSort handles column header clicks for sorting
func (aw *AlertsWindow) handleColumnSort(column int) {
	if aw.sortColumn == column {
		// Same column, toggle sort direction
		aw.sortAscending = !aw.sortAscending
	} else {
		// New column, default to ascending
		aw.sortColumn = column
		aw.sortAscending = true
	}

	// Re-apply filters which will trigger sorting
	aw.applyFilters()
}

// sortFilteredData sorts the filtered data based on current sort settings
func (aw *AlertsWindow) sortFilteredData() {
	if len(aw.filteredData) == 0 {
		return
	}

	sort.Slice(aw.filteredData, func(i, j int) bool {
		var result bool

		switch aw.sortColumn {
		case 0: // Alert name
			result = strings.Compare(aw.filteredData[i].GetAlertName(), aw.filteredData[j].GetAlertName()) < 0
		case 1: // Severity
			severityOrder := map[string]int{"critical": 0, "warning": 1, "info": 2, "unknown": 3}
			sev1, exists1 := severityOrder[aw.filteredData[i].GetSeverity()]
			if !exists1 {
				sev1 = 4
			}
			sev2, exists2 := severityOrder[aw.filteredData[j].GetSeverity()]
			if !exists2 {
				sev2 = 4
			}
			result = sev1 < sev2
		case 2: // Status
			result = strings.Compare(aw.filteredData[i].Status.State, aw.filteredData[j].Status.State) < 0
		case 3: // Team
			result = strings.Compare(aw.filteredData[i].GetTeam(), aw.filteredData[j].GetTeam()) < 0
		case 4: // Summary
			result = strings.Compare(aw.filteredData[i].GetSummary(), aw.filteredData[j].GetSummary()) < 0
		case 5: // Duration
			result = aw.filteredData[i].Duration() < aw.filteredData[j].Duration()
		case 6: // Instance
			result = strings.Compare(aw.filteredData[i].GetInstance(), aw.filteredData[j].GetInstance()) < 0
		default:
			// Default sort by severity then start time
			severityOrder := map[string]int{"critical": 0, "warning": 1, "info": 2, "unknown": 3}
			sev1, exists1 := severityOrder[aw.filteredData[i].GetSeverity()]
			if !exists1 {
				sev1 = 4
			}
			sev2, exists2 := severityOrder[aw.filteredData[j].GetSeverity()]
			if !exists2 {
				sev2 = 4
			}
			if sev1 != sev2 {
				result = sev1 < sev2
			} else {
				result = aw.filteredData[i].StartsAt.After(aw.filteredData[j].StartsAt)
			}
		}

		if !aw.sortAscending {
			result = !result
		}

		return result
	})
}

// Auto-refresh functionality
func (aw *AlertsWindow) startAutoRefresh() {
	if aw.refreshTicker != nil {
		aw.refreshTicker.Stop()
	}

	if aw.autoRefresh {
		aw.refreshTicker = time.NewTicker(30 * time.Second)
		go func() {
			for range aw.refreshTicker.C {
				if aw.autoRefresh {
					aw.loadAlerts()
				}
			}
		}()
	}
}

func (aw *AlertsWindow) stopAutoRefresh() {
	if aw.refreshTicker != nil {
		aw.refreshTicker.Stop()
		aw.refreshTicker = nil
	}
}

// Utility methods
func (aw *AlertsWindow) setStatus(status string) {
	// Ensure UI updates happen on the main thread
	if aw.statusLabel != nil {
		aw.statusLabel.SetText(status)
	}
}

func (aw *AlertsWindow) Show() {
	aw.window.ShowAndRun()
}

// Close method to clean up resources
func (aw *AlertsWindow) Close() {
	aw.stopAutoRefresh()
	close(aw.updateChan) // Close the update channel
	aw.window.Close()
}

// Helper functions
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func formatMap(m map[string]string) string {
	if len(m) == 0 {
		return "None"
	}

	var lines []string
	for k, v := range m {
		lines = append(lines, fmt.Sprintf("- **%s:** %s", k, v))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// loadColumnConfig loads saved column widths from configuration file
func (aw *AlertsWindow) loadColumnConfig() {
	// Try to read existing config file to get saved column widths
	if aw.configPath == "" {
		return
	}

	// Read the config file and extract column widths
	// This is a simple approach - in production you might want more sophisticated config management
	type ConfigFile struct {
		ColumnWidths map[string]float32 `json:"column_widths"`
	}

	if data, err := os.ReadFile(aw.configPath + ".gui"); err == nil {
		var config ConfigFile
		if json.Unmarshal(data, &config) == nil && config.ColumnWidths != nil {
			// Apply saved column widths
			for i, col := range aw.columns {
				if width, exists := config.ColumnWidths[col.Name]; exists {
					aw.columns[i].Width = width
				}
			}
		}
	}
}

// saveColumnConfig saves current column widths to configuration file
func (aw *AlertsWindow) saveColumnConfig() {
	if aw.configPath == "" {
		return
	}

	type ConfigFile struct {
		ColumnWidths map[string]float32 `json:"column_widths"`
	}

	// Create column widths map
	widths := make(map[string]float32)
	for _, col := range aw.columns {
		widths[col.Name] = col.Width
	}

	config := ConfigFile{
		ColumnWidths: widths,
	}

	// Save to a separate GUI config file
	if data, err := json.MarshalIndent(config, "", "  "); err == nil {
		os.WriteFile(aw.configPath+".gui", data, 0644)
	}
}

// saveNotificationConfig saves current notification settings to configuration file
func (aw *AlertsWindow) saveNotificationConfig() {
	if aw.configPath == "" {
		return
	}

	config := notifier.NotificationConfig{
		Enabled:          aw.notificationConfig.Enabled,
		SoundEnabled:     aw.notificationConfig.SoundEnabled,
		SoundPath:        aw.notificationConfig.SoundPath,
		ShowSystem:       aw.notificationConfig.ShowSystem,
		CriticalOnly:     aw.notificationConfig.CriticalOnly,
		MaxNotifications: aw.notificationConfig.MaxNotifications,
		CooldownSeconds:  aw.notificationConfig.CooldownSeconds,
		SeverityRules:    aw.notificationConfig.SeverityRules,
	}

	// Save to notification config file
	if data, err := json.MarshalIndent(config, "", "  "); err == nil {
		os.WriteFile(aw.configPath+".notifications", data, 0644)
	}
}

// updateAutocompleteEntry updates the autocomplete entry with new suggestions
func (aw *AlertsWindow) updateAutocompleteEntry() {
	// Update the autocomplete entry with new suggestions
	if aw.autocompleteEntry != nil && len(aw.searchSuggestions) > 0 {
		aw.autocompleteEntry.SetSuggestions(aw.searchSuggestions)
	}
}

// formatCooldownTime formats cooldown seconds into human readable format
func formatCooldownTime(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	} else if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	} else {
		return fmt.Sprintf("%dh", seconds/3600)
	}
}

// silenceAlert creates a silence for the given alert
func (aw *AlertsWindow) silenceAlert(alert models.Alert) {
	// Show silence duration selection dialog
	durationOptions := []string{"1 hour", "4 hours", "8 hours", "24 hours", "Custom"}

	durationSelect := widget.NewSelect(durationOptions, nil)
	durationSelect.SetSelected("1 hour")

	commentEntry := widget.NewEntry()
	commentEntry.SetPlaceHolder("Reason for silencing (optional)")
	commentEntry.MultiLine = true
	commentEntry.Resize(fyne.NewSize(400, 80))

	content := container.NewVBox(
		widget.NewLabelWithStyle("Silence Alert: "+alert.GetAlertName(), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		widget.NewLabel("Duration:"),
		durationSelect,
		widget.NewLabel("Comment:"),
		commentEntry,
	)

	dialog := dialog.NewCustomConfirm("Silence Alert", "Silence", "Cancel", content, func(confirmed bool) {
		if confirmed {
			aw.createSilenceForAlert(alert, durationSelect.Selected, commentEntry.Text)
		}
	}, aw.window)

	dialog.Resize(fyne.NewSize(500, 300))
	dialog.Show()
}

// createSilenceForAlert creates a silence for the alert with the specified duration
func (aw *AlertsWindow) createSilenceForAlert(alert models.Alert, duration string, comment string) {
	// Parse duration
	var silenceDuration time.Duration
	switch duration {
	case "1 hour":
		silenceDuration = time.Hour
	case "4 hours":
		silenceDuration = 4 * time.Hour
	case "8 hours":
		silenceDuration = 8 * time.Hour
	case "24 hours":
		silenceDuration = 24 * time.Hour
	default:
		silenceDuration = time.Hour // Default to 1 hour
	}

	// Create silence matchers based on alert labels
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

	// Create the silence
	now := time.Now()
	silence := models.Silence{
		Matchers:  matchers,
		StartsAt:  now,
		EndsAt:    now.Add(silenceDuration),
		CreatedBy: "notificator-gui",
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
