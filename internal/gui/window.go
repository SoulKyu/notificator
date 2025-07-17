// internal/gui/window.go
package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"notificator/config"
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

// ConnectionHealth tracks the health of the Alertmanager connection
type ConnectionHealth struct {
	LastSuccessful time.Time
	FailureCount   int
	IsHealthy      bool
}

// AlertGroup represents a group of alerts with the same alertname
type AlertGroup struct {
	AlertName     string
	Alerts        []models.Alert
	IsExpanded    bool
	CriticalCount int
	WarningCount  int
	InfoCount     int
	ActiveCount   int
	TotalCount    int
}

// TableRow represents a row in the grouped table (either group header or individual alert)
type TableRow struct {
	Type       string        // "group" or "alert"
	Group      *AlertGroup   // For group rows
	Alert      *models.Alert // For alert rows
	GroupIndex int           // Index of the group this row belongs to
	AlertIndex int           // Index within the group (for alert rows)
	RowIndex   int           // Global row index for selection tracking
}

// MultiSelectWidget represents a multi-select filter widget
type MultiSelectWidget struct {
	widget.BaseWidget
	label     string
	options   []string
	selected  map[string]bool
	button    *widget.Button
	popup     *widget.PopUp
	window    fyne.Window
	onChange  func(selected map[string]bool)
	container *fyne.Container
}

// AlertsWindow represents the main GUI window with enhanced features
type AlertsWindow struct {
	app                  fyne.App
	window               fyne.Window
	client               *alertmanager.Client
	backendClient        *BackendClient
	backendHealthChecker *BackendHealthChecker
	backendConnected     bool
	backendAuthenticated bool
	alerts               []models.Alert
	alertsMutex          sync.RWMutex // Protects access to alerts
	previousAlerts       []models.Alert
	table                *widget.Table
	data                 binding.UntypedList
	filteredData         []models.Alert

	refreshBtn  *widget.Button
	searchEntry *widget.Entry
	//autocompleteEntry *AutocompleteEntry

	// Main UI components
	toolbar        *fyne.Container
	filters        *fyne.Container
	bulkActions    *fyne.Container
	tableContainer fyne.CanvasObject
	statusBar      *fyne.Container

	// Multi-select filter widgets
	severityMultiSelect *MultiSelectWidget
	statusMultiSelect   *MultiSelectWidget
	teamMultiSelect     *MultiSelectWidget
	ackMultiSelect      *MultiSelectWidget
	commentMultiSelect  *MultiSelectWidget

	statusLabel *widget.Label
	lastUpdate  *widget.Label

	// Grouped display components
	alertGroups    []AlertGroup
	tableRows      []TableRow
	groupedMode    bool
	groupToggleBtn *widget.Button

	autoRefresh      bool
	refreshTicker    *time.Ticker
	refreshInterval  time.Duration
	lastActivity     time.Time
	connectionHealth *ConnectionHealth
	refreshCancel    context.CancelFunc // For goroutine cleanup

	// Active collaboration content tracking for real-time updates
	activeCollaborationContainers map[string]*fyne.Container // alertKey -> collaboration container

	// Cache for acknowledgment and comment counts for sorting performance
	ackCountCache     map[string]int // alertKey -> count
	commentCountCache map[string]int // alertKey -> count
	cacheMutex        sync.RWMutex

	// Resolved alerts cache
	resolvedAlertsCache  *ResolvedAlertsCache
	resolvedDialog       fyne.Window
	resolvedAlertsConfig config.ResolvedAlertsConfig

	// Channel for thread-safe updates
	updateChan chan func()

	// Column configuration
	columns      []ColumnConfig
	columnDialog *dialog.CustomDialog

	// Notification system
	notifier           *notifier.Notifier
	notificationConfig notifier.NotificationConfig

	// Configuration saving
	configPath     string
	fullConfig     interface{}
	config         *ConfigStruct
	originalConfig *config.Config

	// Visual enhancements
	themeVariant string
	themeBtn     *widget.Button

	// Sorting
	sortColumn    int
	sortAscending bool

	// Status bar metrics
	statusBarMetrics *StatusBarMetrics

	// Alert hiding functionality
	hiddenAlertsCache *HiddenAlertsCache
	showHiddenAlerts  bool
	showHiddenBtn     *widget.Button
	
	// Resolved alerts functionality
	showResolvedBtn    *widget.Button
	showResolvedAlerts bool

	// Selection for bulk operations
	selectedAlerts  map[int]bool
	hideSelectedBtn *widget.Button
	selectionLabel  *widget.Label

	// UI initialization flag
	uiReady bool

	// Backend UI components
	backendStatusBtn *widget.Button

	// Background mode components
	trayManager        *TrayManager
	backgroundNotifier *BackgroundNotifier
	isBackgroundMode   bool
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
		{Name: "✓", Width: 40, MinWidth: 30, MaxWidth: 50},
		{Name: "Alert", Width: 200, MinWidth: 100, MaxWidth: 400},
		{Name: "Severity", Width: 120, MinWidth: 100, MaxWidth: 150},
		{Name: "Status", Width: 120, MinWidth: 100, MaxWidth: 150},
		{Name: "Ack", Width: 80, MinWidth: 60, MaxWidth: 120},
		{Name: "Comments", Width: 80, MinWidth: 60, MaxWidth: 120},
		{Name: "Team", Width: 120, MinWidth: 80, MaxWidth: 200},
		{Name: "Summary", Width: 400, MinWidth: 200, MaxWidth: 800},
		{Name: "Duration", Width: 120, MinWidth: 80, MaxWidth: 200},
		{Name: "Instance", Width: 200, MinWidth: 100, MaxWidth: 400},
	}
}

// MultiSelectWidget methods

// NewMultiSelectWidget creates a new multi-select widget
func NewMultiSelectWidget(label string, options []string, window fyne.Window, onChange func(selected map[string]bool)) *MultiSelectWidget {
	ms := &MultiSelectWidget{
		label:    label,
		options:  options,
		selected: make(map[string]bool),
		window:   window,
		onChange: onChange,
	}

	// Select "All" by default
	ms.selected["All"] = true

	ms.ExtendBaseWidget(ms)
	ms.createUI()
	return ms
}

// createUI creates the UI components for the multi-select widget
func (ms *MultiSelectWidget) createUI() {
	ms.button = widget.NewButton(ms.getButtonText(), ms.showPopup)
	ms.button.Importance = widget.LowImportance

	ms.container = container.NewHBox(
		widget.NewLabel(ms.label+":"),
		ms.button,
	)
}

// getButtonText returns the text to display on the button
func (ms *MultiSelectWidget) getButtonText() string {
	selectedCount := len(ms.selected)
	if selectedCount == 0 || (selectedCount == 1 && ms.selected["All"]) {
		return "All"
	}

	if selectedCount == 1 {
		for key := range ms.selected {
			if key != "All" {
				return key
			}
		}
	}

	return fmt.Sprintf("%d selected", selectedCount)
}

// showPopup shows the multi-select popup
func (ms *MultiSelectWidget) showPopup() {
	content := container.NewVBox()

	// Add "All" option first
	allCheck := widget.NewCheck("All", func(checked bool) {
		if checked {
			// Select all and clear others
			ms.selected = map[string]bool{"All": true}
		} else {
			// Deselect all
			delete(ms.selected, "All")
		}
		ms.refreshChecks(content)
		ms.updateButton()
		if ms.onChange != nil {
			ms.onChange(ms.selected)
		}
	})
	allCheck.SetChecked(ms.selected["All"])
	content.Add(allCheck)

	content.Add(widget.NewSeparator())

	// Add individual options
	for _, option := range ms.options {
		if option == "All" {
			continue // Already added above
		}

		optionCopy := option
		check := widget.NewCheck(option, func(checked bool) {
			if checked {
				// Remove "All" if selecting individual items
				delete(ms.selected, "All")
				ms.selected[optionCopy] = true
			} else {
				delete(ms.selected, optionCopy)
				// If nothing selected, select "All"
				if len(ms.selected) == 0 {
					ms.selected["All"] = true
				}
			}
			ms.refreshChecks(content)
			ms.updateButton()
			if ms.onChange != nil {
				ms.onChange(ms.selected)
			}
		})
		check.SetChecked(ms.selected[option])
		content.Add(check)
	}

	// Action buttons
	actionContainer := container.NewHBox(
		widget.NewButton("Clear All", func() {
			ms.selected = map[string]bool{"All": true}
			ms.refreshChecks(content)
			ms.updateButton()
			if ms.onChange != nil {
				ms.onChange(ms.selected)
			}
		}),
		widget.NewButton("Close", func() {
			if ms.popup != nil {
				ms.popup.Hide()
			}
		}),
	)
	content.Add(widget.NewSeparator())
	content.Add(actionContainer)

	// Create scrollable container
	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(200, 300))

	ms.popup = widget.NewPopUp(scroll, ms.window.Canvas())

	// Position popup below button
	buttonPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(ms.button)
	buttonSize := ms.button.Size()
	ms.popup.ShowAtPosition(fyne.NewPos(buttonPos.X, buttonPos.Y+buttonSize.Height))
}

