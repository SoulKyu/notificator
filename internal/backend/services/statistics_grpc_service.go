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
	captureService *StatisticsCaptureService
	workerPool     *StatisticsWorkerPool
}

// NewStatisticsServiceGorm creates a new statistics gRPC service
func NewStatisticsServiceGorm(db *database.GormDB) *StatisticsServiceGorm {
	return &StatisticsServiceGorm{
		db:             db,
		queryService:   NewStatisticsQueryService(db),
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
		UserID:            user.ID,
		StartDate:         startDate,
		EndDate:           endDate,
		GroupBy:           req.GroupBy,
		SecondaryGroupBy:  req.SecondaryGroupBy,
		PeriodType:        req.PeriodType,
		Limit:             int(req.Limit),
		Offset:            int(req.Offset),
		FilterByTimeOfDay: req.FilterByTimeOfDay,
		TimeOfDayStart:    req.TimeOfDayStart,
		TimeOfDayEnd:      req.TimeOfDayEnd,
		IncludeWeekends:   req.IncludeWeekends,
		WeekendMode:       req.WeekendMode,
		Severities:        req.Severities,
		Teams:             req.Teams,
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
			Count:             int32(stats.Count),
			AvgMttrSeconds:    stats.AvgMTTRSeconds,
			TotalMttrSeconds:  int32(stats.TotalMTTRSeconds),
			AvgMttaSeconds:    stats.AvgMTTASeconds,
			AvgFixTimeSeconds: stats.AvgFixTimeSeconds,
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
					Count:             int32(stats.Count),
					AvgMttrSeconds:    stats.AvgMTTRSeconds,
					TotalMttrSeconds:  int32(stats.TotalMTTRSeconds),
					AvgMttaSeconds:    stats.AvgMTTASeconds,
					AvgFixTimeSeconds: stats.AvgFixTimeSeconds,
				}
			}

			pbResponse.Breakdown[i] = pbBreakdownItem
		}
	}

	return pbResponse, nil
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
				Count:             int32(stats.Count),
				AvgMttrSeconds:    stats.AvgMTTRSeconds,
				TotalMttrSeconds:  int32(stats.TotalMTTRSeconds),
				AvgMttaSeconds:    stats.AvgMTTASeconds,
				AvgFixTimeSeconds: stats.AvgFixTimeSeconds,
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

	// Calculate MTTR (Mean Time To Resolve) in seconds
	mttr := resolvedAt.Sub(stat.FiredAt)
	mttrSec := int(mttr.Seconds())
	stat.MTTRSeconds = &mttrSec

	// Calculate Fix Time (resolved - acknowledged) if acknowledged
	if stat.AcknowledgedAt != nil {
		fixTime := resolvedAt.Sub(*stat.AcknowledgedAt)
		fixTimeSec := int(fixTime.Seconds())
		stat.FixTimeSeconds = &fixTimeSec
	}

	// Update in database
	if err := s.db.UpdateAlertStatistic(stat); err != nil {
		log.Printf("❌ Failed to update alert statistic for %s: %v", req.Fingerprint, err)
		return &alertpb.UpdateAlertResolvedResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to update alert statistic: %v", err),
		}, nil
	}

	log.Printf("📊 Updated resolution for alert: fingerprint=%s (MTTR: %ds)", req.Fingerprint, mttrSec)

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
		Teams:           req.Teams,
		AlertNames:      req.AlertNames,
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
			Fingerprint:     item.Fingerprint,
			AlertName:       item.AlertName,
			Severity:        item.Severity,
			OccurrenceCount: int32(item.OccurrenceCount),
			FirstFiredAt:    timestamppb.New(item.FirstFiredAt),
			LastResolvedAt:  timestamppb.New(item.LastResolvedAt),
			TotalMttr:       int32(item.TotalMTTR),
			AvgMttr:         item.AvgMTTR,
			TotalMtta:       int32(item.TotalMTTA),
			AvgMtta:         item.AvgMTTA,
			AvgFixTime:      item.AvgFixTime,
			Labels:          item.Labels,
			Annotations:     item.Annotations,
			Source:          item.Source,
			Instance:        item.Instance,
			Team:            item.Team,
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

		if stat.MTTRSeconds != nil {
			pbStat.MttrSeconds = int32(*stat.MTTRSeconds)
		}

		if stat.MTTASeconds != nil {
			pbStat.MttaSeconds = int32(*stat.MTTASeconds)
		}

		if stat.FixTimeSeconds != nil {
			pbStat.FixTimeSeconds = int32(*stat.FixTimeSeconds)
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

// GetAlertsByName implements the GetAlertsByName RPC method
// Returns all alerts with a specific alert name, respecting filter criteria
func (s *StatisticsServiceGorm) GetAlertsByName(ctx context.Context, req *alertpb.GetAlertsByNameRequest) (*alertpb.GetAlertsByNameResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetAlertsByNameResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.AlertName == "" {
		return &alertpb.GetAlertsByNameResponse{
			Success: false,
			Message: "Alert name is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetAlertsByNameResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Validate time range
	if req.StartDate == nil || req.EndDate == nil {
		return &alertpb.GetAlertsByNameResponse{
			Success: false,
			Message: "Start date and end date are required",
		}, nil
	}

	startDate := req.StartDate.AsTime()
	endDate := req.EndDate.AsTime()

	// Build base query
	query := s.db.GetDB().Model(&models.AlertStatistic{}).
		Where("alert_name = ?", req.AlertName).
		Where("fired_at >= ? AND fired_at <= ?", startDate, endDate)

	// Apply on-call / time of day filter if requested
	if req.FilterByTimeOfDay && req.TimeOfDayStart != "" && req.TimeOfDayEnd != "" {
		query = s.queryService.applyOnCallFilter(query, req.TimeOfDayStart, req.TimeOfDayEnd, resolveWeekendMode(req.WeekendMode, req.IncludeWeekends))
	}

	// Apply severity filter if specified (multi-select, OR logic)
	if len(req.Severities) > 0 {
		query = query.Where("severity IN ?", req.Severities)
	}

	// Apply team filter if specified (multi-select, OR logic)
	if len(req.Teams) > 0 {
		query = query.Where("COALESCE(metadata->'labels'->>'team', 'unknown') IN ?", req.Teams)
	}

	// Get total count first
	var totalCount int64
	if err := query.Count(&totalCount).Error; err != nil {
		log.Printf("Failed to count alerts by name for user %s: %v", user.ID, err)
		return &alertpb.GetAlertsByNameResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to count alerts: %v", err),
		}, nil
	}

	// Apply limit
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100 // Default limit
	}
	query = query.Order("fired_at DESC").Limit(limit)

	// Execute query
	var alerts []models.AlertStatistic
	if err := query.Find(&alerts).Error; err != nil {
		log.Printf("Failed to query alerts by name for user %s: %v", user.ID, err)
		return &alertpb.GetAlertsByNameResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to query alerts: %v", err),
		}, nil
	}

	// Convert to protobuf format
	pbAlerts := make([]*alertpb.AlertStatistic, len(alerts))
	for i, stat := range alerts {
		pbAlerts[i] = &alertpb.AlertStatistic{
			Id:          stat.ID,
			Fingerprint: stat.Fingerprint,
			AlertName:   stat.AlertName,
			Severity:    stat.Severity,
			FiredAt:     timestamppb.New(stat.FiredAt),
			CreatedAt:   timestamppb.New(stat.CreatedAt),
			UpdatedAt:   timestamppb.New(stat.UpdatedAt),
		}

		if stat.ResolvedAt != nil {
			pbAlerts[i].ResolvedAt = timestamppb.New(*stat.ResolvedAt)
		}
		if stat.AcknowledgedAt != nil {
			pbAlerts[i].AcknowledgedAt = timestamppb.New(*stat.AcknowledgedAt)
		}
		if stat.MTTRSeconds != nil {
			pbAlerts[i].MttrSeconds = int32(*stat.MTTRSeconds)
		}
		if stat.MTTASeconds != nil {
			pbAlerts[i].MttaSeconds = int32(*stat.MTTASeconds)
		}
		if stat.FixTimeSeconds != nil {
			pbAlerts[i].FixTimeSeconds = int32(*stat.FixTimeSeconds)
		}
		if stat.Metadata != nil {
			pbAlerts[i].Metadata = stat.Metadata
		}
	}

	return &alertpb.GetAlertsByNameResponse{
		Success:    true,
		Message:    fmt.Sprintf("Found %d alerts", len(pbAlerts)),
		Alerts:     pbAlerts,
		TotalCount: totalCount,
	}, nil
}

