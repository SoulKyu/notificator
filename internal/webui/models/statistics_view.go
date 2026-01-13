package models

import "time"

// StatisticsViewData contains the complete statistics view state
type StatisticsViewData struct {
	// Time range
	TimeRangeType string `json:"time_range_type"` // "this_week", "last_week", "this_month", "last_month", "this_quarter", "last_quarter", "custom"
	CustomStart   string `json:"custom_start,omitempty"` // ISO date
	CustomEnd     string `json:"custom_end,omitempty"`   // ISO date

	// Filters
	HoursFilter string   `json:"hours_filter"` // "24_7", "on_call", "business"
	Teams       []string `json:"teams,omitempty"`
	Severities  []string `json:"severities,omitempty"`

	// Display
	GroupBy       string `json:"group_by"`        // "overall", "severity", "team", "alert_name"
	TopNLimit     int    `json:"top_n_limit"`     // default 10
	SortBy        string `json:"sort_by"`
	SortDirection string `json:"sort_direction"` // "asc", "desc"
}

// StatisticsView represents a saved view configuration (for webui)
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
	Success bool             `json:"success"`
	Views   []StatisticsView `json:"views"`
	Message string           `json:"message,omitempty"`
}

// OnCallConfig represents the on-call schedule configuration (for webui)
type OnCallConfig struct {
	ID              string   `json:"id"`
	OnCallDays      []string `json:"on_call_days"`       // ["mon","tue","wed","thu","fri"]
	OnCallStartTime string   `json:"on_call_start_time"` // "18:00"
	OnCallEndTime   string   `json:"on_call_end_time"`   // "09:00"
	IncludeWeekends bool     `json:"include_weekends"`
}
