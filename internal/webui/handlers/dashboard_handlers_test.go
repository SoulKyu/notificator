package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/services"
)

func bulkActionRequest(t *testing.T, body string) (webuimodels.APIResponse, webuimodels.BulkActionResponse) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("test-session", cookie.NewStore([]byte("test-secret"))))
	router.POST("/bulk-action", BulkActionAlerts)

	req := httptest.NewRequest(http.MethodPost, "/bulk-action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var envelope webuimodels.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}

	var bulk webuimodels.BulkActionResponse
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("failed to re-marshal data: %v", err)
	}
	if err := json.Unmarshal(data, &bulk); err != nil {
		t.Fatalf("failed to decode bulk response: %v", err)
	}

	return envelope, bulk
}

func TestBulkActionEnvelopeReportsTotalFailure(t *testing.T) {
	cache := services.NewAlertCache(nil, nil, 0, 0)
	// Silencing fails for this alert because no Alertmanager client is configured.
	cache.UpdateAlert(&webuimodels.DashboardAlert{Fingerprint: "grouped-fp", GroupName: "grp"})
	SetAlertCache(cache)

	envelope, bulk := bulkActionRequest(t,
		`{"alertFingerprints":["missing-fp"],"groupNames":["grp"],"action":"silence","silenceDuration":3600000000000}`)

	if envelope.Success {
		t.Error("outer success must be false when every target failed")
	}
	if envelope.Error == "" {
		t.Error("outer error summary must be set on failure")
	}
	if bulk.FailedCount != 2 || bulk.ProcessedCount != 0 {
		t.Errorf("expected failedCount=2 processedCount=0, got %d/%d", bulk.FailedCount, bulk.ProcessedCount)
	}
	if len(bulk.Failures) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(bulk.Failures))
	}
	if bulk.Failures[0].Target != "missing-fp" || bulk.Failures[0].Kind != "alert" {
		t.Errorf("unexpected alert failure: %+v", bulk.Failures[0])
	}
	if bulk.Failures[1].Target != "grp" || bulk.Failures[1].Kind != "group" {
		t.Errorf("unexpected group failure: %+v", bulk.Failures[1])
	}
}

func TestBulkActionEnvelopeReportsPartialFailure(t *testing.T) {
	cache := services.NewAlertCache(nil, nil, 0, 0)
	cache.UpdateAlert(&webuimodels.DashboardAlert{Fingerprint: "known-fp"})
	SetAlertCache(cache)

	envelope, bulk := bulkActionRequest(t,
		`{"alertFingerprints":["known-fp","missing-fp"],"action":"unacknowledge"}`)

	if envelope.Success {
		t.Error("outer success must be false on partial failure")
	}
	if bulk.ProcessedCount != 1 || bulk.FailedCount != 1 {
		t.Errorf("expected processedCount=1 failedCount=1, got %d/%d", bulk.ProcessedCount, bulk.FailedCount)
	}
	if len(bulk.Failures) != 1 || bulk.Failures[0].Target != "missing-fp" {
		t.Errorf("expected single failure for missing-fp, got %+v", bulk.Failures)
	}
}

func TestBulkActionEnvelopeSuccess(t *testing.T) {
	cache := services.NewAlertCache(nil, nil, 0, 0)
	cache.UpdateAlert(&webuimodels.DashboardAlert{Fingerprint: "known-fp"})
	SetAlertCache(cache)

	envelope, bulk := bulkActionRequest(t,
		`{"alertFingerprints":["known-fp"],"action":"unacknowledge"}`)

	if !envelope.Success {
		t.Errorf("outer success must be true when nothing failed: %+v", envelope)
	}
	if envelope.Error != "" {
		t.Errorf("outer error must be empty on success, got %q", envelope.Error)
	}
	if bulk.ProcessedCount != 1 || bulk.FailedCount != 0 || len(bulk.Failures) != 0 {
		t.Errorf("unexpected bulk response: %+v", bulk)
	}
}
