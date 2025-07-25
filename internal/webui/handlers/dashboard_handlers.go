package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"notificator/internal/models"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/services"
)

var (
	alertCache *services.AlertCache
	colorService *services.ColorService
	// Store user settings - in production this should be in database
	userSettings = make(map[string]*webuimodels.DashboardSettings)
)

// SetAlertCache sets the global alert cache instance
func SetAlertCache(cache *services.AlertCache) {
	alertCache = cache
}

// SetColorService sets the global color service instance
func SetColorService(cs *services.ColorService) {
	colorService = cs
}

// GetDashboardData returns the dashboard data with applied filters and sorting
func GetDashboardData(c *gin.Context) {
	userID := getCurrentUserID(c)
	
	// Parse filters from query parameters
	filters := parseDashboardFilters(c)
	sorting := parseDashboardSorting(c)
	
	// Get user settings
	settings := getUserSettings(userID)
	
	// Get alerts based on display mode
	var allAlerts []*webuimodels.DashboardAlert
	
	switch filters.DisplayMode {
	case webuimodels.DisplayModeResolved:
		allAlerts = alertCache.GetResolvedAlerts()
	case webuimodels.DisplayModeAcknowledge:
		allAlerts = getAcknowledgedAlerts()
	case webuimodels.DisplayModeFull:
		// Combine active and resolved alerts
		activeAlerts := alertCache.GetAllAlerts()
		resolvedAlerts := alertCache.GetResolvedAlerts()
		allAlerts = append(activeAlerts, resolvedAlerts...)
	default: // DisplayModeClassic
		allAlerts = getStandardAlerts()
	}
	
	// Apply filters
	filteredAlerts := applyDashboardFilters(allAlerts, filters, userID)
	
	// Apply sorting
	sortedAlerts := applySorting(filteredAlerts, sorting)
	
	// Prepare response based on view mode
	var response webuimodels.DashboardResponse
	
	if filters.ViewMode == webuimodels.ViewModeGroup {
		response.Groups = groupAlerts(sortedAlerts)
		response.Alerts = []webuimodels.DashboardAlert{} // Empty in group mode
	} else {
		response.Alerts = convertToResponseAlerts(sortedAlerts)
		response.Groups = []webuimodels.AlertGroup{} // Empty in list mode
	}
	
	// Build metadata
	response.Metadata = buildDashboardMetadata(allAlerts, filteredAlerts, filters, userID)
	response.Settings = *settings
	
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(response))
}

// parseDashboardFilters extracts filters from query parameters
func parseDashboardFilters(c *gin.Context) webuimodels.DashboardFilters {
	filters := webuimodels.DashboardFilters{
		Search:        c.Query("search"),
		Alertmanagers: parseStringArray(c.Query("alertmanagers")),
		Severities:    parseStringArray(c.Query("severities")),
		Statuses:      parseStringArray(c.Query("statuses")),
		Teams:         parseStringArray(c.Query("teams")),
		DisplayMode:   webuimodels.DashboardDisplayMode(c.DefaultQuery("displayMode", "classic")),
		ViewMode:      webuimodels.DashboardViewMode(c.DefaultQuery("viewMode", "list")),
	}
	
	// Parse boolean filters
	if ack := c.Query("acknowledged"); ack != "" {
		if val, err := strconv.ParseBool(ack); err == nil {
			filters.Acknowledged = &val
		}
	}
	
	if comments := c.Query("hasComments"); comments != "" {
		if val, err := strconv.ParseBool(comments); err == nil {
			filters.HasComments = &val
		}
	}
	
	return filters
}

// parseDashboardSorting extracts sorting from query parameters
func parseDashboardSorting(c *gin.Context) webuimodels.DashboardSorting {
	return webuimodels.DashboardSorting{
		Field:     c.DefaultQuery("sortField", "duration"),
		Direction: c.DefaultQuery("sortDirection", "desc"),
	}
}

