package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
	alertpb "notificator/internal/backend/proto/alert"
)

// StatisticsServiceGorm implements the StatisticsService gRPC service
type StatisticsServiceGorm struct {
	alertpb.UnimplementedStatisticsServiceServer
	db             *database.GormDB
	queryService   *StatisticsQueryService
	ruleEngine     *RuleEngine
	captureService *StatisticsCaptureService
	workerPool     *StatisticsWorkerPool
}

// NewStatisticsServiceGorm creates a new statistics gRPC service
func NewStatisticsServiceGorm(db *database.GormDB) *StatisticsServiceGorm {
	return &StatisticsServiceGorm{
		db:             db,
		queryService:   NewStatisticsQueryService(db),
		ruleEngine:     NewRuleEngine(db),
		captureService: NewStatisticsCaptureService(db),
		workerPool:     nil, // Will be set later via SetWorkerPool
	}
}

// SetWorkerPool sets the worker pool for async statistics capture
func (s *StatisticsServiceGorm) SetWorkerPool(pool *StatisticsWorkerPool) {
	s.workerPool = pool
	log.Printf("📊 Statistics gRPC service now using worker pool for async capture")
}

// ==================== Query Statistics ====================

// QueryStatistics implements the QueryStatistics RPC method
func (s *StatisticsServiceGorm) QueryStatistics(ctx context.Context, req *alertpb.QueryStatisticsRequest) (*alertpb.QueryStatisticsResponse, error) {
	if req.SessionId == "" {
		return &alertpb.QueryStatisticsResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.QueryStatisticsResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Validate time range
	if req.StartDate == nil || req.EndDate == nil {
		return &alertpb.QueryStatisticsResponse{
			Success: false,
			Message: "Start date and end date are required",
		}, nil
	}

	// Convert proto timestamps to Go time
	startDate := req.StartDate.AsTime()
	endDate := req.EndDate.AsTime()

	// Build query request
	queryReq := &QueryRequest{
		UserID:      user.ID,
		StartDate:   startDate,
		EndDate:     endDate,
		ApplyRules:  req.ApplyRules,
		GroupBy:     req.GroupBy,
		PeriodType:  req.PeriodType,
		Limit:       int(req.Limit),
		Offset:      int(req.Offset),
	}

	// Execute query
	result, err := s.queryService.QueryStatistics(queryReq)
	if err != nil {
		log.Printf("Failed to query statistics for user %s: %v", user.ID, err)
		return &alertpb.QueryStatisticsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to query statistics: %v", err),
		}, nil
	}

	// Convert to protobuf format
	pbResponse := &alertpb.QueryStatisticsResponse{
		Success: true,
		TimeRange: &alertpb.TimeRange{
			Start: timestamppb.New(result.TimeRange.Start),
			End:   timestamppb.New(result.TimeRange.End),
		},
		TotalAlerts: result.TotalAlerts,
		Statistics:  make(map[string]*alertpb.AggregatedStatistics),
		Message:     "Statistics retrieved successfully",
	}

	// Convert statistics map
	for key, stats := range result.Statistics {
		pbResponse.Statistics[key] = &alertpb.AggregatedStatistics{
			Count:                int32(stats.Count),
			AvgDurationSeconds:   stats.AvgDurationSeconds,
			TotalDurationSeconds: int32(stats.TotalDurationSeconds),
			AvgMttrSeconds:       stats.AvgMTTRSeconds,
		}
	}

	// Convert breakdown (for period grouping)
	if result.Breakdown != nil {
		pbResponse.Breakdown = make([]*alertpb.BreakdownItem, len(result.Breakdown))
		for i, item := range result.Breakdown {
			pbBreakdownItem := &alertpb.BreakdownItem{
				Period:     item.Period,
				StartTime:  timestamppb.New(item.StartTime),
				EndTime:    timestamppb.New(item.EndTime),
				TotalCount: int32(item.TotalCount),
				Statistics: make(map[string]*alertpb.AggregatedStatistics),
			}

			// Convert nested statistics
			for key, stats := range item.Statistics {
				pbBreakdownItem.Statistics[key] = &alertpb.AggregatedStatistics{
					Count:                int32(stats.Count),
					AvgDurationSeconds:   stats.AvgDurationSeconds,
					TotalDurationSeconds: int32(stats.TotalDurationSeconds),
					AvgMttrSeconds:       stats.AvgMTTRSeconds,
				}
			}

			pbResponse.Breakdown[i] = pbBreakdownItem
		}
	}

	return pbResponse, nil
}

