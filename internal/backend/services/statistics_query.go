package services

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"time"

	"gorm.io/gorm"
	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
)

// StatisticsQueryService handles querying and aggregating alert statistics
type StatisticsQueryService struct {
	db         *database.GormDB
	ruleEngine *RuleEngine
}

// NewStatisticsQueryService creates a new statistics query service
func NewStatisticsQueryService(db *database.GormDB) *StatisticsQueryService {
	return &StatisticsQueryService{
		db:         db,
		ruleEngine: NewRuleEngine(db),
	}
}

// QueryRequest represents a statistics query request
type QueryRequest struct {
	UserID             string
	StartDate          time.Time
	EndDate            time.Time
	ApplyRules         bool
	GroupBy            string // "severity", "team", "period", "alert_name"
	PeriodType         string // "hour", "day", "week", "month"
	Limit              int
	Offset             int
	FilterByTimeOfDay  bool   // Enable time-of-day filtering
	TimeOfDayStart     string // "HH:MM" format (e.g., "22:00")
	TimeOfDayEnd       string // "HH:MM" format (e.g., "06:00") - supports cross-midnight
}

// QueryResponse represents the aggregated statistics response
type QueryResponse struct {
	TimeRange    TimeRange                       `json:"time_range"`
	TotalAlerts  int64                           `json:"total_alerts"`
	Statistics   map[string]*models.AggregatedStatistics `json:"statistics"`
	Breakdown    []*BreakdownItem                `json:"breakdown,omitempty"`
}

// TimeRange represents the query time range
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// BreakdownItem represents a single breakdown entry (for period grouping)
type BreakdownItem struct {
	Period     string                                  `json:"period"`
	StartTime  time.Time                               `json:"start_time"`
	EndTime    time.Time                               `json:"end_time"`
	TotalCount int                                     `json:"total_count"`
	Statistics map[string]*models.AggregatedStatistics `json:"statistics"`
}

// QueryStatistics queries alert statistics with filters and aggregation
func (sqs *StatisticsQueryService) QueryStatistics(req *QueryRequest) (*QueryResponse, error) {
	// Validate query request
	if err := sqs.validateQueryRequest(req); err != nil {
		return nil, fmt.Errorf("invalid query request: %w", err)
	}

	// Build base query with time range
	baseQuery := sqs.db.GetDB().Model(&models.AlertStatistic{}).
		Where("fired_at >= ?", req.StartDate).
		Where("fired_at <= ?", req.EndDate)

	// Apply time-of-day filter if enabled
	if req.FilterByTimeOfDay && req.TimeOfDayStart != "" && req.TimeOfDayEnd != "" {
		baseQuery = sqs.applyTimeOfDayFilter(baseQuery, req.TimeOfDayStart, req.TimeOfDayEnd)
	}

	// Apply user's on-call rules if requested
	if req.ApplyRules && req.UserID != "" {
		filteredQuery, err := sqs.ruleEngine.ApplyRulesToQuery(req.UserID, baseQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to apply rules: %w", err)
		}
		baseQuery = filteredQuery
	}

	// Count total alerts
	var totalCount int64
	if err := baseQuery.Count(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count alerts: %w", err)
	}

	// Aggregate based on group_by parameter
	var statistics map[string]*models.AggregatedStatistics
	var breakdown []*BreakdownItem
	var err error

	switch req.GroupBy {
	case "severity":
		statistics, err = sqs.aggregateBySeverity(baseQuery)
	case "team":
		statistics, err = sqs.aggregateByTeam(baseQuery)
	case "alert_name":
		statistics, err = sqs.aggregateByAlertName(baseQuery, req.Limit)
	case "period":
		breakdown, err = sqs.aggregateByPeriod(baseQuery, req.PeriodType, req.StartDate, req.EndDate)
	default:
		// No grouping - return overall statistics
		statistics, err = sqs.aggregateOverall(baseQuery)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to aggregate: %w", err)
	}

	response := &QueryResponse{
		TimeRange: TimeRange{
			Start: req.StartDate,
			End:   req.EndDate,
		},
		TotalAlerts: totalCount,
		Statistics:  statistics,
		Breakdown:   breakdown,
	}

	return response, nil
}

// ==================== Validation ====================

// validateQueryRequest validates the query request parameters
func (sqs *StatisticsQueryService) validateQueryRequest(req *QueryRequest) error {
	// Validate date range
	if req.EndDate.Before(req.StartDate) {
		return fmt.Errorf("end date must be after start date")
	}

	// Calculate time range
	duration := req.EndDate.Sub(req.StartDate)

	// Apply limits based on period type
	switch req.GroupBy {
	case "period":
		switch req.PeriodType {
		case "hour":
			// Max 30 days for hourly (720 periods)
			if duration > 30*24*time.Hour {
				return fmt.Errorf("time range too large for hourly grouping (max 30 days)")
			}
		case "day":
			// Max 365 days for daily (365 periods)
			if duration > 365*24*time.Hour {
				return fmt.Errorf("time range too large for daily grouping (max 365 days)")
			}
		case "week":
			// Max 3 years for weekly (~156 periods)
			if duration > 3*365*24*time.Hour {
				return fmt.Errorf("time range too large for weekly grouping (max 3 years)")
			}
		case "month":
			// Max 10 years for monthly (120 periods)
			if duration > 10*365*24*time.Hour {
				return fmt.Errorf("time range too large for monthly grouping (max 10 years)")
			}
		}
	default:
		// For non-period grouping, allow up to 5 years
		if duration > 5*365*24*time.Hour {
			return fmt.Errorf("time range too large (max 5 years)")
		}
	}

	// Validate limit for alert_name grouping
	if req.GroupBy == "alert_name" && req.Limit > 100 {
		return fmt.Errorf("limit too large for alert_name grouping (max 100)")
	}

	return nil
}

