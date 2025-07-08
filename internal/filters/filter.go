package filters

import (
	"strings"

	"notificator/internal/models"
)

// AlertFilter represents a filter for alerts
type AlertFilter struct {
	SearchText string
	Severity   string
	Status     string
	Team       string
}

// NewAlertFilter creates a new alert filter
func NewAlertFilter() *AlertFilter {
	return &AlertFilter{
		SearchText: "",
		Severity:   "All",
		Status:     "All",
		Team:       "All",
	}
}

// Apply applies the filter to a slice of alerts and returns the filtered results
func (f *AlertFilter) Apply(alerts []models.Alert) []models.Alert {
	if f == nil {
		return alerts
	}

	var filtered []models.Alert

	for _, alert := range alerts {
		if f.matches(alert) {
			filtered = append(filtered, alert)
		}
	}

	return filtered
}

// matches checks if an alert matches the filter criteria
func (f *AlertFilter) matches(alert models.Alert) bool {
	// Apply search text filter
	if f.SearchText != "" {
		searchText := strings.ToLower(f.SearchText)
		searchMatch := strings.Contains(strings.ToLower(alert.GetAlertName()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetSummary()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetTeam()), searchText) ||
			strings.Contains(strings.ToLower(alert.GetInstance()), searchText)

		if !searchMatch {
			return false
		}
	}

	// Apply severity filter
	if f.Severity != "All" && alert.GetSeverity() != f.Severity {
		return false
	}

	// Apply status filter
	if f.Status != "All" && alert.Status.State != f.Status {
		return false
	}

	// Apply team filter
	if f.Team != "All" && alert.GetTeam() != f.Team {
		return false
	}

	return true
}

// SetSearchText sets the search text filter
func (f *AlertFilter) SetSearchText(text string) {
	f.SearchText = text
}

// SetSeverity sets the severity filter
func (f *AlertFilter) SetSeverity(severity string) {
	f.Severity = severity
}

// SetStatus sets the status filter
func (f *AlertFilter) SetStatus(status string) {
	f.Status = status
}

// SetTeam sets the team filter
func (f *AlertFilter) SetTeam(team string) {
	f.Team = team
}

// Clear resets all filter criteria
func (f *AlertFilter) Clear() {
	f.SearchText = ""
	f.Severity = "All"
	f.Status = "All"
	f.Team = "All"
}

// IsEmpty returns true if no filters are applied
func (f *AlertFilter) IsEmpty() bool {
	return f.SearchText == "" &&
		f.Severity == "All" &&
		f.Status == "All" &&
		f.Team == "All"
}
