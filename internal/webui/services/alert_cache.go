package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"notificator/internal/alertmanager"
	"notificator/internal/models"
	"notificator/internal/webui/client"
	webuimodels "notificator/internal/webui/models"
)

// transformStatus converts Alertmanager status values to UI-friendly values
func transformStatus(status string) string {
	switch status {
	case "suppressed":
		return "silenced"
	default:
		return status
	}
}

// transformSeverity converts Alertmanager severity values to normalized values
func transformSeverity(severity string) string {
	switch strings.ToLower(severity) {
	case "information":
		return "info"
	case "critical-daytime":
		return "critical"
	default:
		return strings.ToLower(severity)
	}
}

// AlertCache manages the cached alerts with background refresh and change detection
type AlertCache struct {
	mu                    sync.RWMutex
	alerts                map[string]*webuimodels.DashboardAlert // fingerprint -> alert
	userHiddenAlerts      map[string]map[string]bool             // userID -> fingerprint -> hidden
	alertmanagerClient    *alertmanager.MultiClient
	backendClient         *client.BackendClient
	colorService          *ColorService
	
	// Configuration
	refreshInterval       time.Duration
	
	// Change tracking
	newAlerts             []string // fingerprints of new alerts since last fetch
	resolvedAlertsSince   []string // fingerprints of recently resolved alerts
	
	// Control channels
	ctx                   context.Context
	cancel                context.CancelFunc
	refreshTicker         *time.Ticker
	
	// Callbacks for notifications
	onNewAlert            func(alert *webuimodels.DashboardAlert)
	onResolvedAlert       func(alert *webuimodels.DashboardAlert)
	onAlertChanged        func(alert *webuimodels.DashboardAlert)
}

// NewAlertCache creates a new alert cache service
func NewAlertCache(amClient *alertmanager.MultiClient, backendClient *client.BackendClient) *AlertCache {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &AlertCache{
		alerts:              make(map[string]*webuimodels.DashboardAlert),
		userHiddenAlerts:    make(map[string]map[string]bool),
		alertmanagerClient:  amClient,
		backendClient:       backendClient,
		colorService:        NewColorService(backendClient),
		refreshInterval:     5 * time.Second,
		newAlerts:           make([]string, 0),
		resolvedAlertsSince: make([]string, 0),
		ctx:                 ctx,
		cancel:              cancel,
	}
}

// Start begins the background refresh process
func (ac *AlertCache) Start() {
	// Initial load
	ac.refreshAlerts()
	
	// Start periodic refresh
	ac.refreshTicker = time.NewTicker(ac.refreshInterval)
	go ac.backgroundRefresh()
}

// Stop stops the background refresh process
func (ac *AlertCache) Stop() {
	if ac.cancel != nil {
		ac.cancel()
	}
	if ac.refreshTicker != nil {
		ac.refreshTicker.Stop()
	}
}

// SetRefreshInterval updates the refresh interval
func (ac *AlertCache) SetRefreshInterval(interval time.Duration) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	
	ac.refreshInterval = interval
	if ac.refreshTicker != nil {
		ac.refreshTicker.Stop()
		ac.refreshTicker = time.NewTicker(interval)
	}
}


// SetNotificationCallbacks sets callback functions for alert changes
func (ac *AlertCache) SetNotificationCallbacks(
	onNew func(alert *webuimodels.DashboardAlert),
	onResolved func(alert *webuimodels.DashboardAlert),
	onChange func(alert *webuimodels.DashboardAlert),
) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	
	ac.onNewAlert = onNew
	ac.onResolvedAlert = onResolved
	ac.onAlertChanged = onChange
}

// backgroundRefresh runs the periodic refresh
func (ac *AlertCache) backgroundRefresh() {
	for {
		select {
		case <-ac.ctx.Done():
			return
		case <-ac.refreshTicker.C:
			ac.refreshAlerts()
		}
	}
}

