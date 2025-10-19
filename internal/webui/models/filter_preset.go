package models

import "time"

// FilterPreset represents a saved filter configuration
type FilterPreset struct {
	ID          string             `json:"id"`
	UserID      string             `json:"user_id"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	IsShared    bool               `json:"is_shared"`
	IsDefault   bool               `json:"is_default"`
	FilterData  FilterPresetData   `json:"filter_data"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// FilterPresetData contains the complete dashboard state
type FilterPresetData struct {
	// Filters
	Search        string   `json:"search,omitempty"`
	Alertmanagers []string `json:"alertmanagers,omitempty"`
	Severities    []string `json:"severities,omitempty"`
	Statuses      []string `json:"statuses,omitempty"`
	Teams         []string `json:"teams,omitempty"`
	AlertNames    []string `json:"alert_names,omitempty"`
	Acknowledged  string   `json:"acknowledged,omitempty"` // "yes", "no", "all"
	Comments      string   `json:"comments,omitempty"`     // "with", "without", "all"

	// Display settings
	DisplayMode string `json:"display_mode,omitempty"` // "classic", "full", "resolved", "acknowledge", "hidden"
	ViewMode    string `json:"view_mode,omitempty"`    // "list", "group"
	GroupBy     string `json:"group_by,omitempty"`     // "alertname", "severity", "team", "instance", etc.

	// Sorting
	SortBy        string `json:"sort_by,omitempty"`        // "alertname", "severity", "duration", etc.
	SortDirection string `json:"sort_direction,omitempty"` // "asc", "desc"

	// Pagination
	ItemsPerPage int `json:"items_per_page,omitempty"` // 10, 20, 50, 100, 500
}

// FilterPresetRequest is used for creating/updating presets
type FilterPresetRequest struct {
	Name        string           `json:"name" binding:"required"`
	Description string           `json:"description"`
	IsShared    bool             `json:"is_shared"`
	FilterData  FilterPresetData `json:"filter_data" binding:"required"`
}

// FilterPresetResponse wraps a single preset
type FilterPresetResponse struct {
	Success bool          `json:"success"`
	Preset  *FilterPreset `json:"preset,omitempty"`
	Message string        `json:"message,omitempty"`
}

// FilterPresetsResponse wraps a list of presets
type FilterPresetsResponse struct {
	Success bool            `json:"success"`
	Presets []FilterPreset  `json:"presets"`
	Message string          `json:"message,omitempty"`
}
