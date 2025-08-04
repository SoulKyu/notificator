package handlers

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/sessions"

	"notificator/internal/webui/middleware"
	"notificator/internal/webui/models"
)

func OAuthLogin(c *gin.Context) {
	provider := c.Param("provider")
	
	if !isOAuthEnabled() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("OAuth authentication is not enabled"))
		return
	}

	if !isProviderEnabled(provider) {
		c.JSON(http.StatusBadRequest, models.ErrorResponse(fmt.Sprintf("OAuth provider '%s' is not enabled", provider)))
		return
	}

	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend authentication service is not available"))
		return
	}

	state := generateSecureState()
	
	if err := middleware.SetSessionValue(c, "oauth_state", state); err != nil {
		log.Printf("Failed to store OAuth state in session: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to initialize OAuth flow"))
		return
	}

	if err := middleware.SetSessionValue(c, "oauth_provider", provider); err != nil {
		log.Printf("Failed to store OAuth provider in session: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to initialize OAuth flow"))
		return
	}

	authURL, err := backendClient.GetOAuthAuthURL(provider, state)
	if err != nil {
		log.Printf("Failed to get OAuth auth URL: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to initialize OAuth authentication"))
		return
	}

	logOAuthActivity(nil, provider, "login_initiated", true, "", getClientIP(c), c.GetHeader("User-Agent"))

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"auth_url": authURL,
		"provider": provider,
		"state":    state,
	}))
}

func OAuthCallback(c *gin.Context) {
	provider := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")
	errorDescription := c.Query("error_description")

	if errorParam != "" {
		errorMsg := fmt.Sprintf("OAuth error: %s", errorParam)
		if errorDescription != "" {
			errorMsg = fmt.Sprintf("%s - %s", errorMsg, errorDescription)
		}
		
		logOAuthActivity(nil, provider, "callback_error", false, errorMsg, getClientIP(c), c.GetHeader("User-Agent"))
		
		c.Redirect(http.StatusFound, fmt.Sprintf("/login?error=%s", errorParam))
		return
	}

	if code == "" || state == "" {
		logOAuthActivity(nil, provider, "callback_invalid", false, "Missing code or state parameter", getClientIP(c), c.GetHeader("User-Agent"))
		c.Redirect(http.StatusFound, "/login?error=invalid_callback")
		return
	}

	sessionState := middleware.GetSessionValue(c, "oauth_state")
	sessionProvider := middleware.GetSessionValue(c, "oauth_provider")
	
	if sessionState == "" || sessionProvider == "" || sessionState != state || sessionProvider != provider {
		logOAuthActivity(nil, provider, "callback_csrf", false, "Invalid or mismatched state parameter", getClientIP(c), c.GetHeader("User-Agent"))
		c.Redirect(http.StatusFound, "/login?error=invalid_state")
		return
	}

	session := sessions.Default(c)
	session.Delete("oauth_state")
	session.Delete("oauth_provider")
	session.Save()

	if backendClient == nil || !backendClient.IsConnected() {
		c.Redirect(http.StatusFound, "/login?error=backend_unavailable")
		return
	}

	result, err := backendClient.OAuthCallback(provider, code, state)
	if err != nil {
		log.Printf("OAuth callback failed: %v", err)
		logOAuthActivity(nil, provider, "callback_failed", false, err.Error(), getClientIP(c), c.GetHeader("User-Agent"))
		c.Redirect(http.StatusFound, "/login?error=auth_failed")
		return
	}

	if !result.Success {
		logOAuthActivity(nil, provider, "callback_rejected", false, result.Error, getClientIP(c), c.GetHeader("User-Agent"))
		c.Redirect(http.StatusFound, fmt.Sprintf("/login?error=%s", result.Error))
		return
	}

	if err := middleware.SetSessionValue(c, "session_id", result.SessionID); err != nil {
		log.Printf("Failed to set session ID: %v", err)
		c.Redirect(http.StatusFound, "/login?error=session_failed")
		return
	}

	if err := middleware.SetSessionValue(c, "user_id", result.UserID); err != nil {
		log.Printf("Failed to set user ID: %v", err)
		c.Redirect(http.StatusFound, "/login?error=session_failed")
		return
	}

	if err := middleware.SetSessionValue(c, "username", result.Username); err != nil {
		log.Printf("Failed to set username: %v", err)
		c.Redirect(http.StatusFound, "/login?error=session_failed")
		return
	}

	if err := middleware.SetSessionValue(c, "email", result.Email); err != nil {
		log.Printf("Failed to set email: %v", err)
		c.Redirect(http.StatusFound, "/login?error=session_failed")
		return
	}

	if err := middleware.SetSessionValue(c, "auth_method", fmt.Sprintf("oauth:%s", provider)); err != nil {
		log.Printf("Failed to set auth method: %v", err)
	}

	logOAuthActivity(&result.UserID, provider, "login_success", true, "", getClientIP(c), c.GetHeader("User-Agent"))

	redirectURL := "/dashboard"
	if returnTo := c.Query("return_to"); returnTo != "" && isValidReturnURL(returnTo) {
		redirectURL = returnTo
	}

	c.Redirect(http.StatusFound, redirectURL)
}

