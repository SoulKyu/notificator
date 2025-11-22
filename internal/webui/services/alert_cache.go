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

func transformStatus(status string) string {
	switch status {
	case "suppressed":
		return "silenced"
	default:
		return status
	}
}

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

type AlertCache struct {
	mu                 sync.RWMutex
	alerts             map[string]*webuimodels.DashboardAlert // fingerprint -> alert
	userHiddenAlerts   map[string]map[string]bool             // userID -> fingerprint -> hidden
	alertmanagerClient *alertmanager.MultiClient
	backendClient      *client.BackendClient
	colorService       *ColorService

	// Configuration
	refreshInterval       time.Duration
	resolvedRetentionDays int // Days to keep resolved alerts

	// Change tracking
	newAlerts           []string // fingerprints of new alerts since last fetch
	resolvedAlertsSince []string // fingerprints of recently resolved alerts

	// Control channels
	ctx           context.Context
	cancel        context.CancelFunc
	refreshTicker *time.Ticker
}

func NewAlertCache(amClient *alertmanager.MultiClient, backendClient *client.BackendClient, resolvedRetentionDays int) *AlertCache {
	ctx, cancel := context.WithCancel(context.Background())

	// Ensure valid retention days
	if resolvedRetentionDays <= 0 {
		resolvedRetentionDays = 90 // Default to 90 days
	}

	return &AlertCache{
		alerts:                make(map[string]*webuimodels.DashboardAlert),
		userHiddenAlerts:      make(map[string]map[string]bool),
		alertmanagerClient:    amClient,
		backendClient:         backendClient,
		colorService:          NewColorService(backendClient),
		refreshInterval:       5 * time.Second,
		resolvedRetentionDays: resolvedRetentionDays,
		newAlerts:             make([]string, 0),
		resolvedAlertsSince:   make([]string, 0),
		ctx:                   ctx,
		cancel:                cancel,
	}
}

func (ac *AlertCache) Start() {
	ac.refreshAlerts()

	ac.refreshTicker = time.NewTicker(ac.refreshInterval)
	go ac.backgroundRefresh()
}

func (ac *AlertCache) Stop() {
	if ac.cancel != nil {
		ac.cancel()
	}
	if ac.refreshTicker != nil {
		ac.refreshTicker.Stop()
	}
}

func (ac *AlertCache) SetRefreshInterval(interval time.Duration) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.refreshInterval = interval
	if ac.refreshTicker != nil {
		ac.refreshTicker.Stop()
		ac.refreshTicker = time.NewTicker(interval)
	}
}

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

func (ac *AlertCache) refreshAlerts() {
	if ac.alertmanagerClient == nil {
		return
	}

	alertsWithSource, err := ac.alertmanagerClient.FetchAllAlerts()
	if err != nil {
		log.Printf("Failed to fetch alerts: %v", err)
		return
	}

	log.Printf("Alert cache refresh: fetched %d alerts from Alertmanager", len(alertsWithSource))

	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.newAlerts = make([]string, 0)
	ac.resolvedAlertsSince = make([]string, 0)

	currentFingerprints := make(map[string]bool)

	for _, alertWithSource := range alertsWithSource {
		dashAlert := ac.convertToDashboardAlert(alertWithSource.Alert, alertWithSource.Source)
		fingerprint := dashAlert.Fingerprint

		currentFingerprints[fingerprint] = true

		if existingAlert, exists := ac.alerts[fingerprint]; !exists {
			ac.alerts[fingerprint] = dashAlert
			ac.newAlerts = append(ac.newAlerts, fingerprint)

			// Capture alert fired event for statistics
			go func(alert *webuimodels.DashboardAlert) {
				if ac.backendClient != nil && ac.backendClient.IsConnected() {
					if err := ac.backendClient.CaptureAlertFired(alert); err != nil {
						log.Printf("Failed to capture alert fired statistics for %s: %v", alert.Fingerprint, err)
					}
				}
			}(dashAlert)

		} else {
			ac.updateExistingAlert(existingAlert, dashAlert)
		}
	}

	resolvedCount := 0
	for fingerprint, alert := range ac.alerts {
		if !currentFingerprints[fingerprint] {
			log.Printf("Alert cache: marking alert %s as resolved (not found in current fetch)", fingerprint)
			alert.IsResolved = true
			alert.ResolvedAt = time.Now()
			alert.Status.State = "resolved"
			alert.EndsAt = alert.ResolvedAt

			// Update alert resolved event for statistics
			go func(resolvedAlert *webuimodels.DashboardAlert) {
				if ac.backendClient != nil && ac.backendClient.IsConnected() {
					if err := ac.backendClient.UpdateAlertResolved(resolvedAlert); err != nil {
						log.Printf("Failed to update alert resolved statistics for %s: %v", resolvedAlert.Fingerprint, err)
					}
				}
			}(alert)

			// Capture complete alert data with comments and acknowledgments for backend storage
			go ac.storeResolvedAlertInBackend(alert)

			delete(ac.alerts, fingerprint)

			ac.resolvedAlertsSince = append(ac.resolvedAlertsSince, fingerprint)
			resolvedCount++
		}
	}

	log.Printf("Alert cache refresh complete: %d active alerts, %d newly resolved", len(ac.alerts), resolvedCount)

	ac.loadBackendData()
}

