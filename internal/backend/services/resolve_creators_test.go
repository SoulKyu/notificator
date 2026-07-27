package services

import (
	"testing"

	"notificator/internal/backend/models"
)

func TestResolveCreators(t *testing.T) {
	users := []models.User{
		{ID: "qaqg35xcotnzy8kvqm7zsrv518ylc8ji", Username: "gule", Email: "guix.legrain@gmail.com"},
		{ID: "u2", Username: "alice", Email: "Alice@Example.com"},
	}

	resolved := resolveCreators(users, []string{
		"qaqg35xcotnzy8kvqm7zsrv518ylc8ji", // old silence: stored user ID
		"alice",                            // new silence: stored username
		"ALICE@example.COM",                // external tool typing an email, any case
		"amtool-oncall",                    // genuinely external
		"",                                 // Alertmanager allows an empty createdBy
	})

	want := map[string]string{
		"qaqg35xcotnzy8kvqm7zsrv518ylc8ji": "gule",
		"alice":                            "alice",
		"ALICE@example.COM":                "alice",
	}
	if len(resolved) != len(want) {
		t.Fatalf("resolved %d entries, want %d: %v", len(resolved), len(want), resolved)
	}
	for k, v := range want {
		if resolved[k] != v {
			t.Errorf("resolved[%q] = %q, want %q", k, resolved[k], v)
		}
	}
	if _, ok := resolved["amtool-oncall"]; ok {
		t.Error("external creator must not resolve")
	}
}
