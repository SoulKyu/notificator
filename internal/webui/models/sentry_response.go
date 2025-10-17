package models

import (
	"time"
)

// SentryData represents the combined Sentry data for an alert
type SentryData struct {
	HasSentryLabel  bool                 `json:"has_sentry_label"`
	SentryURL       string               `json:"sentry_url"`
	AuthStatus      SentryAuthStatus     `json:"auth_status"`
	ProjectStats    *SentryStats         `json:"project_stats,omitempty"`
	Issues          []SentryIssue        `json:"issues,omitempty"`
	ProjectInfo     *SentryProjectInfo   `json:"project_info,omitempty"`
	ReleaseInfo     *SentryReleaseInfo   `json:"release_info,omitempty"`
	Error           string               `json:"error,omitempty"`
}

// SentryAuthStatus represents the user's authentication status with Sentry
type SentryAuthStatus struct {
	HasOAuthToken bool   `json:"has_oauth_token"`
	HasAPIToken   bool   `json:"has_api_token"`
	AuthMethod    string `json:"auth_method"` // "oauth", "api_token", "global", "none"
}

// SentryStats represents project statistics from Sentry
// Based on data available from documented Sentry API endpoints
type SentryStats struct {
	CrashFreeSessionRate float64   `json:"crash_free_session_rate"` // Percentage of crash-free sessions (0-100)
	CrashFreeUserRate    float64   `json:"crash_free_user_rate"`    // Percentage of crash-free users (0-100)
	IssueCount           int       `json:"issue_count"`              // Total unique issues
	ApdexScore           float64   `json:"apdex_score"`              // Application performance index (0-1)
	AvailableData        bool      `json:"available_data"`           // Whether any stats data was available
	HasSessionData       bool      `json:"has_session_data"`         // Whether crash-free rate data is available
	HasPerformanceData   bool      `json:"has_performance_data"`     // Whether Apdex/performance data is available
}

// SentryIssue represents an issue from Sentry
type SentryIssue struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Level       string              `json:"level"` // error, warning, info, etc.
	EventCount  int                 `json:"event_count"`
	UserCount   int                 `json:"user_count"`
	LastSeen    time.Time           `json:"last_seen"`
	FirstSeen   time.Time           `json:"first_seen"`
	ShortID     string              `json:"short_id"`
	Status      string              `json:"status"`
	URL         string              `json:"url"`
	Culprit     string              `json:"culprit"`
	Type        string              `json:"type"`
	Environment string              `json:"environment,omitempty"`
	Platform    string              `json:"platform,omitempty"`
	AssignedTo  *SentryAssignee     `json:"assigned_to,omitempty"`
}

// SentryProjectInfo represents parsed project information from Sentry URL
type SentryProjectInfo struct {
	BaseURL      string `json:"base_url"`
	Organization string `json:"organization"`
	ProjectSlug  string `json:"project_slug"`
	ProjectID    string `json:"project_id"`
	Name         string `json:"name,omitempty"`
	Platform     string `json:"platform,omitempty"`
}

// SentryReleaseInfo represents release information from Sentry
type SentryReleaseInfo struct {
	Version     string    `json:"version"`
	DateCreated time.Time `json:"date_created"`
	Author      string    `json:"author,omitempty"`
}

// SentryAssignee represents an assignee for a Sentry issue
type SentryAssignee struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}