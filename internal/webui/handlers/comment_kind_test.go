package handlers

import "testing"

func TestDeriveCommentKind(t *testing.T) {
	cases := []struct{ kind, content, want string }{
		{"silence", "🔇 whatever", "silence"},   // stored kind wins
		{"", "🔔 Alert acknowledged: x", "ack"}, // legacy fallback
		{"", "🔕 Alert unacknowledged: x", "unack"},
		{"", "🔇 Alert silenced for 2h: x", "silence"},
		{"", "✅ Alert resolved: x", "resolve"},
		{"", "just a human note", "comment"},
	}
	for _, c := range cases {
		if got := deriveCommentKind(c.kind, c.content); got != c.want {
			t.Errorf("deriveCommentKind(%q,%q) = %q, want %q", c.kind, c.content, got, c.want)
		}
	}
}
