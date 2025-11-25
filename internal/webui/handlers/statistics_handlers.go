package handlers

import (
	"encoding/json"
	"fmt"
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
		ApplyRules         bool      `json:"apply_rules"`
		GroupBy            string    `json:"group_by"`    // "severity", "team", "period", "alert_name"
		PeriodType         string    `json:"period_type"` // "hour", "day", "week", "month"
		Limit              int32     `json:"limit"`
		Offset             int32     `json:"offset"`
		FilterByTimeOfDay  bool      `json:"filter_by_time_of_day"`  // Enable time-of-day filtering
		TimeOfDayStart     string    `json:"time_of_day_start"`      // "HH:MM" format
		TimeOfDayEnd       string    `json:"time_of_day_end"`        // "HH:MM" format
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Build gRPC request
	req := &alertpb.QueryStatisticsRequest{
		SessionId:          sessionID,
		StartDate:          timestamppb.New(request.StartDate),
		EndDate:            timestamppb.New(request.EndDate),
		ApplyRules:         request.ApplyRules,
		GroupBy:            request.GroupBy,
		PeriodType:         request.PeriodType,
		Limit:              request.Limit,
		Offset:             request.Offset,
		FilterByTimeOfDay:  request.FilterByTimeOfDay,
		TimeOfDayStart:     request.TimeOfDayStart,
		TimeOfDayEnd:       request.TimeOfDayEnd,
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
		"time_range": gin.H{
			"start": resp.TimeRange.Start.AsTime(),
			"end":   resp.TimeRange.End.AsTime(),
		},
		"total_alerts": resp.TotalAlerts,
		"statistics":   convertStatisticsMap(resp.Statistics),
	}

	// Add breakdown if present (for period grouping)
	if len(resp.Breakdown) > 0 {
		result["breakdown"] = convertBreakdownItems(resp.Breakdown)
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(result))
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
		result["earliest_alert"] = resp.EarliestAlert.AsTime()
	}
	if resp.LatestAlert != nil {
		result["latest_alert"] = resp.LatestAlert.AsTime()
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
		ApplyRules        bool      `json:"apply_rules"`
		FilterByTimeOfDay bool      `json:"filter_by_time_of_day"`
		TimeOfDayStart    string    `json:"time_of_day_start"`
		TimeOfDayEnd      string    `json:"time_of_day_end"`
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

	// Query alerts
	alerts, totalCount, err := backendClient.GetAlertsByName(
		sessionID,
		request.AlertName,
		request.StartDate,
		request.EndDate,
		request.ApplyRules,
		request.FilterByTimeOfDay,
		request.TimeOfDayStart,
		request.TimeOfDayEnd,
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
			"id":               alert.Id,
			"fingerprint":      alert.Fingerprint,
			"alert_name":       alert.AlertName,
			"severity":         alert.Severity,
			"fired_at":         alert.FiredAt.AsTime(),
			"resolved_at":      nil,
			"duration_seconds": alert.DurationSeconds,
			"mttr_seconds":     alert.MttrSeconds,
			"metadata":         metadata,
		}
		if alert.ResolvedAt != nil {
			result[i]["resolved_at"] = alert.ResolvedAt.AsTime()
		}
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"alerts":      result,
		"total_count": totalCount,
	}))
}

// ==================== On-Call Rules Endpoints ====================

// GetOnCallRules handles retrieving all on-call rules for the user
func GetOnCallRules(c *gin.Context) {
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

	// Get activeOnly query parameter
	activeOnly := c.DefaultQuery("active_only", "false") == "true"

	// Get rules
	rules, err := backendClient.GetOnCallRules(sessionID, activeOnly)
	if err != nil {
		log.Printf("Failed to get on-call rules: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get rules"))
		return
	}

	// Convert to JSON-friendly format
	result := make([]gin.H, len(rules))
	for i, rule := range rules {
		result[i] = convertRuleToJSON(rule)
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"rules": result,
	}))
}

// GetOnCallRule handles retrieving a specific on-call rule
func GetOnCallRule(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Rule ID is required"))
		return
	}

	// Check backend availability
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Get rule
	rule, err := backendClient.GetOnCallRule(sessionID, ruleID)
	if err != nil {
		log.Printf("Failed to get on-call rule: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get rule"))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(convertRuleToJSON(rule)))
}

