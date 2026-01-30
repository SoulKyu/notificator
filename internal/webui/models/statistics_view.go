package models

import "time"

// StatisticsView represents a saved filter configuration for the statistics dashboard
type StatisticsView struct {
	ID          string             `json:"id"`
	UserID      string             `json:"user_id"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	IsShared    bool               `json:"is_shared"`
	IsDefault   bool               `json:"is_default"`
	ViewData    StatisticsViewData `json:"view_data"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// RelativeTimeConfig represents relative time settings (e.g., "7 days ago")
type RelativeTimeConfig struct {
	Value   int    `json:"value"`             // Numeric value (e.g., 7)
	Unit    string `json:"unit"`              // "minutes", "hours", "days", "weeks", "months"
	AllTime bool   `json:"allTime,omitempty"` // If true, represents "all time" (no start limit)
	Now     bool   `json:"now,omitempty"`     // If true, represents "now" (current time)
}

// StatisticsViewData contains the filter state for the statistics dashboard
type StatisticsViewData struct {
	// Time range mode
	TimeRangeMode string `json:"time_range_mode,omitempty"` // "relative" or "absolute"

	// Relative time settings (for relative mode)
	RelativeFrom  *RelativeTimeConfig `json:"relative_from,omitempty"`
	RelativeUntil *RelativeTimeConfig `json:"relative_until,omitempty"`

	// Absolute date range
	DateRangeType     string `json:"date_range_type,omitempty"` // Legacy field
	StartDate         string `json:"start_date,omitempty"`
	EndDate           string `json:"end_date,omitempty"`
	AbsoluteFromTime  string `json:"absolute_from_time,omitempty"`  // HH:MM format for absolute mode
	AbsoluteUntilTime string `json:"absolute_until_time,omitempty"` // HH:MM format for absolute mode

	// Time of day filtering
	FilterByTimeOfDay bool   `json:"filter_by_time_of_day"`
	TimeOfDayStart    string `json:"time_of_day_start,omitempty"` // "HH:MM" format
	TimeOfDayEnd      string `json:"time_of_day_end,omitempty"`
	UseOnCallPeriod   bool   `json:"use_on_call_period"`  // Use global on-call config
	IncludeWeekends   bool   `json:"include_weekends"`    // Include weekends in time-of-day filter
	WeekendMode       string `json:"weekend_mode"`        // "exclude", "same_hours", "full_weekends"

	// Grouping
	GroupBy    string `json:"group_by,omitempty"`    // "", "severity", "team", "alert_name", "period"
	PeriodType string `json:"period_type,omitempty"` // "hour", "day", "week", "month"

	// Filter arrays
	Severities []string `json:"severities,omitempty"` // Filter by severity levels
	Teams      []string `json:"teams,omitempty"`      // Filter by team names

	// Other filters
	ApplyRules bool `json:"apply_rules"`
	Limit      int  `json:"limit,omitempty"` // For top N alerts when groupBy is "alert_name"
}

// StatisticsViewRequest is used for creating/updating views
type StatisticsViewRequest struct {
	Name        string             `json:"name" binding:"required"`
	Description string             `json:"description"`
	IsShared    bool               `json:"is_shared"`
	ViewData    StatisticsViewData `json:"view_data" binding:"required"`
}

// StatisticsViewResponse wraps a single view
type StatisticsViewResponse struct {
	Success bool            `json:"success"`
	View    *StatisticsView `json:"view,omitempty"`
	Message string          `json:"message,omitempty"`
}

// StatisticsViewsResponse wraps a list of views
type StatisticsViewsResponse struct {
	Success bool              `json:"success"`
	Views   []StatisticsView  `json:"views"`
	Message string            `json:"message,omitempty"`
}
