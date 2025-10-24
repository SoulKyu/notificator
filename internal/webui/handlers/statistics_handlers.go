package handlers

import (
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
		StartDate  time.Time `json:"start_date" binding:"required"`
		EndDate    time.Time `json:"end_date" binding:"required"`
		ApplyRules bool      `json:"apply_rules"`
		GroupBy    string    `json:"group_by"` // "severity", "team", "period", "alert_name"
		PeriodType string    `json:"period_type"` // "hour", "day", "week", "month"
		Limit      int32     `json:"limit"`
		Offset     int32     `json:"offset"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Build gRPC request
	req := &alertpb.QueryStatisticsRequest{
		SessionId:  sessionID,
		StartDate:  timestamppb.New(request.StartDate),
		EndDate:    timestamppb.New(request.EndDate),
		ApplyRules: request.ApplyRules,
		GroupBy:    request.GroupBy,
		PeriodType: request.PeriodType,
		Limit:      request.Limit,
		Offset:     request.Offset,
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
