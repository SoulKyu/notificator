package models

import (
	"time"

	"gorm.io/gorm"
)

// AlertStatistic stores comprehensive statistics for each alert occurrence
// This table captures every alert that fires, with complete metadata for flexible querying
type AlertStatistic struct {
	ID          string `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Fingerprint string `gorm:"not null;size:500;uniqueIndex:idx_unique_fingerprint_fired" json:"fingerprint"`
	AlertName   string `gorm:"not null;size:255;index" json:"alert_name"`
	Severity    string `gorm:"not null;size:50;index:idx_fired_severity" json:"severity"`

	// Complete metadata stored as JSONB for flexible querying
	// Structure: {
	//   "labels": {"team": "platform", "env": "prod", ...},
	//   "annotations": {"summary": "High CPU", "description": "..."},
	//   "source": "alertmanager-prod",
	//   "instance": "server-01:9090",
	//   "generator_url": "http://..."
	// }
	Metadata JSONB `gorm:"type:jsonb;not null" json:"metadata"`

	// Lifecycle timestamps
	FiredAt        time.Time  `gorm:"not null;index;index:idx_fired_severity;uniqueIndex:idx_unique_fingerprint_fired" json:"fired_at"`
	ResolvedAt     *time.Time `gorm:"index" json:"resolved_at,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`

	// Computed metrics (in seconds)
	MTTRSeconds    *int `gorm:"index" json:"mttr_seconds,omitempty"`     // resolved_at - fired_at (Mean Time To Resolve)
	MTTASeconds    *int `gorm:"index" json:"mtta_seconds,omitempty"`     // acknowledged_at - fired_at (Mean Time To Acknowledge)
	FixTimeSeconds *int `gorm:"index" json:"fix_time_seconds,omitempty"` // resolved_at - acknowledged_at (Fix Time after acknowledgment)

	// Housekeeping
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BeforeCreate hook to generate ID
func (as *AlertStatistic) BeforeCreate(tx *gorm.DB) error {
	if as.ID == "" {
		as.ID = GenerateID()
	}
	return nil
}

// TableName specifies the table name for GORM
func (AlertStatistic) TableName() string {
	return "alert_statistics"
}

// StatisticsAggregate stores pre-computed statistics for common queries
// This is optional and used for performance optimization
// Daily/weekly/monthly rollups are computed in the background
type StatisticsAggregate struct {
	ID         string `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID     string `gorm:"not null;size:32;index" json:"user_id"`
	PeriodType string `gorm:"not null;size:20;index" json:"period_type"` // 'daily', 'weekly', 'monthly'
	PeriodStart time.Time `gorm:"not null;index" json:"period_start"`
	PeriodEnd   time.Time `gorm:"not null;index" json:"period_end"`

	// Pre-aggregated statistics stored as JSONB
	// Structure: {
	//   "by_severity": {
	//     "critical": {
	//       "count": 45,
	//       "avg_duration_seconds": 600,
	//       "total_duration_seconds": 27000,
	//       "avg_mttr_seconds": 300
	//     },
	//     "warning": {...}
	//   },
	//   "by_team": {
	//     "platform": {...},
	//     "database": {...}
	//   },
	//   "by_alert": {
	//     "HighCPU": {"count": 15, ...},
	//     "DiskFull": {"count": 8, ...}
	//   }
	// }
	AggregatedData JSONB `gorm:"type:jsonb;not null" json:"aggregated_data"`

	// Housekeeping
	CreatedAt time.Time `json:"created_at"`

	// Relationship
	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"user,omitempty"`
}

// BeforeCreate hook to generate ID
func (sa *StatisticsAggregate) BeforeCreate(tx *gorm.DB) error {
	if sa.ID == "" {
		sa.ID = GenerateID()
	}
	return nil
}

// TableName specifies the table name for GORM
func (StatisticsAggregate) TableName() string {
	return "statistics_aggregates"
}

// AggregatedStatistics represents statistics for a specific grouping
// Used in query responses
type AggregatedStatistics struct {
	Count              int     `json:"count"`
	AvgMTTRSeconds     float64 `json:"avg_mttr_seconds"`      // Mean Time To Resolve (resolved - fired)
	TotalMTTRSeconds   int     `json:"total_mttr_seconds"`
	AvgMTTASeconds     float64 `json:"avg_mtta_seconds"`      // Mean Time To Acknowledge (ack - fired)
	AvgFixTimeSeconds  float64 `json:"avg_fix_time_seconds"`  // Fix Time (resolved - ack)
}
