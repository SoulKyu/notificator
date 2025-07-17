package notifier

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"

	"notificator/internal/audio"
	"notificator/internal/models"
)

// NotificationConfig holds notification settings
type NotificationConfig struct {
	Enabled           bool            `json:"enabled"`
	SoundEnabled      bool            `json:"sound_enabled"`
	SoundPath         string          `json:"sound_path"`
	AudioOutputDevice string          `json:"audio_output_device"`
	ShowSystem        bool            `json:"show_system"`
	CriticalOnly      bool            `json:"critical_only"`
	MaxNotifications  int             `json:"max_notifications"`
	CooldownSeconds   int             `json:"cooldown_seconds"`
	SeverityRules     map[string]bool `json:"severity_rules"`
	RespectFilters    bool            `json:"respect_filters"`
}

// FilterState represents the current UI filter state
type FilterState struct {
	SearchText            string
	SelectedAlertmanagers map[string]bool
	SelectedSeverities    map[string]bool
	SelectedStatuses      map[string]bool
	SelectedTeams         map[string]bool
	ShowHiddenAlerts      bool
}

// Notifier handles alert notifications
type Notifier struct {
	config            NotificationConfig
	app               fyne.App
	lastNotifications map[string]time.Time
	mutex             sync.RWMutex
	soundPlayer       SoundPlayer

	currentFilters *FilterState
	filterMutex    sync.RWMutex
}

// SoundPlayer interface for playing sounds
type SoundPlayer interface {
	PlaySound(soundPath string) error
	PlayDefaultSound(severity string) error
}

// DefaultSoundPlayer implements SoundPlayer
type DefaultSoundPlayer struct{}

// NewNotifier creates a new notification manager
func NewNotifier(config NotificationConfig, app fyne.App) *Notifier {
	// Set defaults if not configured
	if config.MaxNotifications == 0 {
		config.MaxNotifications = 5
	}
	if config.CooldownSeconds == 0 {
		config.CooldownSeconds = 300 // 5 minutes default cooldown
	}
	if config.SeverityRules == nil {
		config.SeverityRules = map[string]bool{
			"critical": true,
			"warning":  true,
			"info":     false,
			"unknown":  false,
		}
	}
	if config.AudioOutputDevice == "" {
		config.AudioOutputDevice = "default"
	}

	// Choose sound player based on configuration
	var soundPlayer SoundPlayer
	if config.AudioOutputDevice != "" && config.AudioOutputDevice != "default" {
		// Use device-aware sound player for specific devices
		soundPlayer = audio.NewDeviceSoundPlayer(config.AudioOutputDevice)
	} else {
		// Use default sound player
		soundPlayer = &DefaultSoundPlayer{}
	}

	return &Notifier{
		config:            config,
		app:               app,
		lastNotifications: make(map[string]time.Time),
		soundPlayer:       soundPlayer,
		currentFilters:    &FilterState{}, // Initialize with empty filters
	}
}

// UpdateFilters updates the current filter state for notification filtering
func (n *Notifier) UpdateFilters(filters FilterState) {
	n.filterMutex.Lock()
	defer n.filterMutex.Unlock()
	n.currentFilters = &filters
}

// GetCurrentFilters returns a copy of the current filter state
func (n *Notifier) GetCurrentFilters() FilterState {
	n.filterMutex.RLock()
	defer n.filterMutex.RUnlock()

	if n.currentFilters == nil {
		return FilterState{}
	}

	// Return a copy to avoid race conditions
	return FilterState{
		SearchText:            n.currentFilters.SearchText,
		SelectedAlertmanagers: copyStringBoolMap(n.currentFilters.SelectedAlertmanagers),
		SelectedSeverities:    copyStringBoolMap(n.currentFilters.SelectedSeverities),
		SelectedStatuses:      copyStringBoolMap(n.currentFilters.SelectedStatuses),
		SelectedTeams:         copyStringBoolMap(n.currentFilters.SelectedTeams),
		ShowHiddenAlerts:      n.currentFilters.ShowHiddenAlerts,
	}
}

// copyStringBoolMap creates a deep copy of a map[string]bool
func copyStringBoolMap(original map[string]bool) map[string]bool {
	if original == nil {
		return make(map[string]bool)
	}
	copy := make(map[string]bool, len(original))
	for k, v := range original {
		copy[k] = v
	}
	return copy
}

// matchesFilters checks if an alert matches the current UI filters
func (n *Notifier) matchesFilters(alert models.Alert) bool {
	if !n.config.RespectFilters {
		return true // If filter respect is disabled, all alerts pass
	}

	filters := n.GetCurrentFilters()

	// Apply search text filter
	if filters.SearchText != "" {
		searchText := strings.ToLower(filters.SearchText)
		searchMatch := strings.Contains(strings.ToLower(alert.GetAlertName()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetSummary()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetTeam()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetInstance()), searchText)
		if !searchMatch {
			return false
		}
	}

	// Apply severity filter
	if filters.SelectedSeverities != nil && !filters.SelectedSeverities["All"] &&
		!filters.SelectedSeverities[alert.GetSeverity()] {
		return false
	}

	// Apply status filter
	if filters.SelectedStatuses != nil && !filters.SelectedStatuses["All"] &&
		!filters.SelectedStatuses[alert.Status.State] {
		return false
	}

	// Apply team filter
	if filters.SelectedTeams != nil && !filters.SelectedTeams["All"] &&
		!filters.SelectedTeams[alert.GetTeam()] {
		return false
	}

	return true
}