// parseStringArray parses comma-separated string into array
func parseStringArray(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// getCurrentUserID gets the current user ID from context
func getCurrentUserID(c *gin.Context) string {
	if userID := middleware.GetSessionValue(c, "user_id"); userID != nil {
		if uid, ok := userID.(string); ok {
			return uid
		}
	}
	// Fallback to header-based user ID for testing
	if user := c.GetHeader("X-User-ID"); user != "" {
		return user
	}
	return "default-user"
}

// getUserSettings returns settings for a user (with defaults if not found)
func getUserSettings(userID string) *webuimodels.DashboardSettings {
	if settings, exists := userSettings[userID]; exists {
		return settings
	}
	
	// Return default settings
	defaultSettings := &webuimodels.DashboardSettings{
		UserID:                  userID,
		Theme:                   "light",
		NotificationsEnabled:    true,
		SoundEnabled:            true,
		ResolvedAlertsRetention: 1,
		RefreshInterval:         5,
		// Simple notification settings
		NotificationDelay:         2000,
		DefaultFilters: webuimodels.DashboardFilters{
			DisplayMode: webuimodels.DisplayModeClassic,
			ViewMode:    webuimodels.ViewModeList,
		},
		DefaultSorting: webuimodels.DashboardSorting{
			Field:     "duration",
			Direction: "desc",
		},
	}
	
	userSettings[userID] = defaultSettings
	return defaultSettings
}

// getStandardAlerts returns only standard alerts (not acknowledged, not resolved)
func getStandardAlerts() []*webuimodels.DashboardAlert {
	allAlerts := alertCache.GetAllAlerts()
	var standardAlerts []*webuimodels.DashboardAlert
	
	for _, alert := range allAlerts {
		if !alert.IsAcknowledged && !alert.IsResolved {
			standardAlerts = append(standardAlerts, alert)
		}
	}
	
	return standardAlerts
}

// getAcknowledgedAlerts returns only acknowledged alerts
func getAcknowledgedAlerts() []*webuimodels.DashboardAlert {
	allAlerts := alertCache.GetAllAlerts()
	var acknowledgedAlerts []*webuimodels.DashboardAlert
	
	for _, alert := range allAlerts {
		if alert.IsAcknowledged {
			acknowledgedAlerts = append(acknowledgedAlerts, alert)
		}
	}
	
	return acknowledgedAlerts
}

// applyDashboardFilters applies all filters to the alert list
func applyDashboardFilters(alerts []*webuimodels.DashboardAlert, filters webuimodels.DashboardFilters, userID string) []*webuimodels.DashboardAlert {
	var filtered []*webuimodels.DashboardAlert
	
	for _, alert := range alerts {
		// Skip hidden alerts for this user
		if alertCache.IsAlertHidden(userID, alert.Fingerprint) {
			continue
		}
		
		// Apply search filter
		if filters.Search != "" && !matchesSearch(alert, filters.Search) {
			continue
		}
		
		// Apply alertmanager filter
		if len(filters.Alertmanagers) > 0 && !contains(filters.Alertmanagers, alert.Source) {
			continue
		}
		
		// Apply severity filter
		if len(filters.Severities) > 0 && !contains(filters.Severities, alert.Severity) {
			continue
		}
		
		// Apply status filter
		if len(filters.Statuses) > 0 && !contains(filters.Statuses, alert.Status.State) {
			continue
		}
		
		// Apply team filter
		if len(filters.Teams) > 0 && !contains(filters.Teams, alert.Team) {
			continue
		}
		
		// Apply acknowledgment filter
		if filters.Acknowledged != nil && alert.IsAcknowledged != *filters.Acknowledged {
			continue
		}
		
		// Apply comments filter
		if filters.HasComments != nil {
			hasComments := alert.CommentCount > 0
			if hasComments != *filters.HasComments {
				continue
			}
		}
		
		filtered = append(filtered, alert)
	}
	
	return filtered
}

// matchesSearch checks if an alert matches the search term
func matchesSearch(alert *webuimodels.DashboardAlert, search string) bool {
	searchLower := strings.ToLower(search)
	
	searchFields := []string{
		alert.AlertName,
		alert.Instance,
		alert.Summary,
		alert.Team,
		alert.Source,
	}
	
	for _, field := range searchFields {
		if strings.Contains(strings.ToLower(field), searchLower) {
			return true
		}
	}
	
	// Also search in labels
	for key, value := range alert.Labels {
		if strings.Contains(strings.ToLower(key), searchLower) ||
		   strings.Contains(strings.ToLower(value), searchLower) {
			return true
		}
	}
	
	return false
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// applySorting sorts alerts based on the sorting configuration
func applySorting(alerts []*webuimodels.DashboardAlert, sorting webuimodels.DashboardSorting) []*webuimodels.DashboardAlert {
	sorted := make([]*webuimodels.DashboardAlert, len(alerts))
	copy(sorted, alerts)
	
	sort.Slice(sorted, func(i, j int) bool {
		var less bool
		
		switch sorting.Field {
		case "alertName":
			less = sorted[i].AlertName < sorted[j].AlertName
		case "severity":
			less = getSeverityPriority(sorted[i].Severity) < getSeverityPriority(sorted[j].Severity)
		case "status":
			less = getStatusPriority(sorted[i].Status.State) < getStatusPriority(sorted[j].Status.State)
		case "instance":
			less = sorted[i].Instance < sorted[j].Instance
		case "team":
			less = sorted[i].Team < sorted[j].Team
		case "duration":
			less = sorted[i].Duration < sorted[j].Duration
		case "source":
			less = sorted[i].Source < sorted[j].Source
		case "startsAt":
			less = sorted[i].StartsAt.Before(sorted[j].StartsAt)
		default:
			// Default to duration
			less = sorted[i].Duration < sorted[j].Duration
		}
		
		if sorting.Direction == "desc" {
			return !less
		}
		return less
	})
	
	return sorted
}

// getSeverityPriority returns numeric priority for severity sorting
func getSeverityPriority(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// getStatusPriority returns numeric priority for status sorting
func getStatusPriority(status string) int {
	switch status {
	case "firing":
		return 3
	case "silenced":
		return 2
	case "resolved":
		return 1
	default:
		return 0
	}
}

// groupAlerts groups alerts by GroupName for group view
func groupAlerts(alerts []*webuimodels.DashboardAlert) []webuimodels.AlertGroup {
	groups := make(map[string]*webuimodels.AlertGroup)
	
	for _, alert := range alerts {
		groupName := alert.GroupName
		if groupName == "" {
			groupName = "Other"
		}
		
		if group, exists := groups[groupName]; exists {
			group.Alerts = append(group.Alerts, *alert)
			group.Count++
			
			// Update worst severity
			if getSeverityPriority(alert.Severity) > getSeverityPriority(group.WorstSeverity) {
				group.WorstSeverity = alert.Severity
			}
		} else {
			groups[groupName] = &webuimodels.AlertGroup{
				GroupName:    groupName,
				Alerts:       []webuimodels.DashboardAlert{*alert},
				Count:        1,
				WorstSeverity: alert.Severity,
			}
		}
	}
	
	// Convert map to slice
	var result []webuimodels.AlertGroup
	for _, group := range groups {
		result = append(result, *group)
	}
	
	// Sort groups by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].GroupName < result[j].GroupName
	})
	
	return result
}

// convertToResponseAlerts converts internal alerts to response format
func convertToResponseAlerts(alerts []*webuimodels.DashboardAlert) []webuimodels.DashboardAlert {
	result := make([]webuimodels.DashboardAlert, len(alerts))
	for i, alert := range alerts {
		result[i] = *alert
	}
	return result
}

// buildDashboardMetadata builds metadata for the dashboard response
func buildDashboardMetadata(allAlerts, filteredAlerts []*webuimodels.DashboardAlert, filters webuimodels.DashboardFilters, userID string) webuimodels.DashboardMetadata {
	counters := webuimodels.DashboardCounters{}
	availableFilters := webuimodels.DashboardAvailableFilters{
		Alertmanagers: []string{},
		Severities:    []string{},
		Statuses:      []string{},
		Teams:         []string{},
	}
	
	// Track unique values for filters
	alertmanagerSet := make(map[string]bool)
	severitySet := make(map[string]bool)
	statusSet := make(map[string]bool)
	teamSet := make(map[string]bool)
	
	// Count statistics from filtered alerts only
	for _, alert := range filteredAlerts {
		switch strings.ToLower(alert.Severity) {
		case "critical":
			counters.Critical++
		case "warning":
			counters.Warning++
		case "info":
			counters.Info++
		}
		
		switch alert.Status.State {
		case "firing":
			counters.Firing++
		case "resolved":
			counters.Resolved++
		}
		
		if alert.IsAcknowledged {
			counters.Acknowledged++
		}
		
		if alert.CommentCount > 0 {
			counters.WithComments++
		}
	}
	
	// Always include resolved alerts in statistics, even if not displayed (for Classic view)
	// Only do this if we're not already in DisplayModeResolved or DisplayModeFull to avoid double counting
	if filters.DisplayMode == webuimodels.DisplayModeClassic || filters.DisplayMode == webuimodels.DisplayModeAcknowledge {
		resolvedAlerts := alertCache.GetResolvedAlerts()
		filteredResolvedAlerts := applyDashboardFilters(resolvedAlerts, filters, userID)
		
		for _, alert := range filteredResolvedAlerts {
			// Only count resolved alerts in the Resolved counter for Classic/Acknowledge views
			// Exclude them from Critical, Warning, Info, Acknowledged, WithComments counters
			// since these views don't display resolved alerts in their main lists
			if alert.Status.State == "resolved" {
				counters.Resolved++
			}
		}
	}
	
	// Collect unique values for filters from all alerts to show available options
	for _, alert := range allAlerts {
		alertmanagerSet[alert.Source] = true
		severitySet[alert.Severity] = true
		statusSet[alert.Status.State] = true
		teamSet[alert.Team] = true
	}
	
	// Convert sets to slices
	for am := range alertmanagerSet {
		availableFilters.Alertmanagers = append(availableFilters.Alertmanagers, am)
	}
	for sev := range severitySet {
		availableFilters.Severities = append(availableFilters.Severities, sev)
	}
	for status := range statusSet {
		availableFilters.Statuses = append(availableFilters.Statuses, status)
	}
	for team := range teamSet {
		availableFilters.Teams = append(availableFilters.Teams, team)
	}
	
	// Sort filter options
	sort.Strings(availableFilters.Alertmanagers)
	sort.Strings(availableFilters.Severities)
	sort.Strings(availableFilters.Statuses)
	sort.Strings(availableFilters.Teams)
	
	return webuimodels.DashboardMetadata{
		TotalAlerts:      len(filteredAlerts), // Now respects filtering
		FilteredCount:    len(filteredAlerts), // Keep for backward compatibility
		LastUpdate:       time.Now(),
		NextUpdate:       time.Now().Add(5 * time.Second), // Based on refresh interval
		Counters:         counters,
		AvailableFilters: availableFilters,
		// AlertmanagerStatus would be populated based on health check
		AlertmanagerStatus: make(map[string]bool),
	}
}

// BulkActionAlerts handles bulk actions on alerts (acknowledge, hide, etc.)
func BulkActionAlerts(c *gin.Context) {
	var request webuimodels.BulkActionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format"))
		return
	}
	
	userID := getCurrentUserID(c)
	response := webuimodels.BulkActionResponse{
		Success: true,
		Errors:  []string{},
	}
	
	// Process individual alerts
	for _, fingerprint := range request.AlertFingerprints {
		if err := processAlertAction(c, fingerprint, request.Action, request.Comment, userID); err != nil {
			response.FailedCount++
			response.Errors = append(response.Errors, err.Error())
		} else {
			response.ProcessedCount++
		}
	}
	
	// Process group actions
	for _, groupName := range request.GroupNames {
		if err := processGroupAction(c, groupName, request.Action, request.Comment, userID); err != nil {
			response.FailedCount++
			response.Errors = append(response.Errors, err.Error())
		} else {
			response.ProcessedCount++
		}
	}
	
	if response.FailedCount > 0 {
		response.Success = false
	}
	
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(response))
}

