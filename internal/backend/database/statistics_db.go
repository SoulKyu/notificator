package database

import (
	"encoding/json"
	"fmt"
	"time"

	"notificator/internal/backend/models"
)

// ==================== Alert Statistics Operations ====================

// CreateAlertStatistic creates a new alert statistics record
func (gdb *GormDB) CreateAlertStatistic(stat *models.AlertStatistic) error {
	if err := gdb.db.Create(stat).Error; err != nil {
		return fmt.Errorf("failed to create alert statistic: %w", err)
	}
	return nil
}

// UpsertAlertStatistic creates or ignores an alert statistic based on unique constraint
// If a record with the same (fingerprint, fired_at) exists, it does nothing (idempotent)
// This prevents duplicate statistics for the same alert occurrence
func (gdb *GormDB) UpsertAlertStatistic(stat *models.AlertStatistic) error {
	// Use FirstOrCreate with the unique key combination
	// This is atomic and handles concurrent calls safely
	result := gdb.db.Where(models.AlertStatistic{
		Fingerprint: stat.Fingerprint,
		FiredAt:     stat.FiredAt,
	}).FirstOrCreate(stat)

	if result.Error != nil {
		return fmt.Errorf("failed to upsert alert statistic: %w", result.Error)
	}

	return nil
}

// GetAlertStatisticByFingerprint retrieves the most recent statistic for a given fingerprint
// Returns the latest record if multiple exist
func (gdb *GormDB) GetAlertStatisticByFingerprint(fingerprint string) (*models.AlertStatistic, error) {
	var stat models.AlertStatistic

	err := gdb.db.
		Where("fingerprint = ?", fingerprint).
		Order("fired_at DESC").
		First(&stat).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get alert statistic: %w", err)
	}

	return &stat, nil
}

// GetAlertStatisticByID retrieves a specific statistic by ID
func (gdb *GormDB) GetAlertStatisticByID(id string) (*models.AlertStatistic, error) {
	var stat models.AlertStatistic

	err := gdb.db.First(&stat, "id = ?", id).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get alert statistic: %w", err)
	}

	return &stat, nil
}

// GetAlertHistoryByFingerprint retrieves ALL statistics for a given fingerprint
// Returns chronological history of fired/resolved events for building timeline
// Limit parameter controls maximum records returned (0 = no limit)
func (gdb *GormDB) GetAlertHistoryByFingerprint(fingerprint string, limit int) ([]*models.AlertStatistic, error) {
	var stats []*models.AlertStatistic

	query := gdb.db.
		Where("fingerprint = ?", fingerprint).
		Order("fired_at DESC") // Most recent first

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get alert history: %w", err)
	}

	return stats, nil
}

// UpdateAlertStatistic updates an existing alert statistic
// Typically used to add resolved_at, acknowledged_at, duration, or MTTR
func (gdb *GormDB) UpdateAlertStatistic(stat *models.AlertStatistic) error {
	if err := gdb.db.Save(stat).Error; err != nil {
		return fmt.Errorf("failed to update alert statistic: %w", err)
	}
	return nil
}

// UpdateAllUnresolvedByFingerprint updates all unresolved statistics for a fingerprint
// This handles the case where duplicate statistics exist (legacy data)
// Returns the number of records updated
func (gdb *GormDB) UpdateAllUnresolvedByFingerprint(fingerprint string, resolvedAt time.Time, durationSeconds int, metadata []byte) (int64, error) {
	result := gdb.db.Model(&models.AlertStatistic{}).
		Where("fingerprint = ? AND resolved_at IS NULL", fingerprint).
		Updates(map[string]interface{}{
			"resolved_at":      resolvedAt,
			"duration_seconds": durationSeconds,
			"metadata":         metadata,
		})

	if result.Error != nil {
		return 0, fmt.Errorf("failed to update unresolved statistics: %w", result.Error)
	}

	return result.RowsAffected, nil
}

// GetAlertStatistics retrieves alert statistics with filters and pagination
// Filters can include time range, severity, alert name, etc.
func (gdb *GormDB) GetAlertStatistics(filters map[string]interface{}, limit, offset int) ([]*models.AlertStatistic, error) {
	var stats []*models.AlertStatistic

	query := gdb.db.Model(&models.AlertStatistic{})

	// Apply filters
	if startTime, ok := filters["start_time"].(time.Time); ok {
		query = query.Where("fired_at >= ?", startTime)
	}
	if endTime, ok := filters["end_time"].(time.Time); ok {
		query = query.Where("fired_at <= ?", endTime)
	}
	if severity, ok := filters["severity"].(string); ok {
		query = query.Where("severity = ?", severity)
	}
	if severities, ok := filters["severities"].([]string); ok && len(severities) > 0 {
		query = query.Where("severity IN ?", severities)
	}
	if alertName, ok := filters["alert_name"].(string); ok {
		query = query.Where("alert_name = ?", alertName)
	}

	// Order by fired_at descending (most recent first)
	query = query.Order("fired_at DESC")

	// Apply pagination
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	err := query.Find(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get alert statistics: %w", err)
	}

	return stats, nil
}

