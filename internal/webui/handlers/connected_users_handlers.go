package handlers

import (
	"log"
	"net/http"

	"notificator/internal/webui/middleware"
	"notificator/internal/webui/models"

	"github.com/gin-gonic/gin"
)

// GetConnectedUsers returns list of connected users (admin only)
// GET /api/admin/connected-users
func GetConnectedUsers(c *gin.Context) {
	if !canImpersonate(c) {
		c.JSON(http.StatusForbidden, models.ErrorResponse("Admin access required"))
		return
	}

	sessionID := middleware.GetSessionIDFromContext(c)
	users, totalCount, err := backendClient.GetConnectedUsers(sessionID)
	if err != nil {
		log.Printf("Error getting connected users: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to get connected users"))
		return
	}

	// Convert to JSON-friendly format
	connectedUsers := make([]gin.H, len(users))
	for i, u := range users {
		connectedUsers[i] = gin.H{
			"user_id":       u.UserID,
			"username":      u.Username,
			"email":         u.Email,
			"session_count": u.SessionCount,
			"last_activity": u.LastActivity.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"users":       connectedUsers,
		"total_count": totalCount,
	})
}
