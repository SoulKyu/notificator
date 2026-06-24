package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/types/known/timestamppb"

	alertpb "notificator/internal/backend/proto/alert"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/templates/pages"
)

// tsToTime converts a possibly-nil protobuf timestamp to time.Time (zero value if nil).
func tsToTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// ==================== Statistics Query Endpoints ====================

// QueryStatistics handles querying alert statistics with filters and aggregations
func QueryStatistics(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	// Check backend availability
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Parse request body
	var request struct {
		StartDate          time.Time `json:"start_date" binding:"required"`
		EndDate            time.Time `json:"end_date" binding:"required"`
		GroupBy            string    `json:"group_by"`           // "severity", "team", "period", "alert_name"
		SecondaryGroupBy   string    `json:"secondary_group_by"` // For period: "severity", "team", "alert_name"
		PeriodType         string    `json:"period_type"`        // "hour", "day", "week", "month"
		Limit              int32     `json:"limit"`
		Offset             int32     `json:"offset"`
		FilterByTimeOfDay  bool      `json:"filter_by_time_of_day"`  // Enable time-of-day filtering
		TimeOfDayStart     string    `json:"time_of_day_start"`      // "HH:MM" format
		TimeOfDayEnd       string    `json:"time_of_day_end"`        // "HH:MM" format
		IncludeWeekends    bool      `json:"include_weekends"`       // Include weekends in time-of-day filter
		WeekendMode        string    `json:"weekend_mode"`           // "exclude", "same_hours", "full_weekends"
		Severities         []string  `json:"severities"`             // Filter by severities (multi-select)
		Teams              []string  `json:"teams"`                  // Filter by teams (multi-select)
		IncludeSilenced    bool      `json:"include_silenced"`       // Include alerts silenced at fire time
		Timezone           string    `json:"timezone"`               // IANA timezone for time-of-day/period bucketing
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	if request.Timezone != "" {
		if _, err := time.LoadLocation(request.Timezone); err != nil {
			c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid timezone"))
			return
		}
	}

	if request.Limit > 1000 {
		request.Limit = 1000
	}

	// Build gRPC request
	req := &alertpb.QueryStatisticsRequest{
		SessionId:          sessionID,
		StartDate:          timestamppb.New(request.StartDate),
		EndDate:            timestamppb.New(request.EndDate),
		GroupBy:            request.GroupBy,
		SecondaryGroupBy:   request.SecondaryGroupBy,
		PeriodType:         request.PeriodType,
		Limit:              request.Limit,
		Offset:             request.Offset,
		FilterByTimeOfDay:  request.FilterByTimeOfDay,
		TimeOfDayStart:     request.TimeOfDayStart,
		TimeOfDayEnd:       request.TimeOfDayEnd,
		IncludeWeekends:    request.IncludeWeekends,
		WeekendMode:        request.WeekendMode,
		Severities:         request.Severities,
		Teams:              request.Teams,
		IncludeSilenced:    request.IncludeSilenced,
		Timezone:           request.Timezone,
	}

	// Query statistics
	resp, err := backendClient.QueryStatistics(sessionID, req)
	if err != nil {
		log.Printf("Failed to query statistics: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to query statistics"))
		return
	}

	if !resp.Success {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse(resp.Message))
		return
	}

	// Convert response to JSON-friendly format
	result := gin.H{
		"total_alerts": resp.TotalAlerts,
		"statistics":   convertStatisticsMap(resp.Statistics),
	}
	if resp.TimeRange != nil {
		result["time_range"] = gin.H{
			"start": tsToTime(resp.TimeRange.Start),
			"end":   tsToTime(resp.TimeRange.End),
		}
	}

	// Add breakdown if present (for period grouping)
	if len(resp.Breakdown) > 0 {
		result["breakdown"] = convertBreakdownItems(resp.Breakdown)
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(result))
}