// refreshAlerts fetches alerts from all Alertmanagers and updates the cache
func (ac *AlertCache) refreshAlerts() {
	if ac.alertmanagerClient == nil {
		return
	}

	// Fetch alerts from all configured Alertmanagers
	alertsWithSource, err := ac.alertmanagerClient.FetchAllAlerts()
	if err != nil {
		log.Printf("Failed to fetch alerts: %v", err)
		return
	}
	
	log.Printf("Alert cache refresh: fetched %d alerts from Alertmanager", len(alertsWithSource))

	ac.mu.Lock()
	defer ac.mu.Unlock()

	// Reset change tracking
	ac.newAlerts = make([]string, 0)
	ac.resolvedAlertsSince = make([]string, 0)

	// Track current fingerprints
	currentFingerprints := make(map[string]bool)
	
	// Process fetched alerts
	for _, alertWithSource := range alertsWithSource {
		dashAlert := ac.convertToDashboardAlert(alertWithSource.Alert, alertWithSource.Source)
		fingerprint := dashAlert.Fingerprint
		
		currentFingerprints[fingerprint] = true
		
		// Check if this is a new alert
		if existingAlert, exists := ac.alerts[fingerprint]; !exists {
			// New alert
			ac.alerts[fingerprint] = dashAlert
			ac.newAlerts = append(ac.newAlerts, fingerprint)
			
			// Call new alert callback
			if ac.onNewAlert != nil {
				go ac.onNewAlert(dashAlert)
			}
		} else {
			// Check for changes
			hasChanged := ac.alertHasChanged(existingAlert, dashAlert)
			
			// Update the alert
			ac.updateExistingAlert(existingAlert, dashAlert)
			
			if hasChanged && ac.onAlertChanged != nil {
				go ac.onAlertChanged(dashAlert)
			}
		}
	}

	// Check for resolved alerts (alerts that are no longer in the current fetch)
	resolvedCount := 0
	for fingerprint, alert := range ac.alerts {
		if !currentFingerprints[fingerprint] {
			// Alert has been resolved (removed from Alertmanager)
			log.Printf("Alert cache: marking alert %s as resolved (not found in current fetch)", fingerprint)
			alert.IsResolved = true
			alert.ResolvedAt = time.Now()
			alert.Status.State = "resolved"
			alert.EndsAt = alert.ResolvedAt
			
			// Capture complete alert data with comments and acknowledgments for backend storage
			go ac.storeResolvedAlertInBackend(alert)
			
			// Remove from active alerts (no longer store in memory)
			delete(ac.alerts, fingerprint)
			
			ac.resolvedAlertsSince = append(ac.resolvedAlertsSince, fingerprint)
			resolvedCount++
			
			// Call resolved alert callback
			if ac.onResolvedAlert != nil {
				go ac.onResolvedAlert(alert)
			}
		}
	}
	
	log.Printf("Alert cache refresh complete: %d active alerts, %d newly resolved", len(ac.alerts), resolvedCount)
	
	// Load acknowledgments and comments from backend
	ac.loadBackendData()
}

// convertToDashboardAlert converts a basic alert to a dashboard alert
func (ac *AlertCache) convertToDashboardAlert(alert models.Alert, source string) *webuimodels.DashboardAlert {
	// Transform labels to include normalized severity
	transformedLabels := make(map[string]string)
	for key, value := range alert.Labels {
		if key == "severity" {
			transformedLabels[key] = transformSeverity(value)
		} else {
			transformedLabels[key] = value
		}
	}
	
	// Create a normalized alert for consistent fingerprint generation
	// This ensures fingerprints are always calculated from normalized labels
	normalizedAlert := models.Alert{
		Labels:       transformedLabels, // Use transformed labels for fingerprint
		Annotations:  alert.Annotations,
		StartsAt:     alert.StartsAt,
		EndsAt:       alert.EndsAt,
		GeneratorURL: alert.GeneratorURL,
		Status:       alert.Status,
		Source:       alert.Source,
	}
	
	transformedStatus := transformStatus(alert.Status.State)
	
	dashAlert := &webuimodels.DashboardAlert{
		Fingerprint:  normalizedAlert.GetFingerprint(), // Use normalized alert for fingerprint
		Labels:       transformedLabels,
		Annotations:  alert.Annotations,
		StartsAt:     alert.StartsAt,
		EndsAt:       alert.EndsAt,
		GeneratorURL: alert.GeneratorURL,
		Source:       source,
		Status: webuimodels.AlertStatus{
			State:       transformedStatus,
			SilencedBy:  alert.Status.SilencedBy,
			InhibitedBy: alert.Status.InhibitedBy,
		},
		UpdatedAt:    time.Now(),
		
		// Computed fields
		AlertName:    alert.GetAlertName(),
		Severity:     transformSeverity(alert.GetSeverity()),
		Instance:     alert.GetInstance(),
		Team:         alert.GetTeam(),
		Summary:      alert.GetSummary(),
		IsResolved:   transformedStatus == "resolved",
	}
	
	// Calculate duration
	if dashAlert.IsResolved {
		dashAlert.Duration = int64(alert.EndsAt.Sub(alert.StartsAt).Seconds())
	} else {
		dashAlert.Duration = int64(time.Since(alert.StartsAt).Seconds())
	}
	
	// Extract group name from labels (if available)
	if groupName, exists := alert.Labels["group"]; exists {
		dashAlert.GroupName = groupName
	} else if alertName, exists := alert.Labels["alertname"]; exists {
		dashAlert.GroupName = alertName // Fallback to alert name
	}
	
	return dashAlert
}