// processAlertAction processes a single alert action
func processAlertAction(c *gin.Context, fingerprint, action, comment, userID string) error {
	alert, exists := alertCache.GetAlert(fingerprint)
	if !exists {
		return fmt.Errorf("alert not found: %s", fingerprint)
	}
	
	switch action {
	case "acknowledge":
		// Store acknowledgment in backend
		if backendClient != nil && backendClient.IsConnected() {
			sessionID := getSessionIDFromContext(c)
			reason := comment
			if reason == "" {
				reason = "Acknowledged from dashboard"
			}
			
			if err := backendClient.AddAcknowledgment(sessionID, fingerprint, reason); err != nil {
				return fmt.Errorf("failed to store acknowledgment in backend: %w", err)
			}
			
			// Also add the acknowledgment reason as a comment for audit trail
			commentContent := fmt.Sprintf("🔔 Alert acknowledged: %s", reason)
			if err := backendClient.AddComment(sessionID, fingerprint, commentContent); err != nil {
				// Log the error but don't fail the acknowledgment if comment fails
				fmt.Printf("Warning: failed to add acknowledgment comment: %v\n", err)
			}
		}
		
		// Update local cache
		alert.IsAcknowledged = true
		alert.AcknowledgedBy = userID
		alert.AcknowledgedAt = time.Now()
		// Always increment comment count since we add an acknowledgment comment
		alert.CommentCount++
		
	case "unacknowledge":
		// Remove acknowledgment from backend
		if backendClient != nil && backendClient.IsConnected() {
			sessionID := getSessionIDFromContext(c)
			if err := backendClient.DeleteAcknowledgment(sessionID, fingerprint); err != nil {
				return fmt.Errorf("failed to remove acknowledgment from backend: %w", err)
			}
			
			// Also add a comment about the unacknowledgment for audit trail
			unackReason := comment
			if unackReason == "" {
				unackReason = "removed acknowledgment"
			}
			commentContent := fmt.Sprintf("🔕 Alert unacknowledged: %s", unackReason)
			if err := backendClient.AddComment(sessionID, fingerprint, commentContent); err != nil {
				// Log the error but don't fail the unacknowledgment if comment fails
				fmt.Printf("Warning: failed to add unacknowledgment comment: %v\n", err)
			}
		}
		
		// Update local cache
		alert.IsAcknowledged = false
		alert.AcknowledgedBy = ""
		alert.AcknowledgedAt = time.Time{}
		// Increment comment count for unacknowledgment comment
		alert.CommentCount++
		
	case "resolve":
		// Mark alert as resolved (this is more of a UI state than backend)
		alert.Status.State = "resolved"
		alert.EndsAt = time.Now()
		alert.IsResolved = true
		alert.ResolvedAt = time.Now()
		
		// Add a comment about resolution for audit trail
		if backendClient != nil && backendClient.IsConnected() {
			sessionID := getSessionIDFromContext(c)
			resolveReason := comment
			if resolveReason == "" {
				resolveReason = "resolved from dashboard"
			}
			commentContent := fmt.Sprintf("✅ Alert resolved: %s", resolveReason)
			if err := backendClient.AddComment(sessionID, fingerprint, commentContent); err != nil {
				// Log the error but don't fail the resolution if comment fails
				fmt.Printf("Warning: failed to add resolution comment: %v\n", err)
			} else {
				// Increment comment count if comment was added successfully
				alert.CommentCount++
			}
		}
		
	case "hide":
		alertCache.SetAlertHidden(userID, fingerprint, true)
		
	case "unhide":
		alertCache.SetAlertHidden(userID, fingerprint, false)
		
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
	
	return nil
}

// processGroupAction processes a group action
func processGroupAction(c *gin.Context, groupName, action, comment, userID string) error {
	// Find all alerts in the group
	allAlerts := alertCache.GetAllAlerts()
	
	for _, alert := range allAlerts {
		if alert.GroupName == groupName {
			if err := processAlertAction(c, alert.Fingerprint, action, comment, userID); err != nil {
				return err
			}
		}
	}
	
	return nil
}

// SaveDashboardSettings saves user dashboard settings
func SaveDashboardSettings(c *gin.Context) {
	var settings webuimodels.DashboardSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid settings format"))
		return
	}
	
	userID := getCurrentUserID(c)
	settings.UserID = userID
	
	// Update cache retention if changed
	if alertCache != nil {
		// Resolved alert retention is now handled by the backend TTL cleanup job
		alertCache.SetRefreshInterval(time.Duration(settings.RefreshInterval) * time.Second)
	}
	
	// Store settings (in production, this should be in database)
	userSettings[userID] = &settings
	
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Settings saved successfully",
	}))
}