// QueryHeatmap handles querying the (day-of-week × hour) alert noise heatmap.
func QueryHeatmap(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	var request struct {
		StartDate       time.Time `json:"start_date" binding:"required"`
		EndDate         time.Time `json:"end_date" binding:"required"`
		Severities      []string  `json:"severities"`
		Teams           []string  `json:"teams"`
		IncludeSilenced bool      `json:"include_silenced"`
		Timezone        string    `json:"timezone"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	if request.Timezone != "" {
		if _, err := time.LoadLocation(request.Timezone); err != nil {
			c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid timezone"))
			return
		}
	}

	req := &alertpb.QueryHeatmapRequest{
		StartDate:       timestamppb.New(request.StartDate),
		EndDate:         timestamppb.New(request.EndDate),
		Severities:      request.Severities,
		Teams:           request.Teams,
		IncludeSilenced: request.IncludeSilenced,
		Timezone:        request.Timezone,
	}

	resp, err := backendClient.QueryHeatmap(sessionID, req)
	if err != nil {
		log.Printf("Failed to query heatmap: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to query heatmap"))
		return
	}

	if !resp.Success {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse(resp.Message))
		return
	}

	cells := make([]gin.H, len(resp.Cells))
	for i, cell := range resp.Cells {
		cells[i] = gin.H{
			"dow":              cell.Dow,
			"hour":             cell.Hour,
			"count":            cell.Count,
			"avg_mttr_seconds": cell.AvgMttrSeconds,
		}
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{"cells": cells}))
}

// QueryFlappingAlerts handles querying the top flapping (frequently re-firing) alerts.
func QueryFlappingAlerts(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	var request struct {
		StartDate       time.Time `json:"start_date" binding:"required"`
		EndDate         time.Time `json:"end_date" binding:"required"`
		Severities      []string  `json:"severities"`
		Teams           []string  `json:"teams"`
		IncludeSilenced bool      `json:"include_silenced"`
		MinFires        int32     `json:"min_fires"`
		Limit           int32     `json:"limit"`
		Timezone        string    `json:"timezone"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	if request.Timezone != "" {
		if _, err := time.LoadLocation(request.Timezone); err != nil {
			c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid timezone"))
			return
		}
	}

	req := &alertpb.QueryFlappingAlertsRequest{
		StartDate:       timestamppb.New(request.StartDate),
		EndDate:         timestamppb.New(request.EndDate),
		Severities:      request.Severities,
		Teams:           request.Teams,
		IncludeSilenced: request.IncludeSilenced,
		MinFires:        request.MinFires,
		Limit:           request.Limit,
		Timezone:        request.Timezone,
	}

	resp, err := backendClient.QueryFlappingAlerts(sessionID, req)
	if err != nil {
		log.Printf("Failed to query flapping alerts: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to query flapping alerts"))
		return
	}

	if !resp.Success {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse(resp.Message))
		return
	}

	alerts := make([]gin.H, len(resp.Alerts))
	for i, a := range resp.Alerts {
		alerts[i] = gin.H{
			"fingerprint":      a.Fingerprint,
			"alert_name":       a.AlertName,
			"team":             a.Team,
			"severity":         a.Severity,
			"fire_count":       a.FireCount,
			"avg_gap_seconds":  a.AvgGapSeconds,
			"fires_per_hour":   a.FiresPerHour,
			"avg_mttr_seconds": a.AvgMttrSeconds,
			"flap_score":       a.FlapScore,
		}
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{"alerts": alerts}))
}

// GetStatisticsSummary handles getting a summary of available statistics
func GetStatisticsSummary(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	// Check backend availability
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Get summary
	resp, err := backendClient.GetStatisticsSummary(sessionID)
	if err != nil {
		log.Printf("Failed to get statistics summary: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get summary"))
		return
	}

	if !resp.Success {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse(resp.Message))
		return
	}

	result := gin.H{
		"total_statistics": resp.TotalStatistics,
		"by_severity":      convertStatisticsMap(resp.BySeverity),
	}

	if resp.EarliestAlert != nil {
		result["earliest_alert"] = tsToTime(resp.EarliestAlert)
	}
	if resp.LatestAlert != nil {
		result["latest_alert"] = tsToTime(resp.LatestAlert)
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(result))
}

