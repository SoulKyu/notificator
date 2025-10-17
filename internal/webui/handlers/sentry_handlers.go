package handlers

import (
	"fmt"
	"log"
	"net/http"

	"notificator/internal/models"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/services"

	"github.com/gin-gonic/gin"
)

var sentryService *services.SentryService

// SetSentryService sets the global Sentry service instance
func SetSentryService(service *services.SentryService) {
	sentryService = service
}

// GetSentryDataForAlert retrieves Sentry data for a specific alert
func GetSentryDataForAlert(c *gin.Context) {
	fingerprint := c.Param("fingerprint")
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Alert fingerprint is required"))
		return
	}

	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	userID := user.ID
	sessionID := middleware.GetSessionIDFromContext(c)

	// Get the alert from cache
	if alertCache == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Alert cache not available"))
		return
	}

	alert := alertCache.GetAlertByFingerprint(fingerprint)
	if alert == nil {
		c.JSON(http.StatusNotFound, webuimodels.ErrorResponse("Alert not found"))
		return
	}

	// Get Sentry data
	if sentryService == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Sentry service not available"))
		return
	}

	// Convert DashboardAlert to models.Alert for the service
	coreAlert := convertToModelsAlert(alert)
	sentryData := sentryService.GetSentryDataForAlert(coreAlert, userID, sessionID)
	c.JSON(http.StatusOK, sentryData)
}

// convertToModelsAlert converts a DashboardAlert to models.Alert
func convertToModelsAlert(alert *webuimodels.DashboardAlert) *models.Alert {
	return &models.Alert{
		Labels:       alert.Labels,
		Annotations:  alert.Annotations,
		StartsAt:     alert.StartsAt,
		EndsAt:       alert.EndsAt,
		GeneratorURL: alert.GeneratorURL,
		Status: models.AlertStatus{
			State: alert.Status.State,
		},
		Source: alert.Source,
	}
}

// GetUserSentryConfig returns the user's Sentry configuration status
func GetUserSentryConfig(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	userID := user.ID

	// For now, return a simple response - in real implementation,
	// we would check the database via backend client
	// Get user's Sentry config from backend
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service unavailable"))
		return
	}

	// Call backend to get Sentry config
	config, err := backendClient.GetUserSentryConfig(userID)
	if err != nil {
		log.Printf("Failed to get user Sentry config: %v", err)
		// Get default base URL from sentry service config
		defaultBaseURL := "https://sentry.io"
		if sentryService != nil && sentryService.GetConfig() != nil && sentryService.GetConfig().BaseURL != "" {
			defaultBaseURL = sentryService.GetConfig().BaseURL
		}
		
		response := gin.H{
			"has_token":   false,
			"base_url":    defaultBaseURL,
			"auth_status": "none",
		}
		c.JSON(http.StatusOK, response)
		return
	}

	hasToken := config != nil
	// Get default base URL from sentry service config
	baseURL := "https://sentry.io"
	if sentryService != nil && sentryService.GetConfig() != nil && sentryService.GetConfig().BaseURL != "" {
		baseURL = sentryService.GetConfig().BaseURL
	}
	if hasToken && config.BaseUrl != "" {
		baseURL = config.BaseUrl
	}

	response := gin.H{
		"has_token": hasToken,
		"base_url":  baseURL,
		"auth_status": func() string {
			if hasToken {
				return "personal_token"
			}
			return "none"
		}(),
	}

	c.JSON(http.StatusOK, response)
}

// SaveUserSentryToken saves the user's Sentry personal access token
func SaveUserSentryToken(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	userID := user.ID

	// Parse request body
	var request struct {
		Token   string `json:"token" binding:"required"`
		BaseURL string `json:"base_url"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Validate base URL if not provided
	if request.BaseURL == "" {
		// Get default base URL from sentry service config
		request.BaseURL = "https://sentry.io"
		if sentryService != nil && sentryService.GetConfig() != nil && sentryService.GetConfig().BaseURL != "" {
			request.BaseURL = sentryService.GetConfig().BaseURL
		}
	}

	// Test the token first
	if sentryService != nil {
		valid, err := sentryService.TestConnection(request.Token, request.BaseURL)
		if err != nil || !valid {
			log.Printf("Sentry token validation failed for user %s: %v", userID, err)
			c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid Sentry token"))
			return
		}
	}

	// Save token to backend via gRPC
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service unavailable"))
		return
	}

	sessionID := middleware.GetSessionIDFromContext(c)
	err := backendClient.SaveUserSentryConfig(userID, request.Token, request.BaseURL, sessionID)
	if err != nil {
		log.Printf("Failed to save Sentry config for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to save Sentry configuration"))
		return
	}

	log.Printf("Sentry token saved for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Sentry token saved successfully",
	})
}

// DeleteUserSentryToken removes the user's Sentry token
func DeleteUserSentryToken(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	userID := user.ID

	// Delete token from backend via gRPC
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service unavailable"))
		return
	}

	sessionID := middleware.GetSessionIDFromContext(c)
	err := backendClient.DeleteUserSentryConfig(userID, sessionID)
	if err != nil {
		log.Printf("Failed to delete Sentry config for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to delete Sentry configuration"))
		return
	}

	log.Printf("Sentry token deleted for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Sentry token removed successfully",
	})
}

// TestSentryConnection tests the user's Sentry token
func TestSentryConnection(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	if sentryService == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Sentry service not available"))
		return
	}

	var testToken string
	var testBaseURL string

	// Check if token and base_url are provided in request body (for testing unsaved tokens)
	var request struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}

	// Try to parse request body - if it fails or is empty, fall back to saved config
	if err := c.ShouldBindJSON(&request); err == nil && request.Token != "" {
		// Use token from request for testing
		testToken = request.Token
		testBaseURL = request.BaseURL
		if testBaseURL == "" {
			// Get default base URL from sentry service config
			testBaseURL = "https://sentry.io"
			if sentryService != nil && sentryService.GetConfig() != nil && sentryService.GetConfig().BaseURL != "" {
				testBaseURL = sentryService.GetConfig().BaseURL
			}
		}
		log.Printf("Testing connection with provided token for user %s", user.ID)
	} else {
		// Fall back to saved configuration
		userID := user.ID

		if backendClient == nil || !backendClient.IsConnected() {
			c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service unavailable"))
			return
		}

		// Get user's Sentry config from database
		config, err := backendClient.GetUserSentryConfig(userID)
		if err != nil {
			log.Printf("Failed to get user Sentry config: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{
				"valid":   false,
				"message": "No Sentry token configured. Please provide a token to test.",
			})
			return
		}

		if config == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"valid":   false,
				"message": "No Sentry token configured. Please provide a token to test.",
			})
			return
		}

		// Note: We can't get the actual encrypted token via gRPC for security reasons
		// This fallback won't work with the current implementation
		c.JSON(http.StatusBadRequest, gin.H{
			"valid":   false,
			"message": "Please provide token in request body for testing",
		})
		return
	}

	// Test the connection using the actual Sentry API
	valid, err := sentryService.TestConnection(testToken, testBaseURL)
	if err != nil {
		log.Printf("Sentry connection test failed: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"valid":   false,
			"message": fmt.Sprintf("Connection failed: %v", err),
		})
		return
	}

	if !valid {
		c.JSON(http.StatusOK, gin.H{
			"valid":   false,
			"message": "Invalid token or connection failed",
		})
		return
	}

	// Connection successful
	response := gin.H{
		"valid":   true,
		"message": "Connection successful",
	}

	c.JSON(http.StatusOK, response)
}
