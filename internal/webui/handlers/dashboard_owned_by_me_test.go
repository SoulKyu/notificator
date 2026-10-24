package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"notificator/internal/webui/client"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"

	"github.com/gin-gonic/gin"
)

// TestApplyDashboardFiltersOwnedByMe guards the shift-handover worklist: with
// ownedByMe set, only alerts acknowledged by the current user must survive.
func TestApplyDashboardFiltersOwnedByMe(t *testing.T) {
	alerts := []*webuimodels.DashboardAlert{
		{Fingerprint: "mine", IsAcknowledged: true, AcknowledgedBy: "alice"},
		{Fingerprint: "theirs", IsAcknowledged: true, AcknowledgedBy: "bob"},
		{Fingerprint: "unacked"},
	}
	ownedByMe := true

	got := applyDashboardFilters(alerts, webuimodels.DashboardFilters{OwnedByMe: &ownedByMe}, "", "alice")

	if len(got) != 1 || got[0].Fingerprint != "mine" {
		t.Fatalf("ownedByMe=true: want only [mine], got %v", fingerprints(got))
	}

	got = applyDashboardFilters(alerts, webuimodels.DashboardFilters{}, "", "alice")
	if len(got) != 3 {
		t.Fatalf("no ownedByMe filter: want all 3 alerts, got %v", fingerprints(got))
	}
}

// An acknowledgment is persisted against the caller's session (the real user),
// so every ownership decision - the Owner cell written optimistically, the Mine
// filter, and the browser's own mirror of it fed by /auth/profile - must use the
// real user too. Switching this back to GetEffectiveUser makes the Owner cell
// flip on the next refresh and Mine list rows the client filters out.
func TestAckOwnerUsernameIgnoresImpersonation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.SessionMiddleware("ack-owner-test-secret", false))
	router.GET("/who", func(c *gin.Context) {
		c.Set("user", &client.User{ID: "id-alice", Username: "alice"})
		if err := middleware.SetSessionValue(c, middleware.ImpersonatingUserID, "id-bob"); err != nil {
			t.Fatalf("set impersonated id: %v", err)
		}
		if err := middleware.SetSessionValue(c, middleware.ImpersonatingUsername, "bob"); err != nil {
			t.Fatalf("set impersonated username: %v", err)
		}

		// Fixture check: impersonation really is in effect for this request.
		if effective := middleware.GetEffectiveUser(c); effective == nil || effective.Username != "bob" {
			t.Fatalf("fixture: want effective user bob, got %+v", effective)
		}

		c.String(http.StatusOK, getAckOwnerUsername(c))
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/who", nil))

	if got := recorder.Body.String(); got != "alice" {
		t.Fatalf("ack owner while impersonating bob: want the real user alice, got %q", got)
	}
}

func fingerprints(alerts []*webuimodels.DashboardAlert) []string {
	out := make([]string, len(alerts))
	for i, a := range alerts {
		out[i] = a.Fingerprint
	}
	return out
}
