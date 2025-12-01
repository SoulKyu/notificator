package models

import "time"

// FilterPreset represents a saved filter configuration
type FilterPreset struct {
	ID            string             `json:"id"`
	UserID        string             `json:"user_id"`
	Name          string             `json:"name"`
	Description   string             `json:"description,omitempty"`
	IsShared      bool               `json:"is_shared"`
	IsDefault     bool               `json:"is_default"`
	FilterData    FilterPresetData   `json:"filter_data"`
	ColumnConfigs []ColumnConfig     `json:"column_configs,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
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

	// Column Configuration
	ColumnConfigs []ColumnConfig `json:"column_configs,omitempty"`

	// Filter-specific hidden alerts (additive with global hidden alerts)
	HiddenAlerts []FilterHiddenAlert `json:"hidden_alerts,omitempty"`

	// Filter-specific hidden rules (additive with global hidden rules)
	HiddenRules []FilterHiddenRule `json:"hidden_rules,omitempty"`
}

// FilterHiddenAlert represents an alert hidden specifically within a saved filter
type FilterHiddenAlert struct {
	Fingerprint string `json:"fingerprint"`
	AlertName   string `json:"alert_name"`
	Instance    string `json:"instance,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// FilterHiddenRule represents a label-based hiding rule specific to a saved filter
type FilterHiddenRule struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	LabelKey    string `json:"label_key"`
	LabelValue  string `json:"label_value"`
	IsRegex     bool   `json:"is_regex"`
	IsEnabled   bool   `json:"is_enabled"`
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

// ColumnConfig represents a single column configuration in the dashboard table
type ColumnConfig struct {
	ID        string `json:"id"`         // Unique ID: "col_alertname", "col_custom_env"
	Label     string `json:"label"`      // Display name: "Alert Name", "Environment"
	FieldType string `json:"field_type"` // "system", "label", "annotation"
	FieldPath string `json:"field_path"` // "alertName", "labels.environment", "annotations.summary"
	Formatter string `json:"formatter"`  // "text", "badge", "duration", "timestamp", "count", "checkbox", "actions"
	Width     int    `json:"width"`      // Column width in pixels (50-800)
	Sortable  bool   `json:"sortable"`   // Can be sorted
	Visible   bool   `json:"visible"`    // Show/hide toggle
	Order     int    `json:"order"`      // Display order (0-based)
	Resizable bool   `json:"resizable"`  // Can be resized
	Critical  bool   `json:"critical"`   // Cannot be deleted (but can be hidden/reordered)
}

// UserColumnPreference stores user's default column configuration
type UserColumnPreference struct {
	UserID        string         `json:"user_id"`
	ColumnConfigs []ColumnConfig `json:"column_configs"`
}
