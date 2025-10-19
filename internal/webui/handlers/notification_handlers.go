package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"notificator/internal/webui/client"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
)

// GetNotificationPreferences retrieves the user's notification preferences
func GetNotificationPreferences(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	// Check if backend client is available
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Get preferences from backend
	prefs, err := backendClient.GetNotificationPreferences(sessionID)
	if err != nil {
		log.Printf("Failed to get notification preferences: %v", err)
		// Return default preferences if failed
		c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
			"browser_notifications_enabled": false,
			"enabled_severities":            []string{"critical", "warning"},
			"sound_notifications_enabled":   true,
		}))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"browser_notifications_enabled": prefs.BrowserNotificationsEnabled,
		"enabled_severities":            prefs.EnabledSeverities,
		"sound_notifications_enabled":   prefs.SoundNotificationsEnabled,
	}))
}

// SaveNotificationPreferences saves the user's notification preferences
func SaveNotificationPreferences(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("User not authenticated"))
		return
	}

	// Parse request body
	var request struct {
		BrowserNotificationsEnabled bool     `json:"browser_notifications_enabled"`
		EnabledSeverities           []string `json:"enabled_severities"`
		SoundNotificationsEnabled   bool     `json:"sound_notifications_enabled"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request: "+err.Error()))
		return
	}

	// Check if backend client is available
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend service not available"))
		return
	}

	// Create preference object
	prefs := &client.NotificationPreferences{
		BrowserNotificationsEnabled: request.BrowserNotificationsEnabled,
		EnabledSeverities:           request.EnabledSeverities,
		SoundNotificationsEnabled:   request.SoundNotificationsEnabled,
	}

	// Save to backend
	err := backendClient.SaveNotificationPreferences(sessionID, prefs)
	if err != nil {
		log.Printf("Failed to save notification preferences: %v", err)
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to save notification preferences"))
		return
	}

	log.Printf("Notification preferences saved successfully")

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message":                       "Notification preferences saved successfully",
		"browser_notifications_enabled": prefs.BrowserNotificationsEnabled,
		"enabled_severities":            prefs.EnabledSeverities,
		"sound_notifications_enabled":   prefs.SoundNotificationsEnabled,
	}))
}
