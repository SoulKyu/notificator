package services

import (
	"testing"
	"time"
)

func TestGenerateDailyPeriods_EqualStartEnd_Terminates(t *testing.T) {
	sqs := &StatisticsQueryService{}
	d := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	done := make(chan []Period, 1)
	go func() { done <- sqs.generateDailyPeriods(d, d) }()
	select {
	case <-done: // returns without hanging
	case <-time.After(2 * time.Second):
		t.Fatal("generateDailyPeriods did not terminate for start == end")
	}
}
