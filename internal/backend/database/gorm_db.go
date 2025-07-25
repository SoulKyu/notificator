package database

import (
	"fmt"
	"log"
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
	db *gorm.DB
}

// NewGormDB creates a new GORM database connection
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
		db, err = gorm.Open(sqlite.Open(cfg.SQLitePath), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
		}
		log.Printf("📊 Connected to SQLite: %s", cfg.SQLitePath)

	case "postgres":
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s",
			cfg.Host, cfg.User, cfg.Password, cfg.Name, cfg.Port, cfg.SSLMode)

		db, err = gorm.Open(postgres.Open(dsn), gormConfig)
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

	return &GormDB{db: db}, nil
}

// AutoMigrate runs database migrations
func (gdb *GormDB) AutoMigrate() error {
	log.Println("🔄 Running database migrations...")

	err := gdb.db.AutoMigrate(
		&models.User{},
		&models.Session{},
		&models.Comment{},
		&models.Acknowledgment{},
		&models.ResolvedAlert{},
		&mainmodels.UserColorPreference{},
	)

	if err != nil {
		return fmt.Errorf("auto migration failed: %w", err)
	}

	log.Println("✅ Database migrations completed")
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

// GetAllAcknowledgedAlerts returns a map of alert_key to their latest acknowledgment
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

// RemoveAllResolvedAlerts removes all resolved alerts from the database
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