// alertHasChanged checks if an alert has meaningful changes
func (ac *AlertCache) alertHasChanged(old, new *webuimodels.DashboardAlert) bool {
	return old.Status.State != new.Status.State ||
		   len(old.Status.SilencedBy) != len(new.Status.SilencedBy) ||
		   len(old.Status.InhibitedBy) != len(new.Status.InhibitedBy) ||
		   !old.EndsAt.Equal(new.EndsAt)
}

// updateExistingAlert updates an existing alert with new data
func (ac *AlertCache) updateExistingAlert(existing, new *webuimodels.DashboardAlert) {
	// Update core fields that can change
	existing.Status = new.Status
	existing.EndsAt = new.EndsAt
	existing.UpdatedAt = new.UpdatedAt
	existing.Duration = new.Duration
	existing.IsResolved = new.IsResolved
	
	// Update annotations in case they changed
	existing.Annotations = new.Annotations
}

// loadBackendData loads acknowledgments from the backend efficiently
func (ac *AlertCache) loadBackendData() {
	log.Printf("loadBackendData called - checking backend connection...")
	if ac.backendClient == nil {
		log.Printf("Backend client is nil - skipping acknowledgment loading")
		return
	}
	if !ac.backendClient.IsConnected() {
		log.Printf("Backend client not connected - skipping acknowledgment loading")
		return
	}
	log.Printf("Backend client is connected - proceeding with acknowledgment loading")
	
	// Load acknowledgments asynchronously to avoid blocking
	go ac.loadAcknowledgmentsEfficiently()
}

// loadAcknowledgmentsEfficiently loads all acknowledgments in a single gRPC call
func (ac *AlertCache) loadAcknowledgmentsEfficiently() {
	log.Printf("Loading all acknowledged alerts from backend...")
	
	// Make a single gRPC call to get all acknowledged alerts
	acknowledgedAlerts, err := ac.backendClient.GetAllAcknowledgedAlerts()
	if err != nil {
		log.Printf("Failed to load acknowledged alerts from backend: %v", err)
		return
	}
	
	log.Printf("Received %d acknowledged alerts from backend", len(acknowledgedAlerts))
	
	// Update alerts that have acknowledgments
	ac.mu.Lock()
	for fingerprint, acknowledgment := range acknowledgedAlerts {
		if alert, exists := ac.alerts[fingerprint]; exists {
			alert.IsAcknowledged = true
			alert.AcknowledgedBy = acknowledgment.Username
			alert.AcknowledgedAt = acknowledgment.CreatedAt.AsTime()
			alert.AcknowledgeReason = acknowledgment.Reason
			alert.CommentCount = 1 // We have at least one acknowledgment
		}
	}
	ac.mu.Unlock()
	
	log.Printf("Successfully updated %d alerts with acknowledgment data", len(acknowledgedAlerts))
}



// GetAllAlerts returns all active alerts
func (ac *AlertCache) GetAllAlerts() []*webuimodels.DashboardAlert {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	
	alerts := make([]*webuimodels.DashboardAlert, 0, len(ac.alerts))
	for _, alert := range ac.alerts {
		alerts = append(alerts, alert)
	}
	
	return alerts
}

// GetResolvedAlerts returns all resolved alerts from the backend
func (ac *AlertCache) GetResolvedAlerts() []*webuimodels.DashboardAlert {
	return ac.GetResolvedAlertsWithPagination(0, 0) // 0,0 means no pagination
}

// GetResolvedAlertsWithPagination returns resolved alerts from the backend with pagination
func (ac *AlertCache) GetResolvedAlertsWithPagination(limit, offset int) []*webuimodels.DashboardAlert {
	if ac.backendClient == nil || !ac.backendClient.IsConnected() {
		log.Printf("Backend client not available for fetching resolved alerts")
		return []*webuimodels.DashboardAlert{}
	}

	// Fetch resolved alerts from backend
	resolvedAlertInfos, err := ac.backendClient.GetResolvedAlerts(limit, offset)
	if err != nil {
		log.Printf("Error fetching resolved alerts from backend: %v", err)
		return []*webuimodels.DashboardAlert{}
	}

	// Convert protobuf ResolvedAlertInfo to DashboardAlert
	alerts := make([]*webuimodels.DashboardAlert, 0, len(resolvedAlertInfos))
	for _, resolvedInfo := range resolvedAlertInfos {
		// Deserialize the alert data from JSON
		var dashAlert webuimodels.DashboardAlert
		if err := json.Unmarshal(resolvedInfo.AlertData, &dashAlert); err != nil {
			log.Printf("Error deserializing resolved alert data for %s: %v", resolvedInfo.Fingerprint, err)
			continue
		}

		// Update timestamps from the resolved alert record
		dashAlert.ResolvedAt = resolvedInfo.ResolvedAt.AsTime()
		dashAlert.IsResolved = true
		dashAlert.Status.State = "resolved"

		alerts = append(alerts, &dashAlert)
	}

	log.Printf("Fetched %d resolved alerts from backend", len(alerts))
	return alerts
}

