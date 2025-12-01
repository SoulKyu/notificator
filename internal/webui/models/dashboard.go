package models

import (
	"encoding/json"
	"strconv"
	"time"
)

// DashboardAlert represents an enhanced alert for the dashboard with additional features
type DashboardAlert struct {
	// Core alert data
	Fingerprint  string            `json:"fingerprint"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt,omitempty"`
	GeneratorURL string            `json:"generatorURL"`
	Source       string            `json:"source"`

	// Status information
	Status AlertStatus `json:"status"`

	// Enhanced dashboard features
	IsAcknowledged    bool      `json:"isAcknowledged"`
	AcknowledgedBy    string    `json:"acknowledgedBy,omitempty"`
	AcknowledgedAt    time.Time `json:"acknowledgedAt,omitempty"`
	AcknowledgeReason string    `json:"acknowledgeReason,omitempty"`

	// Comments and interactions
	CommentCount  int       `json:"commentCount"`
	LastCommentAt time.Time `json:"lastCommentAt,omitempty"`

	// User-specific features
	IsHidden bool      `json:"isHidden"` // Per-user hidden alerts
	HiddenBy string    `json:"hiddenBy,omitempty"`
	HiddenAt time.Time `json:"hiddenAt,omitempty"`

	// Grouping information
	GroupName string `json:"groupName,omitempty"`

	// Computed fields
	Duration   int64  `json:"duration"` // Duration in seconds
	IsResolved bool   `json:"isResolved"`
	Team       string `json:"team"`
	Severity   string `json:"severity"`
	AlertName  string `json:"alertName"`
	Instance   string `json:"instance"`
	Summary    string `json:"summary"`

	// Timestamps for tracking
	UpdatedAt  time.Time `json:"updatedAt"`
	ResolvedAt time.Time `json:"resolvedAt,omitempty"`
}

// AlertStatus represents the enhanced status of an alert
type AlertStatus struct {
	State       string   `json:"state"`       // "firing", "resolved", "silenced"
	SilencedBy  []string `json:"silencedBy"`  // IDs of silences affecting this alert
	InhibitedBy []string `json:"inhibitedBy"` // IDs of alerts inhibiting this alert
}

// DashboardDisplayMode represents the different display modes
type DashboardDisplayMode string

const (
	DisplayModeClassic     DashboardDisplayMode = "classic"     // Only standard alerts (not acknowledged, not resolved)
	DisplayModeFull        DashboardDisplayMode = "full"        // All alerts (acknowledged, resolved, standard)
	DisplayModeResolved    DashboardDisplayMode = "resolved"    // Only resolved alerts
	DisplayModeAcknowledge DashboardDisplayMode = "acknowledge" // Only acknowledged alerts
	DisplayModeHidden      DashboardDisplayMode = "hidden"      // Only hidden alerts
)

// DashboardViewMode represents list vs group view
type DashboardViewMode string

const (
	ViewModeList  DashboardViewMode = "list"  // Standard list view
	ViewModeGroup DashboardViewMode = "group" // Grouped by GroupName
)

// DashboardFilters represents all possible dashboard filters
type DashboardFilters struct {
	Search              string               `json:"search"`
	Alertmanagers       []string             `json:"alertmanagers"`
	Severities          []string             `json:"severities"`
	Statuses            []string             `json:"statuses"`
	Teams               []string             `json:"teams"`
	AlertNames          []string             `json:"alertNames"`
	Acknowledged        *bool                `json:"acknowledged,omitempty"` // nil = all, true = only ack, false = only non-ack
	HasComments         *bool                `json:"hasComments,omitempty"`  // nil = all, true = with comments, false = without
	DisplayMode         DashboardDisplayMode `json:"displayMode"`
	ViewMode            DashboardViewMode    `json:"viewMode"`
	ResolvedAlertsLimit int                  `json:"resolvedAlertsLimit,omitempty"` // Client-side limit for resolved alerts display

	// Filter-specific hidden alerts (from active saved filter, additive with global hidden)
	FilterHiddenAlerts []FilterHiddenAlert `json:"filterHiddenAlerts,omitempty"`
	FilterHiddenRules  []FilterHiddenRule  `json:"filterHiddenRules,omitempty"`
}

