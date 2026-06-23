package handlers

import (
	"log"
	"net/http"
	"time"

	"notificator/config"
	"notificator/internal/webui/middleware"
	"notificator/internal/webui/models"

	"github.com/gin-gonic/gin"
)

var appConfig *config.Config

// SetAppConfig sets the application config for impersonation checks
func SetAppConfig(cfg *config.Config) {
	appConfig = cfg
}

// canImpersonate checks if the current user is allowed to impersonate
func canImpersonate(c *gin.Context) bool {
	if appConfig == nil {
		return false
	}

	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		return false
	}

	// Check by username or email
	return appConfig.Admin.CanImpersonate(user.Username) || appConfig.Admin.CanImpersonate(user.Email)
}

// StartImpersonation starts impersonating a user
// POST /api/impersonate/start
func StartImpersonation(c *gin.Context) {
	if !canImpersonate(c) {
		c.JSON(http.StatusForbidden, models.ErrorResponse("You are not allowed to impersonate users"))
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Username is required"))
		return
	}

	// Get the user to impersonate from the backend
	sessionID := middleware.GetSessionIDFromContext(c)
	users, _, err := backendClient.ListUsers(sessionID, 1000, 0)
	if err != nil {
		log.Printf("Error listing users for impersonation: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to find user"))
		return
	}

	// Find the target user
	var targetUser *struct {
		ID       string
		Username string
	}
	for _, u := range users {
		if u.Username == req.Username {
			targetUser = &struct {
				ID       string
				Username string
			}{ID: u.ID, Username: u.Username}
			break
		}
	}

	if targetUser == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse("User not found"))
		return
	}

	// Don't allow impersonating yourself
	currentUser := middleware.GetCurrentUserFromContext(c)
	if currentUser != nil && currentUser.ID == targetUser.ID {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Cannot impersonate yourself"))
		return
	}

	// Set impersonation in session
	if err := middleware.SetImpersonation(c, targetUser.ID, targetUser.Username); err != nil {
		log.Printf("Error setting impersonation: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to start impersonation"))
		return
	}

	// Log the impersonation
	log.Printf("[IMPERSONATION] User %s started impersonating %s at %s",
		currentUser.Username, targetUser.Username, time.Now().Format(time.RFC3339))

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  "Now impersonating " + targetUser.Username,
		"username": targetUser.Username,
	})
}

// StopImpersonation stops impersonating
// POST /api/impersonate/stop
func StopImpersonation(c *gin.Context) {
	if !middleware.IsImpersonating(c) {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Not currently impersonating"))
		return
	}

	impersonatedUsername := middleware.GetImpersonatedUsername(c)
	startedAt := middleware.GetImpersonationStartedAt(c)

	// Clear impersonation
	if err := middleware.ClearImpersonation(c); err != nil {
		log.Printf("Error clearing impersonation: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to stop impersonation"))
		return
	}

	// Log the end of impersonation
	currentUser := middleware.GetCurrentUserFromContext(c)
	var duration time.Duration
	if startedAt != nil {
		duration = time.Since(*startedAt)
	}
	log.Printf("[IMPERSONATION] User %s stopped impersonating %s at %s (duration: %s)",
		currentUser.Username, impersonatedUsername, time.Now().Format(time.RFC3339), duration)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Stopped impersonating",
	})
}

// ListUsersForImpersonation returns list of users that can be impersonated
// GET /api/impersonate/users
func ListUsersForImpersonation(c *gin.Context) {
	if !canImpersonate(c) {
		c.JSON(http.StatusForbidden, models.ErrorResponse("You are not allowed to impersonate users"))
		return
	}

	sessionID := middleware.GetSessionIDFromContext(c)
	users, totalCount, err := backendClient.ListUsers(sessionID, 1000, 0)
	if err != nil {
		log.Printf("Error listing users: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to list users"))
		return
	}

	// Filter out the current user from the list
	currentUser := middleware.GetCurrentUserFromContext(c)
	var filteredUsers []gin.H
	for _, u := range users {
		if currentUser == nil || u.ID != currentUser.ID {
			filteredUsers = append(filteredUsers, gin.H{
				"id":       u.ID,
				"username": u.Username,
				"email":    u.Email,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"users":       filteredUsers,
		"total_count": totalCount,
	})
}

// GetImpersonationStatus returns current impersonation status
// GET /api/impersonate/status
func GetImpersonationStatus(c *gin.Context) {
	isImpersonating := middleware.IsImpersonating(c)

	response := gin.H{
		"is_impersonating": isImpersonating,
		"can_impersonate":  canImpersonate(c),
	}

	if isImpersonating {
		response["impersonated_user_id"] = middleware.GetImpersonatedUserID(c)
		response["impersonated_username"] = middleware.GetImpersonatedUsername(c)
		if startedAt := middleware.GetImpersonationStartedAt(c); startedAt != nil {
			response["started_at"] = startedAt.Format(time.RFC3339)
		}
	}

	c.JSON(http.StatusOK, response)
}

// HandleImpersonateURLParam middleware checks for ?impersonate=username and starts impersonation
func HandleImpersonateURLParam() gin.HandlerFunc {
	return func(c *gin.Context) {
		username := c.Query("impersonate")
		if username == "" {
			c.Next()
			return
		}

		// Check if user can impersonate
		if !canImpersonate(c) {
			c.Next()
			return
		}

		// Already impersonating the same user?
		if middleware.IsImpersonating(c) && middleware.GetImpersonatedUsername(c) == username {
			c.Next()
			return
		}

		// Get the user to impersonate
		sessionID := middleware.GetSessionIDFromContext(c)
		users, _, err := backendClient.ListUsers(sessionID, 1000, 0)
		if err != nil {
			log.Printf("Error listing users for URL impersonation: %v", err)
			c.Next()
			return
		}

		// Find the target user
		for _, u := range users {
			if u.Username == username {
				currentUser := middleware.GetCurrentUserFromContext(c)
				if currentUser != nil && currentUser.ID != u.ID {
					if err := middleware.SetImpersonation(c, u.ID, u.Username); err == nil {
						log.Printf("[IMPERSONATION] User %s started impersonating %s via URL at %s",
							currentUser.Username, u.Username, time.Now().Format(time.RFC3339))
					}
				}
				break
			}
		}

		c.Next()
	}
}