// applyTimeOfDayFilter adds a WHERE clause to filter by time of day
// Supports cross-midnight ranges (e.g., 22:00 to 06:00 means "night hours")
func (sqs *StatisticsQueryService) applyTimeOfDayFilter(query *gorm.DB, startTime, endTime string) *gorm.DB {
	// Parse HH:MM to minutes since midnight
	startMinutes := parseTimeToMinutes(startTime)
	endMinutes := parseTimeToMinutes(endTime)

	if startMinutes < 0 || endMinutes < 0 {
		// Invalid time format, return query unchanged
		return query
	}

	// Determine if we're using PostgreSQL or SQLite
	isPostgres := sqs.db.IsPostgreSQL()

	if startMinutes <= endMinutes {
		// Same-day range (e.g., 09:00 to 18:00)
		// Filter: time_of_day BETWEEN start AND end
		if isPostgres {
			return query.Where(
				"(EXTRACT(HOUR FROM fired_at) * 60 + EXTRACT(MINUTE FROM fired_at)) BETWEEN ? AND ?",
				startMinutes, endMinutes,
			)
		}
		// SQLite
		return query.Where(
			"(CAST(strftime('%H', fired_at) AS INTEGER) * 60 + CAST(strftime('%M', fired_at) AS INTEGER)) BETWEEN ? AND ?",
			startMinutes, endMinutes,
		)
	}

	// Cross-midnight range (e.g., 22:00 to 06:00)
	// Filter: time_of_day >= start OR time_of_day <= end
	if isPostgres {
		return query.Where(
			"((EXTRACT(HOUR FROM fired_at) * 60 + EXTRACT(MINUTE FROM fired_at)) >= ? OR (EXTRACT(HOUR FROM fired_at) * 60 + EXTRACT(MINUTE FROM fired_at)) <= ?)",
			startMinutes, endMinutes,
		)
	}
	// SQLite
	return query.Where(
		"((CAST(strftime('%H', fired_at) AS INTEGER) * 60 + CAST(strftime('%M', fired_at) AS INTEGER)) >= ? OR (CAST(strftime('%H', fired_at) AS INTEGER) * 60 + CAST(strftime('%M', fired_at) AS INTEGER)) <= ?)",
		startMinutes, endMinutes,
	)
}

// parseTimeToMinutes parses "HH:MM" format to minutes since midnight
// Returns -1 if the format is invalid
func parseTimeToMinutes(timeStr string) int {
	if len(timeStr) != 5 || timeStr[2] != ':' {
		return -1
	}

	hours := 0
	minutes := 0

	// Parse hours
	for i := 0; i < 2; i++ {
		if timeStr[i] < '0' || timeStr[i] > '9' {
			return -1
		}
		hours = hours*10 + int(timeStr[i]-'0')
	}

	// Parse minutes
	for i := 3; i < 5; i++ {
		if timeStr[i] < '0' || timeStr[i] > '9' {
			return -1
		}
		minutes = minutes*10 + int(timeStr[i]-'0')
	}

	if hours > 23 || minutes > 59 {
		return -1
	}

	return hours*60 + minutes
}

// ==================== Aggregation Methods ====================

// aggregateOverall computes overall statistics without grouping
func (sqs *StatisticsQueryService) aggregateOverall(query *gorm.DB) (map[string]*models.AggregatedStatistics, error) {
	var result struct {
		Count              int64
		AvgDurationSeconds float64
		TotalDuration      int64
		AvgMTTRSeconds     float64
	}

	err := query.
		Select(`
			COUNT(*) as count,
			COALESCE(AVG(NULLIF(duration_seconds, 0)), 0) as avg_duration_seconds,
			COALESCE(SUM(duration_seconds), 0) as total_duration,
			COALESCE(AVG(NULLIF(mttr_seconds, 0)), 0) as avg_mttr_seconds
		`).
		Scan(&result).Error

	if err != nil {
		return nil, fmt.Errorf("failed to aggregate: %w", err)
	}

	stats := map[string]*models.AggregatedStatistics{
		"overall": {
			Count:              int(result.Count),
			AvgDurationSeconds: result.AvgDurationSeconds,
			TotalDurationSeconds: int(result.TotalDuration),
			AvgMTTRSeconds:     result.AvgMTTRSeconds,
		},
	}

	return stats, nil
}

