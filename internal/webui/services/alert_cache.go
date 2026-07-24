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
	default:
		return strings.ToLower(severity)
	}
}

// alertFetcher abstracts alertmanager.MultiClient so tests can fake fetches.
type alertFetcher interface {
	FetchAllAlertsDetailed() ([]alertmanager.AlertWithSource, map[string]error)
}

// maxBackendWorkers bounds concurrent backend calls spawned by a refresh cycle,
// so a large diff cannot stampede the backend with thousands of gRPC calls.
const maxBackendWorkers = 8

type AlertCache struct {
	mu                 sync.RWMutex
	alerts             map[string]*webuimodels.DashboardAlert // fingerprint -> alert
	userHiddenAlerts   map[string]map[string]bool             // userID -> fingerprint -> hidden
	alertmanagerClient alertFetcher
	backendClient      *client.BackendClient
	colorService       *ColorService
	backendSem         chan struct{} // semaphore bounding refresh-triggered backend calls

	// Color caching - keyed by userID then fingerprint
	colorsMutex  sync.RWMutex
	cachedColors map[string]map[string]*AlertColorResult // userID -> fingerprint -> color result

	// Configuration
	refreshInterval       time.Duration
	resolvedRetentionDays int // Days to keep resolved alerts

	// Change tracking
	newAlerts           []string // fingerprints of new alerts since last fetch
	resolvedAlertsSince []string // fingerprints of recently resolved alerts

	// SSE pub/sub - subscribers for real-time updates
	subscribers map[chan *webuimodels.DashboardIncrementalUpdate]bool
	subMutex    sync.RWMutex

	// Control channels
	ctx           context.Context
	cancel        context.CancelFunc
	refreshTicker *time.Ticker
	tickerMu      sync.Mutex // guards refreshTicker pointer only
}

func NewAlertCache(amClient *alertmanager.MultiClient, backendClient *client.BackendClient, resolvedRetentionDays int, syncInterval time.Duration) *AlertCache {
	ctx, cancel := context.WithCancel(context.Background())

	// Ensure valid retention days
	if resolvedRetentionDays <= 0 {
		resolvedRetentionDays = 90 // Default to 90 days
	}

	// Ensure valid sync interval (default to 10 seconds if not provided or invalid)
	if syncInterval <= 0 {
		syncInterval = 10 * time.Second
	}

	// Avoid storing a typed-nil *MultiClient in the interface field, which would
	// defeat the nil check in refreshAlerts.
	var fetcher alertFetcher
	if amClient != nil {
		fetcher = amClient
	}

	return &AlertCache{
		alerts:                make(map[string]*webuimodels.DashboardAlert),
		userHiddenAlerts:      make(map[string]map[string]bool),
		alertmanagerClient:    fetcher,
		backendClient:         backendClient,
		colorService:          NewColorService(backendClient),
		cachedColors:          make(map[string]map[string]*AlertColorResult),
		backendSem:            make(chan struct{}, maxBackendWorkers),
		refreshInterval:       syncInterval,
		resolvedRetentionDays: resolvedRetentionDays,
		newAlerts:             make([]string, 0),
		resolvedAlertsSince:   make([]string, 0),
		subscribers:           make(map[chan *webuimodels.DashboardIncrementalUpdate]bool),
		ctx:                   ctx,
		cancel:                cancel,
	}
}

func (ac *AlertCache) Start() {
	ac.refreshAlerts()

	ac.tickerMu.Lock()
	ac.refreshTicker = time.NewTicker(ac.refreshInterval)
	ac.tickerMu.Unlock()
	go ac.backgroundRefresh()
}

func (ac *AlertCache) Stop() {
	if ac.cancel != nil {
		ac.cancel()
	}
	ac.tickerMu.Lock()
	if ac.refreshTicker != nil {
		ac.refreshTicker.Stop()
	}
	ac.tickerMu.Unlock()
}

