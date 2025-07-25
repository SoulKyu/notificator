package models

import (
	"crypto/md5"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Alert represents a single alert from Alertmanager
type Alert struct {
	// Labels contains all the labels attached to this alert
	Labels map[string]string `json:"labels"`

	// Annotations contains additional information about the alert
	Annotations map[string]string `json:"annotations"`

	// StartsAt is when the alert started firing
	StartsAt time.Time `json:"startsAt"`

	// EndsAt is when the alert stopped firing (empty if still active)
	EndsAt time.Time `json:"endsAt"`

	// GeneratorURL is the URL of the Prometheus rule that generated this alert
	GeneratorURL string `json:"generatorURL"`

	// Status represents the current state of the alert
	Status AlertStatus `json:"status"`

	// Source indicates which Alertmanager this alert comes from
	Source string `json:"-"`
}

// AlertStatus represents the state of an alert
type AlertStatus struct {
	State       string   `json:"state"`       // "firing", "resolved", "silenced"
	SilencedBy  []string `json:"silencedBy"`  // IDs of silences that affect this alert
	InhibitedBy []string `json:"inhibitedBy"` // IDs of alerts that inhibit this alert
}

// AlertmanagerResponse represents the response from Alertmanager API
type AlertmanagerResponse struct {
	Status string  `json:"status"`
	Data   []Alert `json:"data"`
}

// AlertmanagerV2Response represents the response from Alertmanager v2 API
// v2 API returns alerts directly as an array, not wrapped in a response object
type AlertmanagerV2Response []Alert

// GetAlertName returns the alertname label value
func (a *Alert) GetAlertName() string {
	if name, exists := a.Labels["alertname"]; exists {
		return name
	}
	return "Unknown"
}

// GetSeverity returns the severity label value
func (a *Alert) GetSeverity() string {
	if severity, exists := a.Labels["severity"]; exists {
		return severity
	}
	return "unknown"
}

// GetInstance returns the instance label value
func (a *Alert) GetInstance() string {
	if instance, exists := a.Labels["instance"]; exists {
		return instance
	}
	return "unknown"
}

// GetSummary returns the summary annotation
func (a *Alert) GetSummary() string {
	if summary, exists := a.Annotations["summary"]; exists {
		return summary
	}
	return "No summary available"
}

// GetTeam returns the team label value
func (a *Alert) GetTeam() string {
	if team, exists := a.Labels["team"]; exists {
		return team
	}
	return "unknown"
}

// IsActive returns true if the alert is currently firing
func (a *Alert) IsActive() bool {
	return a.Status.State == "active"
}

// Duration returns how long the alert has been active
func (a *Alert) Duration() time.Duration {
	if a.IsActive() {
		return time.Since(a.StartsAt)
	}
	return a.EndsAt.Sub(a.StartsAt)
}

// IsSilenced returns true if the alert is currently silenced
func (a *Alert) IsSilenced() bool {
	return a.Status.State == "silenced" || len(a.Status.SilencedBy) > 0
}

// IsInhibited returns true if the alert is currently inhibited by another alert
func (a *Alert) IsInhibited() bool {
	return len(a.Status.InhibitedBy) > 0
}

// GetStatusWithIcon returns the status with appropriate emoji indicators
func (a *Alert) GetStatusWithIcon() string {
	status := a.Status.State

	if a.IsSilenced() {
		return "🔇 " + status
	}

	if a.IsInhibited() {
		return "⏸️ " + status
	}

	switch status {
	case "firing":
		return "🔥 " + status
	case "active":
		return "🔥 " + status
	case "resolved":
		return "✅ " + status
	default:
		return status
	}
}

// GetFingerprint generates a unique fingerprint for the alert based on its labels
func (a *Alert) GetFingerprint() string {
	// Sort labels to ensure consistent fingerprint generation
	var labelPairs []string
	for key, value := range a.Labels {
		labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", key, value))
	}
	sort.Strings(labelPairs)

	// Create a hash from the sorted labels
	labelString := strings.Join(labelPairs, ",")
	hash := md5.Sum([]byte(labelString))
	return fmt.Sprintf("%x", hash)
}

// GetSource returns the source Alertmanager name
func (a *Alert) GetSource() string {
	if a.Source != "" {
		return a.Source
	}
	return "unknown"
}