// DashboardSorting represents sorting configuration
type DashboardSorting struct {
	Field     string `json:"field"`     // Column to sort by
	Direction string `json:"direction"` // "asc" or "desc"
}

// Pagination represents pagination configuration
type Pagination struct {
	Page  int `json:"page"`  // Current page (1-based)
	Limit int `json:"limit"` // Items per page
}

// DashboardSettings represents user-specific dashboard settings
type DashboardSettings struct {
	UserID            string           `json:"userId"`
	Theme             string           `json:"theme"` // "light" or "dark"
	CriticalSoundPath string           `json:"criticalSoundPath"`
	WarningSoundPath  string           `json:"warningSoundPath"`
	RefreshInterval   int              `json:"refreshInterval"` // Seconds between auto-refresh
	DefaultFilters    DashboardFilters `json:"defaultFilters"`
	DefaultSorting    DashboardSorting `json:"defaultSorting"`
	HiddenColumns     []string         `json:"hiddenColumns"`
}

// DashboardIncrementalRequest represents the request body for POST /api/v1/dashboard/incremental
type DashboardIncrementalRequest struct {
	ClientAlerts []string `json:"clientAlerts"` // Array of alert fingerprints the client currently has
}

// DashboardIncrementalUpdate represents changes to alerts since last update
type DashboardIncrementalUpdate struct {
	NewAlerts      []*DashboardAlert      `json:"newAlerts"`      // Alerts added since last check
	UpdatedAlerts  []*DashboardAlert      `json:"updatedAlerts"`  // Alerts that changed
	RemovedAlerts  []string               `json:"removedAlerts"`  // Fingerprints of removed alerts
	Metadata       *DashboardMetadata     `json:"metadata"`       // Updated metadata
	Settings       *DashboardSettings     `json:"settings"`       // Updated settings
	Colors         map[string]interface{} `json:"colors"`         // Color preferences for alerts (fingerprint -> ColorResult)
	LastUpdateTime int64                  `json:"lastUpdateTime"` // Unix timestamp
}

// DashboardResponse represents the API response for dashboard data
type DashboardResponse struct {
	Alerts   []DashboardAlert  `json:"alerts"`
	Groups   []AlertGroup      `json:"groups,omitempty"` // Only present in group view
	Metadata DashboardMetadata `json:"metadata"`
	Settings DashboardSettings `json:"settings"`
}

// AlertGroup represents a group of alerts for group view
type AlertGroup struct {
	GroupName     string           `json:"groupName"`
	Alerts        []DashboardAlert `json:"alerts"`
	Count         int              `json:"count"`
	IsSelected    bool             `json:"isSelected"`
	WorstSeverity string           `json:"worstSeverity"`
}

// DashboardMetadata provides additional information about the dashboard state
type DashboardMetadata struct {
	TotalAlerts        int                       `json:"totalAlerts"`
	FilteredCount      int                       `json:"filteredCount"`
	TotalCount         int                       `json:"totalCount"` // Total count for pagination
	LastUpdate         time.Time                 `json:"lastUpdate"`
	NextUpdate         time.Time                 `json:"nextUpdate"`
	AlertmanagerStatus map[string]bool           `json:"alertmanagerStatus"`
	Counters           DashboardCounters         `json:"counters"`
	AvailableFilters   DashboardAvailableFilters `json:"availableFilters"`
}

// DashboardCounters provides count statistics
type DashboardCounters struct {
	Critical     int `json:"critical"`
	Warning      int `json:"warning"`
	Info         int `json:"info"`
	Firing       int `json:"firing"`
	Resolved     int `json:"resolved"`
	Acknowledged int `json:"acknowledged"`
	WithComments int `json:"withComments"`
}

// DashboardAvailableFilters provides available filter options
type DashboardAvailableFilters struct {
	Alertmanagers []string `json:"alertmanagers"`
	Severities    []string `json:"severities"`
	Statuses      []string `json:"statuses"`
	Teams         []string `json:"teams"`
	AlertNames    []string `json:"alertNames"`
}

