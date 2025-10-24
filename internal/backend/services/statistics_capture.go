package services

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
	webuimodels "notificator/internal/webui/models"
)

// StatisticsCaptureService handles capturing alert statistics
type StatisticsCaptureService struct {
	db *database.GormDB
}

// NewStatisticsCaptureService creates a new statistics capture service
func NewStatisticsCaptureService(db *database.GormDB) *StatisticsCaptureService {
	return &StatisticsCaptureService{
		db: db,
	}
}

// CaptureAlertFired captures statistics when an alert first fires
func (scs *StatisticsCaptureService) CaptureAlertFired(alert *webuimodels.DashboardAlert) error {
	// Extract metadata from alert
	metadata, err := scs.extractMetadata(alert)
	if err != nil {
		return fmt.Errorf("failed to extract metadata: %w", err)
	}

	// Convert metadata to JSONB
	metadataJSON, err := database.BuildMetadataJSON(metadata)
	if err != nil {
		return fmt.Errorf("failed to build metadata JSON: %w", err)
	}

	// Create statistics record
	stat := &models.AlertStatistic{
		Fingerprint: alert.Fingerprint,
		AlertName:   alert.AlertName,
		Severity:    alert.Severity,
		Metadata:    metadataJSON,
		FiredAt:     alert.StartsAt,
	}

	// Save to database
	if err := scs.db.CreateAlertStatistic(stat); err != nil {
		return fmt.Errorf("failed to create alert statistic: %w", err)
	}

	log.Printf("📊 Captured statistics for alert: %s (fingerprint: %s)", alert.AlertName, alert.Fingerprint)
	return nil
}

// UpdateAlertResolved updates statistics when an alert resolves
func (scs *StatisticsCaptureService) UpdateAlertResolved(alert *webuimodels.DashboardAlert) error {
	// Find existing statistic by fingerprint
	stat, err := scs.db.GetAlertStatisticByFingerprint(alert.Fingerprint)
	if err != nil {
		// If not found, it might be an alert that fired before statistics feature was enabled
		// Log warning and skip
		log.Printf("⚠️  Alert statistic not found for fingerprint %s, skipping resolution update", alert.Fingerprint)
		return nil
	}

	// Update resolution data
	stat.ResolvedAt = &alert.ResolvedAt

	// Calculate duration in seconds
	duration := alert.ResolvedAt.Sub(alert.StartsAt)
	durationSec := int(duration.Seconds())
	stat.DurationSeconds = &durationSec

	// IMPORTANT: Update metadata to capture current status at resolution time
	// This ensures we have the correct silenced/suppressed state when the alert resolved
	metadata, err := scs.extractMetadata(alert)
	if err != nil {
		log.Printf("⚠️  Failed to extract metadata for resolved alert %s: %v", alert.Fingerprint, err)
	} else {
		metadataJSON, err := database.BuildMetadataJSON(metadata)
		if err != nil {
			log.Printf("⚠️  Failed to build metadata JSON for resolved alert %s: %v", alert.Fingerprint, err)
		} else {
			stat.Metadata = metadataJSON
			log.Printf("📊 Updated metadata for resolved alert: %s (state: %s)", alert.AlertName, alert.Status.State)
		}
	}

	// Update in database
	if err := scs.db.UpdateAlertStatistic(stat); err != nil {
		return fmt.Errorf("failed to update alert statistic: %w", err)
	}

	log.Printf("📊 Updated resolution for alert: %s (duration: %ds)", alert.AlertName, durationSec)
	return nil
}

// UpdateAlertAcknowledged updates statistics when an alert is acknowledged
func (scs *StatisticsCaptureService) UpdateAlertAcknowledged(alert *webuimodels.DashboardAlert) error {
	// Find existing statistic by fingerprint
	stat, err := scs.db.GetAlertStatisticByFingerprint(alert.Fingerprint)
	if err != nil {
		// If not found, log warning and skip
		log.Printf("⚠️  Alert statistic not found for fingerprint %s, skipping acknowledgment update", alert.Fingerprint)
		return nil
	}

	// Update acknowledgment data
	stat.AcknowledgedAt = &alert.AcknowledgedAt

	// Calculate MTTR (Mean Time To Resolve) in seconds
	// MTTR = time from alert firing to acknowledgment
	mttr := alert.AcknowledgedAt.Sub(alert.StartsAt)
	mttrSec := int(mttr.Seconds())
	stat.MTTRSeconds = &mttrSec

	// Update in database
	if err := scs.db.UpdateAlertStatistic(stat); err != nil {
		return fmt.Errorf("failed to update alert statistic: %w", err)
	}

	log.Printf("📊 Updated acknowledgment for alert: %s (MTTR: %ds)", alert.AlertName, mttrSec)
	return nil
}