func (ac *AlertCache) SetRefreshInterval(interval time.Duration) {
	ac.mu.Lock()
	ac.refreshInterval = interval
	ac.mu.Unlock()

	ac.tickerMu.Lock()
	if ac.refreshTicker != nil {
		ac.refreshTicker.Stop()
		ac.refreshTicker = time.NewTicker(interval)
	}
	ac.tickerMu.Unlock()
}

func (ac *AlertCache) backgroundRefresh() {
	for {
		ac.tickerMu.Lock()
		tickerC := ac.refreshTicker.C
		ac.tickerMu.Unlock()

		select {
		case <-ac.ctx.Done():
			return
		case <-tickerC:
			ac.refreshAlerts()
		}
	}
}

// runBounded runs fn on a goroutine while holding a slot in backendSem, so at
// most maxBackendWorkers refresh-triggered backend calls run concurrently.
func (ac *AlertCache) runBounded(fn func()) {
	go func() {
		ac.backendSem <- struct{}{}
		defer func() { <-ac.backendSem }()
		fn()
	}()
}

func (ac *AlertCache) refreshAlerts() {
	if ac.alertmanagerClient == nil {
		return
	}

	alertsWithSource, fetchErrors := ac.alertmanagerClient.FetchAllAlertsDetailed()
	for source, fetchErr := range fetchErrors {
		log.Printf("Alert cache refresh: failed to fetch alerts from %s, keeping its cached alerts untouched: %v", source, fetchErr)
	}
	if len(alertsWithSource) == 0 && len(fetchErrors) > 0 {
		// No source returned anything usable; leave the cache as-is.
		return
	}

	log.Printf("Alert cache refresh: fetched %d alerts from Alertmanager", len(alertsWithSource))

	ac.mu.Lock()

	ac.newAlerts = make([]string, 0)
	ac.resolvedAlertsSince = make([]string, 0)

	// Track alerts for SSE notification
	var newAlertsForSSE []*webuimodels.DashboardAlert
	var updatedAlertsForSSE []*webuimodels.DashboardAlert

	currentFingerprints := make(map[string]bool)

	for _, alertWithSource := range alertsWithSource {
		dashAlert := ac.convertToDashboardAlert(alertWithSource.Alert, alertWithSource.Source)
		fingerprint := dashAlert.Fingerprint

		currentFingerprints[fingerprint] = true

		if existingAlert, exists := ac.alerts[fingerprint]; !exists {
			ac.alerts[fingerprint] = dashAlert
			ac.newAlerts = append(ac.newAlerts, fingerprint)
			newAlertsForSSE = append(newAlertsForSSE, dashAlert)

			// Capture alert fired event for statistics
			ac.runBounded(func() {
				if ac.backendClient != nil && ac.backendClient.IsConnected() {
					if err := ac.backendClient.CaptureAlertFired(dashAlert); err != nil {
						log.Printf("Failed to capture alert fired statistics for %s: %v", dashAlert.Fingerprint, err)
					}
				}
			})

		} else {
			// Check if alert changed before updating
			if ac.hasAlertChanged(existingAlert, dashAlert) {
				updatedAlertsForSSE = append(updatedAlertsForSSE, dashAlert)
			}
			ac.updateExistingAlert(existingAlert, dashAlert)
		}
	}

	resolvedCount := 0
	var removedFingerprints []string
	for fingerprint, alert := range ac.alerts {
		if !currentFingerprints[fingerprint] {
			// A fingerprint missing from a source that failed to answer this cycle
			// says nothing about the alert's state: keep it untouched instead of
			// mass-resolving an entire unreachable Alertmanager.
			if _, sourceFailed := fetchErrors[alert.Source]; sourceFailed {
				continue
			}

			log.Printf("Alert cache: marking alert %s as resolved (not found in current fetch)", fingerprint)
			alert.IsResolved = true
			alert.ResolvedAt = time.Now()
			alert.Status.State = "resolved"
			alert.EndsAt = alert.ResolvedAt

			// Update alert resolved event for statistics
			ac.runBounded(func() {
				if ac.backendClient != nil && ac.backendClient.IsConnected() {
					if err := ac.backendClient.UpdateAlertResolved(alert); err != nil {
						log.Printf("Failed to update alert resolved statistics for %s: %v", alert.Fingerprint, err)
					}
				}
			})

			// Capture complete alert data with comments and acknowledgments for backend storage.
			// Copy the struct before spawning the goroutine so it operates on a snapshot,
			// not the cache-resident pointer which may be mutated by concurrent writers.
			alertCopy := *alert
			ac.runBounded(func() { ac.storeResolvedAlertInBackend(&alertCopy) })

			delete(ac.alerts, fingerprint)

			ac.resolvedAlertsSince = append(ac.resolvedAlertsSince, fingerprint)
			removedFingerprints = append(removedFingerprints, fingerprint)
			resolvedCount++
		}
	}

	ac.mu.Unlock()

	log.Printf("Alert cache refresh complete: %d active alerts, %d newly resolved", len(ac.alerts), resolvedCount)

	ac.loadBackendData()

	// Refresh color cache for all active users after alerts are updated
	go ac.RefreshAllCachedColors()

	// Notify SSE subscribers if there are any changes
	if len(newAlertsForSSE) > 0 || len(updatedAlertsForSSE) > 0 || len(removedFingerprints) > 0 {
		update := &webuimodels.DashboardIncrementalUpdate{
			NewAlerts:      newAlertsForSSE,
			UpdatedAlerts:  updatedAlertsForSSE,
			RemovedAlerts:  removedFingerprints,
			LastUpdateTime: time.Now().Unix(),
		}
		ac.notifySubscribers(update)
	}
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
	// Check if alert has meaningfully changed before updating UpdatedAt
	if ac.hasAlertChanged(existing, new) {
		existing.UpdatedAt = time.Now()
	}
	// Note: UpdatedAt is NOT updated if alert hasn't changed

	existing.Status = new.Status
	existing.EndsAt = new.EndsAt
	existing.Duration = new.Duration
	existing.IsResolved = new.IsResolved

	existing.Annotations = new.Annotations
}