// GetAlertsByName handles fetching alert occurrences for a specific alert name
func GetAlertsByName(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	// Check backend availability
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Parse request body
	var request struct {
		StartDate         time.Time `json:"start_date" binding:"required"`
		EndDate           time.Time `json:"end_date" binding:"required"`
		AlertName         string    `json:"alert_name" binding:"required"`
		FilterByTimeOfDay bool      `json:"filter_by_time_of_day"`
		TimeOfDayStart    string    `json:"time_of_day_start"`
		TimeOfDayEnd      string    `json:"time_of_day_end"`
		WeekendMode       string    `json:"weekend_mode"`
		Severities        []string  `json:"severities"`
		Teams             []string  `json:"teams"`
		Limit             int32     `json:"limit"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Set default limit
	if request.Limit <= 0 {
		request.Limit = 100
	}
	if request.Limit > 1000 {
		request.Limit = 1000
	}

	// Query alerts
	alerts, totalCount, err := backendClient.GetAlertsByName(
		sessionID,
		request.AlertName,
		request.StartDate,
		request.EndDate,
		false, // applyRules - feature removed
		request.FilterByTimeOfDay,
		request.TimeOfDayStart,
		request.TimeOfDayEnd,
		request.WeekendMode,
		request.Severities,
		request.Teams,
		request.Limit,
	)
	if err != nil {
		log.Printf("Failed to get alerts by name: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get alerts: "+err.Error()))
		return
	}

	// Convert alerts to JSON-friendly format
	result := make([]gin.H, len(alerts))
	for i, alert := range alerts {
		var metadata interface{}
		if alert.Metadata != nil && len(alert.Metadata) > 0 {
			if err := json.Unmarshal(alert.Metadata, &metadata); err != nil {
				metadata = string(alert.Metadata)
			}
		}

		result[i] = gin.H{
			"id":                alert.Id,
			"fingerprint":       alert.Fingerprint,
			"alert_name":        alert.AlertName,
			"severity":          alert.Severity,
			"fired_at":          tsToTime(alert.FiredAt),
			"resolved_at":       nil,
			"mttr_seconds":      alert.MttrSeconds,
			"mtta_seconds":      alert.MttaSeconds,
			"fix_time_seconds":  alert.FixTimeSeconds,
			"metadata":          metadata,
		}
		if alert.ResolvedAt != nil {
			result[i]["resolved_at"] = tsToTime(alert.ResolvedAt)
		}
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"alerts":      result,
		"total_count": totalCount,
	}))
}

// ==================== Helper Functions ====================

// convertStatisticsMap converts a protobuf statistics map to JSON-friendly format
func convertStatisticsMap(pbStats map[string]*alertpb.AggregatedStatistics) map[string]gin.H {
	result := make(map[string]gin.H)
	for key, stats := range pbStats {
		result[key] = gin.H{
			"count":                 stats.Count,
			"avg_mttr_seconds":      stats.AvgMttrSeconds,
			"total_mttr_seconds":    stats.TotalMttrSeconds,
			"avg_mtta_seconds":      stats.AvgMttaSeconds,
			"avg_fix_time_seconds":  stats.AvgFixTimeSeconds,
		}
	}
	return result
}

// convertBreakdownItems converts protobuf breakdown items to JSON-friendly format
func convertBreakdownItems(items []*alertpb.BreakdownItem) []gin.H {
	result := make([]gin.H, len(items))
	for i, item := range items {
		result[i] = gin.H{
			"period":      item.Period,
			"start_time":  tsToTime(item.StartTime),
			"end_time":    tsToTime(item.EndTime),
			"total_count": item.TotalCount,
			"statistics":  convertStatisticsMap(item.Statistics),
		}
	}
	return result
}

// ==================== Page Handlers ====================

// StatisticsDashboardPage serves the alert statistics dashboard page
func StatisticsDashboardPage(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	pageData := pages.StatisticsDashboardData{
		User: pages.ProfileUser{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
		},
	}

	templ.Handler(pages.StatisticsDashboard(pageData)).ServeHTTP(c.Writer, c.Request)
}

// QueryRecentlyResolved handles querying recently resolved alerts
func QueryRecentlyResolved(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	// Check backend availability
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	var req struct {
		StartDate       string   `json:"start_date" binding:"required"`
		EndDate         string   `json:"end_date" binding:"required"`
		Severity        []string `json:"severity"`
		Teams           []string `json:"teams"`
		AlertNames      []string `json:"alert_names"`
		SearchQuery     string   `json:"search_query"`
		IncludeSilenced bool     `json:"include_silenced"`
		Limit           int      `json:"limit"`
		Offset          int      `json:"offset"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Parse dates
	startDate, err := time.Parse(time.RFC3339, req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid start_date format: "+err.Error()))
		return
	}

	endDate, err := time.Parse(time.RFC3339, req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid end_date format: "+err.Error()))
		return
	}

	// Default limit
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	// Query backend
	result, err := backendClient.QueryRecentlyResolved(
		sessionID,
		startDate,
		endDate,
		req.Severity,
		req.Teams,
		req.AlertNames,
		req.SearchQuery,
		req.IncludeSilenced,
		req.Limit,
		req.Offset,
	)

	if err != nil {
		log.Printf("Failed to query recently resolved alerts: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to query recently resolved alerts"))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(result))
}

