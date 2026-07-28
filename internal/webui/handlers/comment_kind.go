package handlers

import "strings"

// deriveCommentKind returns the stored kind, falling back to the emoji prefix for
// legacy comments written before the kind column existed. Emoji-sniffing is confined
// to this legacy fallback — new comments carry an authoritative kind.
func deriveCommentKind(kind, content string) string {
	if kind != "" {
		return kind
	}
	switch {
	case strings.HasPrefix(content, "🔔"):
		return "ack"
	case strings.HasPrefix(content, "🔕"):
		return "unack"
	case strings.HasPrefix(content, "🔇"):
		return "silence"
	case strings.HasPrefix(content, "✅"):
		return "resolve"
	default:
		return "comment"
	}
}
