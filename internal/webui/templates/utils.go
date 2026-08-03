package templates

import (
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