// SaveOnCallRule handles creating a new on-call rule
func SaveOnCallRule(c *gin.Context) {
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
		RuleName   string `json:"rule_name" binding:"required"`
		RuleConfig struct {
			Criteria []struct {
				Type     string   `json:"type"`
				Operator string   `json:"operator"`
				Value    string   `json:"value"`
				Values   []string `json:"values"`
				Key      string   `json:"key"`
				Pattern  string   `json:"pattern"`
			} `json:"criteria" binding:"required"`
			Logic string `json:"logic" binding:"required"`
		} `json:"rule_config" binding:"required"`
		IsActive bool `json:"is_active"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Build gRPC request
	criteria := make([]*alertpb.RuleCriterion, len(request.RuleConfig.Criteria))
	for i, crit := range request.RuleConfig.Criteria {
		criteria[i] = &alertpb.RuleCriterion{
			Type:     crit.Type,
			Operator: crit.Operator,
			Value:    crit.Value,
			Values:   crit.Values,
			Key:      crit.Key,
			Pattern:  crit.Pattern,
		}
	}

	req := &alertpb.SaveOnCallRuleRequest{
		SessionId: sessionID,
		RuleName:  request.RuleName,
		RuleConfig: &alertpb.RuleConfig{
			Criteria: criteria,
			Logic:    request.RuleConfig.Logic,
		},
		IsActive: request.IsActive,
	}

	// Save rule
	rule, err := backendClient.SaveOnCallRule(sessionID, req)
	if err != nil {
		log.Printf("Failed to save on-call rule: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to save rule"))
		return
	}

	log.Printf("On-call rule '%s' saved successfully", request.RuleName)
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(convertRuleToJSON(rule)))
}

// UpdateOnCallRule handles updating an existing on-call rule
func UpdateOnCallRule(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Rule ID is required"))
		return
	}

	// Check backend availability
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Parse request body
	var request struct {
		RuleName   string `json:"rule_name" binding:"required"`
		RuleConfig struct {
			Criteria []struct {
				Type     string   `json:"type"`
				Operator string   `json:"operator"`
				Value    string   `json:"value"`
				Values   []string `json:"values"`
				Key      string   `json:"key"`
				Pattern  string   `json:"pattern"`
			} `json:"criteria" binding:"required"`
			Logic string `json:"logic" binding:"required"`
		} `json:"rule_config" binding:"required"`
		IsActive bool `json:"is_active"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Build gRPC request
	criteria := make([]*alertpb.RuleCriterion, len(request.RuleConfig.Criteria))
	for i, crit := range request.RuleConfig.Criteria {
		criteria[i] = &alertpb.RuleCriterion{
			Type:     crit.Type,
			Operator: crit.Operator,
			Value:    crit.Value,
			Values:   crit.Values,
			Key:      crit.Key,
			Pattern:  crit.Pattern,
		}
	}

	req := &alertpb.UpdateOnCallRuleRequest{
		SessionId: sessionID,
		RuleId:    ruleID,
		RuleName:  request.RuleName,
		RuleConfig: &alertpb.RuleConfig{
			Criteria: criteria,
			Logic:    request.RuleConfig.Logic,
		},
		IsActive: request.IsActive,
	}

	// Update rule
	rule, err := backendClient.UpdateOnCallRule(sessionID, req)
	if err != nil {
		log.Printf("Failed to update on-call rule: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to update rule"))
		return
	}

	log.Printf("On-call rule %s updated successfully", ruleID)
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(convertRuleToJSON(rule)))
}

// DeleteOnCallRule handles deleting an on-call rule
func DeleteOnCallRule(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Rule ID is required"))
		return
	}

	// Check backend availability
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Delete rule
	err := backendClient.DeleteOnCallRule(sessionID, ruleID)
	if err != nil {
		log.Printf("Failed to delete on-call rule: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to delete rule"))
		return
	}

	log.Printf("On-call rule %s deleted successfully", ruleID)
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Rule deleted successfully",
	}))
}