// refreshChecks updates all checkboxes in the popup
func (ms *MultiSelectWidget) refreshChecks(content *fyne.Container) {
	for i, obj := range content.Objects {
		if check, ok := obj.(*widget.Check); ok {
			if i == 0 { // "All" checkbox
				check.SetChecked(ms.selected["All"])
			} else if i > 1 { // Skip separator at index 1
				checkText := check.Text
				check.SetChecked(ms.selected[checkText])
			}
		}
	}
}

// updateButton updates the button text
func (ms *MultiSelectWidget) updateButton() {
	ms.button.SetText(ms.getButtonText())
}

// GetSelected returns the currently selected options
func (ms *MultiSelectWidget) GetSelected() map[string]bool {
	return ms.selected
}

// SetOptions updates the available options
func (ms *MultiSelectWidget) SetOptions(options []string) {
	ms.options = options
	// Reset selection to "All"
	ms.selected = map[string]bool{"All": true}
	ms.updateButton()
}

// CreateRenderer creates the renderer for the widget
func (ms *MultiSelectWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(ms.container)
}

// NewAlertsWindow creates a new alerts window with enhanced features
func NewAlertsWindow(client *alertmanager.Client, configPath string, initialConfig interface{}) *AlertsWindow {
	myApp := app.NewWithID("com.notificator.alerts")

	// The app icon will be set automatically from FyneApp.toml metadata

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
				RespectFilters:   cfg.Notifications.RespectFilters,
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
		refreshInterval:    30 * time.Second,
		lastActivity:       time.Now(),
		connectionHealth:   &ConnectionHealth{LastSuccessful: time.Now(), IsHealthy: true},
		updateChan:         make(chan func(), 1000),
		columns:            getDefaultColumns(),
		notificationConfig: notifConfig,
		configPath:         configPath,
		fullConfig:         initialConfig,
		themeVariant:       "light",
		selectedAlerts:     make(map[int]bool),
		showHiddenAlerts:   false,
		groupedMode:        false, // Start in flat mode
		activeCollaborationContainers: make(map[string]*fyne.Container),
		ackCountCache:     make(map[string]int),
		commentCountCache: make(map[string]int),
		resolvedAlertsCache: NewResolvedAlertsCache(1 * time.Hour), // Will be updated from config
	}
	// Initialize resolved alerts configuration
	if cfg, ok := initialConfig.(*config.Config); ok {
		// Update resolved alerts cache TTL from config
		aw.resolvedAlertsCache.UpdateTTL(cfg.ResolvedAlerts.RetentionDuration)
		// Initialize resolved alerts config
		aw.resolvedAlertsConfig = cfg.ResolvedAlerts
	}
	
	// Initialize backend client if backend is enabled
	if cfg, ok := initialConfig.(*config.Config); ok && cfg.Backend.Enabled {
		aw.backendClient = NewBackendClient(cfg.Backend.GRPCClient)
		aw.backendClient.SetConnectionStateCallback(func(connected bool) {
			aw.backendConnected = connected
			// Ensure UI updates happen on the main thread
			fyne.Do(func() {
				aw.updateBackendUI()
				aw.updateUserInterface() // Use the existing method from auth_dialog.go
			})
		})
		aw.backendClient.SetAuthStateCallback(func(authenticated bool) {
			aw.backendAuthenticated = authenticated
			// Ensure UI updates happen on the main thread
			fyne.Do(func() {
				aw.updateBackendUI()
				aw.updateUserInterface() // Use the existing method from auth_dialog.go
			})
		})
		// Try initial connection after callbacks are set
		go func() {
			aw.backendClient.tryConnect()
			// After connection is established, attempt auto-login if credentials are stored
			aw.tryAutoLogin()
		}()
	} else {
		// Create a disconnected backend client for compatibility
		aw.backendClient = &BackendClient{isConnected: false}
		aw.backendConnected = false
		aw.backendAuthenticated = false
	}

	// Store config reference - handle both config types
	if cfg, ok := initialConfig.(*config.Config); ok {
		// Convert to our internal structure for compatibility
		aw.config = &ConfigStruct{
			GUI: GUIConfigStruct{
				FilterState: FilterStateConfigStruct{
					SearchText:         cfg.GUI.FilterState.SearchText,
					SelectedSeverities: cfg.GUI.FilterState.SelectedSeverities,
					SelectedStatuses:   cfg.GUI.FilterState.SelectedStatuses,
					SelectedTeams:      cfg.GUI.FilterState.SelectedTeams,
				},
			},
		}
		// Store the original config for saving
		aw.originalConfig = cfg
	} else if cfg, ok := initialConfig.(*ConfigStruct); ok {
		aw.config = cfg
	}

	// Initialize hidden alerts cache
	aw.hiddenAlertsCache = NewHiddenAlertsCache(configPath)

	aw.loadColumnConfig()
	aw.loadThemePreference()
	aw.notifier = notifier.NewNotifier(notifConfig, myApp)

	// Create compact backend status button with dropdown menu
	aw.backendStatusBtn = widget.NewButtonWithIcon("", theme.ComputerIcon(), func() {
		aw.showBackendDropdownMenu()
	})
	aw.backendStatusBtn.Importance = widget.MediumImportance
	aw.backendStatusBtn.Resize(fyne.NewSize(50, 32)) // Slightly wider for emoji
	aw.updateBackendUI()

	aw.setupUI()
	if aw.originalConfig != nil && aw.originalConfig.GUI.StartMinimized {
		go func() {
			time.Sleep(2 * time.Second) // Give UI time to initialize
			fyne.Do(func() {
				if aw.trayManager != nil {
					aw.trayManager.HideToBackground()
				}
			})
		}()
	}
	aw.isBackgroundMode = false
	if aw.originalConfig != nil {
		aw.isBackgroundMode = aw.originalConfig.GUI.BackgroundMode
	}

	// Create tray manager
	aw.trayManager = NewTrayManager(myApp, window, aw)

	// Backend menu items are now available through the toolbar button dropdown only

	// Create background notifier with callback to show window
	aw.backgroundNotifier = NewBackgroundNotifier(notifConfig, myApp, func() {
		if aw.trayManager != nil {
			aw.trayManager.ShowWindow()
		}
	})

	// Window close intercept will be handled by TrayManager
	aw.setupKeyboardShortcuts()
	aw.startUpdateHandler()
	aw.startConnectionHealthMonitoring()

	// Mark UI as ready
	aw.uiReady = true

	// Load initial data
	aw.loadInitialData()
	aw.startSmartAutoRefresh()

	aw.updateNotificationFilters()

	return aw
}

// ConfigStruct for type assertion
type ConfigStruct struct {
	Notifications NotificationConfigStruct `json:"notifications"`
	ColumnWidths  map[string]float32       `json:"column_widths"`
	GUI           GUIConfigStruct          `json:"gui"`
}

// GUIConfigStruct contains GUI-specific settings
type GUIConfigStruct struct {
	FilterState    FilterStateConfigStruct `json:"filter_state"`
	MinimizeToTray bool                    `json:"minimize_to_tray"`
	StartMinimized bool                    `json:"start_minimized"`
	ShowTrayIcon   bool                    `json:"show_tray_icon"`
	BackgroundMode bool                    `json:"background_mode"`
}

// FilterStateConfigStruct contains the state of filters
type FilterStateConfigStruct struct {
	SearchText         string          `json:"search_text"`
	SelectedSeverities map[string]bool `json:"selected_severities"`
	SelectedStatuses   map[string]bool `json:"selected_statuses"`
	SelectedTeams      map[string]bool `json:"selected_teams"`
	SelectedAcks       map[string]bool `json:"selected_acks"`
	SelectedComments   map[string]bool `json:"selected_comments"`
}

type NotificationConfigStruct struct {
	Enabled          bool            `json:"enabled"`
	SoundEnabled     bool            `json:"sound_enabled"`
	SoundPath        string          `json:"sound_path"`
	ShowSystem       bool            `json:"show_system"`
	CriticalOnly     bool            `json:"critical_only"`
	MaxNotifications int             `json:"max_notifications"`
	CooldownSeconds  int             `json:"cooldown_seconds"`
	SeverityRules    map[string]bool `json:"severity_rules"`
	RespectFilters   bool            `json:"respect_filters"`
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
	default:
		log.Printf("Update channel full, skipping update")
	}
}

func (aw *AlertsWindow) loadInitialData() {
	aw.alerts = []models.Alert{}
	aw.filteredData = []models.Alert{}

	go func() {
		time.Sleep(1 * time.Second)
		aw.loadAlertsWithCaching()
	}()
}

