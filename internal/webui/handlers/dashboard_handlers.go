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
	alertCache   *services.AlertCache
	colorService *services.ColorService
	// Store user settings - in production this should be in database
	userSettings = make(map[string]*webuimodels.DashboardSettings)
)

func validateCustomDuration(durationStr string) (time.Duration, error) {
	if durationStr == "" {
		return 0, fmt.Errorf("duration cannot be empty")
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration format: %v", err)
	}

	if duration <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}

	maxDuration := 30 * 24 * time.Hour
	if duration > maxDuration {
		return 0, fmt.Errorf("duration cannot exceed 30 days")
	}

	minDuration := 1 * time.Second
	if duration < minDuration {
		return 0, fmt.Errorf("duration must be at least 1 second")
	}

	return duration, nil
}

func SetAlertCache(cache *services.AlertCache) {
	alertCache = cache
}

func SetColorService(cs *services.ColorService) {
	colorService = cs
}

func GetDashboardData(c *gin.Context) {
	userID := getCurrentUserID(c)

	// Parse filters from query parameters
	filters := parseDashboardFilters(c)
	sorting := parseDashboardSorting(c)
	pagination := parsePagination(c)

	// Get user settings
	settings := getUserSettings(userID)

	// Get alerts based on display mode
	var allAlerts []*webuimodels.DashboardAlert

	switch filters.DisplayMode {
	case webuimodels.DisplayModeResolved:
		if filters.ResolvedAlertsLimit > 0 {
			allAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			allAlerts = alertCache.GetResolvedAlerts()
		}
	case webuimodels.DisplayModeAcknowledge:
		allAlerts = getAcknowledgedAlerts()
	case webuimodels.DisplayModeFull:
		// Combine active and resolved alerts
		activeAlerts := alertCache.GetAllAlerts()
		var resolvedAlerts []*webuimodels.DashboardAlert
		if filters.ResolvedAlertsLimit > 0 {
			resolvedAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			resolvedAlerts = alertCache.GetResolvedAlerts()
		}
		allAlerts = append(activeAlerts, resolvedAlerts...)
	default: // DisplayModeClassic
		allAlerts = getStandardAlerts()
	}

	// Apply filters
	filteredAlerts := applyDashboardFilters(allAlerts, filters, userID)

	// Apply sorting
	sortedAlerts := applySorting(filteredAlerts, sorting)
	
	// Store total count before pagination
	totalCount := len(sortedAlerts)

	// Apply pagination
	paginatedAlerts := applyPagination(sortedAlerts, pagination)

	// Prepare response based on view mode
	var response webuimodels.DashboardResponse

	if filters.ViewMode == webuimodels.ViewModeGroup {
		groupBy := c.DefaultQuery("groupBy", "alertname")
		response.Groups = groupAlertsByLabel(paginatedAlerts, groupBy)
		response.Alerts = []webuimodels.DashboardAlert{} // Empty in group mode
	} else {
		response.Alerts = convertToResponseAlerts(paginatedAlerts)
		response.Groups = []webuimodels.AlertGroup{} // Empty in list mode
	}

	// Build metadata
	// For classic mode, we need to pass ALL alerts (including acknowledged) to buildDashboardMetadata
	// so it can properly count acknowledged alerts in its special logic
	var metadataAllAlerts []*webuimodels.DashboardAlert
	if filters.DisplayMode == webuimodels.DisplayModeClassic {
		metadataAllAlerts = alertCache.GetAllAlerts()
	} else {
		metadataAllAlerts = allAlerts
	}
	response.Metadata = buildDashboardMetadata(metadataAllAlerts, filteredAlerts, filters, userID)
	response.Metadata.TotalCount = totalCount // Add total count for pagination
	response.Settings = *settings

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(response))
}

func parseDashboardFilters(c *gin.Context) webuimodels.DashboardFilters {
	filters := webuimodels.DashboardFilters{
		Search:        c.Query("search"),
		Alertmanagers: parseStringArray(c.Query("alertmanagers")),
		Severities:    parseStringArray(c.Query("severities")),
		Statuses:      parseStringArray(c.Query("statuses")),
		Teams:         parseStringArray(c.Query("teams")),
		AlertNames:    parseStringArray(c.Query("alertNames")),
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

	// Parse resolved alerts limit
	if limitStr := c.Query("resolvedAlertsLimit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			filters.ResolvedAlertsLimit = val
		}
	}

	return filters
}