// GetResolvedAlertDetails handles fetching details for a resolved alert from statistics
func GetResolvedAlertDetails(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	fingerprint := c.Param("fingerprint")
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Fingerprint is required"))
		return
	}

	// Check backend availability
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Get alert history from statistics database
	history, err := backendClient.GetAlertHistory(sessionID, fingerprint, 50)
	if err != nil {
		log.Printf("Failed to get alert history for %s: %v", fingerprint, err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get alert history"))
		return
	}

	if len(history) == 0 {
		c.JSON(http.StatusNotFound, webuimodels.ErrorResponse("No statistics found for this alert"))
		return
	}

	// Use the most recent occurrence to build alert details
	latestStat := history[0]

	// Parse metadata from the latest occurrence
	var metadata map[string]interface{}
	if latestStat.Metadata != nil {
		if err := json.Unmarshal(latestStat.Metadata, &metadata); err != nil {
			log.Printf("Failed to parse metadata for %s: %v", fingerprint, err)
			metadata = make(map[string]interface{})
		}
	}

	// Extract labels and annotations from metadata
	labels := make(map[string]string)
	annotations := make(map[string]string)

	if labelsRaw, ok := metadata["labels"].(map[string]interface{}); ok {
		for k, v := range labelsRaw {
			if str, ok := v.(string); ok {
				labels[k] = str
			}
		}
	}

	if annotationsRaw, ok := metadata["annotations"].(map[string]interface{}); ok {
		for k, v := range annotationsRaw {
			if str, ok := v.(string); ok {
				annotations[k] = str
			}
		}
	}

	// Extract other metadata fields
	source := ""
	if s, ok := metadata["source"].(string); ok {
		source = s
	}
	instance := ""
	if i, ok := metadata["instance"].(string); ok {
		instance = i
	}
	generatorURL := ""
	// Capture stores this under the snake_case key "generator_url" (see statistics_capture.go)
	if g, ok := metadata["generator_url"].(string); ok {
		generatorURL = g
	}

	// Build a DashboardAlert-compatible structure for the resolved alert
	alert := &webuimodels.DashboardAlert{
		Fingerprint:  fingerprint,
		Labels:       labels,
		Annotations:  annotations,
		StartsAt:     tsToTime(latestStat.FiredAt),
		GeneratorURL: generatorURL,
		Source:       source,
		IsResolved:   true,
		AlertName:    latestStat.AlertName,
		Severity:     latestStat.Severity,
		Instance:     instance,
		Team:         labels["team"],
		Summary:      annotations["summary"],
		Status: webuimodels.AlertStatus{
			State:       "resolved",
			SilencedBy:  []string{},
			InhibitedBy: []string{},
		},
	}

	// Set EndsAt if resolved
	if latestStat.ResolvedAt != nil {
		alert.EndsAt = tsToTime(latestStat.ResolvedAt)
	}

	// Calculate duration (using MTTR - Mean Time To Resolve)
	if latestStat.MttrSeconds > 0 {
		alert.Duration = int64(latestStat.MttrSeconds)
	}

	// Build alert details response
	details := &webuimodels.AlertDetails{
		Alert:        alert,
		GeneratorURL: generatorURL,
		StartedAt:    tsToTime(latestStat.FiredAt),
	}

	if latestStat.ResolvedAt != nil {
		endTime := tsToTime(latestStat.ResolvedAt)
		details.EndedAt = &endTime
		details.Duration = endTime.Sub(details.StartedAt)
	}

	// Get comments if available
	comments, err := backendClient.GetComments(fingerprint)
	if err == nil && len(comments) > 0 {
		details.Comments = make([]webuimodels.Comment, len(comments))
		for i, comment := range comments {
			details.Comments[i] = webuimodels.Comment{
				ID:        comment.Id,
				Username:  comment.Username,
				UserID:    comment.UserId,
				Content:   comment.Content,
				CreatedAt: tsToTime(comment.CreatedAt),
				UpdatedAt: tsToTime(comment.CreatedAt),
			}
		}
	} else {
		details.Comments = []webuimodels.Comment{}
	}

	// Get acknowledgments if available
	acknowledgments, err := backendClient.GetAcknowledgments(fingerprint)
	if err == nil && len(acknowledgments) > 0 {
		details.Acknowledgments = make([]webuimodels.Acknowledgment, len(acknowledgments))
		for i, ack := range acknowledgments {
			details.Acknowledgments[i] = webuimodels.Acknowledgment{
				ID:        ack.Id,
				Username:  ack.Username,
				UserID:    ack.UserId,
				Reason:    ack.Reason,
				CreatedAt: tsToTime(ack.CreatedAt),
				UpdatedAt: tsToTime(ack.CreatedAt),
			}
		}
	} else {
		details.Acknowledgments = []webuimodels.Acknowledgment{}
	}

	// Initialize empty silences
	details.Silences = []webuimodels.Silence{}

	// Build occurrence history for the response
	occurrences := make([]map[string]interface{}, len(history))
	for i, stat := range history {
		occ := map[string]interface{}{
			"id":          stat.Id,
			"fingerprint": stat.Fingerprint,
			"alert_name":  stat.AlertName,
			"severity":    stat.Severity,
			"fired_at":    tsToTime(stat.FiredAt),
		}

		if stat.ResolvedAt != nil {
			occ["resolved_at"] = tsToTime(stat.ResolvedAt)
		}
		if stat.AcknowledgedAt != nil {
			occ["acknowledged_at"] = tsToTime(stat.AcknowledgedAt)
		}
		if stat.MttrSeconds > 0 {
			occ["mttr_seconds"] = stat.MttrSeconds
		}
		if stat.MttaSeconds > 0 {
			occ["mtta_seconds"] = stat.MttaSeconds
		}
		if stat.FixTimeSeconds > 0 {
			occ["fix_time_seconds"] = stat.FixTimeSeconds
		}

		occurrences[i] = occ
	}

	// Return response with both details and history
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(map[string]interface{}{
		"details":     details,
		"occurrences": occurrences,
		"total_occurrences": len(history),
	}))
}