// alertsChanged detects if alerts have actually changed
func (aw *AlertsWindow) alertsChanged(oldAlerts, newAlerts []models.Alert) bool {
	if len(oldAlerts) != len(newAlerts) {
		return true
	}

	oldMap := make(map[string]models.Alert)
	for _, alert := range oldAlerts {
		key := fmt.Sprintf("%s_%s_%s", alert.GetAlertName(), alert.GetInstance(), alert.Status.State)
		oldMap[key] = alert
	}

	for _, newAlert := range newAlerts {
		key := fmt.Sprintf("%s_%s_%s", newAlert.GetAlertName(), newAlert.GetInstance(), newAlert.Status.State)
		oldAlert, exists := oldMap[key]
		if !exists {
			return true
		}

		if !oldAlert.StartsAt.Equal(newAlert.StartsAt) ||
			!oldAlert.EndsAt.Equal(newAlert.EndsAt) ||
			len(oldAlert.Status.SilencedBy) != len(newAlert.Status.SilencedBy) ||
			len(oldAlert.Status.InhibitedBy) != len(newAlert.Status.InhibitedBy) {
			return true
		}
	}

	return false
}

func (aw *AlertsWindow) updateNotificationFilters() {
	// Update regular notifier
	if aw.notifier == nil {
		return
	}

	// Get current filter states
	var searchText string
	var selectedSeverities, selectedStatuses, selectedTeams map[string]bool

	if aw.searchEntry != nil {
		searchText = aw.searchEntry.Text
	}

	if aw.severityMultiSelect != nil {
		selectedSeverities = aw.severityMultiSelect.GetSelected()
	} else {
		selectedSeverities = map[string]bool{"All": true}
	}

	if aw.statusMultiSelect != nil {
		selectedStatuses = aw.statusMultiSelect.GetSelected()
	} else {
		selectedStatuses = map[string]bool{"All": true}
	}

	if aw.teamMultiSelect != nil {
		selectedTeams = aw.teamMultiSelect.GetSelected()
	} else {
		selectedTeams = map[string]bool{"All": true}
	}

	// Create filter state and update both notifiers
	filterState := notifier.FilterState{
		SearchText:         searchText,
		SelectedSeverities: selectedSeverities,
		SelectedStatuses:   selectedStatuses,
		SelectedTeams:      selectedTeams,
		ShowHiddenAlerts:   aw.showHiddenAlerts,
	}

	aw.notifier.UpdateFilters(filterState)

	// Also update background notifier
	if aw.backgroundNotifier != nil {
		aw.backgroundNotifier.UpdateFilters(filterState)
	}
}

// loadAlertsWithCaching loads alerts with smart caching
func (aw *AlertsWindow) loadAlertsWithCaching() {
	if aw.statusLabel != nil && aw.refreshBtn != nil {
		aw.setStatus("Loading alerts...")
		fyne.Do(func() {
			if aw.refreshBtn != nil {
				aw.refreshBtn.SetText("")
				aw.refreshBtn.Disable()
			}
		})
	}

	go func() {
		alerts, err := aw.client.FetchAlerts()

		fyne.Do(func() {
			if err != nil {
				log.Printf("Failed to fetch alerts: %v", err)
				aw.setStatus(fmt.Sprintf("Error: %v", err))
				aw.connectionHealth.FailureCount++
				aw.connectionHealth.IsHealthy = false

				if aw.refreshBtn != nil && aw.refreshBtn.Text == "Loading..." {
					dialog.ShowError(err, aw.window)
				}
			} else {
				aw.connectionHealth.LastSuccessful = time.Now()
				aw.connectionHealth.FailureCount = 0
				aw.connectionHealth.IsHealthy = true

				if !aw.alertsChanged(aw.alerts, alerts) {
					aw.setStatus("No changes detected")
					if aw.lastUpdate != nil {
						aw.lastUpdate.SetText(time.Now().Format("15:04:05"))
					}
					if aw.refreshBtn != nil {
						aw.refreshBtn.SetText("")
						aw.refreshBtn.Enable()
					}
					return
				}

				aw.notifier.ProcessAlerts(alerts, aw.previousAlerts)
				aw.detectResolvedAlerts(alerts)
				aw.previousAlerts = aw.alerts
				aw.alerts = alerts
				aw.updateTeamFilter()
				aw.safeApplyFilters()
				aw.updateDashboard()
				aw.updateHiddenCountDisplay()
				aw.updateResolvedCountDisplay()

				activeCount := 0
				for _, alert := range alerts {
					if alert.IsActive() {
						activeCount++
					}
				}

				aw.setStatus(fmt.Sprintf("Updated %d alerts (%d active)", len(alerts), activeCount))
				if aw.lastUpdate != nil {
					aw.lastUpdate.SetText(time.Now().Format("15:04:05"))
				}
			}

			if aw.refreshBtn != nil {
				aw.refreshBtn.SetText("")
				aw.refreshBtn.Enable()
			}
		})
	}()
	if aw.trayManager != nil {
		aw.trayManager.UpdateAlertCounts()
	}
}

// loadAlertsInBackground performs background refresh with minimal UI disruption
func (aw *AlertsWindow) loadAlertsInBackground() {
	go func() {
		alerts, err := aw.client.FetchAlerts()

		fyne.Do(func() {
			if err != nil {
				aw.setStatus(fmt.Sprintf("Background refresh failed: %v", err))
				aw.connectionHealth.FailureCount++
				aw.connectionHealth.IsHealthy = false
				return
			}

			aw.connectionHealth.LastSuccessful = time.Now()
			aw.connectionHealth.FailureCount = 0
			aw.connectionHealth.IsHealthy = true

			if aw.alertsChanged(aw.alerts, alerts) {
				if aw.IsBackgroundMode() && aw.backgroundNotifier != nil {
					aw.backgroundNotifier.ProcessAlerts(alerts, aw.previousAlerts)
				} else {
					aw.notifier.ProcessAlerts(alerts, aw.previousAlerts)
				}
				aw.detectResolvedAlerts(alerts)
				aw.previousAlerts = aw.alerts
				aw.alerts = alerts
				aw.updateTeamFilter()
				aw.safeApplyFilters()
				aw.updateDashboard()
				aw.updateHiddenCountDisplay()
				aw.updateResolvedCountDisplay()

				activeCount := 0
				newCritical := 0
				for _, alert := range alerts {
					if alert.IsActive() {
						activeCount++
						if alert.GetSeverity() == "critical" {
							newCritical++
						}
					}
				}

				if newCritical > 0 {
					aw.flashStatus(fmt.Sprintf("🚨 %d critical alerts!", newCritical), 3*time.Second)
				} else {
					aw.setStatus(fmt.Sprintf("Updated: %d alerts (%d active)", len(alerts), activeCount))
				}
				if aw.lastUpdate != nil {
					aw.lastUpdate.SetText(time.Now().Format("15:04:05"))
				}
			}
		})
	}()
}

// flashStatus provides visual feedback for important status changes
func (aw *AlertsWindow) flashStatus(message string, duration time.Duration) {
	if aw.statusLabel == nil {
		return
	}

	originalText := aw.statusLabel.Text
	aw.statusLabel.SetText(message)
	aw.statusLabel.Importance = widget.DangerImportance
	aw.statusLabel.Refresh()

	go func() {
		time.Sleep(duration)
		fyne.Do(func() {
			if aw.statusLabel != nil {
				aw.statusLabel.SetText(originalText)
				aw.statusLabel.Importance = widget.MediumImportance
				aw.statusLabel.Refresh()
			}
		})
	}()
}

func (aw *AlertsWindow) startSmartAutoRefresh() {
	// Clean up any existing ticker and context
	if aw.refreshTicker != nil {
		aw.refreshTicker.Stop()
	}
	if aw.refreshCancel != nil {
		aw.refreshCancel()
	}

	if aw.autoRefresh {
		// Create new context for cancellation
		ctx, cancel := context.WithCancel(context.Background())
		aw.refreshCancel = cancel
		aw.refreshTicker = time.NewTicker(aw.refreshInterval)

		// Update activity time when user interacts
		aw.window.Canvas().SetOnTypedKey(func(key *fyne.KeyEvent) {
			aw.lastActivity = time.Now()
		})

		go func() {
			backgroundRefreshCount := 0

			for {
				select {
				case <-ctx.Done():
					return
				case <-aw.refreshTicker.C:
					if !aw.autoRefresh {
						return
					}

					// Determine adaptive interval based on alert activity
					aw.updateAdaptiveInterval()

					// Determine if user is active (interacted in last 2 minutes)
					userActive := time.Since(aw.lastActivity) < 2*time.Minute

					if userActive {
						// User is actively using the app - normal refresh
						aw.loadAlertsWithCaching()
						backgroundRefreshCount = 0
					} else {
						// User idle - background refresh
						aw.loadAlertsInBackground()
						backgroundRefreshCount++

						// Slow down refresh rate when user is idle for a while
						if backgroundRefreshCount > 10 { // After 5 minutes of inactivity
							if aw.refreshInterval < 60*time.Second {
								aw.updateRefreshInterval(60 * time.Second) // Slower refresh
							}
						}
					}
				}
			}
		}()
	}
}

