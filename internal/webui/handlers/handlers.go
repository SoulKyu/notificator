package handlers

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"notificator/internal/alertmanager"
	"notificator/internal/webui/client"
	"notificator/internal/webui/middleware"
	"notificator/internal/webui/models"
	"notificator/internal/webui/services"
	"notificator/internal/webui/templates/components"
	"notificator/internal/webui/templates/pages"

	"github.com/gin-gonic/gin"
)

func transformStatus(status string) string {
	switch status {
	case "suppressed":
		return "silenced"
	default:
		return status
	}
}

func transformSeverity(severity string) string {
	switch strings.ToLower(severity) {
	case "information":
		return "info"
	case "critical-daytime":
		return "critical"
	default:
		return strings.ToLower(severity)
	}
}

var (
	backendClient      *client.BackendClient
	alertmanagerClient *alertmanager.MultiClient
	dashboardCache     *services.AlertCache
)

func SetBackendClient(client *client.BackendClient) {
	backendClient = client
}

func SetAlertmanagerClient(client *alertmanager.MultiClient) {
	alertmanagerClient = client
}

func getOAuthConfig(c *gin.Context) *pages.OAuthConfig {
	if backendClient == nil || !backendClient.IsConnected() {
		return nil
	}

	config, err := backendClient.GetOAuthConfig()
	if err != nil {
		fmt.Printf("Failed to get OAuth config: %v\n", err)
		return nil
	}

	if !config["enabled"].(bool) {
		return nil
	}

	oauthConfig := &pages.OAuthConfig{
		Enabled:            config["enabled"].(bool),
		DisableClassicAuth: config["disable_classic_auth"].(bool),
		Providers:          make([]components.OAuthProvider, 0),
	}

	if providers, ok := config["providers"].([]map[string]interface{}); ok {
		for _, p := range providers {
			if enabled, ok := p["enabled"].(bool); ok && enabled {
				provider := components.OAuthProvider{
					Name:        p["name"].(string),
					DisplayName: p["display_name"].(string),
					Enabled:     enabled,
				}
				oauthConfig.Providers = append(oauthConfig.Providers, provider)
			}
		}
	}

	return oauthConfig
}

func Login(c *gin.Context) {
	oauthConfig := getOAuthConfig(c)
	if oauthConfig != nil && oauthConfig.DisableClassicAuth {
		c.JSON(http.StatusForbidden, models.ErrorResponse("Username/password authentication is disabled. Please use OAuth authentication."))
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	rememberMe := c.PostForm("remember-me") == "on"

	if username == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Username is required"))
		return
	}
	if password == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Password is required"))
		return
	}

	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend authentication service is not available. Please ensure the backend server is running."))
		return
	}

	result, err := backendClient.Login(username, password)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Authentication service unavailable. Please check if the backend server is running."))
		return
	}

	if !result.Success {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse(result.Error))
		return
	}

	err = middleware.SetSessionValue(c, "session_id", result.SessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to create session"))
		return
	}

	err = middleware.SetSessionValue(c, "user_id", result.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to save user ID"))
		return
	}

	err = middleware.SetSessionValue(c, "username", result.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to save username"))
		return
	}

	err = middleware.SetSessionValue(c, "email", result.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to save email"))
		return
	}

	if rememberMe {
		middleware.SetSessionValue(c, "remember_me", true)
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"message": "Login successful",
		"user": gin.H{
			"id":       result.UserID,
			"username": result.Username,
			"email":    result.Email,
		},
		"redirect": "/dashboard",
	}))
}

func Register(c *gin.Context) {
	oauthConfig := getOAuthConfig(c)
	if oauthConfig != nil && oauthConfig.DisableClassicAuth {
		c.JSON(http.StatusForbidden, models.ErrorResponse("Username/password registration is disabled. Please use OAuth authentication."))
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	email := strings.TrimSpace(c.PostForm("email"))
	password := c.PostForm("password")
	confirmPassword := c.PostForm("confirm-password")

	if username == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Username is required"))
		return
	}
	if email == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Email is required"))
		return
	}
	if password == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Password is required"))
		return
	}
	if password != confirmPassword {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Passwords do not match"))
		return
	}
	if len(password) < 4 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Password must be at least 4 characters long"))
		return
	}

	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend registration service is not available. Please ensure the backend server is running."))
		return
	}

	result, err := backendClient.Register(username, email, password)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Registration service unavailable. Please check if the backend server is running."))
		return
	}

	if !result.Success {
		c.JSON(http.StatusBadRequest, models.ErrorResponse(result.Error))
		return
	}

	c.JSON(http.StatusCreated, models.SuccessResponse(gin.H{
		"message":  "Registration successful",
		"user_id":  result.UserID,
		"redirect": "/login",
	}))
}