// ProcessAlerts checks for new/changed alerts and sends notifications
func (n *Notifier) ProcessAlerts(newAlerts []models.Alert, previousAlerts []models.Alert) {
	if !n.config.Enabled {
		return
	}

	// Create maps for efficient lookup
	prevAlertsMap := make(map[string]models.Alert)
	for _, alert := range previousAlerts {
		key := n.getAlertKey(alert)
		prevAlertsMap[key] = alert
	}

	// Check for new or escalated alerts
	var notifiableAlerts []models.Alert

	for _, alert := range newAlerts {
		key := n.getAlertKey(alert)

		// Skip if alert doesn't match notification rules
		if !n.shouldNotify(alert) {
			continue
		}

		if !n.matchesFilters(alert) {
			continue
		}

		// Check if this is a new alert or status change
		if prevAlert, exists := prevAlertsMap[key]; exists {
			// Check if alert escalated (e.g., warning -> critical)
			if n.isEscalation(prevAlert, alert) {
				notifiableAlerts = append(notifiableAlerts, alert)
			}
		} else {
			// New alert
			if alert.IsActive() { // Only notify for active alerts
				notifiableAlerts = append(notifiableAlerts, alert)
			}
		}
	}

	// Send notifications for qualifying alerts
	n.sendNotifications(notifiableAlerts)
}

// shouldNotify determines if an alert should trigger a notification
func (n *Notifier) shouldNotify(alert models.Alert) bool {
	// Don't notify for silenced alerts
	if alert.IsSilenced() {
		return false
	}

	// Check severity rules
	if enabled, exists := n.config.SeverityRules[alert.GetSeverity()]; !enabled || !exists {
		return false
	}

	// Critical only mode
	if n.config.CriticalOnly && alert.GetSeverity() != "critical" {
		return false
	}

	// Check cooldown
	key := n.getAlertKey(alert)
	n.mutex.RLock()
	lastNotif, exists := n.lastNotifications[key]
	n.mutex.RUnlock()

	if exists && time.Since(lastNotif) < time.Duration(n.config.CooldownSeconds)*time.Second {
		return false
	}

	return true
}

// isEscalation checks if an alert has escalated in severity
func (n *Notifier) isEscalation(oldAlert, newAlert models.Alert) bool {
	severityOrder := map[string]int{
		"info":     1,
		"warning":  2,
		"critical": 3,
	}

	oldSev := severityOrder[oldAlert.GetSeverity()]
	newSev := severityOrder[newAlert.GetSeverity()]

	return newSev > oldSev
}

// sendNotifications sends notifications for the given alerts
func (n *Notifier) sendNotifications(alerts []models.Alert) {
	if len(alerts) == 0 {
		return
	}

	// Limit number of simultaneous notifications
	if len(alerts) > n.config.MaxNotifications {
		alerts = alerts[:n.config.MaxNotifications]
	}

	for _, alert := range alerts {
		go n.sendSingleNotification(alert)
	}
}

// sendSingleNotification sends a notification for a single alert
func (n *Notifier) sendSingleNotification(alert models.Alert) {
	key := n.getAlertKey(alert)

	// Update last notification time
	n.mutex.Lock()
	n.lastNotifications[key] = time.Now()
	n.mutex.Unlock()

	// Send system notification
	if n.config.ShowSystem {
		n.sendSystemNotification(alert)
	}

	// Play sound
	if n.config.SoundEnabled {
		n.playAlertSound(alert)
	}

	// Log notification with filter status
	filterStatus := ""
	if n.config.RespectFilters {
		filterStatus = " (filtered)"
	}
	log.Printf("Notification sent for alert: %s (severity: %s)%s", alert.GetAlertName(), alert.GetSeverity(), filterStatus)
}

// sendSystemNotification sends a system notification
func (n *Notifier) sendSystemNotification(alert models.Alert) {
	var title string
	message := fmt.Sprintf("%s\n%s", alert.GetAlertName(), alert.GetSummary())

	// Truncate message if too long
	if len(message) > 200 {
		message = message[:197] + "..."
	}

	// Add severity emoji to title for visual distinction
	switch alert.GetSeverity() {
	case "critical":
		title = "🔴 Critical Alert"
	case "warning":
		title = "🟡 Warning Alert"
	case "info":
		title = "🔵 Info Alert"
	default:
		title = "⚪ Alert"
	}

	notification := fyne.NewNotification(title, message)
	n.app.SendNotification(notification)
}

