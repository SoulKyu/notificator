package handlers

import (
	"log"
	"net/http"

	"notificator/internal/webui/client"
	"notificator/internal/webui/middleware"
	"notificator/internal/webui/models"

	"github.com/gin-gonic/gin"
)

var statisticsViewBackendClient *client.BackendClient

// SetStatisticsViewBackendClient sets the backend client for statistics view handlers
func SetStatisticsViewBackendClient(c *client.BackendClient) {
	statisticsViewBackendClient = c
}

// GetStatisticsViews handles GET /api/v1/statistics/views
func GetStatisticsViews(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get impersonated user ID if impersonating
	impersonateUserID := middleware.GetImpersonatedUserID(c)

	// Get include_shared parameter (default true)
	includeShared := true
	if c.Query("include_shared") == "false" {
		includeShared = false
	}

	// Get views from backend
	views, err := statisticsViewBackendClient.GetStatisticsViews(sessionID.(string), includeShared, impersonateUserID)
	if err != nil {
		log.Printf("Failed to get statistics views: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to get statistics views",
		})
		return
	}

	c.JSON(http.StatusOK, models.StatisticsViewsResponse{
		Success: true,
		Views:   views,
		Message: "Statistics views retrieved successfully",
	})
}

// CreateStatisticsView handles POST /api/v1/statistics/views
func CreateStatisticsView(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Parse request body
	var req models.StatisticsViewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}

	// Validate required fields
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "View name is required",
		})
		return
	}

	// Get impersonated user ID if impersonating
	impersonateUserID := middleware.GetImpersonatedUserID(c)

	// Create view via backend
	view, err := statisticsViewBackendClient.SaveStatisticsView(
		sessionID.(string),
		req.Name,
		req.Description,
		req.IsShared,
		req.ViewData,
		impersonateUserID,
	)
	if err != nil {
		log.Printf("Failed to create statistics view: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to create statistics view",
		})
		return
	}

	c.JSON(http.StatusCreated, models.StatisticsViewResponse{
		Success: true,
		View:    view,
		Message: "Statistics view created successfully",
	})
}

// UpdateStatisticsView handles PUT /api/v1/statistics/views/:id
func UpdateStatisticsView(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get view ID from path
	viewID := c.Param("id")
	if viewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "View ID is required",
		})
		return
	}

	// Parse request body
	var req models.StatisticsViewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}

	// Validate required fields
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "View name is required",
		})
		return
	}

	// Get impersonated user ID if impersonating
	impersonateUserID := middleware.GetImpersonatedUserID(c)

	// Update view via backend
	view, err := statisticsViewBackendClient.UpdateStatisticsView(
		sessionID.(string),
		viewID,
		req.Name,
		req.Description,
		req.IsShared,
		req.ViewData,
		impersonateUserID,
	)
	if err != nil {
		log.Printf("Failed to update statistics view: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to update statistics view",
		})
		return
	}

	c.JSON(http.StatusOK, models.StatisticsViewResponse{
		Success: true,
		View:    view,
		Message: "Statistics view updated successfully",
	})
}

// DeleteStatisticsView handles DELETE /api/v1/statistics/views/:id
func DeleteStatisticsView(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get view ID from path
	viewID := c.Param("id")
	if viewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "View ID is required",
		})
		return
	}

	// Get impersonated user ID if impersonating
	impersonateUserID := middleware.GetImpersonatedUserID(c)

	// Delete view via backend
	err := statisticsViewBackendClient.DeleteStatisticsView(
		sessionID.(string),
		viewID,
		impersonateUserID,
	)
	if err != nil {
		log.Printf("Failed to delete statistics view: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to delete statistics view",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Statistics view deleted successfully",
	})
}

// SetDefaultStatisticsView handles POST /api/v1/statistics/views/:id/default
func SetDefaultStatisticsView(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get view ID from path (empty string means clear default)
	viewID := c.Param("id")

	// Get impersonated user ID if impersonating
	impersonateUserID := middleware.GetImpersonatedUserID(c)

	// Set default view via backend
	err := statisticsViewBackendClient.SetDefaultStatisticsView(
		sessionID.(string),
		viewID,
		impersonateUserID,
	)
	if err != nil {
		log.Printf("Failed to set default statistics view: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to set default statistics view",
		})
		return
	}

	message := "Default statistics view set successfully"
	if viewID == "" {
		message = "Default statistics view cleared successfully"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": message,
	})
}

// ClearDefaultStatisticsView handles DELETE /api/v1/statistics/views/default
func ClearDefaultStatisticsView(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get impersonated user ID if impersonating
	impersonateUserID := middleware.GetImpersonatedUserID(c)

	// Clear default by passing empty view ID
	err := statisticsViewBackendClient.SetDefaultStatisticsView(
		sessionID.(string),
		"",
		impersonateUserID,
	)
	if err != nil {
		log.Printf("Failed to clear default statistics view: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to clear default statistics view",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Default statistics view cleared successfully",
	})
}