// GetDashboardSettings returns current user settings
func GetDashboardSettings(c *gin.Context) {
	userID := getCurrentUserID(c)
	settings := getUserSettings(userID)
	
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(settings))
}

// getFilteredAndSortedAlerts gets alerts with filters and sorting applied
func getFilteredAndSortedAlerts(filters webuimodels.DashboardFilters, sorting webuimodels.DashboardSorting, userID string) []*webuimodels.DashboardAlert {
	// Get alerts based on display mode
	var allAlerts []*webuimodels.DashboardAlert
	
	switch filters.DisplayMode {
	case webuimodels.DisplayModeResolved:
		allAlerts = alertCache.GetResolvedAlerts()
	case webuimodels.DisplayModeAcknowledge:
		allAlerts = getAcknowledgedAlerts()
	case webuimodels.DisplayModeFull:
		// Combine active and resolved alerts
		activeAlerts := alertCache.GetAllAlerts()
		resolvedAlerts := alertCache.GetResolvedAlerts()
		allAlerts = append(activeAlerts, resolvedAlerts...)
	default: // DisplayModeClassic
		allAlerts = getStandardAlerts()
	}
	
	// Apply filters
	filteredAlerts := applyDashboardFilters(allAlerts, filters, userID)
	
	// Apply sorting
	sortedAlerts := applySorting(filteredAlerts, sorting)
	
	return sortedAlerts
}

