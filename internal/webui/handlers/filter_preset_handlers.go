package handlers

import (
	"log"
	"net/http"

	"notificator/internal/webui/client"
	"notificator/internal/webui/models"

	"github.com/gin-gonic/gin"
)

var filterPresetBackendClient *client.BackendClient

// SetFilterPresetBackendClient sets the backend client for filter preset handlers
func SetFilterPresetBackendClient(c *client.BackendClient) {
	filterPresetBackendClient = c
}

// GetFilterPresets handles GET /api/v1/dashboard/filter-presets
func GetFilterPresets(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get include_shared parameter (default true)
	includeShared := true
	if c.Query("include_shared") == "false" {
		includeShared = false
	}

	// Get presets from backend
	presets, err := filterPresetBackendClient.GetFilterPresets(sessionID.(string), includeShared)
	if err != nil {
		log.Printf("Failed to get filter presets: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to get filter presets",
		})
		return
	}

	c.JSON(http.StatusOK, models.FilterPresetsResponse{
		Success: true,
		Presets: presets,
		Message: "Filter presets retrieved successfully",
	})
}

// CreateFilterPreset handles POST /api/v1/dashboard/filter-presets
func CreateFilterPreset(c *gin.Context) {
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
	var req models.FilterPresetRequest
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
			"message": "Preset name is required",
		})
		return
	}

	// Save preset via backend
	preset, err := filterPresetBackendClient.SaveFilterPreset(
		sessionID.(string),
		req.Name,
		req.Description,
		req.IsShared,
		req.FilterData,
	)
	if err != nil {
		log.Printf("Failed to save filter preset: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to save filter preset",
		})
		return
	}

	c.JSON(http.StatusOK, models.FilterPresetResponse{
		Success: true,
		Preset:  preset,
		Message: "Filter preset saved successfully",
	})
}

// UpdateFilterPreset handles PUT /api/v1/dashboard/filter-presets/:id
func UpdateFilterPreset(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get preset ID from URL
	presetID := c.Param("id")
	if presetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Preset ID is required",
		})
		return
	}

	// Parse request body
	var req models.FilterPresetRequest
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
			"message": "Preset name is required",
		})
		return
	}

	// Update preset via backend
	preset, err := filterPresetBackendClient.UpdateFilterPreset(
		sessionID.(string),
		presetID,
		req.Name,
		req.Description,
		req.IsShared,
		req.FilterData,
	)
	if err != nil {
		log.Printf("Failed to update filter preset: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to update filter preset",
		})
		return
	}

	c.JSON(http.StatusOK, models.FilterPresetResponse{
		Success: true,
		Preset:  preset,
		Message: "Filter preset updated successfully",
	})
}

// DeleteFilterPreset handles DELETE /api/v1/dashboard/filter-presets/:id
func DeleteFilterPreset(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get preset ID from URL
	presetID := c.Param("id")
	if presetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Preset ID is required",
		})
		return
	}

	// Delete preset via backend
	err := filterPresetBackendClient.DeleteFilterPreset(sessionID.(string), presetID)
	if err != nil {
		log.Printf("Failed to delete filter preset: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to delete filter preset",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Filter preset deleted successfully",
	})
}

// SetDefaultFilterPreset handles POST /api/v1/dashboard/filter-presets/:id/default
func SetDefaultFilterPreset(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get preset ID from URL
	presetID := c.Param("id")
	if presetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Preset ID is required",
		})
		return
	}

	// Set default via backend
	err := filterPresetBackendClient.SetDefaultFilterPreset(sessionID.(string), presetID)
	if err != nil {
		log.Printf("Failed to set default filter preset: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to set default filter preset",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Default filter preset set successfully",
	})
}

// GetDefaultFilterPreset handles GET /api/v1/dashboard/filter-presets/default
func GetDefaultFilterPreset(c *gin.Context) {
	// Get session from middleware
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized",
		})
		return
	}

	// Get all presets and find the default one
	presets, err := filterPresetBackendClient.GetFilterPresets(sessionID.(string), false)
	if err != nil {
		log.Printf("Failed to get filter presets: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to get default filter preset",
		})
		return
	}

	// Find default preset
	var defaultPreset *models.FilterPreset
	for i := range presets {
		if presets[i].IsDefault {
			defaultPreset = &presets[i]
			break
		}
	}

	if defaultPreset == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"preset":  nil,
			"message": "No default filter preset found",
		})
		return
	}

	c.JSON(http.StatusOK, models.FilterPresetResponse{
		Success: true,
		Preset:  defaultPreset,
		Message: "Default filter preset retrieved successfully",
	})
}
