package handlers

import (
	"testing"
	"time"
)

func TestSnoozeDurationToExpiresAt_Tomorrow9amUsesGivenTimezone(t *testing.T) {
	tests := []struct {
		name string
		tz   string
	}{
		{"valid IANA zone", "America/New_York"},
		{"empty falls back to UTC", ""},
		{"invalid falls back to UTC", "not/a-zone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expiresAt, err := snoozeDurationToExpiresAt("tomorrow9am", tt.tz)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if expiresAt == nil {
				t.Fatal("expected non-nil expiry")
			}

			wantLoc := time.UTC
			if loc, locErr := time.LoadLocation(tt.tz); locErr == nil {
				wantLoc = loc
			}

			got := expiresAt.In(wantLoc)
			if got.Hour() != 9 || got.Minute() != 0 {
				t.Errorf("expected 9:00 in %s, got %02d:%02d", wantLoc, got.Hour(), got.Minute())
			}
		})
	}
}