// aggregateBySeverity groups statistics by severity level
func (sqs *StatisticsQueryService) aggregateBySeverity(query *gorm.DB) (map[string]*models.AggregatedStatistics, error) {
	type SeverityResult struct {
		Severity           string
		Count              int64
		AvgDurationSeconds float64
		TotalDuration      int64
		AvgMTTRSeconds     float64
	}

	var results []SeverityResult

	err := query.
		Select(`
			severity,
			COUNT(*) as count,
			COALESCE(AVG(NULLIF(duration_seconds, 0)), 0) as avg_duration_seconds,
			COALESCE(SUM(duration_seconds), 0) as total_duration,
			COALESCE(AVG(NULLIF(mttr_seconds, 0)), 0) as avg_mttr_seconds
		`).
		Group("severity").
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to aggregate by severity: %w", err)
	}

	// Convert to map
	stats := make(map[string]*models.AggregatedStatistics)
	for _, r := range results {
		stats[r.Severity] = &models.AggregatedStatistics{
			Count:              int(r.Count),
			AvgDurationSeconds: r.AvgDurationSeconds,
			TotalDurationSeconds: int(r.TotalDuration),
			AvgMTTRSeconds:     r.AvgMTTRSeconds,
		}
	}

	return stats, nil
}

// aggregateByTeam groups statistics by team label
func (sqs *StatisticsQueryService) aggregateByTeam(query *gorm.DB) (map[string]*models.AggregatedStatistics, error) {
	type TeamResult struct {
		Team               string
		Count              int64
		AvgDurationSeconds float64
		TotalDuration      int64
		AvgMTTRSeconds     float64
	}

	var results []TeamResult

	// Extract team from metadata JSONB
	err := query.
		Select(`
			COALESCE(metadata->'labels'->>'team', 'unknown') as team,
			COUNT(*) as count,
			COALESCE(AVG(NULLIF(duration_seconds, 0)), 0) as avg_duration_seconds,
			COALESCE(SUM(duration_seconds), 0) as total_duration,
			COALESCE(AVG(NULLIF(mttr_seconds, 0)), 0) as avg_mttr_seconds
		`).
		Group("team").
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to aggregate by team: %w", err)
	}

	// Convert to map
	stats := make(map[string]*models.AggregatedStatistics)
	for _, r := range results {
		stats[r.Team] = &models.AggregatedStatistics{
			Count:              int(r.Count),
			AvgDurationSeconds: r.AvgDurationSeconds,
			TotalDurationSeconds: int(r.TotalDuration),
			AvgMTTRSeconds:     r.AvgMTTRSeconds,
		}
	}

	return stats, nil
}

// aggregateByAlertName groups statistics by alert name
func (sqs *StatisticsQueryService) aggregateByAlertName(query *gorm.DB, limit int) (map[string]*models.AggregatedStatistics, error) {
	type AlertNameResult struct {
		AlertName          string
		Count              int64
		AvgDurationSeconds float64
		TotalDuration      int64
		AvgMTTRSeconds     float64
	}

	var results []AlertNameResult

	aggregateQuery := query.
		Select(`
			alert_name,
			COUNT(*) as count,
			COALESCE(AVG(NULLIF(duration_seconds, 0)), 0) as avg_duration_seconds,
			COALESCE(SUM(duration_seconds), 0) as total_duration,
			COALESCE(AVG(NULLIF(mttr_seconds, 0)), 0) as avg_mttr_seconds
		`).
		Group("alert_name").
		Order("count DESC")

	// Apply limit if specified
	if limit > 0 {
		aggregateQuery = aggregateQuery.Limit(limit)
	}

	err := aggregateQuery.Scan(&results).Error
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate by alert name: %w", err)
	}

	// Convert to map
	stats := make(map[string]*models.AggregatedStatistics)
	for _, r := range results {
		stats[r.AlertName] = &models.AggregatedStatistics{
			Count:              int(r.Count),
			AvgDurationSeconds: r.AvgDurationSeconds,
			TotalDurationSeconds: int(r.TotalDuration),
			AvgMTTRSeconds:     r.AvgMTTRSeconds,
		}
	}

	return stats, nil
}

// aggregateByPeriod groups statistics by time periods using optimized single query
func (sqs *StatisticsQueryService) aggregateByPeriod(query *gorm.DB, periodType string, startDate, endDate time.Time) ([]*BreakdownItem, error) {
	// Determine SQL date truncation function based on period type
	dateTrunc := sqs.getDateTruncSQL(periodType)

	var results []PeriodSeverityResult

	// Single optimized query with GROUP BY instead of N+1 queries
	err := query.
		Select(fmt.Sprintf(`
			%s as period,
			severity,
			COUNT(*) as count,
			COALESCE(AVG(NULLIF(duration_seconds, 0)), 0) as avg_duration_seconds,
			COALESCE(SUM(duration_seconds), 0) as total_duration,
			COALESCE(AVG(NULLIF(mttr_seconds, 0)), 0) as avg_mttr_seconds
		`, dateTrunc)).
		Group("period, severity").
		Order("period ASC").
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to aggregate by period: %w", err)
	}

	// Convert results to breakdown items
	breakdown := sqs.groupResultsByPeriod(results, periodType, startDate, endDate)

	return breakdown, nil
}