func Logout(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID != "" {
		backendClient.Logout(sessionID)
	}

	err := middleware.ClearSession(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to clear session"))
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"message":  "Logout successful",
		"redirect": "/",
	}))
}

func GetCurrentUser(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse("Not authenticated"))
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
		},
	}))
}

func GetAlerts(c *gin.Context) {
	search := c.Query("search")
	severityFilter := c.Query("severity")
	statusFilter := c.Query("status")

	if alertmanagerClient == nil {
		c.JSON(http.StatusOK, models.SuccessResponse(getMockAlerts(search, severityFilter, statusFilter)))
		return
	}

	alertsWithSource, err := alertmanagerClient.FetchAllAlerts()
	if err != nil {
		c.JSON(http.StatusOK, models.SuccessResponse(getMockAlerts(search, severityFilter, statusFilter)))
		return
	}

	var allAlerts []map[string]interface{}
	for _, alertWithSource := range alertsWithSource {
		alert := alertWithSource.Alert

		transformedLabels := make(map[string]string)
		for key, value := range alert.Labels {
			if key == "severity" {
				transformedLabels[key] = transformSeverity(value)
			} else {
				transformedLabels[key] = value
			}
		}

		webAlert := map[string]interface{}{
			"fingerprint": generateFingerprint(alert.Labels),
			"status": map[string]interface{}{
				"state":       transformStatus(alert.Status.State),
				"silencedBy":  alert.Status.SilencedBy,
				"inhibitedBy": alert.Status.InhibitedBy,
			},
			"labels":       transformedLabels,
			"annotations":  alert.Annotations,
			"startsAt":     alert.StartsAt.Format(time.RFC3339),
			"endsAt":       formatEndTime(alert.EndsAt),
			"updatedAt":    time.Now().Format(time.RFC3339),
			"generatorURL": alert.GeneratorURL,
			"source":       alertWithSource.Source,
		}

		allAlerts = append(allAlerts, webAlert)
	}

	var filteredAlerts []map[string]interface{}
	for _, alert := range allAlerts {
		if !applyFilters(alert, search, severityFilter, statusFilter) {
			continue
		}
		filteredAlerts = append(filteredAlerts, alert)
	}

	c.JSON(http.StatusOK, models.SuccessResponse(filteredAlerts))
}

func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"status":  "ok",
		"service": "webui",
	}))
}

func BackendHealthCheck(c *gin.Context) {
	if backendClient == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend client not initialized"))
		return
	}

	if !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend not connected"))
		return
	}

	err := backendClient.HealthCheck()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse(fmt.Sprintf("Backend health check failed: %v", err)))
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"status":  "ok",
		"backend": "connected",
	}))
}

func AlertmanagerHealthCheck(c *gin.Context) {
	if alertmanagerClient == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Alertmanager client not initialized"))
		return
	}

	healthStatus := alertmanagerClient.GetHealthStatus()
	if len(healthStatus) == 0 {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("No Alertmanager instances configured"))
		return
	}

	allHealthy := true
	for _, healthy := range healthStatus {
		if !healthy {
			allHealthy = false
			break
		}
	}

	status := http.StatusOK
	if !allHealthy {
		status = http.StatusPartialContent
	}

	c.JSON(status, models.SuccessResponse(gin.H{
		"status":    "ok",
		"instances": healthStatus,
		"healthy":   allHealthy,
	}))
}

func IndexPage(c *gin.Context) {
	c.Header("Content-Type", "text/html")
	pages.Index().Render(context.Background(), c.Writer)
}

func PlaygroundPage(c *gin.Context) {
	c.Header("Content-Type", "text/html")

	oauthConfig := getOAuthConfig(c)

	if oauthConfig != nil {
		pages.PlaygroundWithOAuth(oauthConfig).Render(context.Background(), c.Writer)
	} else {
		pages.Playground().Render(context.Background(), c.Writer)
	}
}

func LoginPage(c *gin.Context) {
	c.Header("Content-Type", "text/html")

	oauthConfig := getOAuthConfig(c)

	if oauthConfig != nil {
		pages.LoginWithOAuth(oauthConfig).Render(context.Background(), c.Writer)
	} else {
		pages.Login().Render(context.Background(), c.Writer)
	}
}

