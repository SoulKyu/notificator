package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"notificator/internal/webui/client"
	"notificator/internal/webui/models"
)

type AuthMiddleware struct {
	backendClient *client.BackendClient
}

func NewAuthMiddleware(backendClient *client.BackendClient) *AuthMiddleware {
	return &AuthMiddleware{
		backendClient: backendClient,
	}
}

func (am *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if backend is available
		if am.backendClient == nil || !am.backendClient.IsConnected() {
			c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Authentication service unavailable"))
			c.Abort()
			return
		}

		sessionID := GetSessionID(c)
		if sessionID == "" {
			c.JSON(http.StatusUnauthorized, models.ErrorResponse("Authentication required"))
			c.Abort()
			return
		}

		// Validate session with backend
		user, err := am.backendClient.ValidateSession(sessionID)
		if err != nil {
			// Session is invalid, clear it
			ClearSession(c)
			c.JSON(http.StatusUnauthorized, models.ErrorResponse("Invalid or expired session"))
			c.Abort()
			return
		}

		// Set user in context for handlers to use
		c.Set("user", user)
		c.Set("session_id", sessionID)
		c.Next()
	}
}

func (am *AuthMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip authentication if backend is not available
		if am.backendClient == nil || !am.backendClient.IsConnected() {
			c.Next()
			return
		}

		sessionID := GetSessionID(c)
		if sessionID != "" {
			// Try to validate session, but don't fail if invalid
			user, err := am.backendClient.ValidateSession(sessionID)
			if err == nil && user != nil {
				c.Set("user", user)
				c.Set("session_id", sessionID)
			} else {
				// Clear invalid session
				ClearSession(c)
			}
		}
		c.Next()
	}
}

func (am *AuthMiddleware) RedirectIfNotAuth(redirectTo string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip authentication if backend is not available
		if am.backendClient == nil || !am.backendClient.IsConnected() {
			c.Next()
			return
		}

		sessionID := GetSessionID(c)
		if sessionID == "" {
			c.Redirect(http.StatusFound, redirectTo)
			c.Abort()
			return
		}

		// Validate session with backend
		user, err := am.backendClient.ValidateSession(sessionID)
		if err != nil {
			// Session is invalid, clear it and redirect
			ClearSession(c)
			c.Redirect(http.StatusFound, redirectTo)
			c.Abort()
			return
		}

		// Set user in context
		c.Set("user", user)
		c.Set("session_id", sessionID)
		c.Next()
	}
}

func (am *AuthMiddleware) RedirectIfAuth(redirectTo string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip authentication if backend is not available
		if am.backendClient == nil || !am.backendClient.IsConnected() {
			c.Next()
			return
		}

		sessionID := GetSessionID(c)
		if sessionID != "" {
			// Check if session is still valid
			_, err := am.backendClient.ValidateSession(sessionID)
			if err == nil {
				// User is already authenticated, redirect away
				c.Redirect(http.StatusFound, redirectTo)
				c.Abort()
				return
			} else {
				// Clear invalid session
				ClearSession(c)
			}
		}
		c.Next()
	}
}

// Helper function to get current user from context
func GetCurrentUserFromContext(c *gin.Context) *client.User {
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*client.User); ok {
			return u
		}
	}
	return nil
}

// Helper function to get session ID from context
func GetSessionIDFromContext(c *gin.Context) string {
	if sessionID, exists := c.Get("session_id"); exists {
		if sid, ok := sessionID.(string); ok {
			return sid
		}
	}
	return ""
}