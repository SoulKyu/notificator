package templates

import (
	"fmt"
	"strings"
	"time"
)

func GetInitials(username string) string {
	parts := strings.Fields(username)
	if len(parts) >= 2 {
		return strings.ToUpper(string(parts[0][0]) + string(parts[1][0]))
	}
	if len(username) >= 2 {
		return strings.ToUpper(username[:2])
	}
	return strings.ToUpper(username)
}

func FormatDate(t time.Time) string {
	return t.Format("Jan 2, 2006 at 3:04 PM")
}

func FormatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