// hasAlertChanged compares two alerts to determine if there are meaningful changes
// that warrant updating the UpdatedAt timestamp.
// It compares: Status, IsAcknowledged, CommentCount, Summary
func (ac *AlertCache) hasAlertChanged(existing, new *webuimodels.DashboardAlert) bool {
	// Check Status.State change
	if existing.Status.State != new.Status.State {
		return true
	}

	// Check IsAcknowledged change
	if existing.IsAcknowledged != new.IsAcknowledged {
		return true
	}

	// Check CommentCount change
	if existing.CommentCount != new.CommentCount {
		return true
	}

	// Check Summary change
	if existing.Summary != new.Summary {
		return true
	}

	return false
}

// UpdateAlert adds or updates an alert in the cache with proper UpdatedAt tracking.
// If the alert is new, UpdatedAt is set to current time.
// If the alert exists and has changed, UpdatedAt is updated.
// If the alert exists but hasn't changed, UpdatedAt is preserved.
func (ac *AlertCache) UpdateAlert(alert *webuimodels.DashboardAlert) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if existingAlert, exists := ac.alerts[alert.Fingerprint]; exists {
		// Alert exists - check for changes
		if ac.hasAlertChanged(existingAlert, alert) {
			// Alert has changed - update UpdatedAt
			existingAlert.UpdatedAt = time.Now()
		}
		// Note: If alert hasn't changed, UpdatedAt is preserved

		// Update other fields
		existingAlert.Status = alert.Status
		existingAlert.IsAcknowledged = alert.IsAcknowledged
		existingAlert.CommentCount = alert.CommentCount
		existingAlert.Summary = alert.Summary
		existingAlert.EndsAt = alert.EndsAt
		existingAlert.Duration = alert.Duration
		existingAlert.IsResolved = alert.IsResolved
		existingAlert.Annotations = alert.Annotations
	} else {
		// New alert - set UpdatedAt to current time
		alert.UpdatedAt = time.Now()
		ac.alerts[alert.Fingerprint] = alert
	}
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
	log.Printf("Loading acknowledgments for cached alerts from backend...")

	// Step 1: collect fingerprints under RLock (no write needed)
	ac.mu.RLock()
	fingerprints := make([]string, 0, len(ac.alerts))
	for fingerprint := range ac.alerts {
		fingerprints = append(fingerprints, fingerprint)
	}
	ac.mu.RUnlock()

	// Handle empty case without a backend round-trip
	if len(fingerprints) == 0 {
		log.Printf("No alerts to load acknowledgments for")
		return
	}

	// Step 2: call gRPC with NO lock held
	acknowledgedAlerts, err := ac.backendClient.GetAllAcknowledgedAlerts(fingerprints)
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
	log.Printf("Loading comment counts for all alerts using batch query...")

	// Step 1: collect fingerprints under RLock (no write needed)
	ac.mu.RLock()
	fingerprints := make([]string, 0, len(ac.alerts))
	for fingerprint := range ac.alerts {
		fingerprints = append(fingerprints, fingerprint)
	}
	ac.mu.RUnlock()

	// Handle empty case
	if len(fingerprints) == 0 {
		log.Printf("No alerts to load comment counts for")
		return
	}

	// Step 2: call gRPC with NO lock held
	counts, err := ac.backendClient.GetCommentCountsBatch(fingerprints)
	if err != nil {
		log.Printf("Failed to load comment counts batch: %v", err)
		// Reset all comment counts to 0 on error
		ac.mu.Lock()
		for _, alert := range ac.alerts {
			alert.CommentCount = 0
		}
		ac.mu.Unlock()
		return
	}

	// Step 3: write results back under Lock
	alertsWithComments := 0
	ac.mu.Lock()
	for fingerprint, alert := range ac.alerts {
		if count, exists := counts[fingerprint]; exists {
			alert.CommentCount = count
			if count > 0 {
				alertsWithComments++
			}
		} else {
			alert.CommentCount = 0
		}
	}
	totalAlerts := len(ac.alerts)
	ac.mu.Unlock()

	log.Printf("Successfully loaded comment counts for %d alerts (%d with comments) using batch query", totalAlerts, alertsWithComments)
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

	// Also clear the cached alert colors for this user
	ac.colorsMutex.Lock()
	delete(ac.cachedColors, userID)
	ac.colorsMutex.Unlock()
}