func parseDashboardSorting(c *gin.Context) webuimodels.DashboardSorting {
	return webuimodels.DashboardSorting{
		Field:     c.DefaultQuery("sortField", "duration"),
		Direction: c.DefaultQuery("sortDirection", "desc"),
	}
}

func parsePagination(c *gin.Context) webuimodels.Pagination {
	page := 1
	limit := 50
	
	if p := c.Query("page"); p != "" {
		if val, err := strconv.Atoi(p); err == nil && val > 0 {
			page = val
		}
	}
	
	if l := c.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
			// Cap limit at 500
			if limit > 500 {
				limit = 500
			}
		}
	}
	
	return webuimodels.Pagination{
		Page:  page,
		Limit: limit,
	}
}

func parseStringArray(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

func getCurrentUserID(c *gin.Context) string {
	if userID := middleware.GetSessionValue(c, "user_id"); userID != nil {
		if uid, ok := userID.(string); ok {
			return uid
		}
	}
	if user := c.GetHeader("X-User-ID"); user != "" {
		return user
	}
	return "default-user"
}

func getUserSettings(userID string) *webuimodels.DashboardSettings {
	if settings, exists := userSettings[userID]; exists {
		return settings
	}

	defaultSettings := &webuimodels.DashboardSettings{
		UserID:                  userID,
		Theme:                   "light",
		NotificationsEnabled:    true,
		SoundEnabled:            true,
		ResolvedAlertsRetention: 1,
		RefreshInterval:         5,
		NotificationDelay:       2000,
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

		// Apply alert name filter
		if len(filters.AlertNames) > 0 && !contains(filters.AlertNames, alert.AlertName) {
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

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func applyPagination(alerts []*webuimodels.DashboardAlert, pagination webuimodels.Pagination) []*webuimodels.DashboardAlert {
	// Calculate offset
	offset := (pagination.Page - 1) * pagination.Limit
	
	// Check bounds
	if offset >= len(alerts) {
		return []*webuimodels.DashboardAlert{}
	}
	
	end := offset + pagination.Limit
	if end > len(alerts) {
		end = len(alerts)
	}
	
	return alerts[offset:end]
}

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

func groupAlertsByLabel(alerts []*webuimodels.DashboardAlert, groupByLabel string) []webuimodels.AlertGroup {
	groups := make(map[string]*webuimodels.AlertGroup)

	for _, alert := range alerts {
		// Determine group name based on the label
		var groupName string
		
		switch groupByLabel {
		case "alertname":
			groupName = alert.AlertName
		case "severity":
			groupName = alert.Severity
		case "team":
			groupName = alert.Team
		case "instance":
			groupName = alert.Instance
		default:
			// Try to get the value from labels map
			if val, exists := alert.Labels[groupByLabel]; exists && val != "" {
				groupName = val
			} else {
				groupName = "Other"
			}
		}
		
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
				GroupName:     groupName,
				Alerts:        []webuimodels.DashboardAlert{*alert},
				Count:         1,
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

func convertToResponseAlerts(alerts []*webuimodels.DashboardAlert) []webuimodels.DashboardAlert {
	result := make([]webuimodels.DashboardAlert, len(alerts))
	for i, alert := range alerts {
		result[i] = *alert
	}
	return result
}

func buildDashboardMetadata(allAlerts, filteredAlerts []*webuimodels.DashboardAlert, filters webuimodels.DashboardFilters, userID string) webuimodels.DashboardMetadata {
	counters := webuimodels.DashboardCounters{}
	availableFilters := webuimodels.DashboardAvailableFilters{
		Alertmanagers: []string{},
		Severities:    []string{},
		Statuses:      []string{},
		Teams:         []string{},
		AlertNames:    []string{},
	}

	// Track unique values for filters
	alertmanagerSet := make(map[string]bool)
	severitySet := make(map[string]bool)
	statusSet := make(map[string]bool)
	teamSet := make(map[string]bool)
	alertNameSet := make(map[string]bool)

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

	// Fix acknowledged counter for classic mode - count from all alerts since they're excluded from filtered
	if filters.DisplayMode == webuimodels.DisplayModeClassic {
		// Reset acknowledged counter and count from all alerts
		counters.Acknowledged = 0
		for _, alert := range allAlerts {
			if alert.IsAcknowledged && !alert.IsResolved {
				counters.Acknowledged++
			}
		}
	}

	// Always include resolved alerts in statistics, even if not displayed (for Classic view)
	// Only do this if we're not already in DisplayModeResolved or DisplayModeFull to avoid double counting
	if filters.DisplayMode == webuimodels.DisplayModeClassic || filters.DisplayMode == webuimodels.DisplayModeAcknowledge {
		var resolvedAlerts []*webuimodels.DashboardAlert
		if filters.ResolvedAlertsLimit > 0 {
			resolvedAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			resolvedAlerts = alertCache.GetResolvedAlerts()
		}
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
		if alert.AlertName != "" {
			alertNameSet[alert.AlertName] = true
		}
	}

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
	for alertName := range alertNameSet {
		availableFilters.AlertNames = append(availableFilters.AlertNames, alertName)
	}

	// Sort filter options
	sort.Strings(availableFilters.Alertmanagers)
	sort.Strings(availableFilters.Severities)
	sort.Strings(availableFilters.Statuses)
	sort.Strings(availableFilters.Teams)
	sort.Strings(availableFilters.AlertNames)

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

	// Store silence duration in context for silence actions
	if request.Action == "silence" {
		var silenceDuration time.Duration
		var err error

		if request.SilenceDurationType == "custom" && request.CustomSilenceDuration != "" {
			silenceDuration, err = validateCustomDuration(request.CustomSilenceDuration)
			if err != nil {
				c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse(fmt.Sprintf("Invalid custom duration: %v", err)))
				return
			}
		} else if request.SilenceDuration > 0 {
			silenceDuration = request.SilenceDuration
		} else {
			c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Silence duration must be provided"))
			return
		}

		c.Set("silenceDuration", silenceDuration)
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

	case "silence":
		// Handle silence action
		if err := processSilenceAction(c, fingerprint, comment, userID); err != nil {
			return fmt.Errorf("failed to silence alert: %w", err)
		}

	case "unsilence":
		// Handle unsilence action
		if err := processUnsilenceAction(c, fingerprint, userID); err != nil {
			return fmt.Errorf("failed to unsilence alert: %w", err)
		}

	default:
		return fmt.Errorf("unknown action: %s", action)
	}

	return nil
}

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

	userSettings[userID] = &settings

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Settings saved successfully",
	}))
}

func GetDashboardSettings(c *gin.Context) {
	userID := getCurrentUserID(c)
	settings := getUserSettings(userID)

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(settings))
}

