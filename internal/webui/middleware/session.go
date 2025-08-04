package middleware

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

const SessionName = "notificator-session"

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