// RefreshColorCache refreshes the cached colors for a specific user.
// This should be called during background sync after alerts are updated.
func (ac *AlertCache) RefreshColorCache(userID string) {
	ac.mu.RLock()
	alertCount := len(ac.alerts)
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
	ac.mu.RUnlock()

	if alertCount == 0 {
		return
	}

	// Calculate colors using the color service (this fetches user preferences if needed)
	colorResults := ac.colorService.GetAlertColorsOptimized(modelAlerts, userID)

	// Build the result map keyed by fingerprint
	result := make(map[string]*AlertColorResult)
	for modelAlert, fingerprint := range fingerprintMap {
		if colorResult, exists := colorResults[modelAlert.GetFingerprint()]; exists {
			result[fingerprint] = colorResult
		}
	}

	// Update the cache
	ac.colorsMutex.Lock()
	ac.cachedColors[userID] = result
	ac.colorsMutex.Unlock()

	log.Printf("Color cache refreshed for user %s: %d alerts", userID, len(result))
}

// GetCachedAlertColors returns cached alert colors for a user.
// If colors are not cached for this user, it will calculate and cache them.
// Returns a map of fingerprint -> AlertColorResult.
func (ac *AlertCache) GetCachedAlertColors(userID string) map[string]*AlertColorResult {
	ac.colorsMutex.RLock()
	cached, exists := ac.cachedColors[userID]
	ac.colorsMutex.RUnlock()

	if exists && len(cached) > 0 {
		// Return a copy to avoid race conditions
		result := make(map[string]*AlertColorResult, len(cached))
		for k, v := range cached {
			result[k] = v
		}
		return result
	}

	// Cache miss - refresh and return
	ac.RefreshColorCache(userID)

	ac.colorsMutex.RLock()
	defer ac.colorsMutex.RUnlock()

	cached = ac.cachedColors[userID]
	if cached == nil {
		return make(map[string]*AlertColorResult)
	}

	// Return a copy
	result := make(map[string]*AlertColorResult, len(cached))
	for k, v := range cached {
		result[k] = v
	}
	return result
}