func getFilteredAndSortedAlerts(filters webuimodels.DashboardFilters, sorting webuimodels.DashboardSorting, userID string) []*webuimodels.DashboardAlert {
	// Get alerts based on display mode
	var allAlerts []*webuimodels.DashboardAlert

	switch filters.DisplayMode {
	case webuimodels.DisplayModeResolved:
		if filters.ResolvedAlertsLimit > 0 {
			allAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			allAlerts = alertCache.GetResolvedAlerts()
		}
	case webuimodels.DisplayModeAcknowledge:
		allAlerts = getAcknowledgedAlerts()
	case webuimodels.DisplayModeFull:
		// Combine active and resolved alerts
		activeAlerts := alertCache.GetAllAlerts()
		var resolvedAlerts []*webuimodels.DashboardAlert
		if filters.ResolvedAlertsLimit > 0 {
			resolvedAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			resolvedAlerts = alertCache.GetResolvedAlerts()
		}
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

func getDashboardMetadata(alerts []*webuimodels.DashboardAlert, filters webuimodels.DashboardFilters, userID string) webuimodels.DashboardMetadata {
	// Get all alerts for total counts
	var allAlerts []*webuimodels.DashboardAlert

	switch filters.DisplayMode {
	case webuimodels.DisplayModeResolved:
		if filters.ResolvedAlertsLimit > 0 {
			allAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			allAlerts = alertCache.GetResolvedAlerts()
		}
	case webuimodels.DisplayModeAcknowledge:
		allAlerts = getAcknowledgedAlerts()
	case webuimodels.DisplayModeFull:
		activeAlerts := alertCache.GetAllAlerts()
		var resolvedAlerts []*webuimodels.DashboardAlert
		if filters.ResolvedAlertsLimit > 0 {
			resolvedAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			resolvedAlerts = alertCache.GetResolvedAlerts()
		}
		allAlerts = append(activeAlerts, resolvedAlerts...)
	default: // DisplayModeClassic
		// For classic mode, use all alerts for metadata counting (not just standard alerts)
		// This ensures we can count acknowledged alerts properly in the statistics
		allAlerts = alertCache.GetAllAlerts()
	}

	return buildDashboardMetadata(allAlerts, alerts, filters, userID)
}

func PostDashboardIncremental(c *gin.Context) {
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

	// Get client's current alert fingerprints from POST body
	var req webuimodels.DashboardIncrementalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request body: "+err.Error()))
		return
	}

	clientFingerprints := make(map[string]bool)
	for _, fp := range req.ClientAlerts {
		if fp != "" {
			clientFingerprints[fp] = true
		}
	}

	// Process incremental update
	processIncremental(c, currentAlerts, clientFingerprints, settings, userID)
}

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

	// Get client's current alert fingerprints from query parameter
	clientFingerprintsStr := c.Query("clientAlerts")
	clientFingerprints := make(map[string]bool)
	if clientFingerprintsStr != "" {
		for _, fp := range strings.Split(clientFingerprintsStr, ",") {
			if fp != "" {
				clientFingerprints[fp] = true
			}
		}
	}

	// Process incremental update
	processIncremental(c, currentAlerts, clientFingerprints, settings, userID)
}