// getDateTruncSQL returns the SQL date truncation expression based on database type
func (sqs *StatisticsQueryService) getDateTruncSQL(periodType string) string {
	// Get database dialect
	dialect := sqs.db.GetDB().Dialector.Name()

	// PostgreSQL date_trunc syntax
	if dialect == "postgres" {
		switch periodType {
		case "hour":
			return "date_trunc('hour', fired_at)"
		case "day":
			return "date_trunc('day', fired_at)"
		case "week":
			return "date_trunc('week', fired_at)"
		case "month":
			return "date_trunc('month', fired_at)"
		default:
			return "date_trunc('day', fired_at)"
		}
	}

	// SQLite datetime truncation (less elegant but works)
	switch periodType {
	case "hour":
		return "datetime(fired_at, 'start of hour')"
	case "day":
		return "date(fired_at)"
	case "week":
		return "date(fired_at, 'weekday 0', '-7 days')"
	case "month":
		return "date(fired_at, 'start of month')"
	default:
		return "date(fired_at)"
	}
}

// PeriodSeverityResult represents a query result for period-severity aggregation
type PeriodSeverityResult struct {
	Period             time.Time
	Severity           string
	Count              int64
	AvgDurationSeconds float64
	TotalDuration      int64
	AvgMTTRSeconds     float64
}

// groupResultsByPeriod converts flat query results into hierarchical breakdown items
func (sqs *StatisticsQueryService) groupResultsByPeriod(results []PeriodSeverityResult, periodType string, startDate, endDate time.Time) []*BreakdownItem {

	// Group results by period
	periodMap := make(map[time.Time]*BreakdownItem)

	for _, r := range results {
		item, exists := periodMap[r.Period]
		if !exists {
			item = &BreakdownItem{
				Period:     sqs.formatPeriodLabel(r.Period, periodType),
				StartTime:  r.Period,
				EndTime:    sqs.calculatePeriodEnd(r.Period, periodType),
				TotalCount: 0,
				Statistics: make(map[string]*models.AggregatedStatistics),
			}
			periodMap[r.Period] = item
		}

		// Add severity statistics
		item.Statistics[r.Severity] = &models.AggregatedStatistics{
			Count:                int(r.Count),
			AvgDurationSeconds:   r.AvgDurationSeconds,
			TotalDurationSeconds: int(r.TotalDuration),
			AvgMTTRSeconds:       r.AvgMTTRSeconds,
		}
		item.TotalCount += int(r.Count)
	}

	// Convert map to sorted slice
	breakdown := make([]*BreakdownItem, 0, len(periodMap))
	for _, item := range periodMap {
		breakdown = append(breakdown, item)
	}

	// Sort by start time
	for i := 0; i < len(breakdown)-1; i++ {
		for j := i + 1; j < len(breakdown); j++ {
			if breakdown[i].StartTime.After(breakdown[j].StartTime) {
				breakdown[i], breakdown[j] = breakdown[j], breakdown[i]
			}
		}
	}

	return breakdown
}

// formatPeriodLabel formats period time as readable label
func (sqs *StatisticsQueryService) formatPeriodLabel(t time.Time, periodType string) string {
	switch periodType {
	case "hour":
		return t.Format("2006-01-02 15:00")
	case "day":
		return t.Format("2006-01-02")
	case "week":
		return fmt.Sprintf("Week of %s", t.Format("2006-01-02"))
	case "month":
		return t.Format("2006-01")
	default:
		return t.Format("2006-01-02")
	}
}

// calculatePeriodEnd calculates the end time for a period
func (sqs *StatisticsQueryService) calculatePeriodEnd(start time.Time, periodType string) time.Time {
	switch periodType {
	case "hour":
		return start.Add(time.Hour)
	case "day":
		return start.AddDate(0, 0, 1)
	case "week":
		return start.AddDate(0, 0, 7)
	case "month":
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(0, 0, 1)
	}
}

// ==================== Period Generation ====================

// Period represents a time period
type Period struct {
	Label string
	Start time.Time
	End   time.Time
}

// generatePeriods generates time periods based on type
func (sqs *StatisticsQueryService) generatePeriods(periodType string, start, end time.Time) []Period {
	var periods []Period

	switch periodType {
	case "hour":
		periods = sqs.generateHourlyPeriods(start, end)
	case "day":
		periods = sqs.generateDailyPeriods(start, end)
	case "week":
		periods = sqs.generateWeeklyPeriods(start, end)
	case "month":
		periods = sqs.generateMonthlyPeriods(start, end)
	default:
		// Default to daily
		periods = sqs.generateDailyPeriods(start, end)
	}

	return periods
}

func (sqs *StatisticsQueryService) generateDailyPeriods(start, end time.Time) []Period {
	var periods []Period

	// Truncate to start of day
	current := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())

	for current.Before(end) || current.Equal(end) {
		periodEnd := current.AddDate(0, 0, 1)
		if periodEnd.After(end) {
			periodEnd = end
		}

		periods = append(periods, Period{
			Label: current.Format("2006-01-02"),
			Start: current,
			End:   periodEnd,
		})

		current = periodEnd
	}

	return periods
}