// updateAdaptiveInterval adjusts polling interval based on alert activity
func (aw *AlertsWindow) updateAdaptiveInterval() {
	aw.alertsMutex.RLock()
	alerts := make([]models.Alert, len(aw.alerts))
	copy(alerts, aw.alerts)
	aw.alertsMutex.RUnlock()

	activeCount := 0
	criticalCount := 0
	for _, alert := range alerts {
		if alert.IsActive() {
			activeCount++
			if alert.GetSeverity() == "critical" {
				criticalCount++
			}
		}
	}

	// Adaptive intervals:
	var newInterval time.Duration
	switch {
	case criticalCount > 0:
		newInterval = 15 * time.Second // Fast for critical alerts
	case activeCount > 5:
		newInterval = 20 * time.Second // Medium for many alerts
	case activeCount > 0:
		newInterval = 30 * time.Second // Normal for some alerts
	default:
		newInterval = 60 * time.Second // Slow when all quiet
	}

	// Update ticker if interval changed significantly
	if newInterval != aw.refreshInterval {
		aw.updateRefreshInterval(newInterval)
		aw.setStatus(fmt.Sprintf("Polling every %v (adaptive)", newInterval))
	}
}

// updateRefreshInterval updates the refresh interval
func (aw *AlertsWindow) updateRefreshInterval(interval time.Duration) {
	aw.refreshInterval = interval
	if aw.refreshTicker != nil {
		aw.refreshTicker.Reset(interval)
	}
}

// startConnectionHealthMonitoring monitors connection health
func (aw *AlertsWindow) startConnectionHealthMonitoring() {
	go func() {
		for {
			time.Sleep(5 * time.Minute) // Check every 5 minutes

			if time.Since(aw.connectionHealth.LastSuccessful) > 2*time.Minute {
				aw.connectionHealth.IsHealthy = false
				aw.connectionHealth.FailureCount++

				fyne.Do(func() {
					aw.setStatus(fmt.Sprintf("⚠️ Connection issues detected (%d failures)", aw.connectionHealth.FailureCount))

					// Show reconnection dialog after multiple failures
					if aw.connectionHealth.FailureCount > 3 {
						dialog.ShowInformation("Connection Issues",
							"Having trouble connecting to Alertmanager. Check your connection and configuration.",
							aw.window)
					}
				})
			} else if !aw.connectionHealth.IsHealthy && time.Since(aw.connectionHealth.LastSuccessful) < 30*time.Second {
				// Connection restored
				aw.connectionHealth.IsHealthy = true
				aw.connectionHealth.FailureCount = 0

				fyne.Do(func() {
					aw.setStatus("✅ Connection restored")
				})
			}
		}
	}()
}

// isUIReady checks if the main UI components are initialized
func (aw *AlertsWindow) isUIReady() bool {
	return aw.uiReady && aw.table != nil && aw.searchEntry != nil &&
		aw.severityMultiSelect != nil && aw.statusMultiSelect != nil && aw.teamMultiSelect != nil
}

// safeApplyFilters applies filters only if table is initialized
func (aw *AlertsWindow) safeApplyFilters() {
	if aw.isUIReady() {
		aw.applyFilters()

		// If in grouped mode, refresh the grouped table
		if aw.groupedMode {
			aw.refreshGroupedTable()
		}

		aw.updateNotificationFilters()
	}
}

func (aw *AlertsWindow) applyFilters() {
	filtered := []models.Alert{}
	searchText := strings.ToLower(aw.searchEntry.Text)

	// Get selected filters
	var selectedSeverities, selectedStatuses, selectedTeams, selectedAcks, selectedComments map[string]bool

	if aw.severityMultiSelect != nil {
		selectedSeverities = aw.severityMultiSelect.GetSelected()
	} else {
		selectedSeverities = map[string]bool{"All": true}
	}

	if aw.statusMultiSelect != nil {
		selectedStatuses = aw.statusMultiSelect.GetSelected()
	} else {
		selectedStatuses = map[string]bool{"All": true}
	}

	if aw.teamMultiSelect != nil {
		selectedTeams = aw.teamMultiSelect.GetSelected()
	} else {
		selectedTeams = map[string]bool{"All": true}
	}

	if aw.ackMultiSelect != nil {
		selectedAcks = aw.ackMultiSelect.GetSelected()
	} else {
		selectedAcks = map[string]bool{"All": true}
	}

	if aw.commentMultiSelect != nil {
		selectedComments = aw.commentMultiSelect.GetSelected()
	} else {
		selectedComments = map[string]bool{"All": true}
	}

	// Determine which alerts to process based on view mode
	var alertsToProcess []models.Alert
	
	if aw.showResolvedAlerts {
		// When showing resolved alerts, get them from the cache
		resolvedAlerts := aw.resolvedAlertsCache.GetResolvedAlerts()
		for _, resolvedAlert := range resolvedAlerts {
			alertsToProcess = append(alertsToProcess, resolvedAlert.Alert)
		}
	} else {
		// When showing normal or hidden alerts, use the regular alerts list
		alertsToProcess = aw.alerts
	}

	for _, alert := range alertsToProcess {
		// Check if alert is hidden (only applies to normal alerts view)
		if !aw.showResolvedAlerts {
			if !aw.showHiddenAlerts && aw.hiddenAlertsCache.IsHidden(alert) {
				continue
			}
			if aw.showHiddenAlerts && !aw.hiddenAlertsCache.IsHidden(alert) {
				continue
			}
		}

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

		// Apply multi-select severity filter
		if !selectedSeverities["All"] && !selectedSeverities[alert.GetSeverity()] {
			continue
		}

		// Apply multi-select status filter
		if !selectedStatuses["All"] && !selectedStatuses[alert.Status.State] {
			continue
		}

		// Apply multi-select team filter
		if !selectedTeams["All"] && !selectedTeams[alert.GetTeam()] {
			continue
		}

		// Apply acknowledgment filter (only if user is authenticated)
		if aw.isUserAuthenticated() && !selectedAcks["All"] {
			alertKey := alert.GetFingerprint()
			if alertKey == "" {
				alertKey = fmt.Sprintf("%s_%s", alert.GetAlertName(), alert.GetInstance())
			}
			
			// Check acknowledgment status
			isAcknowledged := aw.isAlertAcknowledged(alertKey)
			
			if selectedAcks["Acknowledged"] && !isAcknowledged {
				continue
			}
			if selectedAcks["Unacknowledged"] && isAcknowledged {
				continue
			}
		}

		// Apply comment filter (only if user is authenticated)
		if aw.isUserAuthenticated() && !selectedComments["All"] {
			alertKey := alert.GetFingerprint()
			if alertKey == "" {
				alertKey = fmt.Sprintf("%s_%s", alert.GetAlertName(), alert.GetInstance())
			}
			
			// Check comment status
			hasComments := aw.alertHasComments(alertKey)
			
			if selectedComments["Has Comments"] && !hasComments {
				continue
			}
			if selectedComments["No Comments"] && hasComments {
				continue
			}
		}

		filtered = append(filtered, alert)
	}

	// Clear selections when filters change
	aw.selectedAlerts = make(map[int]bool)
	if aw.selectionLabel != nil {
		aw.updateSelectionLabel()
	}

	aw.filteredData = filtered
	aw.sortFilteredData()
	aw.updateDashboard()

	if aw.table != nil {
		aw.table.Refresh()
	}
}

// updateTeamFilter populates the team filter with unique teams from alerts
func (aw *AlertsWindow) updateTeamFilter() {
	if aw.teamMultiSelect == nil {
		return
	}

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

	// Preserve current selection when updating options
	currentSelection := aw.teamMultiSelect.GetSelected()
	aw.teamMultiSelect.options = teamOptions

	// Restore selection, but only keep valid options
	validSelection := make(map[string]bool)
	for option, selected := range currentSelection {
		if selected {
			// Check if this option still exists in the new options
			for _, validOption := range teamOptions {
				if option == validOption {
					validSelection[option] = true
					break
				}
			}
		}
	}

	// If no valid selections remain, default to "All"
	if len(validSelection) == 0 {
		validSelection["All"] = true
	}

	aw.teamMultiSelect.selected = validSelection
	aw.teamMultiSelect.updateButton()
}

// clearFilters resets all filters to default state
func (aw *AlertsWindow) clearFilters() {
	aw.searchEntry.SetText("")

	// Reset multi-select filters to "All"
	if aw.severityMultiSelect != nil {
		aw.severityMultiSelect.selected = map[string]bool{"All": true}
		aw.severityMultiSelect.updateButton()
	}

	if aw.statusMultiSelect != nil {
		aw.statusMultiSelect.selected = map[string]bool{"All": true}
		aw.statusMultiSelect.updateButton()
	}

	if aw.teamMultiSelect != nil {
		aw.teamMultiSelect.selected = map[string]bool{"All": true}
		aw.teamMultiSelect.updateButton()
	}

	if aw.ackMultiSelect != nil {
		aw.ackMultiSelect.selected = map[string]bool{"All": true}
		aw.ackMultiSelect.updateButton()
	}

	if aw.commentMultiSelect != nil {
		aw.commentMultiSelect.selected = map[string]bool{"All": true}
		aw.commentMultiSelect.updateButton()
	}

	aw.focusSearchEntry()
	aw.safeApplyFilters()
	aw.saveFilterState()
}