// ==================== Statistics Views ====================

// GetStatisticsViews implements the GetStatisticsViews RPC method
func (s *StatisticsServiceGorm) GetStatisticsViews(ctx context.Context, req *alertpb.GetStatisticsViewsRequest) (*alertpb.GetStatisticsViewsResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetStatisticsViewsResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetStatisticsViewsResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Determine which user ID to use
	userID := user.ID
	if req.GetImpersonateUserId() != "" {
		userID = req.GetImpersonateUserId()
	}

	// Get views from database
	views, err := s.db.GetStatisticsViews(userID, req.IncludeShared)
	if err != nil {
		log.Printf("Failed to get statistics views for user %s: %v", userID, err)
		return &alertpb.GetStatisticsViewsResponse{
			Success: false,
			Message: "Failed to retrieve views",
		}, nil
	}

	// Convert to protobuf format
	pbViews := make([]*alertpb.StatisticsView, len(views))
	for i, view := range views {
		pbViews[i] = modelToProtoStatisticsView(&view)
	}

	return &alertpb.GetStatisticsViewsResponse{
		Success: true,
		Views:   pbViews,
		Message: "Views retrieved successfully",
	}, nil
}

// SaveStatisticsView implements the SaveStatisticsView RPC method
func (s *StatisticsServiceGorm) SaveStatisticsView(ctx context.Context, req *alertpb.SaveStatisticsViewRequest) (*alertpb.SaveStatisticsViewResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SaveStatisticsViewResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.Name == "" {
		return &alertpb.SaveStatisticsViewResponse{
			Success: false,
			Message: "View name is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SaveStatisticsViewResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Determine which user ID to use
	userID := user.ID
	if req.GetImpersonateUserId() != "" {
		userID = req.GetImpersonateUserId()
	}

	// Build view data JSON
	viewDataJSON, err := database.BuildStatisticsViewDataJSON(protoToModelViewData(req.ViewData))
	if err != nil {
		log.Printf("Failed to build view data JSON: %v", err)
		return &alertpb.SaveStatisticsViewResponse{
			Success: false,
			Message: "Failed to process view data",
		}, nil
	}

	// Create view model
	view := &models.StatisticsView{
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		IsShared:    req.IsShared,
		ViewData:    viewDataJSON,
	}

	// Save to database
	savedView, err := s.db.CreateStatisticsView(view)
	if err != nil {
		log.Printf("Failed to save statistics view for user %s: %v", userID, err)
		return &alertpb.SaveStatisticsViewResponse{
			Success: false,
			Message: "Failed to save view",
		}, nil
	}

	log.Printf("Statistics view '%s' saved for user %s", req.Name, userID)

	return &alertpb.SaveStatisticsViewResponse{
		Success: true,
		View:    modelToProtoStatisticsView(savedView),
		Message: "View saved successfully",
	}, nil
}

// UpdateStatisticsView implements the UpdateStatisticsView RPC method
func (s *StatisticsServiceGorm) UpdateStatisticsView(ctx context.Context, req *alertpb.UpdateStatisticsViewRequest) (*alertpb.UpdateStatisticsViewResponse, error) {
	if req.SessionId == "" {
		return &alertpb.UpdateStatisticsViewResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.ViewId == "" {
		return &alertpb.UpdateStatisticsViewResponse{
			Success: false,
			Message: "View ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.UpdateStatisticsViewResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Determine which user ID to use
	userID := user.ID
	if req.GetImpersonateUserId() != "" {
		userID = req.GetImpersonateUserId()
	}

	// Get existing view
	view, err := s.db.GetStatisticsViewByID(req.ViewId)
	if err != nil {
		return &alertpb.UpdateStatisticsViewResponse{
			Success: false,
			Message: "View not found",
		}, nil
	}

	// Verify ownership
	if view.UserID != userID {
		return &alertpb.UpdateStatisticsViewResponse{
			Success: false,
			Message: "Not authorized to update this view",
		}, nil
	}

	// Build view data JSON
	viewDataJSON, err := database.BuildStatisticsViewDataJSON(protoToModelViewData(req.ViewData))
	if err != nil {
		return &alertpb.UpdateStatisticsViewResponse{
			Success: false,
			Message: "Failed to process view data",
		}, nil
	}

	// Update view fields
	view.Name = req.Name
	view.Description = req.Description
	view.IsShared = req.IsShared
	view.ViewData = viewDataJSON

	// Save to database
	if err := s.db.UpdateStatisticsView(view); err != nil {
		log.Printf("Failed to update view %s for user %s: %v", req.ViewId, userID, err)
		return &alertpb.UpdateStatisticsViewResponse{
			Success: false,
			Message: "Failed to update view",
		}, nil
	}

	log.Printf("Statistics view %s updated for user %s", req.ViewId, userID)

	return &alertpb.UpdateStatisticsViewResponse{
		Success: true,
		View:    modelToProtoStatisticsView(view),
		Message: "View updated successfully",
	}, nil
}

// DeleteStatisticsView implements the DeleteStatisticsView RPC method
func (s *StatisticsServiceGorm) DeleteStatisticsView(ctx context.Context, req *alertpb.DeleteStatisticsViewRequest) (*alertpb.DeleteStatisticsViewResponse, error) {
	if req.SessionId == "" {
		return &alertpb.DeleteStatisticsViewResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.ViewId == "" {
		return &alertpb.DeleteStatisticsViewResponse{
			Success: false,
			Message: "View ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.DeleteStatisticsViewResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Determine which user ID to use
	userID := user.ID
	if req.GetImpersonateUserId() != "" {
		userID = req.GetImpersonateUserId()
	}

	// Delete from database (with ownership check)
	if err := s.db.DeleteStatisticsView(req.ViewId, userID); err != nil {
		log.Printf("Failed to delete view %s for user %s: %v", req.ViewId, userID, err)
		return &alertpb.DeleteStatisticsViewResponse{
			Success: false,
			Message: "Failed to delete view",
		}, nil
	}

	log.Printf("Statistics view %s deleted for user %s", req.ViewId, userID)

	return &alertpb.DeleteStatisticsViewResponse{
		Success: true,
		Message: "View deleted successfully",
	}, nil
}

// SetDefaultStatisticsView implements the SetDefaultStatisticsView RPC method
func (s *StatisticsServiceGorm) SetDefaultStatisticsView(ctx context.Context, req *alertpb.SetDefaultStatisticsViewRequest) (*alertpb.SetDefaultStatisticsViewResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SetDefaultStatisticsViewResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SetDefaultStatisticsViewResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Determine which user ID to use
	userID := user.ID
	if req.GetImpersonateUserId() != "" {
		userID = req.GetImpersonateUserId()
	}

	// If view_id is empty, clear the default
	if req.ViewId == "" {
		if err := s.db.ClearDefaultStatisticsView(userID); err != nil {
			log.Printf("Failed to clear default view for user %s: %v", userID, err)
			return &alertpb.SetDefaultStatisticsViewResponse{
				Success: false,
				Message: "Failed to clear default view",
			}, nil
		}
		log.Printf("Default statistics view cleared for user %s", userID)
		return &alertpb.SetDefaultStatisticsViewResponse{
			Success: true,
			Message: "Default view cleared successfully",
		}, nil
	}

	// Set new default
	if err := s.db.SetDefaultStatisticsView(req.ViewId, userID); err != nil {
		log.Printf("Failed to set default view %s for user %s: %v", req.ViewId, userID, err)
		return &alertpb.SetDefaultStatisticsViewResponse{
			Success: false,
			Message: "Failed to set default view",
		}, nil
	}

	log.Printf("Default statistics view set to %s for user %s", req.ViewId, userID)

	return &alertpb.SetDefaultStatisticsViewResponse{
		Success: true,
		Message: "Default view set successfully",
	}, nil
}

// Helper functions for Statistics Views

// modelToProtoStatisticsView converts model StatisticsView to protobuf StatisticsView
func modelToProtoStatisticsView(view *models.StatisticsView) *alertpb.StatisticsView {
	// Parse view data from JSONB
	viewData := database.ParseStatisticsViewData(view.ViewData)

	return &alertpb.StatisticsView{
		Id:          view.ID,
		UserId:      view.UserID,
		Name:        view.Name,
		Description: view.Description,
		IsShared:    view.IsShared,
		IsDefault:   view.IsDefault,
		ViewData:    modelToProtoViewData(viewData),
		CreatedAt:   timestamppb.New(view.CreatedAt),
		UpdatedAt:   timestamppb.New(view.UpdatedAt),
	}
}

// protoToModelViewData converts protobuf StatisticsViewData to model StatisticsViewData
func protoToModelViewData(pbData *alertpb.StatisticsViewData) *models.StatisticsViewData {
	if pbData == nil {
		return &models.StatisticsViewData{}
	}
	result := &models.StatisticsViewData{
		// Time range mode
		TimeRangeMode: pbData.TimeRangeMode,

		// Absolute dates
		DateRangeType:     pbData.DateRangeType,
		StartDate:         pbData.StartDate,
		EndDate:           pbData.EndDate,
		AbsoluteFromTime:  pbData.AbsoluteFromTime,
		AbsoluteUntilTime: pbData.AbsoluteUntilTime,

		// Time of day filtering
		FilterByTimeOfDay: pbData.FilterByTimeOfDay,
		TimeOfDayStart:    pbData.TimeOfDayStart,
		TimeOfDayEnd:      pbData.TimeOfDayEnd,
		UseOnCallPeriod:   pbData.UseOnCallPeriod,
		IncludeWeekends:   pbData.IncludeWeekends,
		WeekendMode:       pbData.WeekendMode,

		// Grouping
		GroupBy:    pbData.GroupBy,
		PeriodType: pbData.PeriodType,

		// Filter arrays
		Severities: pbData.Severities,
		Teams:      pbData.Teams,

		// Other
		ApplyRules: pbData.ApplyRules,
		Limit:      int(pbData.Limit),
	}

	// Handle RelativeFrom
	if pbData.RelativeFrom != nil {
		result.RelativeFrom = &models.RelativeTimeConfig{
			Value:   int(pbData.RelativeFrom.Value),
			Unit:    pbData.RelativeFrom.Unit,
			AllTime: pbData.RelativeFrom.AllTime,
			Now:     pbData.RelativeFrom.Now,
		}
	}

	// Handle RelativeUntil
	if pbData.RelativeUntil != nil {
		result.RelativeUntil = &models.RelativeTimeConfig{
			Value:   int(pbData.RelativeUntil.Value),
			Unit:    pbData.RelativeUntil.Unit,
			AllTime: pbData.RelativeUntil.AllTime,
			Now:     pbData.RelativeUntil.Now,
		}
	}

	return result
}

// modelToProtoViewData converts model StatisticsViewData to protobuf StatisticsViewData
func modelToProtoViewData(data *models.StatisticsViewData) *alertpb.StatisticsViewData {
	if data == nil {
		return &alertpb.StatisticsViewData{}
	}
	result := &alertpb.StatisticsViewData{
		// Time range mode
		TimeRangeMode: data.TimeRangeMode,

		// Absolute dates
		DateRangeType:     data.DateRangeType,
		StartDate:         data.StartDate,
		EndDate:           data.EndDate,
		AbsoluteFromTime:  data.AbsoluteFromTime,
		AbsoluteUntilTime: data.AbsoluteUntilTime,

		// Time of day filtering
		FilterByTimeOfDay: data.FilterByTimeOfDay,
		TimeOfDayStart:    data.TimeOfDayStart,
		TimeOfDayEnd:      data.TimeOfDayEnd,
		UseOnCallPeriod:   data.UseOnCallPeriod,
		IncludeWeekends:   data.IncludeWeekends,
		WeekendMode:       data.WeekendMode,

		// Grouping
		GroupBy:    data.GroupBy,
		PeriodType: data.PeriodType,

		// Filter arrays
		Severities: data.Severities,
		Teams:      data.Teams,

		// Other
		ApplyRules: data.ApplyRules,
		Limit:      int32(data.Limit),
	}

	// Handle RelativeFrom
	if data.RelativeFrom != nil {
		result.RelativeFrom = &alertpb.RelativeTimeConfig{
			Value:   int32(data.RelativeFrom.Value),
			Unit:    data.RelativeFrom.Unit,
			AllTime: data.RelativeFrom.AllTime,
			Now:     data.RelativeFrom.Now,
		}
	}

	// Handle RelativeUntil
	if data.RelativeUntil != nil {
		result.RelativeUntil = &alertpb.RelativeTimeConfig{
			Value:   int32(data.RelativeUntil.Value),
			Unit:    data.RelativeUntil.Unit,
			AllTime: data.RelativeUntil.AllTime,
			Now:     data.RelativeUntil.Now,
		}
	}

	return result
}