func processIncremental(c *gin.Context, currentAlerts []*webuimodels.DashboardAlert, clientFingerprints map[string]bool, settings *webuimodels.DashboardSettings, userID string) {
	// Parse filters from query parameters for metadata
	filters := parseDashboardFilters(c)

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

func getSessionIDFromContext(c *gin.Context) string {
	return middleware.GetSessionID(c)
}

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
	pbPreferences, err := backendClient.GetUserColorPreferences(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get color preferences: "+err.Error()))
		return
	}

	// Convert protobuf preferences to webui models
	var preferences []webuimodels.UserColorPreference
	for _, pbPref := range pbPreferences {
		pref := webuimodels.UserColorPreference{
			ID:                 pbPref.Id,
			UserID:             pbPref.UserId,
			LabelConditions:    pbPref.LabelConditions,
			Color:              pbPref.Color,
			ColorType:          pbPref.ColorType,
			Priority:           int(pbPref.Priority),
			BgLightnessFactor:  float64(pbPref.BgLightnessFactor),
			TextDarknessFactor: float64(pbPref.TextDarknessFactor),
		}

		// Convert timestamps if available
		if pbPref.CreatedAt != nil {
			pref.CreatedAt = pbPref.CreatedAt.AsTime()
		}
		if pbPref.UpdatedAt != nil {
			pref.UpdatedAt = pbPref.UpdatedAt.AsTime()
		}

		preferences = append(preferences, pref)
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"preferences": preferences,
	}))
}

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
		"labels":      sortedLabels,
		"totalAlerts": len(allAlerts),
		"labelCount":  len(labelKeys),
	}))
}

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
		if filters.ResolvedAlertsLimit > 0 {
			allAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			allAlerts = alertCache.GetResolvedAlerts()
		}
	case webuimodels.DisplayModeAcknowledge:
		allAlerts = getAcknowledgedAlerts()
	case webuimodels.DisplayModeFull:
		// Combine active and resolved alerts
		activeAlerts := alertCache.GetAllAlerts()
		var resolvedAlerts []*webuimodels.DashboardAlert
		if filters.ResolvedAlertsLimit > 0 {
			resolvedAlerts = alertCache.GetResolvedAlertsWithLimit(filters.ResolvedAlertsLimit)
		} else {
			resolvedAlerts = alertCache.GetResolvedAlerts()
		}
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
		"colors":     finalResults,
		"colorCount": len(finalResults),
		"timestamp":  time.Now().Unix(),
	}))
}

func convertDashboardToModel(dashAlert *webuimodels.DashboardAlert) *models.Alert {
	// Create alert for color service - it only needs basic fields for color matching
	alert := &models.Alert{
		Labels:       dashAlert.Labels,
		Annotations:  dashAlert.Annotations,
		StartsAt:     dashAlert.StartsAt,
		EndsAt:       dashAlert.EndsAt,
		GeneratorURL: dashAlert.GeneratorURL,
		Status: models.AlertStatus{
			State:       dashAlert.Status.State,
			SilencedBy:  dashAlert.Status.SilencedBy,
			InhibitedBy: dashAlert.Status.InhibitedBy,
		},
	}

	return alert
}

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

