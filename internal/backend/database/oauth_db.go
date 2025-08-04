package database

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"notificator/internal/backend/models"
)

func (gdb *GormDB) CreateOAuthUser(provider, oauthID string, userInfo *models.OAuthUserInfo) (*models.User, error) {
	user := &models.User{
		Username:      userInfo.Username,
		Email:         userInfo.Email,
		OAuthProvider: &provider,
		OAuthID:       &oauthID,
		OAuthEmail:    &userInfo.Email,
		EmailVerified: userInfo.EmailVerified,
	}

	if user.Username == "" {
		user.Username = generateUsernameFromEmail(userInfo.Email)
	}

	if err := gdb.ensureUniqueUsername(user); err != nil {
		return nil, fmt.Errorf("failed to ensure unique username: %w", err)
	}

	tx := gdb.db.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Create(user).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create OAuth user: %w", err)
	}

	if len(userInfo.Groups) > 0 {
		if err := gdb.syncUserGroupsInTx(tx, user.ID, provider, userInfo.Groups); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to sync user groups: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit OAuth user creation: %w", err)
	}

	log.Printf("✅ Created OAuth user: %s (provider: %s, id: %s)", user.Username, provider, oauthID)
	return user, nil
}

func (gdb *GormDB) UpdateOAuthUser(userID string, userInfo *models.OAuthUserInfo) (*models.User, error) {
	var user models.User
	if err := gdb.db.First(&user, "id = ?", userID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	updates := map[string]interface{}{
		"email":          userInfo.Email,
		"o_auth_email":   userInfo.Email,
		"email_verified": userInfo.EmailVerified,
		"last_login":     time.Now(),
	}

	if err := gdb.db.Model(&user).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update OAuth user: %w", err)
	}

	return &user, nil
}

