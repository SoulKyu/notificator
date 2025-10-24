package database

import (
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"notificator/config"
	"notificator/internal/backend/models"
	mainmodels "notificator/internal/models"
)

type GormDB struct {
	db     *gorm.DB
	dbType string // "sqlite" or "postgres"
}

func NewGormDB(dbType string, cfg config.DatabaseConfig) (*GormDB, error) {
	var db *gorm.DB
	var err error

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	switch dbType {
	case "sqlite":
		if cfg.SQLitePath == "" {
			cfg.SQLitePath = "./notificator.db"
		}

		dir := filepath.Dir(cfg.SQLitePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
		}
		log.Printf("📁 Ensured database directory exists: %s", dir)

		db, err = gorm.Open(sqlite.Open(cfg.SQLitePath), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
		}
		log.Printf("📊 Connected to SQLite: %s", cfg.SQLitePath)

	case "postgres":
		// Check for POSTGRES_URL environment variable first
		if postgresURL := os.Getenv("POSTGRES_URL"); postgresURL != "" {
			log.Printf("📊 Using POSTGRES_URL environment variable")
			db, err = gorm.Open(postgres.Open(postgresURL), gormConfig)
		} else {
			// Fall back to individual config values
			dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s",
				cfg.Host, cfg.User, cfg.Password, cfg.Name, cfg.Port, cfg.SSLMode)
			db, err = gorm.Open(postgres.Open(dsn), gormConfig)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
		}
		log.Printf("📊 Connected to PostgreSQL: %s@%s:%d/%s", cfg.User, cfg.Host, cfg.Port, cfg.Name)

	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return &GormDB{
		db:     db,
		dbType: dbType,
	}, nil
}

// GetDBType returns the database type ("sqlite" or "postgres")
func (gdb *GormDB) GetDBType() string {
	return gdb.dbType
}

// GetDB returns the underlying *gorm.DB instance
// Used by services that need direct database access (e.g., rule engine)
func (gdb *GormDB) GetDB() *gorm.DB {
	return gdb.db
}

// IsSQLite returns true if the database is SQLite
func (gdb *GormDB) IsSQLite() bool {
	return gdb.dbType == "sqlite"
}

// IsPostgreSQL returns true if the database is PostgreSQL
func (gdb *GormDB) IsPostgreSQL() bool {
	return gdb.dbType == "postgres"
}

func (gdb *GormDB) AutoMigrate() error {
	log.Println("🔄 Running database migrations...")

	// Run custom migrations first (for data type changes that AutoMigrate can't handle)
	if err := gdb.RunCustomMigrations(); err != nil {
		return fmt.Errorf("custom migrations failed: %w", err)
	}

	err := gdb.db.AutoMigrate(
		&models.User{},
		&models.Session{},
		&models.Comment{},
		&models.Acknowledgment{},
		&models.ResolvedAlert{},
		&mainmodels.UserColorPreference{},
		// Browser notifications
		&models.NotificationPreference{},
		// Hidden alerts tables
		&models.UserHiddenAlert{},
		&models.UserHiddenRule{},
		// Filter presets
		&models.FilterPreset{},
		// OAuth tables
		&models.UserGroup{},
		&models.OAuthToken{},
		&models.OAuthState{},
		&models.OAuthSession{},
		&models.OAuthAuditLog{},
		&models.OAuthGroupCache{},
		// Sentry integration
		&models.UserSentryConfig{},
		// Alert statistics
		&models.AlertStatistic{},
		&models.OnCallRule{},
		&models.StatisticsAggregate{},
	)

	if err != nil {
		return fmt.Errorf("auto migration failed: %w", err)
	}

	// Create PostgreSQL-specific indexes for better JSONB query performance
	if gdb.IsPostgreSQL() {
		if err := gdb.createPostgreSQLIndexes(); err != nil {
			log.Printf("⚠️  Warning: Failed to create PostgreSQL indexes: %v", err)
			// Don't fail migration if index creation fails - indexes are optional optimizations
		}
	}

	log.Println("✅ Database migrations completed")
	return nil
}