// playAlertSound plays a sound for the alert
func (n *Notifier) playAlertSound(alert models.Alert) {
	if n.config.SoundPath != "" {
		if err := n.soundPlayer.PlaySound(n.config.SoundPath); err != nil {
			log.Printf("Failed to play custom sound: %v", err)
			// Fallback to default sound
			n.soundPlayer.PlayDefaultSound(alert.GetSeverity())
		}
	} else {
		// Use built-in sound based on severity
		if err := n.soundPlayer.PlayDefaultSound(alert.GetSeverity()); err != nil {
			log.Printf("Failed to play default sound: %v", err)
		}
	}
}

// getAlertKey creates a unique key for an alert
func (n *Notifier) getAlertKey(alert models.Alert) string {
	return fmt.Sprintf("%s_%s_%s",
		alert.GetAlertName(),
		alert.GetInstance(),
		alert.Labels["job"])
}

// PlayDefaultSound plays system default sound based on severity
func (p *DefaultSoundPlayer) PlayDefaultSound(severity string) error {
	switch runtime.GOOS {
	case "linux":
		return p.playLinuxSound(severity)
	case "darwin":
		return p.playMacSound(severity)
	case "windows":
		return p.playWindowsSound(severity)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// PlaySound plays a custom sound file
func (p *DefaultSoundPlayer) PlaySound(soundPath string) error {
	if !fileExists(soundPath) {
		return fmt.Errorf("sound file not found: %s", soundPath)
	}

	switch runtime.GOOS {
	case "linux":
		return exec.Command("paplay", soundPath).Run()
	case "darwin":
		return exec.Command("afplay", soundPath).Run()
	case "windows":
		return exec.Command("powershell", "-c", fmt.Sprintf("(New-Object Media.SoundPlayer '%s').PlaySync();", soundPath)).Run()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Platform-specific default sound implementations
func (p *DefaultSoundPlayer) playLinuxSound(severity string) error {
	soundName := "dialog-information"
	switch severity {
	case "critical":
		soundName = "dialog-error"
	case "warning":
		soundName = "dialog-warning"
	}

	// Try multiple Linux sound systems
	commands := [][]string{
		{"paplay", "/usr/share/sounds/freedesktop/stereo/" + soundName + ".oga"},
		{"aplay", "/usr/share/sounds/alsa/Front_Left.wav"},
		{"speaker-test", "-t", "sine", "-f", "1000", "-l", "1"},
	}

	for _, cmd := range commands {
		if err := exec.Command(cmd[0], cmd[1:]...).Run(); err == nil {
			return nil
		}
	}

	// Fallback: terminal bell
	fmt.Print("\a")
	return nil
}

func (p *DefaultSoundPlayer) playMacSound(severity string) error {
	// Use different beep patterns for different severities
	switch severity {
	case "critical":
		// Multiple rapid beeps for critical
		return exec.Command("osascript", "-e", "beep 3").Run()
	case "warning":
		// Double beep for warning
		return exec.Command("osascript", "-e", "beep 2").Run()
	default:
		// Single beep for others
		return exec.Command("osascript", "-e", "beep 1").Run()
	}
}

func (p *DefaultSoundPlayer) playWindowsSound(severity string) error {
	// Use different beep frequencies for different severities
	switch severity {
	case "critical":
		// High frequency, longer duration for critical
		return exec.Command("powershell", "-c", "[console]::beep(1000,500)").Run()
	case "warning":
		// Medium frequency for warning
		return exec.Command("powershell", "-c", "[console]::beep(800,300)").Run()
	default:
		// Lower frequency for others
		return exec.Command("powershell", "-c", "[console]::beep(600,200)").Run()
	}
}

// Utility functions
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

// GetDefaultSoundPath returns a reasonable default sound path for the platform
func GetDefaultSoundPath() string {
	switch runtime.GOOS {
	case "linux":
		paths := []string{
			"/usr/share/sounds/freedesktop/stereo/dialog-error.oga",
			"/usr/share/sounds/alsa/Front_Left.wav",
			"/usr/share/sounds/sound-icons/prompt.wav",
		}
		for _, path := range paths {
			if fileExists(path) {
				return path
			}
		}
	case "darwin":
		return "/System/Library/Sounds/Glass.aiff"
	case "windows":
		return "C:\\Windows\\Media\\Windows Ding.wav"
	}
	return ""
}

// CreateDefaultNotificationConfig returns a default notification configuration
func CreateDefaultNotificationConfig() NotificationConfig {
	return NotificationConfig{
		Enabled:           true,
		SoundEnabled:      true,
		SoundPath:         GetDefaultSoundPath(),
		AudioOutputDevice: "default",
		ShowSystem:        true,
		CriticalOnly:      false,
		MaxNotifications:  5,
		CooldownSeconds:   300, // 5 minutes
		SeverityRules: map[string]bool{
			"critical": true,
			"warning":  true,
			"info":     false,
			"unknown":  false,
		},
		RespectFilters: true,
	}
}