func (ac *AlertCache) convertToDashboardAlert(alert models.Alert, source string) *webuimodels.DashboardAlert {
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
		UpdatedAt: time.Now(),

		AlertName:  alert.GetAlertName(),
		Severity:   transformSeverity(alert.GetSeverity()),
		Instance:   alert.GetInstance(),
		Team:       alert.GetTeam(),
		Summary:    alert.GetSummary(),
		IsResolved: transformedStatus == "resolved",
	}

	if dashAlert.IsResolved {
		dashAlert.Duration = int64(alert.EndsAt.Sub(alert.StartsAt).Seconds())
	} else {
		dashAlert.Duration = int64(time.Since(alert.StartsAt).Seconds())
	}

	if groupName, exists := alert.Labels["group"]; exists {
		dashAlert.GroupName = groupName
	} else if alertName, exists := alert.Labels["alertname"]; exists {
		dashAlert.GroupName = alertName
	}

	return dashAlert
}

// func (ac *AlertCache) alertHasChanged(old, new *webuimodels.DashboardAlert) bool {
// 	return old.Status.State != new.Status.State ||
// 		len(old.Status.SilencedBy) != len(new.Status.SilencedBy) ||
// 		len(old.Status.InhibitedBy) != len(new.Status.InhibitedBy) ||
// 		!old.EndsAt.Equal(new.EndsAt)
// }

func (ac *AlertCache) updateExistingAlert(existing, new *webuimodels.DashboardAlert) {
	existing.Status = new.Status
	existing.EndsAt = new.EndsAt
	existing.UpdatedAt = new.UpdatedAt
	existing.Duration = new.Duration
	existing.IsResolved = new.IsResolved

	existing.Annotations = new.Annotations
}

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

	go ac.loadAcknowledgmentsEfficiently()
	go ac.loadCommentCountsEfficiently()
}

func (ac *AlertCache) loadAcknowledgmentsEfficiently() {
	log.Printf("Loading all acknowledged alerts from backend...")

	acknowledgedAlerts, err := ac.backendClient.GetAllAcknowledgedAlerts()
	if err != nil {
		log.Printf("Failed to load acknowledged alerts from backend: %v", err)
		return
	}

	log.Printf("Received %d acknowledged alerts from backend", len(acknowledgedAlerts))

	ac.mu.Lock()
	for fingerprint, acknowledgment := range acknowledgedAlerts {
		if alert, exists := ac.alerts[fingerprint]; exists {
			alert.IsAcknowledged = true
			alert.AcknowledgedBy = acknowledgment.Username
			alert.AcknowledgedAt = acknowledgment.CreatedAt.AsTime()
			alert.AcknowledgeReason = acknowledgment.Reason

			// Note: Comment counts are loaded separately by loadCommentCountsEfficiently()
			// Note: We don't capture statistics here because this is loading historical
			// acknowledgments. Statistics should only be captured when alerts are
			// acknowledged in real-time to avoid negative MTTR calculations.
		}
	}
	ac.mu.Unlock()

	log.Printf("Successfully updated %d alerts with acknowledgment data", len(acknowledgedAlerts))
}

func (ac *AlertCache) loadCommentCountsEfficiently() {
	log.Printf("Loading comment counts for all alerts...")

	ac.mu.Lock()
	defer ac.mu.Unlock()

	count := 0
	for fingerprint, alert := range ac.alerts {
		if comments, err := ac.backendClient.GetComments(fingerprint); err == nil {
			alert.CommentCount = len(comments)
			if len(comments) > 0 {
				count++
			}
		} else {
			alert.CommentCount = 0
		}
	}

	log.Printf("Successfully loaded comment counts for %d alerts (%d with comments)", len(ac.alerts), count)
}

func (ac *AlertCache) GetAllAlerts() []*webuimodels.DashboardAlert {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	alerts := make([]*webuimodels.DashboardAlert, 0, len(ac.alerts))
	for _, alert := range ac.alerts {
		alerts = append(alerts, alert)
	}

	return alerts
}

func (ac *AlertCache) GetResolvedAlerts() []*webuimodels.DashboardAlert {
	return ac.GetResolvedAlertsWithPagination(0, 0)
}

func (ac *AlertCache) GetResolvedAlertsWithLimit(limit int) []*webuimodels.DashboardAlert {
	if limit <= 0 {
		return ac.GetResolvedAlerts()
	}
	return ac.GetResolvedAlertsWithPagination(limit, 0)
}