// CountAlertStatistics counts total statistics matching filters
func (gdb *GormDB) CountAlertStatistics(filters map[string]interface{}) (int64, error) {
	var count int64

	query := gdb.db.Model(&models.AlertStatistic{})

	// Apply same filters as GetAlertStatistics
	if startTime, ok := filters["start_time"].(time.Time); ok {
		query = query.Where("fired_at >= ?", startTime)
	}
	if endTime, ok := filters["end_time"].(time.Time); ok {
		query = query.Where("fired_at <= ?", endTime)
	}
	if severity, ok := filters["severity"].(string); ok {
		query = query.Where("severity = ?", severity)
	}
	if severities, ok := filters["severities"].([]string); ok && len(severities) > 0 {
		query = query.Where("severity IN ?", severities)
	}
	if alertName, ok := filters["alert_name"].(string); ok {
		query = query.Where("alert_name = ?", alertName)
	}

	err := query.Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count alert statistics: %w", err)
	}

	return count, nil
}

// DeleteOldStatistics deletes statistics older than the retention period
// retentionDays: number of days to keep statistics
// Returns: number of deleted records
func (gdb *GormDB) DeleteOldStatistics(retentionDays int) (int64, error) {
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)

	result := gdb.db.
		Where("fired_at < ?", cutoffDate).
		Delete(&models.AlertStatistic{})

	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete old statistics: %w", result.Error)
	}

	return result.RowsAffected, nil
}

// ==================== On-Call Rules Operations ====================

// SaveOnCallRule creates or updates an on-call rule
func (gdb *GormDB) SaveOnCallRule(rule *models.OnCallRule) error {
	if err := gdb.db.Save(rule).Error; err != nil {
		return fmt.Errorf("failed to save on-call rule: %w", err)
	}
	return nil
}

// GetOnCallRules retrieves all rules for a user
// If activeOnly is true, only returns active rules
func (gdb *GormDB) GetOnCallRules(userID string, activeOnly bool) ([]*models.OnCallRule, error) {
	var rules []*models.OnCallRule

	query := gdb.db.Where("user_id = ?", userID)

	if activeOnly {
		query = query.Where("is_active = ?", true)
	}

	query = query.Order("created_at DESC")

	err := query.Find(&rules).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get on-call rules: %w", err)
	}

	return rules, nil
}

// GetActiveOnCallRules retrieves only active rules for a user
func (gdb *GormDB) GetActiveOnCallRules(userID string) ([]*models.OnCallRule, error) {
	return gdb.GetOnCallRules(userID, true)
}

// GetOnCallRuleByID retrieves a specific rule by ID
func (gdb *GormDB) GetOnCallRuleByID(ruleID string) (*models.OnCallRule, error) {
	var rule models.OnCallRule

	err := gdb.db.First(&rule, "id = ?", ruleID).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get on-call rule: %w", err)
	}

	return &rule, nil
}

// UpdateOnCallRule updates an existing on-call rule
func (gdb *GormDB) UpdateOnCallRule(rule *models.OnCallRule) error {
	if err := gdb.db.Save(rule).Error; err != nil {
		return fmt.Errorf("failed to update on-call rule: %w", err)
	}
	return nil
}

// DeleteOnCallRule deletes an on-call rule by ID
func (gdb *GormDB) DeleteOnCallRule(ruleID string) error {
	result := gdb.db.Delete(&models.OnCallRule{}, "id = ?", ruleID)

	if result.Error != nil {
		return fmt.Errorf("failed to delete on-call rule: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("on-call rule not found: %s", ruleID)
	}

	return nil
}

// ==================== Statistics Aggregates Operations ====================

// SaveStatisticsAggregate creates or updates a statistics aggregate record
func (gdb *GormDB) SaveStatisticsAggregate(aggregate *models.StatisticsAggregate) error {
	if err := gdb.db.Save(aggregate).Error; err != nil {
		return fmt.Errorf("failed to save statistics aggregate: %w", err)
	}
	return nil
}

// GetStatisticsAggregates retrieves aggregates for a user within a time range
func (gdb *GormDB) GetStatisticsAggregates(userID, periodType string, startTime, endTime time.Time) ([]*models.StatisticsAggregate, error) {
	var aggregates []*models.StatisticsAggregate

	query := gdb.db.
		Where("user_id = ?", userID).
		Where("period_type = ?", periodType).
		Where("period_start >= ?", startTime).
		Where("period_end <= ?", endTime).
		Order("period_start DESC")

	err := query.Find(&aggregates).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics aggregates: %w", err)
	}

	return aggregates, nil
}

// ==================== Helper Functions ====================

// BuildMetadataJSON converts alert metadata to JSONB
func BuildMetadataJSON(metadata map[string]interface{}) (models.JSONB, error) {
	jsonData, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	return models.JSONB(jsonData), nil
}

// BuildRuleConfigJSON converts rule config to JSONB
func BuildRuleConfigJSON(config *models.RuleConfig) (models.JSONB, error) {
	jsonData, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rule config: %w", err)
	}
	return models.JSONB(jsonData), nil
}

// ParseRuleConfig parses JSONB rule_config into RuleConfig struct
func ParseRuleConfig(jsonb models.JSONB) (*models.RuleConfig, error) {
	var config models.RuleConfig

	if err := json.Unmarshal([]byte(jsonb), &config); err != nil {
		return nil, fmt.Errorf("failed to parse rule config: %w", err)
	}

	return &config, nil
}