// ==================== On-Call Rules Management ====================

// SaveOnCallRule implements the SaveOnCallRule RPC method
func (s *StatisticsServiceGorm) SaveOnCallRule(ctx context.Context, req *alertpb.SaveOnCallRuleRequest) (*alertpb.SaveOnCallRuleResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SaveOnCallRuleResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.RuleName == "" {
		return &alertpb.SaveOnCallRuleResponse{
			Success: false,
			Message: "Rule name is required",
		}, nil
	}

	if req.RuleConfig == nil {
		return &alertpb.SaveOnCallRuleResponse{
			Success: false,
			Message: "Rule configuration is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SaveOnCallRuleResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Convert protobuf rule config to model
	ruleConfig := protoToModelRuleConfig(req.RuleConfig)

	// Validate rule
	if err := s.ruleEngine.ValidateRule(ruleConfig); err != nil {
		return &alertpb.SaveOnCallRuleResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid rule configuration: %v", err),
		}, nil
	}

	// Build rule config JSONB
	ruleConfigJSON, err := database.BuildRuleConfigJSON(ruleConfig)
	if err != nil {
		log.Printf("Failed to build rule config JSON: %v", err)
		return &alertpb.SaveOnCallRuleResponse{
			Success: false,
			Message: "Failed to process rule configuration",
		}, nil
	}

	// Create rule model
	rule := &models.OnCallRule{
		UserID:     user.ID,
		RuleName:   req.RuleName,
		RuleConfig: ruleConfigJSON,
		IsActive:   req.IsActive,
	}

	// Save to database
	if err := s.db.SaveOnCallRule(rule); err != nil {
		log.Printf("Failed to save on-call rule for user %s: %v", user.ID, err)
		return &alertpb.SaveOnCallRuleResponse{
			Success: false,
			Message: "Failed to save rule",
		}, nil
	}

	log.Printf("On-call rule '%s' saved for user %s", req.RuleName, user.ID)

	// Convert to protobuf format
	pbRule := modelToProtoRule(rule, ruleConfig)

	return &alertpb.SaveOnCallRuleResponse{
		Success: true,
		Rule:    pbRule,
		Message: "Rule saved successfully",
	}, nil
}

// GetOnCallRules implements the GetOnCallRules RPC method
func (s *StatisticsServiceGorm) GetOnCallRules(ctx context.Context, req *alertpb.GetOnCallRulesRequest) (*alertpb.GetOnCallRulesResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetOnCallRulesResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetOnCallRulesResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get rules from database
	rules, err := s.db.GetOnCallRules(user.ID, req.ActiveOnly)
	if err != nil {
		log.Printf("Failed to get on-call rules for user %s: %v", user.ID, err)
		return &alertpb.GetOnCallRulesResponse{
			Success: false,
			Message: "Failed to retrieve rules",
		}, nil
	}

	// Convert to protobuf format
	pbRules := make([]*alertpb.OnCallRule, len(rules))
	for i, rule := range rules {
		ruleConfig, err := database.ParseRuleConfig(rule.RuleConfig)
		if err != nil {
			log.Printf("Failed to parse rule config for rule %s: %v", rule.ID, err)
			continue
		}

		pbRules[i] = modelToProtoRule(rule, ruleConfig)
	}

	return &alertpb.GetOnCallRulesResponse{
		Success: true,
		Rules:   pbRules,
		Message: "Rules retrieved successfully",
	}, nil
}

// GetOnCallRule implements the GetOnCallRule RPC method
func (s *StatisticsServiceGorm) GetOnCallRule(ctx context.Context, req *alertpb.GetOnCallRuleRequest) (*alertpb.GetOnCallRuleResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetOnCallRuleResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.RuleId == "" {
		return &alertpb.GetOnCallRuleResponse{
			Success: false,
			Message: "Rule ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetOnCallRuleResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get rule from database
	rule, err := s.db.GetOnCallRuleByID(req.RuleId)
	if err != nil {
		return &alertpb.GetOnCallRuleResponse{
			Success: false,
			Message: "Rule not found",
		}, nil
	}

	// Verify ownership
	if rule.UserID != user.ID {
		return &alertpb.GetOnCallRuleResponse{
			Success: false,
			Message: "Not authorized to access this rule",
		}, nil
	}

	// Parse rule config
	ruleConfig, err := database.ParseRuleConfig(rule.RuleConfig)
	if err != nil {
		log.Printf("Failed to parse rule config: %v", err)
		return &alertpb.GetOnCallRuleResponse{
			Success: false,
			Message: "Failed to parse rule configuration",
		}, nil
	}

	// Convert to protobuf format
	pbRule := modelToProtoRule(rule, ruleConfig)

	return &alertpb.GetOnCallRuleResponse{
		Success: true,
		Rule:    pbRule,
		Message: "Rule retrieved successfully",
	}, nil
}

// UpdateOnCallRule implements the UpdateOnCallRule RPC method
func (s *StatisticsServiceGorm) UpdateOnCallRule(ctx context.Context, req *alertpb.UpdateOnCallRuleRequest) (*alertpb.UpdateOnCallRuleResponse, error) {
	if req.SessionId == "" {
		return &alertpb.UpdateOnCallRuleResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.RuleId == "" {
		return &alertpb.UpdateOnCallRuleResponse{
			Success: false,
			Message: "Rule ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.UpdateOnCallRuleResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get existing rule
	rule, err := s.db.GetOnCallRuleByID(req.RuleId)
	if err != nil {
		return &alertpb.UpdateOnCallRuleResponse{
			Success: false,
			Message: "Rule not found",
		}, nil
	}

	// Verify ownership
	if rule.UserID != user.ID {
		return &alertpb.UpdateOnCallRuleResponse{
			Success: false,
			Message: "Not authorized to update this rule",
		}, nil
	}

	// Convert and validate new rule config
	ruleConfig := protoToModelRuleConfig(req.RuleConfig)
	if err := s.ruleEngine.ValidateRule(ruleConfig); err != nil {
		return &alertpb.UpdateOnCallRuleResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid rule configuration: %v", err),
		}, nil
	}

	// Build rule config JSONB
	ruleConfigJSON, err := database.BuildRuleConfigJSON(ruleConfig)
	if err != nil {
		return &alertpb.UpdateOnCallRuleResponse{
			Success: false,
			Message: "Failed to process rule configuration",
		}, nil
	}

	// Update rule fields
	rule.RuleName = req.RuleName
	rule.RuleConfig = ruleConfigJSON
	rule.IsActive = req.IsActive

	// Save to database
	if err := s.db.UpdateOnCallRule(rule); err != nil {
		log.Printf("Failed to update rule %s for user %s: %v", req.RuleId, user.ID, err)
		return &alertpb.UpdateOnCallRuleResponse{
			Success: false,
			Message: "Failed to update rule",
		}, nil
	}

	log.Printf("On-call rule %s updated for user %s", req.RuleId, user.ID)

	// Convert to protobuf format
	pbRule := modelToProtoRule(rule, ruleConfig)

	return &alertpb.UpdateOnCallRuleResponse{
		Success: true,
		Rule:    pbRule,
		Message: "Rule updated successfully",
	}, nil
}

// DeleteOnCallRule implements the DeleteOnCallRule RPC method
func (s *StatisticsServiceGorm) DeleteOnCallRule(ctx context.Context, req *alertpb.DeleteOnCallRuleRequest) (*alertpb.DeleteOnCallRuleResponse, error) {
	if req.SessionId == "" {
		return &alertpb.DeleteOnCallRuleResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.RuleId == "" {
		return &alertpb.DeleteOnCallRuleResponse{
			Success: false,
			Message: "Rule ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.DeleteOnCallRuleResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get existing rule to verify ownership
	rule, err := s.db.GetOnCallRuleByID(req.RuleId)
	if err != nil {
		return &alertpb.DeleteOnCallRuleResponse{
			Success: false,
			Message: "Rule not found",
		}, nil
	}

	// Verify ownership
	if rule.UserID != user.ID {
		return &alertpb.DeleteOnCallRuleResponse{
			Success: false,
			Message: "Not authorized to delete this rule",
		}, nil
	}

	// Delete from database
	if err := s.db.DeleteOnCallRule(req.RuleId); err != nil {
		log.Printf("Failed to delete rule %s for user %s: %v", req.RuleId, user.ID, err)
		return &alertpb.DeleteOnCallRuleResponse{
			Success: false,
			Message: "Failed to delete rule",
		}, nil
	}

	log.Printf("On-call rule %s deleted for user %s", req.RuleId, user.ID)

	return &alertpb.DeleteOnCallRuleResponse{
		Success: true,
		Message: "Rule deleted successfully",
	}, nil
}

// TestOnCallRule implements the TestOnCallRule RPC method
func (s *StatisticsServiceGorm) TestOnCallRule(ctx context.Context, req *alertpb.TestOnCallRuleRequest) (*alertpb.TestOnCallRuleResponse, error) {
	if req.SessionId == "" {
		return &alertpb.TestOnCallRuleResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.RuleConfig == nil {
		return &alertpb.TestOnCallRuleResponse{
			Success: false,
			Message: "Rule configuration is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.TestOnCallRuleResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Convert protobuf rule config to model
	ruleConfig := protoToModelRuleConfig(req.RuleConfig)

	// Test rule
	sampleSize := int(req.SampleSize)
	if sampleSize <= 0 {
		sampleSize = 10
	}

	matches, totalCount, err := s.ruleEngine.TestRule(user.ID, ruleConfig, sampleSize)
	if err != nil {
		log.Printf("Failed to test rule for user %s: %v", user.ID, err)
		return &alertpb.TestOnCallRuleResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to test rule: %v", err),
		}, nil
	}

	// Convert sample alerts to protobuf format
	pbSampleAlerts := make([]*alertpb.AlertStatistic, len(matches))
	for i, stat := range matches {
		pbSampleAlerts[i] = &alertpb.AlertStatistic{
			Id:          stat.ID,
			Fingerprint: stat.Fingerprint,
			AlertName:   stat.AlertName,
			Severity:    stat.Severity,
			Metadata:    []byte(stat.Metadata),
			FiredAt:     timestamppb.New(stat.FiredAt),
			CreatedAt:   timestamppb.New(stat.CreatedAt),
			UpdatedAt:   timestamppb.New(stat.UpdatedAt),
		}

		if stat.ResolvedAt != nil {
			pbSampleAlerts[i].ResolvedAt = timestamppb.New(*stat.ResolvedAt)
		}
		if stat.AcknowledgedAt != nil {
			pbSampleAlerts[i].AcknowledgedAt = timestamppb.New(*stat.AcknowledgedAt)
		}
		if stat.DurationSeconds != nil {
			pbSampleAlerts[i].DurationSeconds = int32(*stat.DurationSeconds)
		}
		if stat.MTTRSeconds != nil {
			pbSampleAlerts[i].MttrSeconds = int32(*stat.MTTRSeconds)
		}
	}

	return &alertpb.TestOnCallRuleResponse{
		Success:      true,
		TotalMatches: totalCount,
		SampleAlerts: pbSampleAlerts,
		Message:      fmt.Sprintf("Rule matches %d alerts", totalCount),
	}, nil
}

// ==================== Statistics Summary ====================

// GetStatisticsSummary implements the GetStatisticsSummary RPC method
func (s *StatisticsServiceGorm) GetStatisticsSummary(ctx context.Context, req *alertpb.GetStatisticsSummaryRequest) (*alertpb.GetStatisticsSummaryResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetStatisticsSummaryResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetStatisticsSummaryResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get summary
	summary, err := s.queryService.GetStatisticsSummary(user.ID)
	if err != nil {
		log.Printf("Failed to get statistics summary for user %s: %v", user.ID, err)
		return &alertpb.GetStatisticsSummaryResponse{
			Success: false,
			Message: "Failed to retrieve summary",
		}, nil
	}

	// Convert by_severity map
	bySeverity := make(map[string]*alertpb.AggregatedStatistics)
	if severityStats, ok := summary["by_severity"].(map[string]*models.AggregatedStatistics); ok {
		for key, stats := range severityStats {
			bySeverity[key] = &alertpb.AggregatedStatistics{
				Count:                int32(stats.Count),
				AvgDurationSeconds:   stats.AvgDurationSeconds,
				TotalDurationSeconds: int32(stats.TotalDurationSeconds),
				AvgMttrSeconds:       stats.AvgMTTRSeconds,
			}
		}
	}

	// Extract timestamps
	var earliestAlert, latestAlert *timestamppb.Timestamp
	if t, ok := summary["earliest_alert"].(time.Time); ok && !t.IsZero() {
		earliestAlert = timestamppb.New(t)
	}
	if t, ok := summary["latest_alert"].(time.Time); ok && !t.IsZero() {
		latestAlert = timestamppb.New(t)
	}

	return &alertpb.GetStatisticsSummaryResponse{
		Success:         true,
		TotalStatistics: summary["total_statistics"].(int64),
		BySeverity:      bySeverity,
		EarliestAlert:   earliestAlert,
		LatestAlert:     latestAlert,
		Message:         "Summary retrieved successfully",
	}, nil
}

// ==================== Helper Functions ====================

// protoToModelRuleConfig converts protobuf RuleConfig to model RuleConfig
func protoToModelRuleConfig(pbConfig *alertpb.RuleConfig) *models.RuleConfig {
	criteria := make([]models.RuleCriterion, len(pbConfig.Criteria))
	for i, pbCriterion := range pbConfig.Criteria {
		criteria[i] = models.RuleCriterion{
			Type:     pbCriterion.Type,
			Operator: pbCriterion.Operator,
			Value:    pbCriterion.Value,
			Values:   pbCriterion.Values,
			Key:      pbCriterion.Key,
			Pattern:  pbCriterion.Pattern,
		}
	}

	return &models.RuleConfig{
		Criteria: criteria,
		Logic:    pbConfig.Logic,
	}
}

// modelToProtoRule converts model OnCallRule to protobuf OnCallRule
func modelToProtoRule(rule *models.OnCallRule, config *models.RuleConfig) *alertpb.OnCallRule {
	pbCriteria := make([]*alertpb.RuleCriterion, len(config.Criteria))
	for i, criterion := range config.Criteria {
		pbCriteria[i] = &alertpb.RuleCriterion{
			Type:     criterion.Type,
			Operator: criterion.Operator,
			Value:    criterion.Value,
			Values:   criterion.Values,
			Key:      criterion.Key,
			Pattern:  criterion.Pattern,
		}
	}

	pbConfig := &alertpb.RuleConfig{
		Criteria: pbCriteria,
		Logic:    config.Logic,
	}

	return &alertpb.OnCallRule{
		Id:         rule.ID,
		UserId:     rule.UserID,
		RuleName:   rule.RuleName,
		RuleConfig: pbConfig,
		IsActive:   rule.IsActive,
		CreatedAt:  timestamppb.New(rule.CreatedAt),
		UpdatedAt:  timestamppb.New(rule.UpdatedAt),
	}
}

// Helper to marshal metadata to JSON
func metadataToJSON(metadata map[string]interface{}) []byte {
	if metadata == nil {
		return []byte("{}")
	}
	jsonBytes, err := json.Marshal(metadata)
	if err != nil {
		log.Printf("Failed to marshal metadata: %v", err)
		return []byte("{}")
	}
	return jsonBytes
}

// ==================== Alert Statistics Capture ====================

// CaptureAlertFired implements the CaptureAlertFired RPC method
func (s *StatisticsServiceGorm) CaptureAlertFired(ctx context.Context, req *alertpb.CaptureAlertFiredRequest) (*alertpb.CaptureAlertFiredResponse, error) {
	if req.Fingerprint == "" {
		return &alertpb.CaptureAlertFiredResponse{
			Success: false,
			Message: "Fingerprint is required",
		}, nil
	}

	if req.AlertName == "" {
		return &alertpb.CaptureAlertFiredResponse{
			Success: false,
			Message: "Alert name is required",
		}, nil
	}

	// Parse metadata from bytes
	var metadata map[string]interface{}
	if len(req.Metadata) > 0 {
		if err := json.Unmarshal(req.Metadata, &metadata); err != nil {
			log.Printf("Warning: Failed to unmarshal metadata for alert %s: %v", req.Fingerprint, err)
			metadata = make(map[string]interface{})
		}
	}

	// Convert metadata to JSONB
	metadataJSON, err := database.BuildMetadataJSON(metadata)
	if err != nil {
		log.Printf("Warning: Failed to build metadata JSON for alert %s: %v", req.Fingerprint, err)
		metadataJSON = models.JSONB("{}")
	}

	// Create alert statistic record
	stat := &models.AlertStatistic{
		Fingerprint: req.Fingerprint,
		AlertName:   req.AlertName,
		Severity:    req.Severity,
		FiredAt:     req.StartsAt.AsTime(),
		Metadata:    metadataJSON,
	}

	// Save to database
	if err := s.db.CreateAlertStatistic(stat); err != nil {
		log.Printf("❌ Failed to capture alert statistic for %s: %v", req.Fingerprint, err)
		return &alertpb.CaptureAlertFiredResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to capture alert statistic: %v", err),
		}, nil
	}

	log.Printf("📊 Captured statistics for alert: %s (fingerprint: %s)", req.AlertName, req.Fingerprint)

	return &alertpb.CaptureAlertFiredResponse{
		Success: true,
		Message: "Alert statistic captured successfully",
	}, nil
}

// UpdateAlertResolved implements the UpdateAlertResolved RPC method
func (s *StatisticsServiceGorm) UpdateAlertResolved(ctx context.Context, req *alertpb.UpdateAlertResolvedRequest) (*alertpb.UpdateAlertResolvedResponse, error) {
	if req.Fingerprint == "" {
		return &alertpb.UpdateAlertResolvedResponse{
			Success: false,
			Message: "Fingerprint is required",
		}, nil
	}

	// Find existing statistic by fingerprint
	stat, err := s.db.GetAlertStatisticByFingerprint(req.Fingerprint)
	if err != nil {
		// Alert statistic not found - this is okay, might have fired before statistics were enabled
		log.Printf("⚠️  Alert statistic not found for fingerprint %s, skipping resolution update", req.Fingerprint)
		return &alertpb.UpdateAlertResolvedResponse{
			Success: true,
			Message: "Alert statistic not found (likely fired before statistics enabled)",
		}, nil
	}

	// Update resolution data
	resolvedAt := req.ResolvedAt.AsTime()
	stat.ResolvedAt = &resolvedAt

	// Calculate duration in seconds
	duration := resolvedAt.Sub(stat.FiredAt)
	durationSec := int(duration.Seconds())
	stat.DurationSeconds = &durationSec

	// Update in database
	if err := s.db.UpdateAlertStatistic(stat); err != nil {
		log.Printf("❌ Failed to update alert statistic for %s: %v", req.Fingerprint, err)
		return &alertpb.UpdateAlertResolvedResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to update alert statistic: %v", err),
		}, nil
	}

	log.Printf("📊 Updated resolution for alert: fingerprint=%s (duration: %ds)", req.Fingerprint, durationSec)

	return &alertpb.UpdateAlertResolvedResponse{
		Success: true,
		Message: "Alert statistic updated successfully",
	}, nil
}

// UpdateAlertAcknowledged implements the UpdateAlertAcknowledged RPC method
func (s *StatisticsServiceGorm) UpdateAlertAcknowledged(ctx context.Context, req *alertpb.UpdateAlertAcknowledgedRequest) (*alertpb.UpdateAlertAcknowledgedResponse, error) {
	if req.Fingerprint == "" {
		return &alertpb.UpdateAlertAcknowledgedResponse{
			Success: false,
			Message: "Fingerprint is required",
		}, nil
	}

	// Find existing statistic by fingerprint
	stat, err := s.db.GetAlertStatisticByFingerprint(req.Fingerprint)
	if err != nil {
		// Alert statistic not found - this is okay, might have fired before statistics were enabled
		log.Printf("⚠️  Alert statistic not found for fingerprint %s, skipping acknowledgment update", req.Fingerprint)
		return &alertpb.UpdateAlertAcknowledgedResponse{
			Success: true,
			Message: "Alert statistic not found (likely fired before statistics enabled)",
		}, nil
	}

	// Update acknowledgment data
	acknowledgedAt := req.AcknowledgedAt.AsTime()
	stat.AcknowledgedAt = &acknowledgedAt

	// Calculate MTTR (Mean Time To Resolve) in seconds
	// MTTR = time from alert firing to acknowledgment
	mttr := acknowledgedAt.Sub(stat.FiredAt)
	mttrSec := int(mttr.Seconds())
	stat.MTTRSeconds = &mttrSec

	// Update in database
	if err := s.db.UpdateAlertStatistic(stat); err != nil {
		log.Printf("❌ Failed to update alert statistic for %s: %v", req.Fingerprint, err)
		return &alertpb.UpdateAlertAcknowledgedResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to update alert statistic: %v", err),
		}, nil
	}

	log.Printf("📊 Updated acknowledgment for alert: fingerprint=%s (MTTR: %ds)", req.Fingerprint, mttrSec)

	return &alertpb.UpdateAlertAcknowledgedResponse{
		Success: true,
		Message: "Alert statistic updated successfully",
	}, nil
}

// ==================== Recently Resolved Alerts ====================

// QueryRecentlyResolved implements the QueryRecentlyResolved RPC method
func (s *StatisticsServiceGorm) QueryRecentlyResolved(ctx context.Context, req *alertpb.QueryRecentlyResolvedRequest) (*alertpb.QueryRecentlyResolvedResponse, error) {
	// Validate session
	if req.SessionId == "" {
		return &alertpb.QueryRecentlyResolvedResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.QueryRecentlyResolvedResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Default to last 24 hours if not specified
	startDate := req.StartDate.AsTime()
	endDate := req.EndDate.AsTime()

	if startDate.IsZero() {
		startDate = time.Now().Add(-24 * time.Hour)
	}
	if endDate.IsZero() {
		endDate = time.Now()
	}

	// Build query request
	queryReq := &ResolvedAlertsQueryRequest{
		UserID:          user.ID,
		StartDate:       startDate,
		EndDate:         endDate,
		Severity:        req.Severity,
		Team:            req.Team,
		AlertName:       req.AlertName,
		SearchQuery:     req.SearchQuery,
		IncludeSilenced: req.IncludeSilenced,
		Limit:           int(req.Limit),
		Offset:          int(req.Offset),
	}

	// Execute query
	result, err := s.queryService.QueryResolvedAlerts(queryReq)
	if err != nil {
		log.Printf("Failed to query recently resolved alerts for user %s: %v", user.ID, err)
		return &alertpb.QueryRecentlyResolvedResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to query: %v", err),
		}, nil
	}

	// Convert to proto
	alerts := make([]*alertpb.ResolvedAlertItem, len(result.Alerts))
	for i, item := range result.Alerts {
		alerts[i] = &alertpb.ResolvedAlertItem{
			Fingerprint:      item.Fingerprint,
			AlertName:        item.AlertName,
			Severity:         item.Severity,
			OccurrenceCount:  int32(item.OccurrenceCount),
			FirstFiredAt:     timestamppb.New(item.FirstFiredAt),
			LastResolvedAt:   timestamppb.New(item.LastResolvedAt),
			TotalDuration:    int32(item.TotalDuration),
			AvgDuration:      item.AvgDuration,
			TotalMttr:        int32(item.TotalMTTR),
			AvgMttr:          item.AvgMTTR,
			Labels:           item.Labels,
			Annotations:      item.Annotations,
			Source:           item.Source,
			Instance:         item.Instance,
			Team:             item.Team,
		}
	}

	return &alertpb.QueryRecentlyResolvedResponse{
		Success:    true,
		Message:    fmt.Sprintf("Found %d resolved alerts", len(alerts)),
		Alerts:     alerts,
		TotalCount: result.TotalCount,
		StartDate:  timestamppb.New(result.StartDate),
		EndDate:    timestamppb.New(result.EndDate),
	}, nil
}

// GetAlertHistory retrieves the occurrence history for a specific alert fingerprint
func (s *StatisticsServiceGorm) GetAlertHistory(
	ctx context.Context,
	req *alertpb.GetAlertHistoryRequest,
) (*alertpb.GetAlertHistoryResponse, error) {
	// Validate request
	if req.Fingerprint == "" {
		return &alertpb.GetAlertHistoryResponse{
			Success: false,
			Message: "Fingerprint is required",
		}, nil
	}

	// Default limit to 50 if not specified or 0
	limit := int(req.Limit)
	if limit == 0 {
		limit = 50
	}

	// Get history from database
	stats, err := s.db.GetAlertHistoryByFingerprint(req.Fingerprint, limit)
	if err != nil {
		fmt.Printf("Error getting alert history for fingerprint %s: %v\n", req.Fingerprint, err)
		return &alertpb.GetAlertHistoryResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to retrieve alert history: %v", err),
		}, nil
	}

	// Convert to protobuf format
	history := make([]*alertpb.AlertStatistic, len(stats))
	for i, stat := range stats {
		pbStat := &alertpb.AlertStatistic{
			Id:          stat.ID,
			Fingerprint: stat.Fingerprint,
			AlertName:   stat.AlertName,
			Severity:    stat.Severity,
			FiredAt:     timestamppb.New(stat.FiredAt),
		}

		if stat.ResolvedAt != nil {
			pbStat.ResolvedAt = timestamppb.New(*stat.ResolvedAt)
		}

		if stat.AcknowledgedAt != nil {
			pbStat.AcknowledgedAt = timestamppb.New(*stat.AcknowledgedAt)
		}

		if stat.DurationSeconds != nil {
			pbStat.DurationSeconds = int32(*stat.DurationSeconds)
		}

		if stat.MTTRSeconds != nil {
			pbStat.MttrSeconds = int32(*stat.MTTRSeconds)
		}

		// Convert metadata JSONB to bytes
		if stat.Metadata != nil {
			pbStat.Metadata = stat.Metadata
		}

		history[i] = pbStat
	}

	return &alertpb.GetAlertHistoryResponse{
		Success: true,
		Message: fmt.Sprintf("Found %d occurrences", len(history)),
		History: history,
	}, nil
}
