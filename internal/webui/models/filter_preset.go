package models

import (
	"fmt"
	"sort"
	"time"

	backendmodels "notificator/internal/backend/models"
)

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
	OwnedByMe     bool     `json:"owned_by_me,omitempty"`  // acknowledge mode: only alerts acked by the preset's user

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
	Formatter string `json:"formatter"`  // see ValidColumnFormatters
	Width     int    `json:"width"`      // Column width in pixels (50-800)
	Sortable  bool   `json:"sortable"`   // Can be sorted
	Visible   bool   `json:"visible"`    // Show/hide toggle
	Order     int    `json:"order"`      // Display order (0-based)
	Resizable bool   `json:"resizable"`  // Can be resized
	Critical  bool   `json:"critical"`   // Cannot be deleted (but can be hidden/reordered)
}

// NormalizeColumnConfigs is the one gate every column-config save path goes
// through (column preferences, filter preset create and update). Those paths
// used to carry their own copy of the rules, which is how the "ackage"
// formatter ended up rejected on save while the client happily rendered it:
// the formatter allowlist now lives in backendmodels.ValidColumnFormatters
// only.
//
// Order is a position, not user data. It used to be validated for uniqueness
// and rejected on collision, but a client has no single place to mint it -
// system defaults carry literals, custom columns used to count the array
// length - so a saved layout could hold two columns at the same order and
// then fail *every* save, including a no-op one, with no way out of the modal.
// A position has an obvious canonical repair, so it is repaired here instead:
// configs come back sorted by their incoming order and renumbered 0..n-1. The
// remaining rules (duplicate IDs, width, formatter, field type) are genuine
// validation and still reject.
func NormalizeColumnConfigs(configs []ColumnConfig) ([]ColumnConfig, error) {
	normalized := make([]ColumnConfig, len(configs))
	copy(normalized, configs)
	sort.SliceStable(normalized, func(i, j int) bool { return normalized[i].Order < normalized[j].Order })

	seenIDs := make(map[string]bool, len(normalized))

	for i := range normalized {
		col := &normalized[i]

		if seenIDs[col.ID] {
			return nil, fmt.Errorf("Duplicate column ID: %s", col.ID)
		}
		seenIDs[col.ID] = true

		if col.Width < 50 || col.Width > 800 {
			return nil, fmt.Errorf("Column '%s' width must be between 50 and 800 pixels", col.ID)
		}

		if !backendmodels.ValidColumnFormatters[col.Formatter] {
			return nil, fmt.Errorf("Invalid formatter '%s' for column '%s'", col.Formatter, col.ID)
		}

		if col.FieldType != "system" && col.FieldType != "label" && col.FieldType != "annotation" {
			return nil, fmt.Errorf("Invalid field type '%s' for column '%s'", col.FieldType, col.ID)
		}

		col.Order = i
	}

	return normalized, nil
}

// UserColumnPreference stores user's default column configuration
type UserColumnPreference struct {
	UserID        string         `json:"user_id"`
	ColumnConfigs []ColumnConfig `json:"column_configs"`
}