func (sqs *StatisticsQueryService) generateHourlyPeriods(start, end time.Time) []Period {
	var periods []Period

	// Truncate to start of hour
	current := time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, start.Location())

	for current.Before(end) || current.Equal(end) {
		periodEnd := current.Add(time.Hour)
		if periodEnd.After(end) {
			periodEnd = end
		}

		periods = append(periods, Period{
			Label: current.Format("2006-01-02 15:00"),
			Start: current,
			End:   periodEnd,
		})

		current = periodEnd
	}

	return periods
}

func (sqs *StatisticsQueryService) generateWeeklyPeriods(start, end time.Time) []Period {
	var periods []Period

	// Truncate to start of week (Monday)
	current := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	// Go back to Monday
	for current.Weekday() != time.Monday {
		current = current.AddDate(0, 0, -1)
	}

	for current.Before(end) || current.Equal(end) {
		periodEnd := current.AddDate(0, 0, 7)
		if periodEnd.After(end) {
			periodEnd = end
		}

		periods = append(periods, Period{
			Label: fmt.Sprintf("Week of %s", current.Format("2006-01-02")),
			Start: current,
			End:   periodEnd,
		})

		current = periodEnd
	}

	return periods
}

func (sqs *StatisticsQueryService) generateMonthlyPeriods(start, end time.Time) []Period {
	var periods []Period

	// Truncate to start of month
	current := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())

	for current.Before(end) || current.Equal(end) {
		periodEnd := current.AddDate(0, 1, 0)
		if periodEnd.After(end) {
			periodEnd = end
		}

		periods = append(periods, Period{
			Label: current.Format("2006-01"),
			Start: current,
			End:   periodEnd,
		})

		current = periodEnd
	}

	return periods
}

// ==================== Helper Methods ====================

// GetStatisticsSummary returns a summary of available statistics
func (sqs *StatisticsQueryService) GetStatisticsSummary(userID string) (map[string]interface{}, error) {
	baseQuery := sqs.db.GetDB().Model(&models.AlertStatistic{})

	// Total count
	var totalCount int64
	if err := baseQuery.Count(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	// Count by severity
	severityStats, err := sqs.aggregateBySeverity(baseQuery.Session(&gorm.Session{}))
	if err != nil {
		return nil, fmt.Errorf("failed to get severity breakdown: %w", err)
	}

	// Date range
	var dateRange struct {
		MinDate time.Time
		MaxDate time.Time
	}
	if err := baseQuery.Select("MIN(fired_at) as min_date, MAX(fired_at) as max_date").Scan(&dateRange).Error; err != nil {
		return nil, fmt.Errorf("failed to get date range: %w", err)
	}

	summary := map[string]interface{}{
		"total_statistics":   totalCount,
		"by_severity":        severityStats,
		"earliest_alert":     dateRange.MinDate,
		"latest_alert":       dateRange.MaxDate,
		"user_id":            userID,
		"generated_at":       time.Now(),
	}

	return summary, nil
}

// ==================== Recently Resolved Alerts ====================

// ResolvedAlertsQueryRequest represents a query for recently resolved alerts
type ResolvedAlertsQueryRequest struct {
	UserID               string   // User ID for hidden alerts filtering
	StartDate            time.Time
	EndDate              time.Time
	Severity             []string // Optional filter (OR logic)
	Teams                []string // Optional filter (OR logic)
	AlertNames           []string // Optional filter (OR logic, supports LIKE)
	SearchQuery          string   // Search across alert name, instance, summary, description
	IncludeSilenced      bool     // Whether to include silenced (suppressed) alerts (default: false)
	Limit                int
	Offset               int
}

// ResolvedAlertItem represents a single resolved alert with full details
type ResolvedAlertItem struct {
	Fingerprint        string
	AlertName          string
	Severity           string
	OccurrenceCount    int                    // How many times this alert resolved in time range
	FirstFiredAt       time.Time              // Earliest fired_at
	LastResolvedAt     time.Time              // Most recent resolved_at
	TotalDuration      int                    // Sum of all durations
	AvgDuration        float64                // Average duration
	TotalMTTR          int                    // Sum of all MTTR
	AvgMTTR            float64                // Average MTTR
	Metadata           map[string]interface{} // Parsed JSONB
	Source             string
	Instance           string
	Team               string
	Labels             map[string]string
	Annotations        map[string]string
}

// ResolvedAlertsQueryResponse represents the response
type ResolvedAlertsQueryResponse struct {
	Alerts     []*ResolvedAlertItem
	TotalCount int64
	StartDate  time.Time
	EndDate    time.Time
}

// getHiddenFingerprints returns fingerprints hidden by user via direct hiding or rules
func (sqs *StatisticsQueryService) getHiddenFingerprints(userID string, allFingerprints []string) ([]string, error) {
	if userID == "" || len(allFingerprints) == 0 {
		return []string{}, nil
	}

	var hiddenFingerprints []string

	// Get directly hidden alerts for this user
	var directHidden []string
	err := sqs.db.GetDB().Table("user_hidden_alerts").
		Where("user_id = ?", userID).
		Where("fingerprint IN ?", allFingerprints).
		Pluck("fingerprint", &directHidden).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query hidden alerts: %w", err)
	}
	hiddenFingerprints = append(hiddenFingerprints, directHidden...)

	// Get hidden rules for this user
	var rules []models.UserHiddenRule
	err = sqs.db.GetDB().
		Where("user_id = ? AND is_enabled = ?", userID, true).
		Find(&rules).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query hidden rules: %w", err)
	}

	// For each rule, find matching fingerprints based on label matching
	if len(rules) > 0 {
		// Query alert statistics to get labels for fingerprints
		// Use array_agg to get the latest metadata for each fingerprint
		var stats []struct {
			Fingerprint string
			Metadata    string
		}
		err = sqs.db.GetDB().Table("alert_statistics").
			Where("fingerprint IN ?", allFingerprints).
			Select("fingerprint, (array_agg(metadata ORDER BY resolved_at DESC))[1] as metadata").
			Group("fingerprint").
			Scan(&stats).Error

		if err != nil {
			return nil, fmt.Errorf("failed to query alert metadata: %w", err)
		}

		// Check each alert against rules
		for _, stat := range stats {
			// Parse metadata to get labels
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(stat.Metadata), &metadata); err != nil {
				continue
			}

			labelsRaw, ok := metadata["labels"]
			if !ok {
				continue
			}

			labels, ok := labelsRaw.(map[string]interface{})
			if !ok {
				continue
			}

			// Check if any rule matches
			for _, rule := range rules {
				labelValue, exists := labels[rule.LabelKey]
				if !exists {
					continue
				}

				labelStr, ok := labelValue.(string)
				if !ok {
					continue
				}

				matches := false
				if rule.IsRegex {
					// Regex match
					re, err := regexp.Compile(rule.LabelValue)
					if err != nil {
						log.Printf("Warning: invalid regex in hidden rule %s: %v", rule.ID, err)
						continue
					}
					matches = re.MatchString(labelStr)
				} else {
					// Exact match
					matches = labelStr == rule.LabelValue
				}

				if matches {
					hiddenFingerprints = append(hiddenFingerprints, stat.Fingerprint)
					break // Don't check more rules for this fingerprint
				}
			}
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	unique := []string{}
	for _, fp := range hiddenFingerprints {
		if !seen[fp] {
			seen[fp] = true
			unique = append(unique, fp)
		}
	}

	return unique, nil
}