// GetCachedUserIDs returns a list of user IDs that have cached colors.
// This is useful for refreshing colors for all active users during background sync.
func (ac *AlertCache) GetCachedUserIDs() []string {
	ac.colorsMutex.RLock()
	defer ac.colorsMutex.RUnlock()

	userIDs := make([]string, 0, len(ac.cachedColors))
	for userID := range ac.cachedColors {
		userIDs = append(userIDs, userID)
	}
	return userIDs
}

// RefreshAllCachedColors refreshes colors for all users that have cached colors.
// This is called during background sync after alerts are updated.
func (ac *AlertCache) RefreshAllCachedColors() {
	userIDs := ac.GetCachedUserIDs()
	if len(userIDs) == 0 {
		return
	}

	log.Printf("Refreshing color cache for %d users", len(userIDs))
	for _, userID := range userIDs {
		ac.RefreshColorCache(userID)
	}
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

func (ac *AlertCache) RemoveAllResolvedAlerts(sessionID string) error {
	if ac.backendClient == nil || !ac.backendClient.IsConnected() {
		return fmt.Errorf("backend client not available")
	}

	log.Printf("Removing all resolved alerts from backend")

	if err := ac.backendClient.RemoveAllResolvedAlerts(sessionID); err != nil {
		log.Printf("Error removing all resolved alerts from backend: %v", err)
		return err
	}

	log.Printf("Successfully removed all resolved alerts from backend")
	return nil
}

// Subscribe creates a new channel and registers it for receiving incremental updates.
// Returns a channel that will receive DashboardIncrementalUpdate when alerts change.
// The caller must call Unsubscribe when done to prevent resource leaks.
func (ac *AlertCache) Subscribe() chan *webuimodels.DashboardIncrementalUpdate {
	ch := make(chan *webuimodels.DashboardIncrementalUpdate, 10) // Buffered channel to prevent blocking

	ac.subMutex.Lock()
	ac.subscribers[ch] = true
	subscriberCount := len(ac.subscribers)
	ac.subMutex.Unlock()

	log.Printf("SSE subscriber added, total subscribers: %d", subscriberCount)
	return ch
}

// Unsubscribe removes a channel from the subscribers list and closes it.
// This should be called when a client disconnects to clean up resources.
func (ac *AlertCache) Unsubscribe(ch chan *webuimodels.DashboardIncrementalUpdate) {
	ac.subMutex.Lock()
	defer ac.subMutex.Unlock()

	if _, exists := ac.subscribers[ch]; exists {
		delete(ac.subscribers, ch)
		close(ch)
		log.Printf("SSE subscriber removed, total subscribers: %d", len(ac.subscribers))
	}
}

// notifySubscribers sends an incremental update to all active subscribers.
// Uses non-blocking sends to prevent slow subscribers from blocking the refresh cycle.
func (ac *AlertCache) notifySubscribers(update *webuimodels.DashboardIncrementalUpdate) {
	ac.subMutex.RLock()
	defer ac.subMutex.RUnlock()

	if len(ac.subscribers) == 0 {
		return
	}

	log.Printf("Notifying %d SSE subscribers of alert changes", len(ac.subscribers))

	for ch := range ac.subscribers {
		// Non-blocking send to prevent slow subscribers from blocking
		select {
		case ch <- update:
			// Successfully sent
		default:
			// Channel buffer full, skip this update for this subscriber
			log.Printf("SSE subscriber channel full, skipping update")
		}
	}
}

// GetSubscriberCount returns the current number of SSE subscribers.
// Useful for monitoring and debugging.
func (ac *AlertCache) GetSubscriberCount() int {
	ac.subMutex.RLock()
	defer ac.subMutex.RUnlock()
	return len(ac.subscribers)
}