// BulkActionRequest represents a request to perform actions on multiple alerts
type BulkActionRequest struct {
	AlertFingerprints     []string      `json:"alertFingerprints"`
	GroupNames            []string      `json:"groupNames,omitempty"`            // For group actions
	Action                string        `json:"action"`                          // "acknowledge", "hide", "unhide", "silence"
	Comment               string        `json:"comment,omitempty"`               // Optional comment for acknowledgment
	SilenceDuration       time.Duration `json:"silenceDuration,omitempty"`       // Duration for silence action (backward compatibility)
	SilenceDurationType   string        `json:"silenceDurationType,omitempty"`   // "preset" or "custom"
	CustomSilenceDuration string        `json:"customSilenceDuration,omitempty"` // Custom duration string (e.g., "1h30m")
}

// BulkActionResponse represents the response to a bulk action
type BulkActionResponse struct {
	Success        bool     `json:"success"`
	ProcessedCount int      `json:"processedCount"`
	FailedCount    int      `json:"failedCount"`
	Errors         []string `json:"errors,omitempty"`
}

// AlertDetails represents detailed information about an alert for the modal
type AlertDetails struct {
	Alert           *DashboardAlert  `json:"alert"`
	Acknowledgments []Acknowledgment `json:"acknowledgments,omitempty"`
	Comments        []Comment        `json:"comments,omitempty"`
	Silences        []Silence        `json:"silences,omitempty"`
	GeneratorURL    string           `json:"generatorURL,omitempty"`
	StartedAt       time.Time        `json:"startedAt"`
	EndedAt         *time.Time       `json:"endedAt,omitempty"`
	Duration        time.Duration    `json:"duration"`
}

// Acknowledgment represents an alert acknowledgment
type Acknowledgment struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	UserID    string    `json:"userId"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Comment represents an alert comment
type Comment struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	UserID    string    `json:"userId"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Silence represents a silence affecting an alert
type Silence struct {
	ID        string           `json:"id"`
	CreatedBy string           `json:"createdBy"`
	Comment   string           `json:"comment"`
	StartsAt  time.Time        `json:"startsAt"`
	EndsAt    time.Time        `json:"endsAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Matchers  []SilenceMatcher `json:"matchers"`
	Status    SilenceStatus    `json:"status"`
}

// SilenceMatcher represents a silence matcher
type SilenceMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
}

// SilenceStatus represents the status of a silence
type SilenceStatus struct {
	State string `json:"state"` // "active", "pending", "expired"
}

// UserColorPreference represents a user-defined color preference for alerts
type UserColorPreference struct {
	ID                 string            `json:"id"`
	UserID             string            `json:"userId"`
	LabelConditions    map[string]string `json:"labelConditions"`    // Label conditions that must match
	Color              string            `json:"color"`              // Color value (e.g., "GRAY", "#FF5733", "red-500")
	ColorType          string            `json:"colorType"`          // Type: "severity", "custom", "tailwind"
	Priority           int               `json:"priority"`           // Higher numbers = higher priority
	BgLightnessFactor  float32           `json:"bgLightnessFactor"`  // Background lightness factor (0.0-1.0)
	TextDarknessFactor float32           `json:"textDarknessFactor"` // Text darkness factor (0.0-1.0)
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

// UnmarshalJSON implements custom JSON unmarshaling to handle string-to-int conversion for Priority field
func (u *UserColorPreference) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid recursion
	type Alias UserColorPreference
	aux := &struct {
		Priority interface{} `json:"priority"`
		*Alias
	}{
		Alias: &Alias{},
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Copy all fields from aux to u
	*u = UserColorPreference(*aux.Alias)

	// Handle priority conversion from string or number to int
	switch p := aux.Priority.(type) {
	case float64:
		u.Priority = int(p)
	case string:
		if p == "" {
			u.Priority = 0
		} else {
			priority, err := strconv.Atoi(p)
			if err != nil {
				u.Priority = 0 // Default to 0 if conversion fails
			} else {
				u.Priority = priority
			}
		}
	case int:
		u.Priority = p
	case nil:
		u.Priority = 0
	default:
		u.Priority = 0
	}

	return nil
}