// toggleGroupedMode toggles between grouped and flat table view
func (aw *AlertsWindow) toggleGroupedMode() {
	aw.groupedMode = !aw.groupedMode

	if aw.groupedMode {
		aw.groupToggleBtn.SetText("Flat View")
		aw.refreshGroupedTable()
	} else {
		aw.groupToggleBtn.SetText("Group View")
		// Clear selections when switching to flat mode
		aw.selectedAlerts = make(map[int]bool)
		aw.updateSelectionLabel()
	}

	// Recreate the entire UI to properly switch table modes
	aw.setupUI()
}

// toggleGroupExpansion toggles the expansion state of a group
func (aw *AlertsWindow) toggleGroupExpansion(groupIndex int) {
	if groupIndex < len(aw.alertGroups) {
		aw.alertGroups[groupIndex].IsExpanded = !aw.alertGroups[groupIndex].IsExpanded

		// Recreate table rows
		aw.tableRows = aw.createTableRowsFromGroups(aw.alertGroups)

		// Refresh table
		if aw.table != nil {
			aw.table.Refresh()
		}
	}
}

// expandAllGroups expands all alert groups
func (aw *AlertsWindow) expandAllGroups() {
	for i := range aw.alertGroups {
		aw.alertGroups[i].IsExpanded = true
	}
	aw.tableRows = aw.createTableRowsFromGroups(aw.alertGroups)
	if aw.table != nil {
		aw.table.Refresh()
	}
}

// collapseAllGroups collapses all alert groups
func (aw *AlertsWindow) collapseAllGroups() {
	for i := range aw.alertGroups {
		aw.alertGroups[i].IsExpanded = false
	}
	aw.tableRows = aw.createTableRowsFromGroups(aw.alertGroups)
	if aw.table != nil {
		aw.table.Refresh()
	}
}

// refreshGroupedTable refreshes the grouped table with current data
func (aw *AlertsWindow) refreshGroupedTable() {
	if !aw.groupedMode {
		return
	}

	// Preserve expansion states
	expansionStates := make(map[string]bool)
	for _, group := range aw.alertGroups {
		expansionStates[group.AlertName] = group.IsExpanded
	}

	// Recreate groups
	aw.alertGroups = aw.createGroupedAlertsFromFiltered()

	// Restore expansion states
	for i, group := range aw.alertGroups {
		if expanded, exists := expansionStates[group.AlertName]; exists {
			aw.alertGroups[i].IsExpanded = expanded
		}
	}

	// Recreate table rows
	aw.tableRows = aw.createTableRowsFromGroups(aw.alertGroups)

	// Clear selections as row indices have changed
	aw.selectedAlerts = make(map[int]bool)
	aw.updateSelectionLabel()

	if aw.table != nil {
		aw.table.Refresh()
	}
}

// selectGroupAlertsQuietly selects/deselects all alerts in a group without triggering table refresh
func (aw *AlertsWindow) selectGroupAlertsQuietly(groupIndex int, selected bool) {
	if groupIndex >= len(aw.alertGroups) {
		return
	}

	// Find all rows belonging to this group
	for _, row := range aw.tableRows {
		if row.Type == "alert" && row.GroupIndex == groupIndex {
			if selected {
				aw.selectedAlerts[row.RowIndex] = true
			} else {
				delete(aw.selectedAlerts, row.RowIndex)
			}
		}
	}
}

// areAllGroupAlertsSelected checks if all alerts in a group are selected
func (aw *AlertsWindow) areAllGroupAlertsSelected(groupIndex int) bool {
	if groupIndex >= len(aw.alertGroups) {
		return false
	}

	alertCount := 0
	selectedCount := 0

	for _, row := range aw.tableRows {
		if row.Type == "alert" && row.GroupIndex == groupIndex {
			alertCount++
			if aw.selectedAlerts[row.RowIndex] {
				selectedCount++
			}
		}
	}

	return alertCount > 0 && alertCount == selectedCount
}

// selectAllVisibleAlerts selects all currently visible alerts
func (aw *AlertsWindow) selectAllVisibleAlerts() {
	// Check if all are currently selected
	totalAlerts := 0
	selectedAlerts := 0

	for _, row := range aw.tableRows {
		if row.Type == "alert" {
			totalAlerts++
			if aw.selectedAlerts[row.RowIndex] {
				selectedAlerts++
			}
		}
	}

	allSelected := totalAlerts > 0 && totalAlerts == selectedAlerts

	if allSelected {
		// Deselect all
		aw.selectedAlerts = make(map[int]bool)
	} else {
		// Select all visible alerts
		for _, row := range aw.tableRows {
			if row.Type == "alert" {
				aw.selectedAlerts[row.RowIndex] = true
			}
		}
	}

	aw.updateSelectionLabel()
	if aw.table != nil {
		aw.table.Refresh()
	}
}

// handleTableClick handles clicks on table cells
func (aw *AlertsWindow) handleTableClick(cellID widget.TableCellID) {
	// Skip checkbox column clicks
	if cellID.Col == 0 {
		return
	}

	if cellID.Row > 0 && cellID.Row-1 < len(aw.tableRows) {
		row := aw.tableRows[cellID.Row-1]

		switch row.Type {
		case "group":
			// Toggle group expansion on click
			aw.toggleGroupExpansion(row.GroupIndex)
		case "alert":
			// Show alert details
			if row.Alert != nil {
				aw.showAlertDetails(*row.Alert)
			}
		}
	}
}

// getSelectedAlertsFromGrouped returns the actual alert objects that are selected in grouped mode
func (aw *AlertsWindow) getSelectedAlertsFromGrouped() []models.Alert {
	var selectedAlerts []models.Alert

	for _, row := range aw.tableRows {
		if row.Type == "group" && aw.selectedAlerts[row.RowIndex] && row.Group != nil {
			// Group is selected - add all alerts in the group
			selectedAlerts = append(selectedAlerts, row.Group.Alerts...)
		} else if row.Type == "alert" && aw.selectedAlerts[row.RowIndex] && row.Alert != nil {
			// Individual alert is selected
			selectedAlerts = append(selectedAlerts, *row.Alert)
		}
	}

	return selectedAlerts
}

// getSelectedAlertsForAction returns the actual alerts that are selected (works for both modes)
func (aw *AlertsWindow) getSelectedAlertsForAction() []models.Alert {
	if aw.groupedMode {
		return aw.getSelectedAlertsFromGrouped()
	} else {
		var selectedAlerts []models.Alert
		for index := range aw.selectedAlerts {
			if index < len(aw.filteredData) {
				selectedAlerts = append(selectedAlerts, aw.filteredData[index])
			}
		}
		return selectedAlerts
	}
}

// Event handlers
func (aw *AlertsWindow) handleRefresh() {
	aw.loadAlertsWithCaching()
}

func (aw *AlertsWindow) handleExport() {
	dialog.ShowInformation("Export", "Export functionality coming soon!", aw.window)
}

func (aw *AlertsWindow) handleSettings() {
	aw.showNotificationSettings()
}

func (aw *AlertsWindow) focusSearchEntry() {
	if aw.searchEntry != nil {
		aw.window.Canvas().Focus(aw.searchEntry)
	}
}

func (aw *AlertsWindow) setupKeyboardShortcuts() {
	aw.window.Canvas().SetOnTypedKey(func(key *fyne.KeyEvent) {
		// Update activity time for smart refresh
		aw.lastActivity = time.Now()

		if key.Name == fyne.KeyF && (key.Physical.ScanCode == 0 || key.Physical.ScanCode == 33) {
			aw.focusSearchEntry()
		}

		// Toggle grouped mode with Ctrl+G
		if key.Name == fyne.KeyG && (key.Physical.ScanCode == 0 || key.Physical.ScanCode == 34) {
			aw.toggleGroupedMode()
		}

		// Expand all groups with Ctrl+E
		if key.Name == fyne.KeyE && aw.groupedMode {
			aw.expandAllGroups()
		}

		// Collapse all groups with Ctrl+C
		if key.Name == fyne.KeyC && aw.groupedMode {
			aw.collapseAllGroups()
		}

		// Toggle background mode with Ctrl+B
		if key.Name == fyne.KeyB {
			aw.ToggleBackgroundMode()
		}

		// Hide to background with Ctrl+H
		if key.Name == fyne.KeyH {
			if aw.trayManager != nil {
				aw.trayManager.HideToBackground()
			}
		}

	})
}