// getDashboardMetadata builds metadata from alerts
func getDashboardMetadata(alerts []*webuimodels.DashboardAlert, filters webuimodels.DashboardFilters, userID string) webuimodels.DashboardMetadata {
	// Get all alerts for total counts
	var allAlerts []*webuimodels.DashboardAlert
	
	switch filters.DisplayMode {
	case webuimodels.DisplayModeResolved:
		allAlerts = alertCache.GetResolvedAlerts()
	case webuimodels.DisplayModeAcknowledge:
		allAlerts = getAcknowledgedAlerts()
	case webuimodels.DisplayModeFull:
		activeAlerts := alertCache.GetAllAlerts()
		resolvedAlerts := alertCache.GetResolvedAlerts()
		allAlerts = append(activeAlerts, resolvedAlerts...)
	default: // DisplayModeClassic
		allAlerts = getStandardAlerts()
	}
	
	return buildDashboardMetadata(allAlerts, alerts, filters, userID)
}

// GetDashboardIncremental returns only changes since last update timestamp
func GetDashboardIncremental(c *gin.Context) {
	userID := getCurrentUserID(c)
	
	// Parse last update timestamp from query parameter - for future use
	_ = c.Query("lastUpdate")
	
	// Parse filters from query parameters
	filters := parseDashboardFilters(c)
	sorting := parseDashboardSorting(c)
	
	// Get user settings
	settings := getUserSettings(userID)
	
	if alertCache == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Alert cache service not available"))
		return
	}
	
	// Get current alerts
	currentAlerts := getFilteredAndSortedAlerts(filters, sorting, userID)
	
	// Get client's current alert fingerprints from request
	clientFingerprintsStr := c.Query("clientAlerts")
	clientFingerprints := make(map[string]bool)
	if clientFingerprintsStr != "" {
		for _, fp := range strings.Split(clientFingerprintsStr, ",") {
			if fp != "" {
				clientFingerprints[fp] = true
			}
		}
	}
	
	// Compare current alerts with client's alerts
	newAlerts := []*webuimodels.DashboardAlert{}
	updatedAlerts := []*webuimodels.DashboardAlert{}
	removedAlerts := []string{}
	
	// Track current fingerprints for removal detection
	currentFingerprints := make(map[string]bool)
	
	for _, alert := range currentAlerts {
		currentFingerprints[alert.Fingerprint] = true
		
		if !clientFingerprints[alert.Fingerprint] {
			// Alert not in client's list = new alert
			newAlerts = append(newAlerts, alert)
		} else {
			// Alert exists in client, check if it was updated since lastUpdate
			// For simplicity, we'll include it as updated if it's recent
			// In a real implementation, you'd track alert modification times
			updatedAlerts = append(updatedAlerts, alert)
		}
	}
	
	// Find removed alerts (in client but not in current)
	for fingerprint := range clientFingerprints {
		if !currentFingerprints[fingerprint] {
			removedAlerts = append(removedAlerts, fingerprint)
		}
	}
	
	// Get updated metadata
	metadata := getDashboardMetadata(currentAlerts, filters, userID)
	
	// Get colors for new and updated alerts
	var colorsMap map[string]interface{}
	sessionID := getSessionIDFromContext(c)
	if colorService != nil && sessionID != "" && (len(newAlerts) > 0 || len(updatedAlerts) > 0) {
		// Combine new and updated alerts for color processing
		alertsForColors := make([]*webuimodels.DashboardAlert, 0, len(newAlerts)+len(updatedAlerts))
		alertsForColors = append(alertsForColors, newAlerts...)
		alertsForColors = append(alertsForColors, updatedAlerts...)
		
		// Convert to model alerts
		modelAlerts := make([]*models.Alert, len(alertsForColors))
		fingerprintToAlert := make(map[string]*models.Alert)
		
		for i, alert := range alertsForColors {
			modelAlert := convertDashboardToModel(alert)
			modelAlerts[i] = modelAlert
			fingerprintToAlert[alert.Fingerprint] = modelAlert
		}
		
		// Get colors
		colorResults := colorService.GetAlertColorsOptimized(modelAlerts, sessionID)
		
		// Remap results to use correct fingerprints
		finalResults := make(map[string]interface{})
		for fingerprint, alert := range fingerprintToAlert {
			generatedFingerprint := alert.GetFingerprint()
			if colorResult, exists := colorResults[generatedFingerprint]; exists {
				finalResults[fingerprint] = colorResult
			}
		}
		
		colorsMap = finalResults
	}
	
	// Create incremental response
	now := time.Now().Unix()
	incrementalUpdate := webuimodels.DashboardIncrementalUpdate{
		NewAlerts:      newAlerts,
		UpdatedAlerts:  updatedAlerts,
		RemovedAlerts:  removedAlerts,
		Metadata:       &metadata,
		Settings:       settings,
		Colors:         colorsMap,
		LastUpdateTime: now,
	}
	
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(incrementalUpdate))
}