// UpdateAlertResolvedMinimal updates statistics with minimal data (for worker pool)
func (scs *StatisticsCaptureService) UpdateAlertResolvedMinimal(fingerprint string, resolvedAtInterface interface{}) error {
	stat, err := scs.db.GetAlertStatisticByFingerprint(fingerprint)
	if err != nil {
		log.Printf("⚠️  Alert statistic not found for fingerprint %s, skipping resolution update", fingerprint)
		return nil
	}

	// Type assert resolved time
	resolvedAt, ok := resolvedAtInterface.(time.Time)
	if !ok {
		return fmt.Errorf("invalid resolved_at type")
	}

	stat.ResolvedAt = &resolvedAt
	duration := resolvedAt.Sub(stat.FiredAt)
	durationSec := int(duration.Seconds())
	stat.DurationSeconds = &durationSec

	if err := scs.db.UpdateAlertStatistic(stat); err != nil {
		return fmt.Errorf("failed to update alert statistic: %w", err)
	}

	return nil
}

// UpdateAlertAcknowledgedMinimal updates statistics with minimal data (for worker pool)
func (scs *StatisticsCaptureService) UpdateAlertAcknowledgedMinimal(fingerprint string, acknowledgedAtInterface interface{}) error {
	stat, err := scs.db.GetAlertStatisticByFingerprint(fingerprint)
	if err != nil {
		log.Printf("⚠️  Alert statistic not found for fingerprint %s, skipping acknowledgment update", fingerprint)
		return nil
	}

	// Type assert acknowledged time
	acknowledgedAt, ok := acknowledgedAtInterface.(time.Time)
	if !ok {
		return fmt.Errorf("invalid acknowledged_at type")
	}

	stat.AcknowledgedAt = &acknowledgedAt
	mttr := acknowledgedAt.Sub(stat.FiredAt)
	mttrSec := int(mttr.Seconds())
	stat.MTTRSeconds = &mttrSec

	if err := scs.db.UpdateAlertStatistic(stat); err != nil {
		return fmt.Errorf("failed to update alert statistic: %w", err)
	}

	return nil
}

// extractMetadata extracts all relevant metadata from an alert into a map
// This includes labels, annotations, source, instance, etc.
func (scs *StatisticsCaptureService) extractMetadata(alert *webuimodels.DashboardAlert) (map[string]interface{}, error) {
	metadata := make(map[string]interface{})

	// Extract labels
	if alert.Labels != nil && len(alert.Labels) > 0 {
		metadata["labels"] = alert.Labels
	}

	// Extract annotations
	if alert.Annotations != nil && len(alert.Annotations) > 0 {
		metadata["annotations"] = alert.Annotations
	}

	// Extract source (Alertmanager)
	if alert.Source != "" {
		metadata["source"] = alert.Source
	}

	// Extract instance
	if alert.Instance != "" {
		metadata["instance"] = alert.Instance
	}

	// Extract generator URL
	if alert.GeneratorURL != "" {
		metadata["generator_url"] = alert.GeneratorURL
	}

	// Extract team (from labels if available)
	if alert.Team != "" {
		metadata["team"] = alert.Team
	}

	// Extract status information
	if alert.Status.State != "" {
		metadata["status"] = map[string]interface{}{
			"state":        alert.Status.State,
			"silenced_by":  alert.Status.SilencedBy,
			"inhibited_by": alert.Status.InhibitedBy,
		}
	}

	// Extract summary (from annotations if available)
	if alert.Summary != "" {
		metadata["summary"] = alert.Summary
	}

	return metadata, nil
}

// CaptureAlertBatch captures multiple alerts in a batch
// Useful for initial backfill or bulk operations
func (scs *StatisticsCaptureService) CaptureAlertBatch(alerts []*webuimodels.DashboardAlert) error {
	successCount := 0
	errorCount := 0

	for _, alert := range alerts {
		if err := scs.CaptureAlertFired(alert); err != nil {
			log.Printf("❌ Failed to capture alert %s: %v", alert.Fingerprint, err)
			errorCount++
		} else {
			successCount++
		}
	}

	log.Printf("📊 Batch capture complete: %d succeeded, %d failed", successCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("batch capture completed with %d errors", errorCount)
	}

	return nil
}

// GetStatisticsSummary returns a summary of statistics for debugging/monitoring
func (scs *StatisticsCaptureService) GetStatisticsSummary() (map[string]interface{}, error) {
	// Get total count
	filters := make(map[string]interface{})
	totalCount, err := scs.db.CountAlertStatistics(filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get count for last 24 hours
	filters["start_time"] = time.Now().Add(-24 * time.Hour)
	last24hCount, err := scs.db.CountAlertStatistics(filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get 24h count: %w", err)
	}

	// Get count for last 7 days
	filters["start_time"] = time.Now().Add(-7 * 24 * time.Hour)
	last7dCount, err := scs.db.CountAlertStatistics(filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get 7d count: %w", err)
	}

	summary := map[string]interface{}{
		"total_statistics":  totalCount,
		"last_24h":          last24hCount,
		"last_7d":           last7dCount,
		"capture_enabled":   true,
		"last_check":        time.Now(),
	}

	return summary, nil
}

// ParseMetadata parses metadata JSONB back to a map
func ParseMetadata(metadataJSON models.JSONB) (map[string]interface{}, error) {
	var metadata map[string]interface{}

	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return metadata, nil
}