// TestOnCallRule handles testing an on-call rule without saving it
func TestOnCallRule(c *gin.Context) {
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
		RuleConfig struct {
			Criteria []struct {
				Type     string   `json:"type"`
				Operator string   `json:"operator"`
				Value    string   `json:"value"`
				Values   []string `json:"values"`
				Key      string   `json:"key"`
				Pattern  string   `json:"pattern"`
			} `json:"criteria" binding:"required"`
			Logic string `json:"logic" binding:"required"`
		} `json:"rule_config" binding:"required"`
		SampleSize int32 `json:"sample_size"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Build gRPC request
	criteria := make([]*alertpb.RuleCriterion, len(request.RuleConfig.Criteria))
	for i, crit := range request.RuleConfig.Criteria {
		criteria[i] = &alertpb.RuleCriterion{
			Type:     crit.Type,
			Operator: crit.Operator,
			Value:    crit.Value,
			Values:   crit.Values,
			Key:      crit.Key,
			Pattern:  crit.Pattern,
		}
	}

	req := &alertpb.TestOnCallRuleRequest{
		SessionId: sessionID,
		RuleConfig: &alertpb.RuleConfig{
			Criteria: criteria,
			Logic:    request.RuleConfig.Logic,
		},
		SampleSize: request.SampleSize,
	}

	// Test rule
	resp, err := backendClient.TestOnCallRule(sessionID, req)
	if err != nil {
		log.Printf("Failed to test on-call rule: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to test rule"))
		return
	}

	if !resp.Success {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse(resp.Message))
		return
	}

	// Convert sample alerts to JSON-friendly format
	samples := make([]gin.H, len(resp.SampleAlerts))
	for i, alert := range resp.SampleAlerts {
		samples[i] = gin.H{
			"id":          alert.Id,
			"fingerprint": alert.Fingerprint,
			"alert_name":  alert.AlertName,
			"severity":    alert.Severity,
			"fired_at":    alert.FiredAt.AsTime(),
		}
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"total_matches": resp.TotalMatches,
		"sample_alerts": samples,
		"message":       resp.Message,
	}))
}

// ==================== Helper Functions ====================

// convertStatisticsMap converts a protobuf statistics map to JSON-friendly format
func convertStatisticsMap(pbStats map[string]*alertpb.AggregatedStatistics) map[string]gin.H {
	result := make(map[string]gin.H)
	for key, stats := range pbStats {
		result[key] = gin.H{
			"count":                  stats.Count,
			"avg_duration_seconds":   stats.AvgDurationSeconds,
			"total_duration_seconds": stats.TotalDurationSeconds,
			"avg_mttr_seconds":       stats.AvgMttrSeconds,
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
			"start_time":  item.StartTime.AsTime(),
			"end_time":    item.EndTime.AsTime(),
			"total_count": item.TotalCount,
			"statistics":  convertStatisticsMap(item.Statistics),
		}
	}
	return result
}

// convertRuleToJSON converts a protobuf rule to JSON-friendly format
func convertRuleToJSON(rule *alertpb.OnCallRule) gin.H {
	criteria := make([]gin.H, len(rule.RuleConfig.Criteria))
	for i, crit := range rule.RuleConfig.Criteria {
		criteria[i] = gin.H{
			"type":     crit.Type,
			"operator": crit.Operator,
			"value":    crit.Value,
			"values":   crit.Values,
			"key":      crit.Key,
			"pattern":  crit.Pattern,
		}
	}

	return gin.H{
		"id":        rule.Id,
		"user_id":   rule.UserId,
		"rule_name": rule.RuleName,
		"rule_config": gin.H{
			"criteria": criteria,
			"logic":    rule.RuleConfig.Logic,
		},
		"is_active":  rule.IsActive,
		"created_at": rule.CreatedAt.AsTime(),
		"updated_at": rule.UpdatedAt.AsTime(),
	}
}

// ==================== Page Handlers ====================

// OnCallRulesPage serves the on-call rules configuration page
func OnCallRulesPage(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	pageData := pages.OnCallRulesData{
		User: pages.ProfileUser{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
		},
	}

	templ.Handler(pages.OnCallRules(pageData)).ServeHTTP(c.Writer, c.Request)
}

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
		StartDate       string   `json:"start_date"`
		EndDate         string   `json:"end_date"`
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
	if g, ok := metadata["generatorURL"].(string); ok {
		generatorURL = g
	}

	// Build a DashboardAlert-compatible structure for the resolved alert
	alert := &webuimodels.DashboardAlert{
		Fingerprint:  fingerprint,
		Labels:       labels,
		Annotations:  annotations,
		StartsAt:     latestStat.FiredAt.AsTime(),
		GeneratorURL: generatorURL,
		Source:       source,
		IsResolved:   true,
		AlertName:    latestStat.AlertName,
		Severity:     latestStat.Severity,
		Instance:     instance,
		Team:         labels["team"],
		Summary:      annotations["summary"],
	}

	// Set EndsAt if resolved
	if latestStat.ResolvedAt != nil {
		alert.EndsAt = latestStat.ResolvedAt.AsTime()
	}

	// Calculate duration
	if latestStat.DurationSeconds > 0 {
		alert.Duration = int64(latestStat.DurationSeconds)
	}

	// Build alert details response
	details := &webuimodels.AlertDetails{
		Alert:        alert,
		GeneratorURL: generatorURL,
		StartedAt:    latestStat.FiredAt.AsTime(),
	}

	if latestStat.ResolvedAt != nil {
		endTime := latestStat.ResolvedAt.AsTime()
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
				CreatedAt: comment.CreatedAt.AsTime(),
				UpdatedAt: comment.CreatedAt.AsTime(),
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
				ID:        fmt.Sprintf("%d", ack.Id),
				Username:  ack.Username,
				UserID:    fmt.Sprintf("%d", ack.UserId),
				Reason:    ack.Reason,
				CreatedAt: ack.CreatedAt.AsTime(),
				UpdatedAt: ack.CreatedAt.AsTime(),
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
			"fired_at":    stat.FiredAt.AsTime(),
		}

		if stat.ResolvedAt != nil {
			occ["resolved_at"] = stat.ResolvedAt.AsTime()
		}
		if stat.AcknowledgedAt != nil {
			occ["acknowledged_at"] = stat.AcknowledgedAt.AsTime()
		}
		if stat.DurationSeconds > 0 {
			occ["duration_seconds"] = stat.DurationSeconds
		}
		if stat.MttrSeconds > 0 {
			occ["mttr_seconds"] = stat.MttrSeconds
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