// QueryResolvedAlerts queries recently resolved alerts
func (sqs *StatisticsQueryService) QueryResolvedAlerts(req *ResolvedAlertsQueryRequest) (*ResolvedAlertsQueryResponse, error) {
	// Validate request
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	// Build base query - only resolved alerts
	baseQuery := sqs.db.GetDB().Model(&models.AlertStatistic{}).
		Where("resolved_at IS NOT NULL").
		Where("resolved_at >= ?", req.StartDate).
		Where("resolved_at <= ?", req.EndDate)

	// Apply filters
	if len(req.Severity) > 0 {
		baseQuery = baseQuery.Where("severity IN ?", req.Severity)
	}
	if len(req.AlertNames) > 0 {
		// OR logic for multiple alert names
		orConditions := baseQuery.Where("1 = 0") // Start with false condition
		for _, alertName := range req.AlertNames {
			orConditions = orConditions.Or("alert_name LIKE ?", "%"+alertName+"%")
		}
		baseQuery = baseQuery.Where(orConditions)
	}
	if len(req.Teams) > 0 {
		// OR logic for multiple teams - Query JSONB metadata
		baseQuery = baseQuery.Where("metadata->'labels'->>'team' IN ?", req.Teams)
	}
	if req.SearchQuery != "" {
		// Search across multiple fields: alert_name, fingerprint, and JSONB metadata fields
		searchPattern := "%" + req.SearchQuery + "%"
		baseQuery = baseQuery.Where(
			"alert_name ILIKE ? OR fingerprint ILIKE ? OR "+
				"metadata->>'source' ILIKE ? OR metadata->>'instance' ILIKE ? OR "+
				"metadata->'annotations'->>'summary' ILIKE ? OR metadata->'annotations'->>'description' ILIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	// Apply hidden alerts filtering
	// Get hidden fingerprints for this user in the time range BEFORE executing any queries
	var hiddenFingerprints []string
	if req.UserID != "" {
		// Create a separate query to get potentially matching fingerprints
		// This query is independent and won't interfere with baseQuery
		fingerprintCheckQuery := sqs.db.GetDB().Model(&models.AlertStatistic{}).
			Select("DISTINCT fingerprint").
			Where("resolved_at IS NOT NULL").
			Where("resolved_at >= ?", req.StartDate).
			Where("resolved_at <= ?", req.EndDate)

		// Apply the same filters to get accurate fingerprint list
		if len(req.Severity) > 0 {
			fingerprintCheckQuery = fingerprintCheckQuery.Where("severity IN ?", req.Severity)
		}
		if len(req.AlertNames) > 0 {
			// OR logic for multiple alert names
			orConditions := fingerprintCheckQuery.Where("1 = 0") // Start with false condition
			for _, alertName := range req.AlertNames {
				orConditions = orConditions.Or("alert_name LIKE ?", "%"+alertName+"%")
			}
			fingerprintCheckQuery = fingerprintCheckQuery.Where(orConditions)
		}
		if len(req.Teams) > 0 {
			// OR logic for multiple teams
			fingerprintCheckQuery = fingerprintCheckQuery.Where("metadata->'labels'->>'team' IN ?", req.Teams)
		}
		if req.SearchQuery != "" {
			searchPattern := "%" + req.SearchQuery + "%"
			fingerprintCheckQuery = fingerprintCheckQuery.Where(
				"alert_name ILIKE ? OR fingerprint ILIKE ? OR "+
					"metadata->>'source' ILIKE ? OR metadata->>'instance' ILIKE ? OR "+
					"metadata->'annotations'->>'summary' ILIKE ? OR metadata->'annotations'->>'description' ILIKE ?",
				searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern,
			)
		}

		var allFingerprints []string
		err := fingerprintCheckQuery.Pluck("fingerprint", &allFingerprints).Error

		if err != nil {
			log.Printf("Warning: failed to get fingerprints for hidden alerts check: %v", err)
		} else if len(allFingerprints) > 0 {
			hiddenFingerprints, err = sqs.getHiddenFingerprints(req.UserID, allFingerprints)
			if err != nil {
				log.Printf("Warning: failed to get hidden fingerprints: %v", err)
				hiddenFingerprints = []string{} // Reset on error
			}
		}

		// Apply the filter to baseQuery BEFORE it gets executed
		if len(hiddenFingerprints) > 0 {
			baseQuery = baseQuery.Where("fingerprint NOT IN ?", hiddenFingerprints)
		}
	}

	// Exclude silenced alerts by default (unless explicitly included)
	if !req.IncludeSilenced {
		// Exclude alerts that were silenced when they resolved
		// Since we now update metadata on resolution, this accurately captures the silenced state
		// Use COALESCE to handle NULL values (alerts without status field should be included)
		baseQuery = baseQuery.Where("COALESCE(metadata->'status'->>'state', '') != ?", "suppressed")
	}

	// Get aggregated results grouped by fingerprint
	type AggregatedResult struct {
		Fingerprint      string
		AlertName        string
		Severity         string
		OccurrenceCount  int
		FirstFiredAt     time.Time
		LastResolvedAt   time.Time
		TotalDuration    int64
		AvgDuration      float64
		TotalMTTR        int64
		AvgMTTR          float64
		Metadata         string // Latest metadata (JSONB as string)
	}

	// Aggregate query - note: source and instance are in metadata JSONB, not separate columns
	var aggregatedResults []AggregatedResult
	aggregateQuery := baseQuery.
		Select(`
			fingerprint,
			alert_name,
			severity,
			COUNT(*) as occurrence_count,
			MIN(fired_at) as first_fired_at,
			MAX(resolved_at) as last_resolved_at,
			COALESCE(SUM(duration_seconds), 0) as total_duration,
			COALESCE(AVG(NULLIF(duration_seconds, 0)), 0) as avg_duration,
			COALESCE(SUM(mttr_seconds), 0) as total_mttr,
			COALESCE(AVG(NULLIF(mttr_seconds, 0)), 0) as avg_mttr,
			(array_agg(metadata ORDER BY resolved_at DESC))[1] as metadata
		`).
		Group("fingerprint, alert_name, severity")

	// Get total count of unique fingerprints (before pagination)
	var totalCount int64
	countQuery := sqs.db.GetDB().Model(&models.AlertStatistic{}).
		Select("COUNT(DISTINCT fingerprint)").
		Where("resolved_at IS NOT NULL").
		Where("resolved_at >= ?", req.StartDate).
		Where("resolved_at <= ?", req.EndDate)

	if len(req.Severity) > 0 {
		countQuery = countQuery.Where("severity IN ?", req.Severity)
	}
	if len(req.AlertNames) > 0 {
		// OR logic for multiple alert names
		orConditions := countQuery.Where("1 = 0")
		for _, alertName := range req.AlertNames {
			orConditions = orConditions.Or("alert_name LIKE ?", "%"+alertName+"%")
		}
		countQuery = countQuery.Where(orConditions)
	}
	if len(req.Teams) > 0 {
		// OR logic for multiple teams
		countQuery = countQuery.Where("metadata->'labels'->>'team' IN ?", req.Teams)
	}
	if req.SearchQuery != "" {
		searchPattern := "%" + req.SearchQuery + "%"
		countQuery = countQuery.Where(
			"alert_name ILIKE ? OR fingerprint ILIKE ? OR "+
				"metadata->>'source' ILIKE ? OR metadata->>'instance' ILIKE ? OR "+
				"metadata->'annotations'->>'summary' ILIKE ? OR metadata->'annotations'->>'description' ILIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	// Apply hidden alerts filtering to count query
	// Reuse hiddenFingerprints we already calculated above
	if len(hiddenFingerprints) > 0 {
		countQuery = countQuery.Where("fingerprint NOT IN ?", hiddenFingerprints)
	}

	// Apply the same silenced filter to count query
	if !req.IncludeSilenced {
		countQuery = countQuery.Where("COALESCE(metadata->'status'->>'state', '') != ?", "suppressed")
	}

	if err := countQuery.Count(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count unique fingerprints: %w", err)
	}

	// Execute aggregated query with pagination
	err := aggregateQuery.
		Order("last_resolved_at DESC").
		Limit(req.Limit).
		Offset(req.Offset).
		Scan(&aggregatedResults).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query resolved alerts: %w", err)
	}

	// Convert to response items
	items := make([]*ResolvedAlertItem, 0, len(aggregatedResults))
	for _, result := range aggregatedResults {
		item, err := sqs.convertAggregatedToResolvedAlertItem(result)
		if err != nil {
			log.Printf("Warning: failed to convert aggregated result for %s: %v", result.Fingerprint, err)
			continue
		}
		items = append(items, item)
	}

	return &ResolvedAlertsQueryResponse{
		Alerts:     items,
		TotalCount: totalCount,
		StartDate:  req.StartDate,
		EndDate:    req.EndDate,
	}, nil
}

// convertAggregatedToResolvedAlertItem converts aggregated result to ResolvedAlertItem
func (sqs *StatisticsQueryService) convertAggregatedToResolvedAlertItem(result interface{}) (*ResolvedAlertItem, error) {
	// Type assertion to get the AggregatedResult struct
	type AggregatedResult struct {
		Fingerprint      string
		AlertName        string
		Severity         string
		OccurrenceCount  int
		FirstFiredAt     time.Time
		LastResolvedAt   time.Time
		TotalDuration    int64
		AvgDuration      float64
		TotalMTTR        int64
		AvgMTTR          float64
		Metadata         string
	}

	// Convert result interface to struct using reflection or type assertion
	var aggResult AggregatedResult
	switch v := result.(type) {
	case AggregatedResult:
		aggResult = v
	default:
		// Use reflection to convert
		resultValue := reflect.ValueOf(result)
		aggResult = AggregatedResult{
			Fingerprint:      resultValue.FieldByName("Fingerprint").String(),
			AlertName:        resultValue.FieldByName("AlertName").String(),
			Severity:         resultValue.FieldByName("Severity").String(),
			OccurrenceCount:  int(resultValue.FieldByName("OccurrenceCount").Int()),
			FirstFiredAt:     resultValue.FieldByName("FirstFiredAt").Interface().(time.Time),
			LastResolvedAt:   resultValue.FieldByName("LastResolvedAt").Interface().(time.Time),
			TotalDuration:    resultValue.FieldByName("TotalDuration").Int(),
			AvgDuration:      resultValue.FieldByName("AvgDuration").Float(),
			TotalMTTR:        resultValue.FieldByName("TotalMTTR").Int(),
			AvgMTTR:          resultValue.FieldByName("AvgMTTR").Float(),
			Metadata:         resultValue.FieldByName("Metadata").String(),
		}
	}

	// Parse metadata
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(aggResult.Metadata), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	item := &ResolvedAlertItem{
		Fingerprint:     aggResult.Fingerprint,
		AlertName:       aggResult.AlertName,
		Severity:        aggResult.Severity,
		OccurrenceCount: aggResult.OccurrenceCount,
		FirstFiredAt:    aggResult.FirstFiredAt,
		LastResolvedAt:  aggResult.LastResolvedAt,
		TotalDuration:   int(aggResult.TotalDuration),
		AvgDuration:     aggResult.AvgDuration,
		TotalMTTR:       int(aggResult.TotalMTTR),
		AvgMTTR:         aggResult.AvgMTTR,
		Metadata:        metadata,
		// Source and Instance will be extracted from metadata below
	}

	// Extract nested fields from metadata
	if labels, ok := metadata["labels"].(map[string]interface{}); ok {
		item.Labels = make(map[string]string)
		for k, v := range labels {
			if str, ok := v.(string); ok {
				item.Labels[k] = str
			}
		}
		// Extract commonly used fields
		if team, ok := labels["team"].(string); ok {
			item.Team = team
		}
	}

	if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
		item.Annotations = make(map[string]string)
		for k, v := range annotations {
			if str, ok := v.(string); ok {
				item.Annotations[k] = str
			}
		}
	}

	if source, ok := metadata["source"].(string); ok {
		item.Source = source
	}

	if instance, ok := metadata["instance"].(string); ok {
		item.Instance = instance
	}

	return item, nil
}

// GetRecentlyResolvedAlerts gets resolved alerts from last N hours
func (sqs *StatisticsQueryService) GetRecentlyResolvedAlerts(hours int, limit int) (*ResolvedAlertsQueryResponse, error) {
	now := time.Now()
	startDate := now.Add(-time.Duration(hours) * time.Hour)

	return sqs.QueryResolvedAlerts(&ResolvedAlertsQueryRequest{
		StartDate: startDate,
		EndDate:   now,
		Limit:     limit,
		Offset:    0,
	})
}
