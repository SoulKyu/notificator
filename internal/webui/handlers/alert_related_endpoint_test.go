package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/services"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// seedCache installs a live cache holding alerts, restored after the test.
func seedCache(t *testing.T, alerts ...*webuimodels.DashboardAlert) {
	t.Helper()
	previous := alertCache
	t.Cleanup(func() { alertCache = previous })

	cache := services.NewAlertCache(nil, nil, 0, 0) // never Start()ed: no polling, no backend
	for _, a := range alerts {
		cache.UpdateAlert(a)
	}
	alertCache = cache
}

func firingAlert(fp string, labels map[string]string) *webuimodels.DashboardAlert {
	return &webuimodels.DashboardAlert{
		Fingerprint: fp,
		AlertName:   labels["alertname"],
		Labels:      labels,
		Status:      webuimodels.AlertStatus{State: "firing"},
		StartsAt:    time.Now().Add(-time.Minute),
	}
}

// relatedRequest serves GET /alert/:fingerprint/related through a real router so
// the session middleware the handler reads from is in place. query is the view
// state the dashboard client sends along (e.g. filterHiddenRules).
func relatedRequest(fingerprint string, query ...string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("test-session", cookie.NewStore([]byte("test-secret"))))
	router.GET("/alert/:fingerprint/related", HandleGetRelatedAlerts)

	target := "/alert/" + fingerprint + "/related"
	if len(query) > 0 && query[0] != "" {
		target += "?" + query[0]
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func getRelatedGroups(t *testing.T, fingerprint string, query ...string) []RelatedLabelGroup {
	t.Helper()
	rec := relatedRequest(fingerprint, query...)

	if rec.Code != http.StatusOK {
		t.Fatalf("related endpoint returned %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Groups []RelatedLabelGroup `json:"groups"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding related payload: %v", err)
	}
	return payload.Data.Groups
}

// The recurring defect: the panel counted alerts the dashboard had already
// filtered out. Assert the endpoint's count against the rows the dashboard
// itself returns for the label filter that same group hands over.
func TestRelatedEndpointCountMatchesDashboardRows(t *testing.T) {
	target := firingAlert("target", map[string]string{"alertname": "DiskFull", "cluster": "prod"})
	peers := []*webuimodels.DashboardAlert{
		firingAlert("f1", map[string]string{"alertname": "MemHigh", "cluster": "prod"}),
		firingAlert("f2", map[string]string{"alertname": "MemHigh", "cluster": "prod"}),
		firingAlert("f3", map[string]string{"alertname": "MemHigh", "cluster": "prod"}),
		// Neighbours the dashboard hides in classic mode — they must not be counted.
		func() *webuimodels.DashboardAlert {
			a := firingAlert("resolved", map[string]string{"alertname": "MemHigh", "cluster": "prod"})
			a.IsResolved = true
			a.Status.State = "resolved"
			return a
		}(),
		func() *webuimodels.DashboardAlert {
			a := firingAlert("acked", map[string]string{"alertname": "MemHigh", "cluster": "prod"})
			a.IsAcknowledged = true
			return a
		}(),
		// Substring neighbour: only an exact label filter excludes it.
		firingAlert("eu1", map[string]string{"alertname": "MemHigh", "cluster": "prod-eu"}),
		firingAlert("eu2", map[string]string{"alertname": "MemHigh", "cluster": "prod-eu"}),
	}
	seedCache(t, append([]*webuimodels.DashboardAlert{target}, peers...)...)

	groups := getRelatedGroups(t, "target")
	if len(groups) != 1 {
		t.Fatalf("expected one cluster group, got %+v", groups)
	}
	group := groups[0]
	if group.LabelKey != "cluster" || group.LabelValue != "prod" {
		t.Fatalf("expected cluster=prod group, got %s=%s", group.LabelKey, group.LabelValue)
	}
	if group.Count != 4 {
		t.Fatalf("expected 4 firing alerts on cluster=prod (target + f1..f3), got %d", group.Count)
	}
	for _, a := range group.Alerts {
		if a.Fingerprint == "resolved" || a.Fingerprint == "acked" {
			t.Fatalf("%s is not in the dashboard's classic view but is listed as firing", a.Fingerprint)
		}
	}

	// What "Filter dashboard on this" produces: classic view + this label filter.
	rows := applyDashboardFilters(getStandardAlerts(), webuimodels.DashboardFilters{
		DisplayMode:  webuimodels.DisplayModeClassic,
		LabelFilters: []webuimodels.LabelFilter{{Key: group.LabelKey, Value: group.LabelValue}},
	}, "", "")
	if len(rows) != group.Count {
		t.Fatalf("panel says %d firing, the same filter yields %d dashboard rows", group.Count, len(rows))
	}
}

// The escalation case: a saved filter preset mutes part of the group. Those
// alerts are absent from the table, so they must be absent from the count the
// panel promises — the handler no longer decides that on its own, it asks
// applyDashboardFilters with the same hidden state the table was loaded with.
func TestRelatedEndpointHonoursPresetHiddenRules(t *testing.T) {
	previousService := hiddenAlertsService
	t.Cleanup(func() { hiddenAlertsService = previousService })
	hiddenAlertsService = services.NewHiddenAlertsService(stubHiddenBackend{})

	alerts := []*webuimodels.DashboardAlert{
		firingAlert("target", map[string]string{"alertname": "DiskFull", "cluster": "prod"}),
		firingAlert("keep1", map[string]string{"alertname": "MemHigh", "cluster": "prod"}),
		firingAlert("keep2", map[string]string{"alertname": "MemHigh", "cluster": "prod"}),
		firingAlert("noisy1", map[string]string{"alertname": "NoisyProbe", "cluster": "prod"}),
		firingAlert("noisy2", map[string]string{"alertname": "NoisyProbe", "cluster": "prod"}),
		firingAlert("noisy3", map[string]string{"alertname": "NoisyProbe", "cluster": "prod"}),
		// Off-cluster ballast: without it cluster=prod covers the whole active set
		// and the group is dropped as degenerate.
		firingAlert("dev1", map[string]string{"alertname": "MemHigh", "cluster": "dev"}),
		firingAlert("dev2", map[string]string{"alertname": "MemHigh", "cluster": "dev"}),
		firingAlert("dev3", map[string]string{"alertname": "MemHigh", "cluster": "dev"}),
		firingAlert("dev4", map[string]string{"alertname": "MemHigh", "cluster": "dev"}),
	}
	seedCache(t, alerts...)

	hiddenRules := []webuimodels.FilterHiddenRule{{
		Name: "mute probes", LabelKey: "alertname", LabelValue: "NoisyProbe", IsEnabled: true,
	}}
	encoded, err := json.Marshal(hiddenRules)
	if err != nil {
		t.Fatalf("encoding hidden rules: %v", err)
	}
	query := "filterHiddenRules=" + url.QueryEscape(string(encoded))

	groups := getRelatedGroups(t, "target", query)
	if len(groups) != 1 {
		t.Fatalf("expected one cluster group, got %+v", groups)
	}
	group := groups[0]
	for _, a := range group.Alerts {
		if a.AlertName == "NoisyProbe" {
			t.Fatalf("%s is hidden by the active preset but is listed as firing", a.Fingerprint)
		}
	}

	// The rows "Filter dashboard on this" lands on: classic + this label filter,
	// with the preset's hidden rules still in force.
	rows := applyDashboardFilters(getStandardAlerts(), webuimodels.DashboardFilters{
		DisplayMode:       webuimodels.DisplayModeClassic,
		LabelFilters:      []webuimodels.LabelFilter{{Key: group.LabelKey, Value: group.LabelValue}},
		FilterHiddenRules: hiddenRules,
	}, "", "")
	if len(rows) != group.Count {
		t.Fatalf("panel says %d firing, the same filter yields %d dashboard rows", group.Count, len(rows))
	}
	if group.Count != 3 {
		t.Fatalf("expected target + keep1 + keep2, got %d", group.Count)
	}
}

func TestRelatedEndpointIsolatedAlert(t *testing.T) {
	seedCache(t,
		firingAlert("alone", map[string]string{"alertname": "OnlyOne", "cluster": "lab"}),
		firingAlert("other", map[string]string{"alertname": "Something", "cluster": "prod"}),
	)

	if groups := getRelatedGroups(t, "alone"); len(groups) != 0 {
		t.Fatalf("expected no correlating groups, got %+v", groups)
	}
}

func TestRelatedEndpointUnknownFingerprint(t *testing.T) {
	seedCache(t, firingAlert("known", map[string]string{"cluster": "prod"}))

	if rec := relatedRequest("nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown fingerprint, got %d", rec.Code)
	}
}