func processSilenceAction(c *gin.Context, fingerprint, comment, userID string) error {
	if alertmanagerClient == nil {
		return fmt.Errorf("alertmanager client not available")
	}

	// Get the alert from cache to extract labels
	alert, exists := alertCache.GetAlert(fingerprint)
	if !exists {
		return fmt.Errorf("alert not found: %s", fingerprint)
	}

	// Get silence duration from context (set in BulkActionAlerts)
	silenceDurationInterface, exists := c.Get("silenceDuration")
	if !exists {
		return fmt.Errorf("silence duration not provided")
	}

	silenceDuration, ok := silenceDurationInterface.(time.Duration)
	if !ok {
		return fmt.Errorf("invalid silence duration format")
	}

	// Create silence matchers from alert labels
	var matchers []models.SilenceMatcher
	for key, value := range alert.Labels {
		// Skip certain labels that shouldn't be used for silencing
		if key == "__name__" || key == "__tmp_" {
			continue
		}

		matchers = append(matchers, models.SilenceMatcher{
			Name:    key,
			Value:   value,
			IsRegex: false,
			IsEqual: true,
		})
	}

	if len(matchers) == 0 {
		return fmt.Errorf("no suitable labels found for creating silence")
	}

	// Create silence object
	now := time.Now()
	silence := models.Silence{
		Matchers:  matchers,
		StartsAt:  now,
		EndsAt:    now.Add(silenceDuration),
		CreatedBy: userID,
		Comment:   comment,
		Status: models.SilenceStatus{
			State: "active",
		},
	}

	// Get all alertmanager clients and create silence on each
	allClients := alertmanagerClient.GetAllClients()
	var errors []error

	for name, client := range allClients {
		createdSilence, err := client.CreateSilence(silence)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create silence on %s: %w", name, err))
			continue
		}

		// Log success
		fmt.Printf("Created silence %s on alertmanager %s\n", createdSilence.ID, name)
	}

	// If all clients failed, return error
	if len(errors) == len(allClients) && len(allClients) > 0 {
		return fmt.Errorf("failed to create silence on all alertmanagers: %v", errors)
	}

	// If some failed but not all, log warnings but continue
	if len(errors) > 0 {
		fmt.Printf("Warning: failed to create silence on some alertmanagers: %v\n", errors)
	}

	return nil
}

func processUnsilenceAction(c *gin.Context, fingerprint, userID string) error {
	if alertmanagerClient == nil {
		return fmt.Errorf("alertmanager client not available")
	}

	// Get the alert from cache
	alert := alertCache.GetAlertByFingerprint(fingerprint)
	if alert == nil {
		return fmt.Errorf("alert not found: %s", fingerprint)
	}

	// Check if alert has any active silences
	if len(alert.Status.SilencedBy) == 0 {
		return fmt.Errorf("alert is not silenced")
	}

	// Get all alertmanager clients and delete silences from each
	allClients := alertmanagerClient.GetAllClients()
	var errors []error

	successCount := 0

	for _, silenceID := range alert.Status.SilencedBy {
		for name, client := range allClients {
			err := client.DeleteSilence(silenceID)
			if err != nil {
				// If silence not found, it might already be deleted or on different AM
				if !strings.Contains(err.Error(), "not found") {
					errors = append(errors, fmt.Errorf("failed to delete silence %s on %s: %w", silenceID, name, err))
				}
				continue
			}

			// Log success
			fmt.Printf("Deleted silence %s on alertmanager %s\n", silenceID, name)
			successCount++
			break // Only delete from one AM per silence ID
		}
	}

	// If no silences were successfully deleted, return error
	if successCount == 0 && len(errors) > 0 {
		return fmt.Errorf("failed to delete any silences: %v", errors)
	}

	// If some failed but not all, log warnings but continue
	if len(errors) > 0 {
		fmt.Printf("Warning: failed to delete some silences: %v\n", errors)
	}

	return nil
}

func GetUserNotificationPreferences(c *gin.Context) {
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Get session ID for backend authentication
	sessionID := getSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Authentication required"))
		return
	}

	// Get notification preferences from backend
	pbPreference, err := backendClient.GetUserNotificationPreferences(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get notification preferences: "+err.Error()))
		return
	}

	// Convert protobuf to WebUI format
	preference := webuimodels.UserNotificationPreferenceFromProtobuf(pbPreference)
	if preference == nil {
		// Return default preferences if none exist
		preference = webuimodels.GetDefaultNotificationPreference()
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"preference": preference,
	}))
}

func SaveUserNotificationPreferences(c *gin.Context) {
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Get session ID for backend authentication
	sessionID := getSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Authentication required"))
		return
	}

	// Parse request body
	var request struct {
		Preference *webuimodels.UserNotificationPreference `json:"preference"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format: "+err.Error()))
		return
	}

	if request.Preference == nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Preference data is required"))
		return
	}

	// Convert to protobuf format
	pbPreference := request.Preference.ToProtobuf()

	// Save preferences using backend client
	err := backendClient.SaveUserNotificationPreferences(sessionID, pbPreference)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to save notification preferences: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Notification preferences saved successfully",
	}))
}