func (ac *AlertCache) GetResolvedAlertsWithPagination(limit, offset int) []*webuimodels.DashboardAlert {
	if ac.backendClient == nil || !ac.backendClient.IsConnected() {
		log.Printf("Backend client not available for fetching resolved alerts")
		return []*webuimodels.DashboardAlert{}
	}

	resolvedAlertInfos, err := ac.backendClient.GetResolvedAlerts(limit, offset)
	if err != nil {
		log.Printf("Error fetching resolved alerts from backend: %v", err)
		return []*webuimodels.DashboardAlert{}
	}

	alerts := make([]*webuimodels.DashboardAlert, 0, len(resolvedAlertInfos))
	for _, resolvedInfo := range resolvedAlertInfos {
		var dashAlert webuimodels.DashboardAlert
		if err := json.Unmarshal(resolvedInfo.AlertData, &dashAlert); err != nil {
			log.Printf("Error deserializing resolved alert data for %s: %v", resolvedInfo.Fingerprint, err)
			continue
		}

		dashAlert.ResolvedAt = resolvedInfo.ResolvedAt.AsTime()
		dashAlert.IsResolved = true
		dashAlert.Status.State = "resolved"

		alerts = append(alerts, &dashAlert)
	}

	log.Printf("Fetched %d resolved alerts from backend", len(alerts))
	return alerts
}

func (ac *AlertCache) GetAlert(fingerprint string) (*webuimodels.DashboardAlert, bool) {
	ac.mu.RLock()

	if alert, exists := ac.alerts[fingerprint]; exists {
		ac.mu.RUnlock()
		return alert, true
	}

	ac.mu.RUnlock()

	if ac.backendClient != nil && ac.backendClient.IsConnected() {
		if resolvedInfo, err := ac.backendClient.GetResolvedAlert(fingerprint); err == nil {
			var dashAlert webuimodels.DashboardAlert
			if err := json.Unmarshal(resolvedInfo.AlertData, &dashAlert); err == nil {
				dashAlert.ResolvedAt = resolvedInfo.ResolvedAt.AsTime()
				dashAlert.IsResolved = true
				dashAlert.Status.State = "resolved"
				return &dashAlert, true
			}
		}
	}

	return nil, false
}

func (ac *AlertCache) GetNewAlertsSinceLastFetch() []string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	result := make([]string, len(ac.newAlerts))
	copy(result, ac.newAlerts)
	return result
}

func (ac *AlertCache) GetResolvedAlertsSinceLastFetch() []string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	result := make([]string, len(ac.resolvedAlertsSince))
	copy(result, ac.resolvedAlertsSince)
	return result
}

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

func (ac *AlertCache) IsAlertHidden(userID, fingerprint string) bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	if userHidden, exists := ac.userHiddenAlerts[userID]; exists {
		return userHidden[fingerprint]
	}

	return false
}

func (ac *AlertCache) GetAlertByFingerprint(fingerprint string) *webuimodels.DashboardAlert {
	if alert, exists := ac.GetAlert(fingerprint); exists {
		return alert
	}
	return nil
}

func (ac *AlertCache) GetAlertColors(fingerprint, userID string) *AlertColorResult {
	alert := ac.GetAlertByFingerprint(fingerprint)
	if alert == nil {
		return nil
	}

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

func (ac *AlertCache) GetAllAlertColors(userID string) map[string]*AlertColorResult {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	var modelAlerts []*models.Alert
	fingerprintMap := make(map[*models.Alert]string)

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

	colorResults := ac.colorService.GetAlertColorsOptimized(modelAlerts, userID)

	result := make(map[string]*AlertColorResult)
	for modelAlert, fingerprint := range fingerprintMap {
		if colorResult, exists := colorResults[modelAlert.GetFingerprint()]; exists {
			result[fingerprint] = colorResult
		}
	}

	return result
}

func (ac *AlertCache) InvalidateColorCache(userID string) {
	ac.colorService.InvalidateUserCache(userID)
}

func (ac *AlertCache) storeResolvedAlertInBackend(alert *webuimodels.DashboardAlert) {
	if ac.backendClient == nil || !ac.backendClient.IsConnected() {
		log.Printf("Backend client not available for storing resolved alert %s", alert.Fingerprint)
		return
	}

	log.Printf("Storing resolved alert %s in backend with complete data", alert.Fingerprint)

	alertData, err := json.Marshal(alert)
	if err != nil {
		log.Printf("Error serializing alert data for %s: %v", alert.Fingerprint, err)
		return
	}

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

	// Convert days to hours for backend API
	ttlHours := ac.resolvedRetentionDays * 24

	if err := ac.backendClient.CreateResolvedAlert(
		alert.Fingerprint,
		alert.Source,
		alertData,
		comments,
		acknowledgments,
		ttlHours,
	); err != nil {
		log.Printf("Error storing resolved alert %s in backend: %v", alert.Fingerprint, err)
	} else {
		log.Printf("Successfully stored resolved alert %s in backend", alert.Fingerprint)
	}
}

func (ac *AlertCache) RemoveAllResolvedAlerts() error {
	if ac.backendClient == nil || !ac.backendClient.IsConnected() {
		return fmt.Errorf("backend client not available")
	}

	log.Printf("Removing all resolved alerts from backend")

	if err := ac.backendClient.RemoveAllResolvedAlerts(); err != nil {
		log.Printf("Error removing all resolved alerts from backend: %v", err)
		return err
	}

	log.Printf("Successfully removed all resolved alerts from backend")
	return nil
}
