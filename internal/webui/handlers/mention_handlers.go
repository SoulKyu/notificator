package handlers

import (
	"log"
	"net/http"

	"notificator/internal/webui/models"

	"github.com/gin-gonic/gin"
)

// GetMentionableUsers returns usernames matching the q prefix, for the
// @mention autocomplete in the comment box. Only id and username are
// exposed: every authenticated user can call this, so emails stay private.
// GET /api/v1/users/mentionable?q=<prefix>
func GetMentionableUsers(c *gin.Context) {
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend not available"))
		return
	}

	users, err := backendClient.SearchUsers(c.Query("q"), 8)
	if err != nil {
		log.Printf("Error searching mentionable users: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to search users"))
		return
	}

	out := make([]gin.H, len(users))
	for i, u := range users {
		out[i] = gin.H{
			"id":       u.ID,
			"username": u.Username,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"users":   out,
	})
}