func (gdb *GormDB) GetUserByOAuthID(provider, oauthID string) (*models.User, error) {
	var user models.User
	err := gdb.db.Where("o_auth_provider = ? AND o_auth_id = ?", provider, oauthID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (gdb *GormDB) LinkOAuthToExistingUser(userID, provider, oauthID string, userInfo *models.OAuthUserInfo) error {
	updates := map[string]interface{}{
		"o_auth_provider": provider,
		"o_auth_id":       oauthID,
		"o_auth_email":    userInfo.Email,
		"email_verified":  userInfo.EmailVerified,
	}

	return gdb.db.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error
}

func (gdb *GormDB) SyncUserGroups(userID, provider string, groups []models.OAuthGroupInfo) error {
	tx := gdb.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := gdb.syncUserGroupsInTx(tx, userID, provider, groups); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}

func (gdb *GormDB) syncUserGroupsInTx(tx *gorm.DB, userID, provider string, groups []models.OAuthGroupInfo) error {
	if err := tx.Where("user_id = ? AND provider = ?", userID, provider).Delete(&models.UserGroup{}).Error; err != nil {
		return fmt.Errorf("failed to delete existing groups: %w", err)
	}

	for _, group := range groups {
		permissions, _ := json.Marshal(group.Permissions)
		userGroup := &models.UserGroup{
			UserID:      userID,
			Provider:    provider,
			GroupName:   group.Name,
			GroupID:     group.ID,
			GroupType:   group.Type,
			Permissions: models.JSONB(permissions),
		}

		if err := tx.Create(userGroup).Error; err != nil {
			return fmt.Errorf("failed to create group %s: %w", group.Name, err)
		}
	}

	log.Printf("✅ Synced %d groups for user %s (provider: %s)", len(groups), userID, provider)
	return nil
}

func (gdb *GormDB) GetUserGroups(userID string) ([]models.UserGroup, error) {
	var groups []models.UserGroup
	err := gdb.db.Where("user_id = ?", userID).
		Order("provider, group_name").
		Find(&groups).Error
	return groups, err
}

func (gdb *GormDB) GetUserGroupsByProvider(userID, provider string) ([]models.UserGroup, error) {
	var groups []models.UserGroup
	err := gdb.db.Where("user_id = ? AND provider = ?", userID, provider).
		Order("group_name").
		Find(&groups).Error
	return groups, err
}

func (gdb *GormDB) GetUsersByGroup(provider, groupName string) ([]models.User, error) {
	var users []models.User
	err := gdb.db.Joins("JOIN user_groups ON user_groups.user_id = users.id").
		Where("user_groups.provider = ? AND user_groups.group_name = ?", provider, groupName).
		Find(&users).Error
	return users, err
}

func (gdb *GormDB) HasGroupPermission(userID, permission string) (bool, error) {
	var count int64
	err := gdb.db.Model(&models.UserGroup{}).
		Where("user_id = ? AND JSON_EXTRACT(permissions, '$.?') IS NOT NULL", userID, permission).
		Count(&count).Error
	return count > 0, err
}

func (gdb *GormDB) StoreOAuthToken(userID, provider string, accessToken, refreshToken, tokenType string, expiresAt *time.Time, scopes []string) error {
	gdb.db.Where("user_id = ? AND provider = ?", userID, provider).Delete(&models.OAuthToken{})

	scopesJSON, _ := json.Marshal(scopes)
	token := &models.OAuthToken{
		UserID:       userID,
		Provider:     provider,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		ExpiresAt:    expiresAt,
		Scopes:       string(scopesJSON),
	}

	return gdb.db.Create(token).Error
}

func (gdb *GormDB) GetOAuthToken(userID, provider string) (*models.OAuthToken, error) {
	var token models.OAuthToken
	err := gdb.db.Where("user_id = ? AND provider = ?", userID, provider).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (gdb *GormDB) RefreshOAuthToken(userID, provider, accessToken, refreshToken string, expiresAt *time.Time) error {
	updates := map[string]interface{}{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_at":    expiresAt,
		"updated_at":    time.Now(),
	}

	return gdb.db.Model(&models.OAuthToken{}).
		Where("user_id = ? AND provider = ?", userID, provider).
		Updates(updates).Error
}

func (gdb *GormDB) DeleteOAuthToken(userID, provider string) error {
	return gdb.db.Where("user_id = ? AND provider = ?", userID, provider).Delete(&models.OAuthToken{}).Error
}

func (gdb *GormDB) CleanupExpiredOAuthTokens() (int64, error) {
	result := gdb.db.Where("expires_at IS NOT NULL AND expires_at < ?", time.Now()).Delete(&models.OAuthToken{})
	return result.RowsAffected, result.Error
}

func generateSecureID(length int) string {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("oauth_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func (gdb *GormDB) CreateOAuthState(provider, state, sessionID string, expiresAt time.Time) error {
	oauthState := &models.OAuthState{
		ID:        generateSecureID(32),
		Provider:  provider,
		State:     state,
		SessionID: sessionID,
		ExpiresAt: expiresAt,
	}

	return gdb.db.Create(oauthState).Error
}

func (gdb *GormDB) ValidateAndDeleteOAuthState(provider, state string) (*models.OAuthState, error) {
	var oauthState models.OAuthState

	tx := gdb.db.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	err := tx.Where("provider = ? AND state = ? AND expires_at > ?", provider, state, time.Now()).
		First(&oauthState).Error
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("invalid or expired OAuth state: %w", err)
	}

	if err := tx.Delete(&oauthState).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to delete OAuth state: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	return &oauthState, nil
}

func (gdb *GormDB) CleanupExpiredOAuthStates() (int64, error) {
	result := gdb.db.Where("expires_at < ?", time.Now()).Delete(&models.OAuthState{})
	return result.RowsAffected, result.Error
}

func (gdb *GormDB) CreateOAuthSession(provider, state, redirectURI string, scopes []string, expiresAt time.Time) (*models.OAuthSession, error) {
	scopesJSON, _ := json.Marshal(scopes)
	session := &models.OAuthSession{
		Provider:    provider,
		State:       state,
		RedirectURI: redirectURI,
		Scopes:      string(scopesJSON),
		ExpiresAt:   expiresAt,
	}

	if err := gdb.db.Create(session).Error; err != nil {
		return nil, err
	}

	return session, nil
}

func (gdb *GormDB) GetOAuthSession(state string) (*models.OAuthSession, error) {
	var session models.OAuthSession
	err := gdb.db.Where("state = ? AND expires_at > ?", state, time.Now()).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (gdb *GormDB) UpdateOAuthSession(sessionID, userID string) error {
	return gdb.db.Model(&models.OAuthSession{}).
		Where("id = ?", sessionID).
		Update("user_id", userID).Error
}

func (gdb *GormDB) CleanupExpiredOAuthSessions() (int64, error) {
	result := gdb.db.Where("expires_at < ?", time.Now()).Delete(&models.OAuthSession{})
	return result.RowsAffected, result.Error
}

func (gdb *GormDB) StoreGroupCache(userID, provider string, groups []models.OAuthGroupInfo, expiresAt time.Time) error {
	gdb.db.Where("user_id = ? AND provider = ?", userID, provider).Delete(&models.OAuthGroupCache{})

	groupsJSON, err := json.Marshal(groups)
	if err != nil {
		return fmt.Errorf("failed to marshal groups: %w", err)
	}

	cache := &models.OAuthGroupCache{
		UserID:    userID,
		Provider:  provider,
		Groups:    models.JSONB(groupsJSON),
		ExpiresAt: expiresAt,
	}

	return gdb.db.Create(cache).Error
}

func (gdb *GormDB) GetGroupCache(userID, provider string) (*models.OAuthGroupCache, error) {
	var cache models.OAuthGroupCache
	err := gdb.db.Where("user_id = ? AND provider = ? AND expires_at > ?", userID, provider, time.Now()).
		First(&cache).Error
	if err != nil {
		return nil, err
	}
	return &cache, nil
}

func (gdb *GormDB) CleanupExpiredGroupCache() (int64, error) {
	result := gdb.db.Where("expires_at < ?", time.Now()).Delete(&models.OAuthGroupCache{})
	return result.RowsAffected, result.Error
}

func (gdb *GormDB) LogOAuthActivity(userID *string, provider, action string, success bool, errorMsg, ipAddress, userAgent string, metadata map[string]interface{}) error {
	metadataJSON, _ := json.Marshal(metadata)

	auditLog := &models.OAuthAuditLog{
		UserID:    userID,
		Provider:  provider,
		Action:    action,
		Success:   success,
		Error:     errorMsg,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Metadata:  models.JSONB(metadataJSON),
	}

	return gdb.db.Create(auditLog).Error
}

func (gdb *GormDB) GetOAuthAuditLogs(limit, offset int, provider string, userID *string) ([]models.OAuthAuditLog, error) {
	var logs []models.OAuthAuditLog

	query := gdb.db.Model(&models.OAuthAuditLog{}).Order("created_at DESC")

	if provider != "" {
		query = query.Where("provider = ?", provider)
	}

	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	err := query.Find(&logs).Error
	return logs, err
}

func (gdb *GormDB) GetOAuthProviderStats() ([]models.OAuthProviderStats, error) {
	var stats []models.OAuthProviderStats

	rows, err := gdb.db.Raw(`
		SELECT 
			o_auth_provider as provider,
			COUNT(*) as total_users,
			COUNT(CASE WHEN last_login > ? THEN 1 END) as active_users
		FROM users 
		WHERE o_auth_provider IS NOT NULL 
		GROUP BY o_auth_provider
	`, time.Now().AddDate(0, -1, 0)).Rows()

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var stat models.OAuthProviderStats
		if err := rows.Scan(&stat.Provider, &stat.TotalUsers, &stat.ActiveUsers); err != nil {
			continue
		}

		gdb.db.Raw("SELECT COUNT(*) FROM user_groups WHERE provider = ?", stat.Provider).Row().Scan(&stat.TotalGroups)

		gdb.db.Raw("SELECT AVG(group_count) FROM (SELECT COUNT(*) as group_count FROM user_groups WHERE provider = ? GROUP BY user_id) as subq", stat.Provider).Row().Scan(&stat.AverageGroups)

		stats = append(stats, stat)
	}

	return stats, nil
}

func generateUsernameFromEmail(email string) string {
	if email == "" {
		return fmt.Sprintf("user_%d", time.Now().Unix())
	}

	parts := []rune(email)
	var username []rune
	for _, r := range parts {
		if r == '@' {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			username = append(username, r)
		}
	}

	result := string(username)
	if result == "" {
		return fmt.Sprintf("user_%d", time.Now().Unix())
	}

	return result
}

func (gdb *GormDB) ensureUniqueUsername(user *models.User) error {
	originalUsername := user.Username
	counter := 1

	for {
		var count int64
		err := gdb.db.Model(&models.User{}).Where("username = ?", user.Username).Count(&count).Error
		if err != nil {
			return err
		}

		if count == 0 {
			break
		}

		user.Username = fmt.Sprintf("%s_%d", originalUsername, counter)
		counter++

		if counter > 1000 {
			return fmt.Errorf("unable to generate unique username for: %s", originalUsername)
		}
	}

	return nil
}