// getSessionIDForUser gets the session ID for the current user
// This should be called with the Gin context to get the actual session ID
func getSessionIDFromContext(c *gin.Context) string {
	return middleware.GetSessionID(c)
}

// GetAlertDetails returns detailed information about a specific alert
func GetAlertDetails(c *gin.Context) {
	fingerprint := c.Param("id")
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Alert fingerprint is required"))
		return
	}
	
	// Get the alert from cache
	alert := alertCache.GetAlertByFingerprint(fingerprint)
	if alert == nil {
		c.JSON(http.StatusNotFound, webuimodels.ErrorResponse("Alert not found"))
		return
	}
	
	// Build detailed alert information
	details := &webuimodels.AlertDetails{
		Alert: alert,
	}
	
	// Get acknowledgments if backend is available
	if backendClient != nil && backendClient.IsConnected() {
		acknowledgments, err := backendClient.GetAcknowledgments(fingerprint)
		if err == nil {
			// Convert backend acknowledgments to webui models
			details.Acknowledgments = make([]webuimodels.Acknowledgment, len(acknowledgments))
			for i, ack := range acknowledgments {
				details.Acknowledgments[i] = webuimodels.Acknowledgment{
					ID:        fmt.Sprintf("%d", ack.Id),
					Username:  ack.Username,
					UserID:    fmt.Sprintf("%d", ack.UserId),
					Reason:    ack.Reason,
					CreatedAt: ack.CreatedAt.AsTime(),
					UpdatedAt: ack.CreatedAt.AsTime(), // Use CreatedAt if UpdatedAt not available
				}
			}
		}
		
		// Get comments if backend is available
		comments, err := backendClient.GetComments(fingerprint)
		if err == nil {
			// Convert backend comments to webui models
			details.Comments = make([]webuimodels.Comment, len(comments))
			for i, comment := range comments {
				details.Comments[i] = webuimodels.Comment{
					ID:        comment.Id,
					Username:  comment.Username,
					UserID:    comment.UserId,
					Content:   comment.Content,
					CreatedAt: comment.CreatedAt.AsTime(),
					UpdatedAt: comment.CreatedAt.AsTime(), // Use CreatedAt if UpdatedAt not available
				}
			}
		} else {
			details.Comments = []webuimodels.Comment{}
		}
		
		// Note: Silences would need to be implemented in backend client
		// For now, initialize empty slice
		details.Silences = []webuimodels.Silence{}
	}
	
	// Get additional metadata
	if alert.GeneratorURL != "" {
		details.GeneratorURL = alert.GeneratorURL
	}
	
	// Calculate timing information
	now := time.Now()
	details.StartedAt = alert.StartsAt
	details.Duration = now.Sub(alert.StartsAt)
	if !alert.EndsAt.IsZero() {
		endTime := alert.EndsAt
		details.EndedAt = &endTime
		details.Duration = alert.EndsAt.Sub(alert.StartsAt)
	}
	
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(details))
}