// handleColumnSort handles column header clicks for sorting
func (aw *AlertsWindow) handleColumnSort(column int) {
	// Skip sorting for checkbox column
	if column == 0 {
		return
	}

	// Adjust column index for checkbox column
	adjustedColumn := column - 1

	if aw.sortColumn == adjustedColumn {
		// Same column, toggle sort direction
		aw.sortAscending = !aw.sortAscending
	} else {
		aw.sortColumn = adjustedColumn
		aw.sortAscending = true
	}

	// Update count caches for new data
	aw.updateCountCaches()
	
	// Re-apply filters which will trigger sorting
	aw.safeApplyFilters()
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
		case 3: // Acknowledgment
			// Sort by acknowledgment status (acknowledged alerts first when ascending)
			ackCountI := aw.getAcknowledgmentCountForSort(aw.filteredData[i])
			ackCountJ := aw.getAcknowledgmentCountForSort(aw.filteredData[j])
			result = ackCountI > ackCountJ // More acknowledgments = higher priority
		case 4: // Comments
			// Sort by comment count
			commentCountI := aw.getCommentCountForSort(aw.filteredData[i])
			commentCountJ := aw.getCommentCountForSort(aw.filteredData[j])
			result = commentCountI > commentCountJ // More comments = higher priority  
		case 5: // Team
			result = strings.Compare(aw.filteredData[i].GetTeam(), aw.filteredData[j].GetTeam()) < 0
		case 6: // Summary
			result = strings.Compare(aw.filteredData[i].GetSummary(), aw.filteredData[j].GetSummary()) < 0
		case 7: // Duration
			result = aw.filteredData[i].Duration() < aw.filteredData[j].Duration()
		case 8: // Instance
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

// detectResolvedAlerts detects alerts that were resolved (disappeared from active alerts)
func (aw *AlertsWindow) detectResolvedAlerts(newAlerts []models.Alert) {
	// Check if resolved alerts tracking is enabled
	if cfg, ok := aw.fullConfig.(*config.Config); ok && !cfg.ResolvedAlerts.Enabled {
		return
	}
	
	// Create a map of current alert fingerprints for quick lookup
	currentAlerts := make(map[string]bool)
	for _, alert := range newAlerts {
		currentAlerts[alert.GetFingerprint()] = true
	}
	
	// Check previous alerts to find resolved ones
	for _, prevAlert := range aw.alerts {
		fingerprint := prevAlert.GetFingerprint()
		
		// If alert is not in current alerts and not already in resolved cache, it's resolved
		if !currentAlerts[fingerprint] {
			if _, exists := aw.resolvedAlertsCache.Get(fingerprint); !exists {
				// Alert is resolved, add to cache and send notification
				aw.resolvedAlertsCache.Add(prevAlert)
				aw.sendResolvedAlertNotification(prevAlert)
			}
		}
	}
}

// sendResolvedAlertNotification sends a notification for a resolved alert
func (aw *AlertsWindow) sendResolvedAlertNotification(alert models.Alert) {
	// Check if resolved notifications are enabled
	if !aw.getResolvedNotificationsEnabled() {
		return
	}
	
	// Send system notification directly
	title := "Alert Resolved"
	message := fmt.Sprintf("🟢 %s has been resolved", alert.GetAlertName())
	aw.sendSystemNotification(title, message)
}

// sendSystemNotification sends a system notification
func (aw *AlertsWindow) sendSystemNotification(title, message string) {
	go func() {
		switch runtime.GOOS {
		case "windows":
			exec.Command("powershell", "-Command", fmt.Sprintf(`
				[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
				[Windows.UI.Notifications.ToastNotification, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
				[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
				$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
				$template.GetElementsByTagName("text")[0].AppendChild($template.CreateTextNode("%s")) | Out-Null
				$template.GetElementsByTagName("text")[1].AppendChild($template.CreateTextNode("%s")) | Out-Null
				$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
				[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Notificator").Show($toast)
			`, title, message)).Run()
		case "darwin":
			exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "%s" sound name "default"`, message, title)).Run()
		case "linux":
			if _, err := exec.LookPath("notify-send"); err == nil {
				exec.Command("notify-send", title, message, "-i", "dialog-information").Run()
			}
		}
	}()
}

// getResolvedNotificationsEnabled gets the resolved notifications setting
func (aw *AlertsWindow) getResolvedNotificationsEnabled() bool {
	if cfg, ok := aw.fullConfig.(*config.Config); ok {
		return cfg.ResolvedAlerts.Enabled && cfg.ResolvedAlerts.NotificationsEnabled
	}
	return true // Default enabled
}

// getAcknowledgmentCountForSort gets acknowledgment count for sorting using cache
func (aw *AlertsWindow) getAcknowledgmentCountForSort(alert models.Alert) int {
	// If backend is not available, return 0
	if !aw.hasBackendSupport() || !aw.isUserAuthenticated() {
		return 0
	}

	alertKey := alert.GetFingerprint()
	if alertKey == "" {
		alertKey = fmt.Sprintf("%s_%s", alert.GetAlertName(), alert.GetInstance())
	}

	aw.cacheMutex.RLock()
	count, exists := aw.ackCountCache[alertKey]
	aw.cacheMutex.RUnlock()

	if exists {
		return count
	}

	// If not in cache, load asynchronously and return 0 for now
	go aw.loadAcknowledgmentCountAsync(alertKey)
	return 0
}

// getCommentCountForSort gets comment count for sorting using cache
func (aw *AlertsWindow) getCommentCountForSort(alert models.Alert) int {
	// If backend is not available, return 0
	if !aw.hasBackendSupport() || !aw.isUserAuthenticated() {
		return 0
	}

	alertKey := alert.GetFingerprint()
	if alertKey == "" {
		alertKey = fmt.Sprintf("%s_%s", alert.GetAlertName(), alert.GetInstance())
	}

	aw.cacheMutex.RLock()
	count, exists := aw.commentCountCache[alertKey]
	aw.cacheMutex.RUnlock()

	if exists {
		return count
	}

	// If not in cache, load asynchronously and return 0 for now
	go aw.loadCommentCountAsync(alertKey)
	return 0
}

// loadAcknowledgmentCountAsync loads acknowledgment count and updates cache
func (aw *AlertsWindow) loadAcknowledgmentCountAsync(alertKey string) {
	acknowledgments, err := aw.getAlertAcknowledgments(alertKey)
	if err != nil {
		return
	}

	count := len(acknowledgments)
	aw.cacheMutex.Lock()
	aw.ackCountCache[alertKey] = count
	aw.cacheMutex.Unlock()
}

// loadCommentCountAsync loads comment count and updates cache
func (aw *AlertsWindow) loadCommentCountAsync(alertKey string) {
	comments, err := aw.getAlertComments(alertKey)
	if err != nil {
		return
	}

	count := len(comments)
	aw.cacheMutex.Lock()
	aw.commentCountCache[alertKey] = count
	aw.cacheMutex.Unlock()
}

// updateCountCaches updates both acknowledgment and comment count caches for all visible alerts
func (aw *AlertsWindow) updateCountCaches() {
	if !aw.hasBackendSupport() || !aw.isUserAuthenticated() {
		return
	}

	// Update cache for all filtered alerts
	for _, alert := range aw.filteredData {
		alertKey := alert.GetFingerprint()
		if alertKey == "" {
			alertKey = fmt.Sprintf("%s_%s", alert.GetAlertName(), alert.GetInstance())
		}

		// Load counts asynchronously
		go aw.loadAcknowledgmentCountAsync(alertKey)
		go aw.loadCommentCountAsync(alertKey)
	}
}

func (aw *AlertsWindow) stopAutoRefresh() {
	if aw.refreshTicker != nil {
		aw.refreshTicker.Stop()
		aw.refreshTicker = nil
	}
	if aw.refreshCancel != nil {
		aw.refreshCancel()
		aw.refreshCancel = nil
	}
}

// Utility methods
func (aw *AlertsWindow) setStatus(status string) {
	if aw.statusLabel != nil {
		fyne.Do(func() {
			aw.statusLabel.SetText(status)
		})
	}
}

func (aw *AlertsWindow) Show() {
	aw.window.ShowAndRun()
}

// Close method to clean up resources
func (aw *AlertsWindow) Close() {
	aw.stopAutoRefresh()
	close(aw.updateChan)
	aw.window.Close()
}

// Alert hiding methods

// hideSelectedAlerts hides all currently selected alerts
func (aw *AlertsWindow) hideSelectedAlerts() {
	selectedAlerts := aw.getSelectedAlertsForAction()
	if len(selectedAlerts) == 0 {
		dialog.ShowInformation("No Selection", "Please select alerts to hide using the checkboxes", aw.window)
		return
	}

	// Show reason dialog
	reasonEntry := widget.NewEntry()
	reasonEntry.SetPlaceHolder("Optional reason for hiding these alerts...")
	reasonEntry.MultiLine = true

	selectedCount := len(selectedAlerts)
	content := container.NewVBox(
		widget.NewLabelWithStyle(fmt.Sprintf("Hide %d selected alerts?", selectedCount),
			fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		widget.NewLabel("Reason (optional):"),
		reasonEntry,
		widget.NewSeparator(),
		widget.NewLabel("Hidden alerts will be removed from the main view but can be shown again later."),
	)

	dialog := dialog.NewCustomConfirm("Hide Alerts", "Hide", "Cancel", content, func(confirmed bool) {
		if confirmed {
			reason := reasonEntry.Text
			hiddenCount := 0

			// Hide each selected alert
			for _, alert := range selectedAlerts {
				if err := aw.hiddenAlertsCache.HideAlert(alert, reason); err != nil {
					log.Printf("Failed to hide alert %s: %v", alert.GetAlertName(), err)
				} else {
					hiddenCount++
				}
			}

			// Clear selections and refresh
			aw.selectedAlerts = make(map[int]bool)
			aw.safeApplyFilters()
			aw.updateHiddenCountDisplay()

			aw.setStatus(fmt.Sprintf("Hidden %d alerts", hiddenCount))
		}
	}, aw.window)

	dialog.Resize(fyne.NewSize(500, 300))
	dialog.Show()
}

// toggleShowHidden toggles between showing normal alerts and hidden alerts
func (aw *AlertsWindow) toggleShowHidden() {
	aw.showHiddenAlerts = !aw.showHiddenAlerts

	// Update the button text with count information
	aw.updateHiddenCountDisplay()

	if aw.hideSelectedBtn != nil {
		if aw.showHiddenAlerts {
			aw.hideSelectedBtn.SetText("Unhide Selected")
		} else {
			aw.hideSelectedBtn.SetText("Hide Selected")
		}
	}

	// Clear selections when switching views
	aw.selectedAlerts = make(map[int]bool)
	aw.updateSelectionLabel()
	aw.safeApplyFilters()

	aw.updateNotificationFilters()
}

// toggleShowResolved toggles between showing normal alerts and resolved alerts
func (aw *AlertsWindow) toggleShowResolved() {
	aw.showResolvedAlerts = !aw.showResolvedAlerts

	if aw.showResolvedAlerts {
		aw.showResolvedBtn.SetText("Show Normal Alerts")
		aw.showResolvedBtn.SetIcon(theme.ViewRefreshIcon())
	} else {
		count := aw.getResolvedAlertsCount()
		if count > 0 {
			aw.showResolvedBtn.SetText(fmt.Sprintf("Resolved (%d)", count))
		} else {
			aw.showResolvedBtn.SetText("Resolved Alerts")
		}
		aw.showResolvedBtn.SetIcon(theme.ConfirmIcon())
	}

	// Clear selections when switching views
	aw.selectedAlerts = make(map[int]bool)
	aw.updateSelectionLabel()
	aw.safeApplyFilters()

	aw.updateNotificationFilters()
}

// unhideSelectedAlerts unhides selected alerts (when viewing hidden alerts)
func (aw *AlertsWindow) unhideSelectedAlerts() {
	if !aw.showHiddenAlerts {
		return
	}

	selectedAlerts := aw.getSelectedAlertsForAction()
	if len(selectedAlerts) == 0 {
		dialog.ShowInformation("No Selection", "Please select alerts to unhide using the checkboxes", aw.window)
		return
	}

	selectedCount := len(selectedAlerts)
	content := widget.NewLabelWithStyle(
		fmt.Sprintf("Unhide %d selected alerts?\n\nThey will appear in the main alert view again.", selectedCount),
		fyne.TextAlignCenter, fyne.TextStyle{})

	dialog := dialog.NewConfirm("Unhide Alerts", content.Text, func(confirmed bool) {
		if confirmed {
			unhiddenCount := 0

			for _, alert := range selectedAlerts {
				if err := aw.hiddenAlertsCache.UnhideAlert(alert); err != nil {
					log.Printf("Failed to unhide alert %s: %v", alert.GetAlertName(), err)
				} else {
					unhiddenCount++
				}
			}

			// Clear selections and refresh
			aw.selectedAlerts = make(map[int]bool)
			aw.safeApplyFilters()
			aw.updateHiddenCountDisplay()

			aw.setStatus(fmt.Sprintf("Unhidden %d alerts", unhiddenCount))
		}
	}, aw.window)

	dialog.Show()
}

// updateBackendUI updates the backend button's appearance based on connection status
func (aw *AlertsWindow) updateBackendUI() {
	if aw.backendStatusBtn == nil {
		return
	}
	

	if aw.backendClient == nil {
		// Backend not configured
		aw.backendStatusBtn.SetIcon(theme.InfoIcon())
		aw.backendStatusBtn.SetText("")
		aw.backendStatusBtn.Importance = widget.LowImportance
		aw.backendStatusBtn.Refresh() // Force UI refresh
		return
	}

	if aw.backendConnected {
		if aw.backendAuthenticated {
			// Connected and authenticated - show blue button with validated emoji
			aw.backendStatusBtn.SetIcon(nil) // Clear icon
			aw.backendStatusBtn.SetText("✅") // Show emoji instead
			aw.backendStatusBtn.Importance = widget.SuccessImportance
			aw.backendStatusBtn.Refresh() // Force UI refresh
		} else {
			// Connected but not authenticated - show orange button with warning icon
			aw.backendStatusBtn.SetIcon(theme.WarningIcon())
			aw.backendStatusBtn.SetText("")
			aw.backendStatusBtn.Importance = widget.WarningImportance
			aw.backendStatusBtn.Refresh() // Force UI refresh
		}
	} else {
		// Disconnected - show red button with error icon
		aw.backendStatusBtn.SetIcon(theme.ErrorIcon())
		aw.backendStatusBtn.SetText("")
		aw.backendStatusBtn.Importance = widget.DangerImportance
		aw.backendStatusBtn.Refresh() // Force UI refresh
	}
}

// showBackendDropdownMenu shows a dropdown menu with backend options
func (aw *AlertsWindow) showBackendDropdownMenu() {
	if aw.backendClient == nil {
		// Backend not configured
		menu := fyne.NewMenu("Backend",
			fyne.NewMenuItem("Not Configured", func() {
				dialog.ShowInformation("Backend", "Backend is not configured in your settings", aw.window)
			}),
		)
		widget.ShowPopUpMenuAtPosition(menu, aw.window.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(aw.backendStatusBtn))
		return
	}

	// Create menu items based on connection state
	var menuItems []*fyne.MenuItem
	
	// Add Login/Logout based on authentication state
	if aw.backendAuthenticated {
		menuItems = append(menuItems, fyne.NewMenuItem("Logout", func() {
			aw.showLogoutDialog()
		}))
	} else {
		menuItems = append(menuItems, fyne.NewMenuItem("Login", func() {
			aw.showAuthDialog()
		}))
	}
	
	// Add separator
	menuItems = append(menuItems, fyne.NewMenuItemSeparator())
	
	// Add Connection Status
	menuItems = append(menuItems, fyne.NewMenuItem("Connection Status", func() {
		status := aw.backendClient.GetConnectionStatus()
		dialog.ShowInformation("Backend Status", status.String(), aw.window)
	}))
	
	// Add Reconnect
	menuItems = append(menuItems, fyne.NewMenuItem("Reconnect", func() {
		aw.backendClient.Reconnect()
	}))
	
	menu := fyne.NewMenu("Backend", menuItems...)
	widget.ShowPopUpMenuAtPosition(menu, aw.window.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(aw.backendStatusBtn))
}


func (aw *AlertsWindow) updateHiddenCountDisplay() {
	if aw.showHiddenBtn == nil || aw.hiddenAlertsCache == nil {
		return
	}

	count := aw.hiddenAlertsCache.GetHiddenCount()
	
	if aw.showHiddenAlerts {
		// When viewing hidden alerts, show "Show Normal Alerts"
		aw.showHiddenBtn.SetText("Show Normal Alerts")
	} else {
		// When viewing normal alerts, show "Hidden (X)" where X is the count
		if count > 0 {
			aw.showHiddenBtn.SetText(fmt.Sprintf("Hidden (%d)", count))
		} else {
			aw.showHiddenBtn.SetText("Hidden")
		}
	}
}

// updateResolvedCountDisplay updates the resolved alerts button text with count
func (aw *AlertsWindow) updateResolvedCountDisplay() {
	if aw.showResolvedBtn == nil {
		return
	}
	
	if aw.showResolvedAlerts {
		// When showing resolved alerts, button should say "Show Normal Alerts"
		aw.showResolvedBtn.SetText("Show Normal Alerts")
		aw.showResolvedBtn.SetIcon(theme.ViewRefreshIcon())
	} else {
		// When showing normal alerts, button should show resolved count
		count := aw.getResolvedAlertsCount()
		if count > 0 {
			aw.showResolvedBtn.SetText(fmt.Sprintf("Resolved (%d)", count))
		} else {
			aw.showResolvedBtn.SetText("Resolved Alerts")
		}
		aw.showResolvedBtn.SetIcon(theme.ConfirmIcon())
	}
}

// selectAllAlerts selects or deselects all visible alerts (flat mode)
func (aw *AlertsWindow) selectAllAlerts() {
	if aw.groupedMode {
		aw.selectAllVisibleAlerts()
		return
	}

	// Check if all are currently selected
	allSelected := len(aw.selectedAlerts) == len(aw.filteredData)

	if allSelected {
		// Deselect all
		aw.selectedAlerts = make(map[int]bool)
	} else {
		// Select all
		for i := 0; i < len(aw.filteredData); i++ {
			aw.selectedAlerts[i] = true
		}
	}

	// Update selection label and refresh table
	aw.updateSelectionLabel()
	if aw.table != nil {
		aw.table.Refresh()
	}
}

// getSelectedCount returns the number of currently selected alerts
func (aw *AlertsWindow) getSelectedCount() int {
	return len(aw.selectedAlerts)
}

// updateSelectionLabel updates the selection count label
func (aw *AlertsWindow) updateSelectionLabel() {
	if aw.selectionLabel == nil {
		return
	}

	count := aw.getSelectedCount()
	if count == 0 {
		aw.selectionLabel.SetText("No alerts selected")
	} else {
		aw.selectionLabel.SetText(fmt.Sprintf("%d alert(s) selected", count))
	}
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

// Configuration loading/saving methods
func (aw *AlertsWindow) loadColumnConfig() {
	if aw.configPath == "" {
		return
	}

	type ConfigFile struct {
		ColumnWidths map[string]float32 `json:"column_widths"`
	}

	if data, err := os.ReadFile(aw.configPath + ".gui"); err == nil {
		var config ConfigFile
		if json.Unmarshal(data, &config) == nil && config.ColumnWidths != nil {
			for i, col := range aw.columns {
				if width, exists := config.ColumnWidths[col.Name]; exists {
					aw.columns[i].Width = width
				}
			}
		}
	}
}

func (aw *AlertsWindow) saveColumnConfig() error {
	if aw.configPath == "" {
		return nil
	}

	type ConfigFile struct {
		ColumnWidths map[string]float32 `json:"column_widths"`
	}

	widths := make(map[string]float32)
	for _, col := range aw.columns {
		widths[col.Name] = col.Width
	}

	config := ConfigFile{
		ColumnWidths: widths,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal column config: %w", err)
	}

	if err := os.WriteFile(aw.configPath+".gui", data, 0644); err != nil {
		return fmt.Errorf("failed to write column config: %w", err)
	}
	return nil
}

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
		RespectFilters:   aw.notificationConfig.RespectFilters,
	}

	if data, err := json.MarshalIndent(config, "", "  "); err == nil {
		os.WriteFile(aw.configPath+".notifications", data, 0644)
	}
}

// Filter state persistence methods

// saveFilterState saves the current filter state to configuration
func (aw *AlertsWindow) saveFilterState() {
	if aw.originalConfig == nil || aw.configPath == "" {
		return
	}

	// Get current filter state
	var searchText string
	var selectedSeverities, selectedStatuses, selectedTeams, selectedAcks, selectedComments map[string]bool

	if aw.searchEntry != nil {
		searchText = aw.searchEntry.Text
	}

	if aw.severityMultiSelect != nil {
		selectedSeverities = aw.severityMultiSelect.GetSelected()
	} else {
		selectedSeverities = map[string]bool{"All": true}
	}

	if aw.statusMultiSelect != nil {
		selectedStatuses = aw.statusMultiSelect.GetSelected()
	} else {
		selectedStatuses = map[string]bool{"All": true}
	}

	if aw.teamMultiSelect != nil {
		selectedTeams = aw.teamMultiSelect.GetSelected()
	} else {
		selectedTeams = map[string]bool{"All": true}
	}

	if aw.ackMultiSelect != nil {
		selectedAcks = aw.ackMultiSelect.GetSelected()
	} else {
		selectedAcks = map[string]bool{"All": true}
	}

	if aw.commentMultiSelect != nil {
		selectedComments = aw.commentMultiSelect.GetSelected()
	} else {
		selectedComments = map[string]bool{"All": true}
	}

	// Update the original config structure
	aw.originalConfig.GUI.FilterState.SearchText = searchText
	aw.originalConfig.GUI.FilterState.SelectedSeverities = selectedSeverities
	aw.originalConfig.GUI.FilterState.SelectedStatuses = selectedStatuses
	aw.originalConfig.GUI.FilterState.SelectedTeams = selectedTeams
	aw.originalConfig.GUI.FilterState.SelectedAcks = selectedAcks
	aw.originalConfig.GUI.FilterState.SelectedComments = selectedComments

	// Save the original config to file
	if err := aw.originalConfig.SaveToFile(aw.configPath); err != nil {
		log.Printf("Failed to save filter state: %v", err)
	}
}

// loadFilterState loads the filter state from configuration
func (aw *AlertsWindow) loadFilterState() {
	if aw.config == nil {
		return
	}

	filterState := aw.config.GUI.FilterState

	// Restore search text
	if aw.searchEntry != nil && filterState.SearchText != "" {
		aw.searchEntry.SetText(filterState.SearchText)
	}

	// Restore severity filter
	if aw.severityMultiSelect != nil && filterState.SelectedSeverities != nil {
		aw.severityMultiSelect.selected = make(map[string]bool)
		for k, v := range filterState.SelectedSeverities {
			aw.severityMultiSelect.selected[k] = v
		}
		aw.severityMultiSelect.updateButton()
	}

	// Restore status filter
	if aw.statusMultiSelect != nil && filterState.SelectedStatuses != nil {
		aw.statusMultiSelect.selected = make(map[string]bool)
		for k, v := range filterState.SelectedStatuses {
			aw.statusMultiSelect.selected[k] = v
		}
		aw.statusMultiSelect.updateButton()
	}

	// Restore team filter
	if aw.teamMultiSelect != nil && filterState.SelectedTeams != nil {
		aw.teamMultiSelect.selected = make(map[string]bool)
		for k, v := range filterState.SelectedTeams {
			aw.teamMultiSelect.selected[k] = v
		}
		aw.teamMultiSelect.updateButton()
	}

	// Restore acknowledgment filter
	if aw.ackMultiSelect != nil && filterState.SelectedAcks != nil {
		aw.ackMultiSelect.selected = make(map[string]bool)
		for k, v := range filterState.SelectedAcks {
			aw.ackMultiSelect.selected[k] = v
		}
		aw.ackMultiSelect.updateButton()
	}

	// Restore comment filter
	if aw.commentMultiSelect != nil && filterState.SelectedComments != nil {
		aw.commentMultiSelect.selected = make(map[string]bool)
		for k, v := range filterState.SelectedComments {
			aw.commentMultiSelect.selected[k] = v
		}
		aw.commentMultiSelect.updateButton()
	}
}

// ToggleBackgroundMode toggles between normal and background mode
func (aw *AlertsWindow) ToggleBackgroundMode() {
	if aw.trayManager != nil {
		aw.trayManager.ToggleBackgroundMode()
		aw.isBackgroundMode = aw.trayManager.IsBackgroundMode()

		// Save state to config
		if aw.originalConfig != nil {
			aw.originalConfig.GUI.BackgroundMode = aw.isBackgroundMode
			aw.saveConfig()
		}
	}
}
func (aw *AlertsWindow) saveConfig() {
	if aw.originalConfig != nil && aw.configPath != "" {
		err := aw.originalConfig.SaveToFile(aw.configPath)
		if err != nil {
			log.Printf("Failed to save config: %v", err)
		}
	}
}
func (aw *AlertsWindow) IsBackgroundMode() bool {
	if aw.trayManager != nil {
		return aw.trayManager.IsBackgroundMode()
	}
	return aw.isBackgroundMode
}

// isAlertAcknowledged checks if an alert has any acknowledgments
func (aw *AlertsWindow) isAlertAcknowledged(alertKey string) bool {
	if !aw.isUserAuthenticated() {
		return false
	}
	
	acknowledgments, err := aw.getAlertAcknowledgments(alertKey)
	if err != nil {
		return false
	}
	
	return len(acknowledgments) > 0
}

// alertHasComments checks if an alert has any comments
func (aw *AlertsWindow) alertHasComments(alertKey string) bool {
	if !aw.isUserAuthenticated() {
		return false
	}
	
	comments, err := aw.getAlertComments(alertKey)
	if err != nil {
		return false
	}
	
	return len(comments) > 0
}

// tryAutoLogin attempts to auto-login using stored credentials
func (aw *AlertsWindow) tryAutoLogin() {
	// Wait for backend connection to be established
	for i := 0; i < 30; i++ { // Wait up to 30 seconds
		if aw.backendClient != nil && aw.backendClient.IsConnected() {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if aw.backendClient == nil || !aw.backendClient.IsConnected() {
		log.Printf("Backend not connected, skipping auto-login")
		return
	}

	// Create a temporary auth dialog to use credential loading methods
	authDialog := NewAuthDialog(aw)
	credentials, err := authDialog.loadCredentials()
	if err != nil {
		log.Printf("Failed to load credentials for auto-login: %v", err)
		return
	}

	if credentials != nil && credentials.RememberMe {
		log.Printf("Attempting auto-login for user: %s", credentials.Username)
		
		// Perform auto-login
		resp, err := aw.backendClient.Login(credentials.Username, credentials.Password)
		if err != nil {
			log.Printf("Auto-login failed: %v", err)
			return
		}

		if !resp.Success {
			log.Printf("Auto-login failed: %s", resp.Message)
			return
		}

		log.Printf("Auto-login successful for user: %s", resp.User.Username)
		
		// Update UI on main thread
		fyne.Do(func() {
			aw.updateUserInterface()
		})
	}
}
