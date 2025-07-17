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
)

type GormDB struct {
	db *gorm.DB
}

// NewGormDB creates a new GORM database connection
func NewGormDB(dbType string, cfg config.DatabaseConfig) (*GormDB, error) {
	var db *gorm.DB
	var err error

	// Configure GORM logger
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

	// Configure connection pool
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
	)

	if err != nil {
		return fmt.Errorf("auto migration failed: %w", err)
	}

	log.Println("✅ Database migrations completed")
	return nil
}

// User operations
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
	
	// Search by username prefix (case-insensitive)
	err := gdb.db.Where("LOWER(username) LIKE LOWER(?)", query+"%").
		Limit(limit).
		Order("username").
		Find(&users).Error
	
	if err != nil {
		return nil, fmt.Errorf("failed to search users: %w", err)
	}
	
	return users, nil
}

// Session operations
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

// Comment operations
func (gdb *GormDB) CreateComment(alertKey, userID, content string) (*models.CommentWithUser, error) {
	comment := &models.Comment{
		AlertKey: alertKey,
		UserID:   userID,
		Content:  content,
	}

	if err := gdb.db.Create(comment).Error; err != nil {
		return nil, fmt.Errorf("failed to create comment: %w", err)
	}

	// Get comment with user info
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

// Acknowledgment operations
func (gdb *GormDB) CreateAcknowledgment(alertKey, userID, reason string) (*models.AcknowledgmentWithUser, error) {
	// Delete existing acknowledgment first (upsert behavior)
	gdb.db.Where("alert_key = ? AND user_id = ?", alertKey, userID).Delete(&models.Acknowledgment{})

	ack := &models.Acknowledgment{
		AlertKey: alertKey,
		UserID:   userID,
		Reason:   reason,
	}

	if err := gdb.db.Create(ack).Error; err != nil {
		return nil, fmt.Errorf("failed to create acknowledgment: %w", err)
	}

	// Get acknowledgment with user info
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