// GetAlert returns a specific alert by fingerprint
func (ac *AlertCache) GetAlert(fingerprint string) (*webuimodels.DashboardAlert, bool) {
	ac.mu.RLock()
	
	// Check active alerts first
	if alert, exists := ac.alerts[fingerprint]; exists {
		ac.mu.RUnlock()
		return alert, true
	}
	
	ac.mu.RUnlock()
	
	// Check resolved alerts in backend
	if ac.backendClient != nil && ac.backendClient.IsConnected() {
		if resolvedInfo, err := ac.backendClient.GetResolvedAlert(fingerprint); err == nil {
			// Deserialize the alert data from JSON
			var dashAlert webuimodels.DashboardAlert
			if err := json.Unmarshal(resolvedInfo.AlertData, &dashAlert); err == nil {
				// Update timestamps from the resolved alert record
				dashAlert.ResolvedAt = resolvedInfo.ResolvedAt.AsTime()
				dashAlert.IsResolved = true
				dashAlert.Status.State = "resolved"
				return &dashAlert, true
			}
		}
	}
	
	return nil, false
}

// GetNewAlertsSinceLastFetch returns fingerprints of new alerts
func (ac *AlertCache) GetNewAlertsSinceLastFetch() []string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	
	result := make([]string, len(ac.newAlerts))
	copy(result, ac.newAlerts)
	return result
}

// GetResolvedAlertsSinceLastFetch returns fingerprints of recently resolved alerts
func (ac *AlertCache) GetResolvedAlertsSinceLastFetch() []string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	
	result := make([]string, len(ac.resolvedAlertsSince))
	copy(result, ac.resolvedAlertsSince)
	return result
}

// SetAlertHidden marks an alert as hidden for a specific user
func (ac *AlertCache) SetAlertHidden(userID, fingerprint string, hidden bool) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	
	if ac.userHiddenAlerts[userID] == nil {
		ac.userHiddenAlerts[userID] = make(map[string]bool)
	}
	
	if hidden {
		ac.userHiddenAlerts[userID][fingerprint] = true
	} else {
		delete(ac.userHiddenAlerts[userID], fingerprint)
	}
	
	// Update the alert if it exists
	if alert, exists := ac.alerts[fingerprint]; exists {
		alert.IsHidden = hidden
		if hidden {
			alert.HiddenBy = userID
			alert.HiddenAt = time.Now()
		}
	}
}

// IsAlertHidden checks if an alert is hidden for a specific user
func (ac *AlertCache) IsAlertHidden(userID, fingerprint string) bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	
	if userHidden, exists := ac.userHiddenAlerts[userID]; exists {
		return userHidden[fingerprint]
	}
	
	return false
}

// GetAlertByFingerprint retrieves a specific alert by its fingerprint
func (ac *AlertCache) GetAlertByFingerprint(fingerprint string) *webuimodels.DashboardAlert {
	if alert, exists := ac.GetAlert(fingerprint); exists {
		return alert
	}
	return nil
}

// GetAlertColors returns color configuration for a single alert
func (ac *AlertCache) GetAlertColors(fingerprint, userID string) *AlertColorResult {
	alert := ac.GetAlertByFingerprint(fingerprint)
	if alert == nil {
		return nil
	}

	// Convert dashboard alert to models.Alert for color service
	modelAlert := &models.Alert{
		Labels:       alert.Labels,
		Annotations:  alert.Annotations,
		StartsAt:     alert.StartsAt,
		EndsAt:       alert.EndsAt,
		GeneratorURL: alert.GeneratorURL,
		Source:       alert.Source,
		Status: models.AlertStatus{
			State:       alert.Status.State,
			SilencedBy:  alert.Status.SilencedBy,
			InhibitedBy: alert.Status.InhibitedBy,
		},
	}

	return ac.colorService.GetAlertColors(modelAlert, userID)
}