// AddAlertComment adds a comment to an alert
func AddAlertComment(c *gin.Context) {
	fingerprint := c.Param("id")
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Alert fingerprint is required"))
		return
	}

	// Parse request body
	var request struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format: "+err.Error()))
		return
	}

	// Validate content length
	if len(strings.TrimSpace(request.Content)) == 0 {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Comment content cannot be empty"))
		return
	}

	if len(request.Content) > 1000 {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Comment content cannot exceed 1000 characters"))
		return
	}

	// Check if backend is available
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	sessionID := getSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Authentication required"))
		return
	}

	// Verify the alert exists
	alert := alertCache.GetAlertByFingerprint(fingerprint)
	if alert == nil {
		c.JSON(http.StatusNotFound, webuimodels.ErrorResponse("Alert not found"))
		return
	}

	// Add comment via backend
	err := backendClient.AddComment(sessionID, fingerprint, strings.TrimSpace(request.Content))
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to add comment: "+err.Error()))
		return
	}

	// Update comment count in alert cache
	alert.CommentCount++
	alert.LastCommentAt = time.Now()

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Comment added successfully",
	}))
}

// DeleteAlertComment deletes a comment from an alert
func DeleteAlertComment(c *gin.Context) {
	fingerprint := c.Param("id")
	commentID := c.Param("commentId")
	
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Alert fingerprint is required"))
		return
	}

	if commentID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Comment ID is required"))
		return
	}

	// Check if backend is available
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	sessionID := getSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Authentication required"))
		return
	}

	// Verify the alert exists
	alert := alertCache.GetAlertByFingerprint(fingerprint)
	if alert == nil {
		c.JSON(http.StatusNotFound, webuimodels.ErrorResponse("Alert not found"))
		return
	}

	// Delete comment via backend
	err := backendClient.DeleteComment(sessionID, commentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to delete comment: "+err.Error()))
		return
	}

	// Update comment count in alert cache (decrement if > 0)
	if alert.CommentCount > 0 {
		alert.CommentCount--
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Comment deleted successfully",
	}))
}

// GetUserColorPreferences returns the user's color preferences
func GetUserColorPreferences(c *gin.Context) {
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	sessionID := getSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Authentication required"))
		return
	}

	// Get color preferences from backend
	preferences, err := backendClient.GetUserColorPreferences(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get color preferences: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"preferences": preferences,
	}))
}

