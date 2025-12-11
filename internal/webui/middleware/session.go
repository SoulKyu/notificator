package middleware

import (
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

const SessionName = "notificator-session"

// Impersonation session keys
const (
	ImpersonatingUserID       = "impersonating_user_id"
	ImpersonatingUsername     = "impersonating_username"
	ImpersonationStartedAt    = "impersonation_started_at"
)

func SessionMiddleware(secret string) gin.HandlerFunc {
	store := cookie.NewStore([]byte(secret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: 0,     // Default SameSite behavior
	})
	return sessions.Sessions(SessionName, store)
}

func SetSessionValue(c *gin.Context, key string, value interface{}) error {
	session := sessions.Default(c)
	session.Set(key, value)
	return session.Save()
}

func GetSessionValue(c *gin.Context, key string) interface{} {
	session := sessions.Default(c)
	return session.Get(key)
}

func ClearSession(c *gin.Context) error {
	session := sessions.Default(c)
	session.Clear()
	return session.Save()
}

func GetSessionID(c *gin.Context) string {
	if sessionID := GetSessionValue(c, "session_id"); sessionID != nil {
		if sid, ok := sessionID.(string); ok {
			return sid
		}
	}
	return ""
}

func GetCurrentUser(c *gin.Context) map[string]interface{} {
	userID := GetSessionValue(c, "user_id")
	username := GetSessionValue(c, "username")
	email := GetSessionValue(c, "email")

	if userID == nil || username == nil {
		return nil
	}

	return map[string]interface{}{
		"id":       userID,
		"username": username,
		"email":    email,
	}
}

// Impersonation helper functions

// SetImpersonation starts impersonating a user
func SetImpersonation(c *gin.Context, userID, username string) error {
	session := sessions.Default(c)
	session.Set(ImpersonatingUserID, userID)
	session.Set(ImpersonatingUsername, username)
	session.Set(ImpersonationStartedAt, time.Now().Unix())
	return session.Save()
}

// ClearImpersonation stops impersonating
func ClearImpersonation(c *gin.Context) error {
	session := sessions.Default(c)
	session.Delete(ImpersonatingUserID)
	session.Delete(ImpersonatingUsername)
	session.Delete(ImpersonationStartedAt)
	return session.Save()
}

// IsImpersonating returns true if the current session is impersonating another user
func IsImpersonating(c *gin.Context) bool {
	return GetSessionValue(c, ImpersonatingUserID) != nil
}

// GetImpersonatedUserID returns the ID of the user being impersonated, or empty string if not impersonating
func GetImpersonatedUserID(c *gin.Context) string {
	if userID := GetSessionValue(c, ImpersonatingUserID); userID != nil {
		if uid, ok := userID.(string); ok {
			return uid
		}
	}
	return ""
}

// GetImpersonatedUsername returns the username of the user being impersonated
func GetImpersonatedUsername(c *gin.Context) string {
	if username := GetSessionValue(c, ImpersonatingUsername); username != nil {
		if uname, ok := username.(string); ok {
			return uname
		}
	}
	return ""
}

// GetImpersonationStartedAt returns when impersonation started
func GetImpersonationStartedAt(c *gin.Context) *time.Time {
	if startedAt := GetSessionValue(c, ImpersonationStartedAt); startedAt != nil {
		if ts, ok := startedAt.(int64); ok {
			t := time.Unix(ts, 0)
			return &t
		}
	}
	return nil
}