// createPostgreSQLIndexes creates PostgreSQL-specific indexes for optimal performance
func (gdb *GormDB) createPostgreSQLIndexes() error {
	log.Println("Creating PostgreSQL-specific indexes...")

	indexes := []string{
		// GIN index on alert_statistics.metadata for fast JSONB queries
		`CREATE INDEX IF NOT EXISTS idx_alert_statistics_metadata_gin
		 ON alert_statistics USING GIN (metadata)`,

		// GIN index specifically for labels queries (more targeted)
		`CREATE INDEX IF NOT EXISTS idx_alert_statistics_metadata_labels_gin
		 ON alert_statistics USING GIN ((metadata->'labels'))`,

		// GIN index on on_call_rules.rule_config for rule queries
		`CREATE INDEX IF NOT EXISTS idx_on_call_rules_config_gin
		 ON on_call_rules USING GIN (rule_config)`,

		// GIN index on statistics_aggregates.aggregated_data
		`CREATE INDEX IF NOT EXISTS idx_statistics_aggregates_data_gin
		 ON statistics_aggregates USING GIN (aggregated_data)`,
	}

	for _, indexSQL := range indexes {
		if err := gdb.db.Exec(indexSQL).Error; err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	log.Println("✅ PostgreSQL indexes created successfully")
	return nil
}

func (gdb *GormDB) CreateUser(username, email, passwordHash string) (*models.User, error) {
	user := &models.User{
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
	}

	if err := gdb.db.Create(user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

func (gdb *GormDB) GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	err := gdb.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (gdb *GormDB) GetUserByID(userID string) (*models.User, error) {
	var user models.User
	err := gdb.db.First(&user, "id = ?", userID).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (gdb *GormDB) UpdateLastLogin(userID string) error {
	now := time.Now()
	return gdb.db.Model(&models.User{}).Where("id = ?", userID).Update("last_login", &now).Error
}

func (gdb *GormDB) SearchUsers(query string, limit int) ([]models.User, error) {
	var users []models.User

	err := gdb.db.Where("LOWER(username) LIKE LOWER(?)", query+"%").
		Limit(limit).
		Order("username").
		Find(&users).Error

	if err != nil {
		return nil, fmt.Errorf("failed to search users: %w", err)
	}

	return users, nil
}

func (gdb *GormDB) CreateSession(userID, sessionID string, expiresAt time.Time) error {
	session := &models.Session{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}

	return gdb.db.Create(session).Error
}

func (gdb *GormDB) GetUserBySession(sessionID string) (*models.User, error) {
	var user models.User
	err := gdb.db.Joins("JOIN sessions ON sessions.user_id = users.id").
		Where("sessions.id = ? AND sessions.expires_at > ?", sessionID, time.Now()).
		First(&user).Error

	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (gdb *GormDB) DeleteSession(sessionID string) error {
	return gdb.db.Delete(&models.Session{}, "id = ?", sessionID).Error
}

func (gdb *GormDB) CleanupExpiredSessions() error {
	return gdb.db.Where("expires_at < ?", time.Now()).Delete(&models.Session{}).Error
}

func (gdb *GormDB) CreateComment(alertKey, userID, content string) (*models.CommentWithUser, error) {
	comment := &models.Comment{
		AlertKey: alertKey,
		UserID:   userID,
		Content:  content,
	}

	if err := gdb.db.Create(comment).Error; err != nil {
		return nil, fmt.Errorf("failed to create comment: %w", err)
	}

	return gdb.GetCommentWithUser(comment.ID)
}

func (gdb *GormDB) GetCommentWithUser(commentID string) (*models.CommentWithUser, error) {
	var result models.CommentWithUser
	err := gdb.db.Table("comments").
		Select("comments.*, users.username").
		Joins("JOIN users ON users.id = comments.user_id").
		Where("comments.id = ?", commentID).
		First(&result).Error

	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (gdb *GormDB) GetComments(alertKey string) ([]models.CommentWithUser, error) {
	var comments []models.CommentWithUser
	err := gdb.db.Table("comments").
		Select("comments.*, users.username").
		Joins("JOIN users ON users.id = comments.user_id").
		Where("comments.alert_key = ?", alertKey).
		Order("comments.created_at ASC").
		Find(&comments).Error

	return comments, err
}

func (gdb *GormDB) DeleteComment(commentID, userID string) error {
	result := gdb.db.Where("id = ? AND user_id = ?", commentID, userID).Delete(&models.Comment{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("comment not found or not authorized")
	}
	return nil
}

func (gdb *GormDB) CreateAcknowledgment(alertKey, userID, reason string) (*models.AcknowledgmentWithUser, error) {
	gdb.db.Where("alert_key = ? AND user_id = ?", alertKey, userID).Delete(&models.Acknowledgment{})

	ack := &models.Acknowledgment{
		AlertKey: alertKey,
		UserID:   userID,
		Reason:   reason,
	}

	if err := gdb.db.Create(ack).Error; err != nil {
		return nil, fmt.Errorf("failed to create acknowledgment: %w", err)
	}

	return gdb.GetAcknowledgmentWithUser(ack.ID)
}

func (gdb *GormDB) GetAcknowledgmentWithUser(ackID string) (*models.AcknowledgmentWithUser, error) {
	var result models.AcknowledgmentWithUser
	err := gdb.db.Table("acknowledgments").
		Select("acknowledgments.*, users.username").
		Joins("JOIN users ON users.id = acknowledgments.user_id").
		Where("acknowledgments.id = ?", ackID).
		First(&result).Error

	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (gdb *GormDB) GetAcknowledgments(alertKey string) ([]models.AcknowledgmentWithUser, error) {
	var acks []models.AcknowledgmentWithUser
	err := gdb.db.Table("acknowledgments").
		Select("acknowledgments.*, users.username").
		Joins("JOIN users ON users.id = acknowledgments.user_id").
		Where("acknowledgments.alert_key = ?", alertKey).
		Order("acknowledgments.created_at DESC").
		Find(&acks).Error

	return acks, err
}

func (gdb *GormDB) DeleteAcknowledgment(alertKey, userID string) error {
	result := gdb.db.Where("alert_key = ? AND user_id = ?", alertKey, userID).Delete(&models.Acknowledgment{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("acknowledgment not found")
	}
	return nil
}

func (gdb *GormDB) GetAllAcknowledgedAlerts() (map[string]models.AcknowledgmentWithUser, error) {
	var acks []models.AcknowledgmentWithUser

	err := gdb.db.Table("acknowledgments").
		Select("acknowledgments.*, users.username").
		Joins("JOIN users ON users.id = acknowledgments.user_id").
		Joins("JOIN (SELECT alert_key, MAX(created_at) as max_created FROM acknowledgments GROUP BY alert_key) latest ON acknowledgments.alert_key = latest.alert_key AND acknowledgments.created_at = latest.max_created").
		Find(&acks).Error

	if err != nil {
		return nil, err
	}

	result := make(map[string]models.AcknowledgmentWithUser)
	for _, ack := range acks {
		result[ack.AlertKey] = ack
	}

	return result, nil
}

func (gdb *GormDB) CreateResolvedAlert(fingerprint, source string, alertData, comments, acknowledgments []byte, ttlHours int) (*models.ResolvedAlert, error) {
	now := time.Now()
	resolvedAlert := &models.ResolvedAlert{
		Fingerprint:     fingerprint,
		AlertData:       models.JSONB(alertData),
		Comments:        models.JSONB(comments),
		Acknowledgments: models.JSONB(acknowledgments),
		ResolvedAt:      now,
		ExpiresAt:       now.Add(time.Duration(ttlHours) * time.Hour),
		Source:          source,
	}

	if err := gdb.db.Create(resolvedAlert).Error; err != nil {
		return nil, fmt.Errorf("failed to create resolved alert: %w", err)
	}

	return resolvedAlert, nil
}

func (gdb *GormDB) GetResolvedAlerts(limit, offset int) ([]models.ResolvedAlert, error) {
	var resolvedAlerts []models.ResolvedAlert

	query := gdb.db.Where("expires_at > ?", time.Now()).
		Order("resolved_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	err := query.Find(&resolvedAlerts).Error
	return resolvedAlerts, err
}

func (gdb *GormDB) GetResolvedAlert(fingerprint string) (*models.ResolvedAlert, error) {
	var resolvedAlert models.ResolvedAlert
	err := gdb.db.Where("fingerprint = ? AND expires_at > ?", fingerprint, time.Now()).
		First(&resolvedAlert).Error

	if err != nil {
		return nil, err
	}

	return &resolvedAlert, nil
}

func (gdb *GormDB) CleanupExpiredResolvedAlerts() (int64, error) {
	result := gdb.db.Where("expires_at < ?", time.Now()).Delete(&models.ResolvedAlert{})
	return result.RowsAffected, result.Error
}

func (gdb *GormDB) GetResolvedAlertsCount() (int64, error) {
	var count int64
	err := gdb.db.Model(&models.ResolvedAlert{}).
		Where("expires_at > ?", time.Now()).
		Count(&count).Error
	return count, err
}

func (gdb *GormDB) RemoveAllResolvedAlerts() (int64, error) {
	result := gdb.db.Delete(&models.ResolvedAlert{}, "1 = 1")
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (gdb *GormDB) SaveUserColorPreferences(userID string, preferences []mainmodels.UserColorPreference) error {
	tx := gdb.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&mainmodels.UserColorPreference{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete existing preferences: %w", err)
	}

	for _, pref := range preferences {
		pref.UserID = userID
		if err := tx.Create(&pref).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to create preference: %w", err)
		}
	}

	return tx.Commit().Error
}

func (gdb *GormDB) GetUserColorPreferences(userID string) ([]mainmodels.UserColorPreference, error) {
	var preferences []mainmodels.UserColorPreference
	err := gdb.db.Where("user_id = ?", userID).
		Order("priority DESC, created_at ASC").
		Find(&preferences).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get user color preferences: %w", err)
	}

	return preferences, nil
}

func (gdb *GormDB) DeleteUserColorPreference(userID, preferenceID string) error {
	result := gdb.db.Where("id = ? AND user_id = ?", preferenceID, userID).Delete(&mainmodels.UserColorPreference{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("color preference not found or not authorized")
	}
	return nil
}

// generateUUID generates a simple UUID for database records
func generateUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

// Hidden Alerts Methods

// CreateUserHiddenAlert creates a new hidden alert for a user
func (gdb *GormDB) CreateUserHiddenAlert(userID, fingerprint, alertName, instance, reason string) (*models.UserHiddenAlert, error) {
	hiddenAlert := &models.UserHiddenAlert{
		UserID:      userID,
		Fingerprint: fingerprint,
		AlertName:   alertName,
		Instance:    instance,
		Reason:      reason,
	}

	if err := gdb.db.Create(hiddenAlert).Error; err != nil {
		return nil, fmt.Errorf("failed to create hidden alert: %w", err)
	}
	
	return hiddenAlert, nil
}

// SaveHiddenAlert saves or updates a hidden alert for a user
func (gdb *GormDB) SaveHiddenAlert(userID, fingerprint, alertName, instance, reason string) error {
	hiddenAlert := &models.UserHiddenAlert{
		UserID:      userID,
		Fingerprint: fingerprint,
		AlertName:   alertName,
		Instance:    instance,
		Reason:      reason,
	}

	// Check if already exists
	var existing models.UserHiddenAlert
	err := gdb.db.Where("user_id = ? AND fingerprint = ?", userID, fingerprint).First(&existing).Error
	
	if err == gorm.ErrRecordNotFound {
		// Create new
		if err := gdb.db.Create(hiddenAlert).Error; err != nil {
			return fmt.Errorf("failed to create hidden alert: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to query existing hidden alert: %w", err)
	} else {
		// Update existing
		existing.Reason = reason
		existing.AlertName = alertName
		existing.Instance = instance
		if err := gdb.db.Save(&existing).Error; err != nil {
			return fmt.Errorf("failed to update hidden alert: %w", err)
		}
	}
	
	return nil
}

// RemoveHiddenAlert removes a hidden alert for a user
func (gdb *GormDB) RemoveHiddenAlert(userID, fingerprint string) error {
	result := gdb.db.Where("user_id = ? AND fingerprint = ?", userID, fingerprint).Delete(&models.UserHiddenAlert{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// RemoveUserHiddenAlert removes a hidden alert for a user (alias for consistency)
func (gdb *GormDB) RemoveUserHiddenAlert(userID, fingerprint string) error {
	return gdb.RemoveHiddenAlert(userID, fingerprint)
}

// GetUserHiddenAlerts gets all hidden alerts for a user
func (gdb *GormDB) GetUserHiddenAlerts(userID string) ([]models.UserHiddenAlert, error) {
	var hiddenAlerts []models.UserHiddenAlert
	err := gdb.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&hiddenAlerts).Error
	
	if err != nil {
		return nil, fmt.Errorf("failed to get user hidden alerts: %w", err)
	}
	
	return hiddenAlerts, nil
}

// SaveHiddenRule saves or updates a hidden rule for a user
func (gdb *GormDB) SaveHiddenRule(userID string, rule *models.UserHiddenRule) error {
	rule.UserID = userID
	
	if rule.ID == "" {
		// Create new
		if err := gdb.db.Create(rule).Error; err != nil {
			return fmt.Errorf("failed to create hidden rule: %w", err)
		}
	} else {
		// Update existing
		if err := gdb.db.Where("id = ? AND user_id = ?", rule.ID, userID).Updates(rule).Error; err != nil {
			return fmt.Errorf("failed to update hidden rule: %w", err)
		}
	}
	
	return nil
}

// SaveUserHiddenRule saves or updates a hidden rule for a user (alias for consistency)
func (gdb *GormDB) SaveUserHiddenRule(userID string, rule *models.UserHiddenRule) (*models.UserHiddenRule, error) {
	rule.UserID = userID
	
	if rule.ID == "" {
		// Create new
		if err := gdb.db.Create(rule).Error; err != nil {
			return nil, fmt.Errorf("failed to create hidden rule: %w", err)
		}
	} else {
		// Update existing
		if err := gdb.db.Where("id = ? AND user_id = ?", rule.ID, userID).Updates(rule).Error; err != nil {
			return nil, fmt.Errorf("failed to update hidden rule: %w", err)
		}
	}
	
	return rule, nil
}

// RemoveHiddenRule removes a hidden rule for a user
func (gdb *GormDB) RemoveHiddenRule(userID, ruleID string) error {
	result := gdb.db.Where("id = ? AND user_id = ?", ruleID, userID).Delete(&models.UserHiddenRule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("hidden rule not found or not authorized")
	}
	return nil
}

// RemoveUserHiddenRule removes a hidden rule for a user (alias for consistency)
func (gdb *GormDB) RemoveUserHiddenRule(userID, ruleID string) error {
	return gdb.RemoveHiddenRule(userID, ruleID)
}

// GetUserHiddenRules gets all hidden rules for a user
func (gdb *GormDB) GetUserHiddenRules(userID string) ([]models.UserHiddenRule, error) {
	var rules []models.UserHiddenRule
	err := gdb.db.Where("user_id = ? AND is_enabled = ?", userID, true).
		Order("priority DESC, created_at ASC").
		Find(&rules).Error
	
	if err != nil {
		return nil, fmt.Errorf("failed to get user hidden rules: %w", err)
	}
	
	return rules, nil
}

// ClearAllHiddenAlerts removes all hidden alerts for a user
func (gdb *GormDB) ClearAllHiddenAlerts(userID string) error {
	result := gdb.db.Where("user_id = ?", userID).Delete(&models.UserHiddenAlert{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// ClearUserHiddenAlerts removes all hidden alerts for a user (alias for consistency)
func (gdb *GormDB) ClearUserHiddenAlerts(userID string) (int64, error) {
	result := gdb.db.Where("user_id = ?", userID).Delete(&models.UserHiddenAlert{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// Filter Presets Methods

// CreateFilterPreset creates a new filter preset for a user
func (gdb *GormDB) CreateFilterPreset(preset *models.FilterPreset) (*models.FilterPreset, error) {
	if err := gdb.db.Create(preset).Error; err != nil {
		return nil, fmt.Errorf("failed to create filter preset: %w", err)
	}
	return preset, nil
}

// GetFilterPresets gets all filter presets for a user (private + shared)
func (gdb *GormDB) GetFilterPresets(userID string, includeShared bool) ([]models.FilterPreset, error) {
	var presets []models.FilterPreset

	query := gdb.db.Where("user_id = ?", userID)

	if includeShared {
		// Get user's own presets + shared presets from others
		query = gdb.db.Where("user_id = ? OR is_shared = ?", userID, true)
	}

	err := query.Order("is_default DESC, created_at DESC").Find(&presets).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get filter presets: %w", err)
	}

	return presets, nil
}

// GetFilterPresetByID gets a specific filter preset by ID
func (gdb *GormDB) GetFilterPresetByID(id string) (*models.FilterPreset, error) {
	var preset models.FilterPreset
	err := gdb.db.Where("id = ?", id).First(&preset).Error
	if err != nil {
		return nil, err
	}
	return &preset, nil
}

// UpdateFilterPreset updates an existing filter preset
func (gdb *GormDB) UpdateFilterPreset(preset *models.FilterPreset) error {
	if err := gdb.db.Save(preset).Error; err != nil {
		return fmt.Errorf("failed to update filter preset: %w", err)
	}
	return nil
}

// DeleteFilterPreset deletes a filter preset (with ownership check)
func (gdb *GormDB) DeleteFilterPreset(id, userID string) error {
	result := gdb.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.FilterPreset{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("filter preset not found or not authorized")
	}
	return nil
}

// SetDefaultFilterPreset sets a filter preset as default (and unsets others)
func (gdb *GormDB) SetDefaultFilterPreset(id, userID string) error {
	tx := gdb.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Unset all defaults for this user
	if err := tx.Model(&models.FilterPreset{}).
		Where("user_id = ?", userID).
		Update("is_default", false).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to unset existing defaults: %w", err)
	}

	// Set the new default (with ownership check)
	result := tx.Model(&models.FilterPreset{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("is_default", true)

	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		return fmt.Errorf("filter preset not found or not authorized")
	}

	return tx.Commit().Error
}

// GetDefaultFilterPreset gets the default filter preset for a user
func (gdb *GormDB) GetDefaultFilterPreset(userID string) (*models.FilterPreset, error) {
	var preset models.FilterPreset
	err := gdb.db.Where("user_id = ? AND is_default = ?", userID, true).First(&preset).Error
	if err != nil {
		return nil, err
	}
	return &preset, nil
}
