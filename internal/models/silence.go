package models

import (
	"strings"
	"time"
)

// Silence represents a silence from Alertmanager
type Silence struct {
	ID        string           `json:"id"`
	Matchers  []SilenceMatcher `json:"matchers"`
	StartsAt  time.Time        `json:"startsAt"`
	EndsAt    time.Time        `json:"endsAt"`
	CreatedBy string           `json:"createdBy"`
	Comment   string           `json:"comment"`
	Status    SilenceStatus    `json:"status"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

// SilenceMatcher represents a matcher for a silence
type SilenceMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

// SilenceStatus represents the status of a silence
type SilenceStatus struct {
	State string `json:"state"` // "expired", "active", "pending"
}

// IsActive returns true if the silence is currently active
func (s *Silence) IsActive() bool {
	return s.Status.State == "active"
}

// IsExpired returns true if the silence has expired
func (s *Silence) IsExpired() bool {
	return s.Status.State == "expired"
}

// IsPending returns true if the silence is pending (not yet started)
func (s *Silence) IsPending() bool {
	return s.Status.State == "pending"
}

// Duration returns how long the silence is/was active
func (s *Silence) Duration() time.Duration {
	return s.EndsAt.Sub(s.StartsAt)
}

// TimeRemaining returns how much time is left for an active silence
func (s *Silence) TimeRemaining() time.Duration {
	if !s.IsActive() {
		return 0
	}
	remaining := time.Until(s.EndsAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// GetMatchersString returns a human-readable string of the matchers
func (s *Silence) GetMatchersString() string {
	if len(s.Matchers) == 0 {
		return "No matchers"
	}

	var result []string
	for _, matcher := range s.Matchers {
		operator := "="
		if !matcher.IsEqual {
			operator = "!="
		}
		if matcher.IsRegex {
			operator += "~"
		}
		result = append(result, matcher.Name+operator+matcher.Value)
	}

	return strings.Join(result, ", ")
}