func RegisterPage(c *gin.Context) {
	oauthConfig := getOAuthConfig(c)
	if oauthConfig != nil && oauthConfig.DisableClassicAuth {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	c.Header("Content-Type", "text/html")
	pages.Register().Render(context.Background(), c.Writer)
}

func DashboardPage(c *gin.Context) {
	c.Header("Content-Type", "text/html")
	pages.NewDashboard().Render(context.Background(), c.Writer)
}

func StandaloneAlertPage(c *gin.Context) {
	c.Header("Content-Type", "text/html")
	pages.AlertDetailsStandalone().Render(context.Background(), c.Writer)
}

func generateFingerprint(labels map[string]string) string {
	var labelPairs []string
	for key, value := range labels {
		labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", key, value))
	}
	sort.Strings(labelPairs)
	labelString := strings.Join(labelPairs, ",")
	hash := md5.Sum([]byte(labelString))
	return fmt.Sprintf("%x", hash)
}

func formatEndTime(endTime time.Time) string {
	if endTime.IsZero() {
		return "0001-01-01T00:00:00.000Z"
	}
	return endTime.Format(time.RFC3339)
}

func applyFilters(alert map[string]interface{}, search, severityFilter, statusFilter string) bool {
	if severityFilter != "" {
		if labels, ok := alert["labels"].(map[string]string); ok {
			if alertSeverity, exists := labels["severity"]; !exists || alertSeverity != severityFilter {
				return false
			}
		}
	}

	if statusFilter != "" {
		if statusMap, ok := alert["status"].(map[string]interface{}); ok {
			if alertStatus, exists := statusMap["state"]; !exists || alertStatus != statusFilter {
				return false
			}
		}
	}

	if search != "" {
		searchLower := strings.ToLower(search)
		found := false

		if labels, ok := alert["labels"].(map[string]string); ok {
			if alertname, exists := labels["alertname"]; exists {
				if strings.Contains(strings.ToLower(alertname), searchLower) {
					found = true
				}
			}

			if instance, exists := labels["instance"]; exists {
				if strings.Contains(strings.ToLower(instance), searchLower) {
					found = true
				}
			}
		}

		if annotations, ok := alert["annotations"].(map[string]string); ok {
			if summary, exists := annotations["summary"]; exists {
				if strings.Contains(strings.ToLower(summary), searchLower) {
					found = true
				}
			}
		}

		if !found {
			return false
		}
	}

	return true
}

func getMockAlerts(search, severityFilter, statusFilter string) []map[string]interface{} {
	allAlerts := []map[string]interface{}{
		{
			"fingerprint": "3f4e5d6c7b8a9e2d",
			"status": map[string]interface{}{
				"state": "firing",
			},
			"labels": map[string]string{
				"alertname": "NodeDown",
				"instance":  "node-exporter:9100",
				"job":       "node-exporter",
				"severity":  "critical",
			},
			"annotations": map[string]string{
				"summary":     "Node Exporter down",
				"description": "Node Exporter has been down for more than 5 minutes.",
			},
			"startsAt":     "2025-01-20T10:30:00.000Z",
			"endsAt":       "0001-01-01T00:00:00.000Z",
			"updatedAt":    "2025-01-20T10:30:00.000Z",
			"generatorURL": "http://prometheus:9090/graph?g0.expr=up%7Bjob%3D%22node-exporter%22%7D+%3D%3D+0&g0.tab=1",
			"source":       "mock-alertmanager",
		},
		{
			"fingerprint": "8f2a3e4d5c6b7a1e",
			"status": map[string]interface{}{
				"state": "firing",
			},
			"labels": map[string]string{
				"alertname": "HighMemoryUsage",
				"instance":  "app-server-01:8080",
				"job":       "app-servers",
				"severity":  "warning",
			},
			"annotations": map[string]string{
				"summary":     "High memory usage detected",
				"description": "Memory usage is above 85% for more than 10 minutes.",
			},
			"startsAt":     "2025-01-20T10:15:00.000Z",
			"endsAt":       "0001-01-01T00:00:00.000Z",
			"updatedAt":    "2025-01-20T10:45:00.000Z",
			"generatorURL": "http://prometheus:9090/graph?g0.expr=memory_usage+%3E+85&g0.tab=1",
			"source":       "mock-alertmanager",
		},
	}

	var filteredAlerts []map[string]interface{}
	for _, alert := range allAlerts {
		if !applyFilters(alert, search, severityFilter, statusFilter) {
			continue
		}
		filteredAlerts = append(filteredAlerts, alert)
	}

	return filteredAlerts
}
