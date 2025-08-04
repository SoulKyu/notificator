package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	
	"notificator/internal/webui/middleware"
	"notificator/internal/webui/models"
	"notificator/internal/webui/templates/pages"
)

func ProfilePage(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	sessionID := middleware.GetSessionID(c)
	
	authMethod := middleware.GetSessionValue(c, "auth_method")
	var oauthProvider *string
	if authMethodStr, ok := authMethod.(string); ok && strings.HasPrefix(authMethodStr, "oauth:") {
		provider := strings.TrimPrefix(authMethodStr, "oauth:")
		if user.OAuthProvider == nil {
			oauthProvider = &provider
		} else {
			oauthProvider = user.OAuthProvider
		}
	} else {
		oauthProvider = user.OAuthProvider
	}
	
	profileData := pages.ProfileData{
		User: pages.ProfileUser{
			ID:            user.ID,
			Username:      user.Username,
			Email:         user.Email,
			OAuthProvider: oauthProvider,
			OAuthID:       user.OAuthID,
			CreatedAt:     time.Now().AddDate(0, -3, -15),
			LastLogin:     &[]time.Time{time.Now().Add(-2 * time.Hour)}[0],
			EmailVerified: user.Email != "",
		},
		SessionInfo: pages.SessionInfo{
			SessionID: sessionID,
			CreatedAt: time.Now().Add(-30 * time.Minute),
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		},
		Stats: pages.UserStats{
			TotalAlerts:    156,
			ActiveAlerts:   12,
			ResolvedAlerts: 144,
			LastActivity:   &[]time.Time{time.Now().Add(-5 * time.Minute)}[0],
		},
	}

	templ.Handler(pages.Profile(profileData)).ServeHTTP(c.Writer, c.Request)
}

func GetProfileData(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse("Not authenticated"))
		return
	}

	authMethod := middleware.GetSessionValue(c, "auth_method")
	var oauthProvider *string
	if authMethodStr, ok := authMethod.(string); ok && strings.HasPrefix(authMethodStr, "oauth:") {
		provider := strings.TrimPrefix(authMethodStr, "oauth:")
		if user.OAuthProvider == nil {
			oauthProvider = &provider
		} else {
			oauthProvider = user.OAuthProvider
		}
	} else {
		oauthProvider = user.OAuthProvider
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"user": gin.H{
			"id":             user.ID,
			"username":       user.Username,
			"email":          user.Email,
			"oauth_provider": oauthProvider,
			"oauth_id":       user.OAuthID,
		},
		"stats": gin.H{
			"acknowledged_alerts": 42,
			"comments":           17,
			"color_preferences":  3,
		},
	}))
}