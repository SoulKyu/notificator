package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"notificator/internal/backend/models"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
)

// GetUserHiddenAlerts returns the list of hidden alerts for the current user
func GetUserHiddenAlerts(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Unauthorized"))
		return
	}

	hiddenAlerts, err := hiddenAlertsService.GetUserHiddenAlerts(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get hidden alerts"))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"hiddenAlerts": hiddenAlerts,
		"count":        len(hiddenAlerts),
	}))
}

// HideAlert hides a specific alert for the current user
func HideAlert(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Unauthorized"))
		return
	}

	var request struct {
		Fingerprint string `json:"fingerprint" binding:"required"`
		AlertName   string `json:"alertName"`
		Instance    string `json:"instance"`
		Reason      string `json:"reason"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format"))
		return
	}

	// Get the alert from cache to get full details
	alert, exists := alertCache.GetAlert(request.Fingerprint)
	if exists {
		err := hiddenAlertsService.HideAlert(sessionID, alert, request.Reason)
		if err != nil {
			c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to hide alert"))
			return
		}
	} else {
		// If alert not in cache, create a minimal alert object
		alert := &webuimodels.DashboardAlert{
			Fingerprint: request.Fingerprint,
			AlertName:   request.AlertName,
			Instance:    request.Instance,
		}
		err := hiddenAlertsService.HideAlert(sessionID, alert, request.Reason)
		if err != nil {
			c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to hide alert"))
			return
		}
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Alert hidden successfully",
	}))
}

// UnhideAlert unhides a specific alert for the current user
func UnhideAlert(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Unauthorized"))
		return
	}

	fingerprint := c.Param("fingerprint")
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Fingerprint is required"))
		return
	}

	err := hiddenAlertsService.UnhideAlert(sessionID, fingerprint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to unhide alert"))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Alert unhidden successfully",
	}))
}

// GetUserHiddenRules returns the list of hidden rules for the current user
func GetUserHiddenRules(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Unauthorized"))
		return
	}

	rules, err := hiddenAlertsService.GetUserHiddenRules(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to get hidden rules"))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"rules": rules,
		"count": len(rules),
	}))
}

// CreateHiddenRule creates a new hidden rule for the current user
func CreateHiddenRule(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Unauthorized"))
		return
	}

	var rule models.UserHiddenRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format"))
		return
	}

	// Clear ID to ensure new rule is created
	rule.ID = ""

	err := hiddenAlertsService.SaveHiddenRule(sessionID, &rule)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to create hidden rule: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Hidden rule created successfully",
		"rule":    rule,
	}))
}

// UpdateHiddenRule updates an existing hidden rule for the current user
func UpdateHiddenRule(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Unauthorized"))
		return
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Rule ID is required"))
		return
	}

	var rule models.UserHiddenRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format"))
		return
	}

	rule.ID = ruleID

	err := hiddenAlertsService.SaveHiddenRule(sessionID, &rule)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to update hidden rule"))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Hidden rule updated successfully",
		"rule":    rule,
	}))
}

// DeleteHiddenRule deletes a hidden rule for the current user
func DeleteHiddenRule(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Unauthorized"))
		return
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Rule ID is required"))
		return
	}

	err := hiddenAlertsService.RemoveHiddenRule(sessionID, ruleID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to delete hidden rule"))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "Hidden rule deleted successfully",
	}))
}

// ClearAllHiddenAlerts removes all hidden alerts for the current user
func ClearAllHiddenAlerts(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Unauthorized"))
		return
	}

	err := hiddenAlertsService.ClearAllHiddenAlerts(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, webuimodels.ErrorResponse("Failed to clear hidden alerts"))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"message": "All hidden alerts cleared successfully",
	}))
}