func GetOAuthProviders(c *gin.Context) {
	if !isOAuthEnabled() {
		c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
			"providers": []gin.H{},
			"enabled":   false,
		}))
		return
	}

	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend service unavailable"))
		return
	}

	providers, err := backendClient.GetOAuthProviders()
	if err != nil {
		log.Printf("Failed to get OAuth providers: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to retrieve OAuth providers"))
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"providers": providers,
		"enabled":   true,
	}))
}

func OAuthLogout(c *gin.Context) {
	sessionID := middleware.GetSessionID(c)
	userID := middleware.GetSessionValue(c, "user_id")
	authMethod := middleware.GetSessionValue(c, "auth_method")

	authMethodStr, ok := authMethod.(string)
	if !ok || authMethodStr == "" || !strings.HasPrefix(authMethodStr, "oauth:") {
		Logout(c)
		return
	}

	provider := strings.TrimPrefix(authMethodStr, "oauth:")

	if sessionID != "" && backendClient != nil {
		backendClient.Logout(sessionID)
	}

	if userIDStr, ok := userID.(string); ok && userIDStr != "" {
		logOAuthActivity(&userIDStr, provider, "logout", true, "", getClientIP(c), c.GetHeader("User-Agent"))
	}

	err := middleware.ClearSession(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to clear session"))
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"message":  "OAuth logout successful",
		"redirect": "/",
	}))
}

func GetUserGroups(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse("Not authenticated"))
		return
	}

	if !user.IsOAuthUser() {
		c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
			"groups": []gin.H{},
			"oauth":  false,
		}))
		return
	}

	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend service unavailable"))
		return
	}

	groups, err := backendClient.GetUserGroups(user.ID)
	if err != nil {
		log.Printf("Failed to get user groups: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to retrieve user groups"))
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"groups": groups,
		"oauth":  true,
	}))
}

func SyncUserGroups(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse("Not authenticated"))
		return
	}

	if !user.IsOAuthUser() {
		c.JSON(http.StatusBadRequest, models.ErrorResponse("Group sync is only available for OAuth users"))
		return
	}

	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse("Backend service unavailable"))
		return
	}

	provider := *user.OAuthProvider
	err := backendClient.SyncUserGroups(user.ID, provider)
	if err != nil {
		log.Printf("Failed to sync user groups: %v", err)
		logOAuthActivity(&user.ID, provider, "group_sync_failed", false, err.Error(), getClientIP(c), c.GetHeader("User-Agent"))
		c.JSON(http.StatusInternalServerError, models.ErrorResponse(fmt.Sprintf("Failed to sync groups: %v", err)))
		return
	}

	logOAuthActivity(&user.ID, provider, "group_sync_success", true, "", getClientIP(c), c.GetHeader("User-Agent"))

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"message": "Groups synchronized successfully",
	}))
}


func generateSecureState() string {
	return fmt.Sprintf("state_%d_%s", getCurrentTimestamp(), generateRandomString(16))
}

func isOAuthEnabled() bool {
	if backendClient == nil {
		return false
	}
	
	enabled, err := backendClient.IsOAuthEnabled()
	if err != nil {
		log.Printf("Failed to check OAuth enabled status: %v", err)
		return false
	}
	
	return enabled
}

func isProviderEnabled(provider string) bool {
	if backendClient == nil {
		return false
	}
	
	providers, err := backendClient.GetOAuthProviders()
	if err != nil {
		return false
	}
	
	for _, p := range providers {
		if p["name"] == provider && p["enabled"] == true {
			return true
		}
	}
	
	return false
}

func getClientIP(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-For"); forwarded != "" {
		if ips := strings.Split(forwarded, ","); len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	
	if realIP := c.GetHeader("X-Real-Ip"); realIP != "" {
		return realIP
	}
	
	return c.ClientIP()
}

func logOAuthActivity(userID *string, provider, action string, success bool, errorMsg, ipAddress, userAgent string) {
	if backendClient == nil {
		return
	}
	
	metadata := map[string]interface{}{
		"provider":   provider,
		"action":     action,
		"success":    success,
		"ip_address": ipAddress,
		"user_agent": userAgent,
	}
	
	if errorMsg != "" {
		metadata["error"] = errorMsg
	}
	
	log.Printf("OAuth Activity: user=%v, provider=%s, action=%s, success=%t", userID, provider, action, success)
}

func isValidReturnURL(url string) bool {
	if strings.HasPrefix(url, "/") && !strings.HasPrefix(url, "//") {
		return true
	}
	
	return false
}

func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}