package models

import (
	"encoding/json"
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

// UnmarshalJSON defaults IsEqual to true. Alertmanager versions before 0.22 omit the
// field entirely, and a missing "isEqual" means an equality matcher — decoding it as
// false would silently negate every matcher.
func (m *SilenceMatcher) UnmarshalJSON(data []byte) error {
	type alias SilenceMatcher
	aux := struct {
		IsEqual *bool `json:"isEqual"`
		*alias
	}{alias: (*alias)(m)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	m.IsEqual = aux.IsEqual == nil || *aux.IsEqual
	return nil
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