// SaveUserColorPreferences saves the user's color preferences
func SaveUserColorPreferences(c *gin.Context) {
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	sessionID := getSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Authentication required"))
		return
	}

	// Parse request body
	var request struct {
		Preferences []webuimodels.UserColorPreference `json:"preferences"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format: "+err.Error()))
		return
	}

	// Convert to backend format and save
	err := backendClient.SaveUserColorPreferences(sessionID, request.Preferences)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to save color preferences: "+err.Error()))
		return
	}

	// Invalidate color cache for the user to force reload
	if colorService != nil && sessionID != "" {
		colorService.InvalidateUserCache(sessionID)
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Color preferences saved successfully",
	}))
}

// DeleteUserColorPreference deletes a specific color preference
func DeleteUserColorPreference(c *gin.Context) {
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	sessionID := getSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Authentication required"))
		return
	}

	preferenceID := c.Param("id")
	if preferenceID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Preference ID is required"))
		return
	}

	// Delete color preference via backend
	err := backendClient.DeleteUserColorPreference(sessionID, preferenceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to delete color preference: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Color preference deleted successfully",
	}))
}


// GetAvailableAlertLabels returns all unique labels and their values from current alerts
func GetAvailableAlertLabels(c *gin.Context) {
	if alertCache == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Alert cache service not available"))
		return
	}

	// Get all current alerts
	allAlerts := alertCache.GetAllAlerts()
	
	// Build label-value map
	labelValues := make(map[string][]string)
	labelSet := make(map[string]map[string]bool) // for deduplication
	
	for _, alert := range allAlerts {
		for labelKey, labelValue := range alert.Labels {
			if labelSet[labelKey] == nil {
				labelSet[labelKey] = make(map[string]bool)
			}
			labelSet[labelKey][labelValue] = true
		}
	}
	
	// Convert sets to sorted slices
	for labelKey, valueSet := range labelSet {
		var values []string
		for value := range valueSet {
			values = append(values, value)
		}
		// Simple sort
		for i := 0; i < len(values); i++ {
			for j := i + 1; j < len(values); j++ {
				if values[i] > values[j] {
					values[i], values[j] = values[j], values[i]
				}
			}
		}
		labelValues[labelKey] = values
	}
	
	// Sort label keys as well
	var labelKeys []string
	for labelKey := range labelValues {
		labelKeys = append(labelKeys, labelKey)
	}
	for i := 0; i < len(labelKeys); i++ {
		for j := i + 1; j < len(labelKeys); j++ {
			if labelKeys[i] > labelKeys[j] {
				labelKeys[i], labelKeys[j] = labelKeys[j], labelKeys[i]
			}
		}
	}
	
	// Build sorted result
	sortedLabels := make(map[string][]string)
	for _, key := range labelKeys {
		sortedLabels[key] = labelValues[key]
	}
	
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"labels": sortedLabels,
		"totalAlerts": len(allAlerts),
		"labelCount": len(labelKeys),
	}))
}

// GetAlertColors returns color configuration for all alerts based on user preferences
func GetAlertColors(c *gin.Context) {
	// Get session ID for backend authentication
	sessionID := getSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Authentication required"))
		return
	}

	userID := getCurrentUserID(c)
	
	// Parse filters from query parameters (same as dashboard data)
	filters := parseDashboardFilters(c)
	
	// Get alerts based on display mode (same logic as dashboard data)
	var allAlerts []*webuimodels.DashboardAlert
	
	switch filters.DisplayMode {
	case webuimodels.DisplayModeResolved:
		allAlerts = alertCache.GetResolvedAlerts()
	case webuimodels.DisplayModeAcknowledge:
		allAlerts = getAcknowledgedAlerts()
	case webuimodels.DisplayModeFull:
		// Combine active and resolved alerts
		activeAlerts := alertCache.GetAllAlerts()
		resolvedAlerts := alertCache.GetResolvedAlerts()
		allAlerts = append(activeAlerts, resolvedAlerts...)
	default: // DisplayModeClassic
		allAlerts = getStandardAlerts()
	}
	
	// Apply filters (same as dashboard data)
	filteredAlerts := applyDashboardFilters(allAlerts, filters, userID)
	
	// Build fingerprint to alert mapping
	fingerprintToAlert := make(map[string]*models.Alert)
	var modelAlerts []*models.Alert
	
	for _, alert := range filteredAlerts {
		modelAlert := convertDashboardToModel(alert)
		modelAlerts = append(modelAlerts, modelAlert)
		// Use the existing fingerprint from DashboardAlert
		fingerprintToAlert[alert.Fingerprint] = modelAlert
	}
	
	// Check if color service is available
	if colorService == nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Color service not available"))
		return
	}
	
	// Get optimized colors for all alerts
	colorResults := colorService.GetAlertColorsOptimized(modelAlerts, sessionID)
	
	// Remap results to use the correct fingerprints
	finalResults := make(map[string]*services.AlertColorResult)
	for fingerprint, alert := range fingerprintToAlert {
		// Find the color result for this alert (it might have wrong fingerprint)
		generatedFingerprint := alert.GetFingerprint()
		if colorResult, exists := colorResults[generatedFingerprint]; exists {
			finalResults[fingerprint] = colorResult
		}
	}
	
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"colors": finalResults,
		"colorCount": len(finalResults),
		"timestamp": time.Now().Unix(),
	}))
}

// convertDashboardToModel converts a DashboardAlert to models.Alert for color service
func convertDashboardToModel(dashAlert *webuimodels.DashboardAlert) *models.Alert {
	// Create alert for color service - it only needs basic fields for color matching
	alert := &models.Alert{
		Labels:      dashAlert.Labels,
		Annotations: dashAlert.Annotations,
		StartsAt:    dashAlert.StartsAt,
		EndsAt:      dashAlert.EndsAt,
		GeneratorURL: dashAlert.GeneratorURL,
		Status: models.AlertStatus{
			State:       dashAlert.Status.State,
			SilencedBy:  dashAlert.Status.SilencedBy,
			InhibitedBy: dashAlert.Status.InhibitedBy,
		},
	}
	
	return alert
}

// RemoveAllResolvedAlerts removes all resolved alerts from the backend
func RemoveAllResolvedAlerts(c *gin.Context) {
	if alertCache == nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Alert cache not available"))
		return
	}

	// Call alert cache to remove all resolved alerts
	if err := alertCache.RemoveAllResolvedAlerts(); err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse(fmt.Sprintf("Failed to remove resolved alerts: %v", err)))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "All resolved alerts have been removed successfully",
	}))
}