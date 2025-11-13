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
	DurationSeconds *int `gorm:"index" json:"duration_seconds,omitempty"` // resolved_at - fired_at
	MTTRSeconds     *int `gorm:"index" json:"mttr_seconds,omitempty"`     // acknowledged_at - fired_at (Mean Time To Resolve)

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

// OnCallRule represents a user-defined rule for classifying alerts as "on-call"
// Users create multi-criteria rules (labels, severity, alert names) to define
// which alerts are important to them during on-call periods
type OnCallRule struct {
	ID       string `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID   string `gorm:"not null;size:32;index" json:"user_id"`
	RuleName string `gorm:"not null;size:255" json:"rule_name"`

	// Rule definition stored as JSONB
	// Structure: {
	//   "criteria": [
	//     {"type": "severity", "operator": "in", "values": ["critical", "warning"]},
	//     {"type": "label", "key": "team", "operator": "equals", "value": "platform"},
	//     {"type": "alert_name", "operator": "regex", "pattern": "^PagerDuty.*"}
	//   ],
	//   "logic": "AND"
	// }
	RuleConfig JSONB `gorm:"type:jsonb;not null" json:"rule_config"`

	// Active/inactive toggle
	IsActive bool `gorm:"default:true;index" json:"is_active"`

	// Housekeeping
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Relationship
	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"user,omitempty"`
}

// BeforeCreate hook to generate ID
func (ocr *OnCallRule) BeforeCreate(tx *gorm.DB) error {
	if ocr.ID == "" {
		ocr.ID = GenerateID()
	}
	return nil
}

// TableName specifies the table name for GORM
func (OnCallRule) TableName() string {
	return "on_call_rules"
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

// RuleCriterion represents a single criterion in an on-call rule
// Used for parsing and validating rule configurations
type RuleCriterion struct {
	Type     string   `json:"type"`               // "severity", "label", "alert_name"
	Operator string   `json:"operator"`           // "equals", "in", "regex", "contains", etc.
	Key      string   `json:"key,omitempty"`      // For label criteria: label key
	Value    string   `json:"value,omitempty"`    // For single-value operators
	Values   []string `json:"values,omitempty"`   // For multi-value operators (e.g., "in")
	Pattern  string   `json:"pattern,omitempty"`  // For regex/pattern matching
}

// RuleConfig represents the complete configuration of an on-call rule
// Used for parsing rule_config JSONB field
type RuleConfig struct {
	Criteria []RuleCriterion `json:"criteria"` // List of criteria
	Logic    string          `json:"logic"`    // "AND" or "OR"
}

// AggregatedStatistics represents statistics for a specific grouping
// Used in query responses
type AggregatedStatistics struct {
	Count              int     `json:"count"`
	AvgDurationSeconds float64 `json:"avg_duration_seconds"`
	TotalDurationSeconds int   `json:"total_duration_seconds"`
	AvgMTTRSeconds     float64 `json:"avg_mttr_seconds"`
}