// GetAllAlertColors returns color configurations for all alerts for a user (optimized)
func (ac *AlertCache) GetAllAlertColors(userID string) map[string]*AlertColorResult {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	// Convert dashboard alerts to models.Alert slice
	var modelAlerts []*models.Alert
	fingerprintMap := make(map[*models.Alert]string) // reverse mapping

	for fingerprint, dashAlert := range ac.alerts {
		modelAlert := &models.Alert{
			Labels:       dashAlert.Labels,
			Annotations:  dashAlert.Annotations,
			StartsAt:     dashAlert.StartsAt,
			EndsAt:       dashAlert.EndsAt,
			GeneratorURL: dashAlert.GeneratorURL,
			Source:       dashAlert.Source,
			Status: models.AlertStatus{
				State:       dashAlert.Status.State,
				SilencedBy:  dashAlert.Status.SilencedBy,
				InhibitedBy: dashAlert.Status.InhibitedBy,
			},
		}
		modelAlerts = append(modelAlerts, modelAlert)
		fingerprintMap[modelAlert] = fingerprint
	}

	// Get colors for all alerts efficiently
	colorResults := ac.colorService.GetAlertColorsOptimized(modelAlerts, userID)

	// Convert back to fingerprint-based map
	result := make(map[string]*AlertColorResult)
	for modelAlert, fingerprint := range fingerprintMap {
		if colorResult, exists := colorResults[modelAlert.GetFingerprint()]; exists {
			result[fingerprint] = colorResult
		}
	}

	return result
}

// InvalidateColorCache invalidates the color cache for a user
func (ac *AlertCache) InvalidateColorCache(userID string) {
	ac.colorService.InvalidateUserCache(userID)
}

// storeResolvedAlertInBackend captures complete alert data and stores it in the backend
func (ac *AlertCache) storeResolvedAlertInBackend(alert *webuimodels.DashboardAlert) {
	if ac.backendClient == nil || !ac.backendClient.IsConnected() {
		log.Printf("Backend client not available for storing resolved alert %s", alert.Fingerprint)
		return
	}

	log.Printf("Storing resolved alert %s in backend with complete data", alert.Fingerprint)

	// Serialize the complete DashboardAlert as JSON
	alertData, err := json.Marshal(alert)
	if err != nil {
		log.Printf("Error serializing alert data for %s: %v", alert.Fingerprint, err)
		return
	}

	// Get comments for this alert
	var comments []byte
	if ac.backendClient != nil {
		if commentsData, err := ac.backendClient.GetComments(alert.Fingerprint); err == nil {
			if commentsJSON, err := json.Marshal(commentsData); err == nil {
				comments = commentsJSON
			} else {
				log.Printf("Error serializing comments for %s: %v", alert.Fingerprint, err)
			}
		} else {
			log.Printf("Error fetching comments for %s: %v", alert.Fingerprint, err)
		}
	}

	// Get acknowledgments for this alert  
	var acknowledgments []byte
	if ac.backendClient != nil {
		if acksData, err := ac.backendClient.GetAcknowledgments(alert.Fingerprint); err == nil {
			if acksJSON, err := json.Marshal(acksData); err == nil {
				acknowledgments = acksJSON
			} else {
				log.Printf("Error serializing acknowledgments for %s: %v", alert.Fingerprint, err)
			}
		} else {
			log.Printf("Error fetching acknowledgments for %s: %v", alert.Fingerprint, err)
		}
	}

	// Store resolved alert in backend via gRPC
	if err := ac.backendClient.CreateResolvedAlert(
		alert.Fingerprint,
		alert.Source,
		alertData,
		comments,
		acknowledgments,
		24, // TTL in hours (default 24)
	); err != nil {
		log.Printf("Error storing resolved alert %s in backend: %v", alert.Fingerprint, err)
	} else {
		log.Printf("Successfully stored resolved alert %s in backend", alert.Fingerprint)
	}
}

// RemoveAllResolvedAlerts removes all resolved alerts from the backend
func (ac *AlertCache) RemoveAllResolvedAlerts() error {
	if ac.backendClient == nil || !ac.backendClient.IsConnected() {
		return fmt.Errorf("backend client not available")
	}

	log.Printf("Removing all resolved alerts from backend")
	
	// Call backend to remove all resolved alerts
	if err := ac.backendClient.RemoveAllResolvedAlerts(); err != nil {
		log.Printf("Error removing all resolved alerts from backend: %v", err)
		return err
	}

	log.Printf("Successfully removed all resolved alerts from backend")
	return nil
}