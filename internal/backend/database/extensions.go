package database

import (
	"notificator/internal/backend/models"
	"time"
)

// GetStatistics returns database statistics
func (gdb *GormDB) GetStatistics() (map[string]int64, error) {
	stats := make(map[string]int64)

	// Get counts
	var usersCount, commentsCount, acksCount, activeSessions int64

	if err := gdb.db.Model(&models.User{}).Count(&usersCount).Error; err != nil {
		return nil, err
	}

	if err := gdb.db.Model(&models.Comment{}).Count(&commentsCount).Error; err != nil {
		return nil, err
	}

	if err := gdb.db.Model(&models.Acknowledgment{}).Count(&acksCount).Error; err != nil {
		return nil, err
	}

	if err := gdb.db.Model(&models.Session{}).Where("expires_at > ?", time.Now()).Count(&activeSessions).Error; err != nil {
		return nil, err
	}

	stats["users"] = usersCount
	stats["comments"] = commentsCount
	stats["acknowledgments"] = acksCount
	stats["active_sessions"] = activeSessions

	return stats, nil
}

// GetStats returns detailed server statistics (alternative method name)
func (gdb *GormDB) GetStats() (map[string]int64, error) {
	return gdb.GetStatistics()
}

// HealthCheck performs a simple database health check
func (gdb *GormDB) HealthCheck() error {
	// Simple health check - try to query users table
	var count int64
	return gdb.db.Model(&models.User{}).Count(&count).Error
}

// GetUserCommentCount returns the number of comments for a user
func (gdb *GormDB) GetUserCommentCount(userID string) (int64, error) {
	var count int64
	err := gdb.db.Model(&models.Comment{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// GetRecentComments returns recent comments with user information
func (gdb *GormDB) GetRecentComments(limit int) ([]models.CommentWithUser, error) {
	var comments []models.CommentWithUser
	err := gdb.db.Table("comments").
		Select("comments.*, users.username").
		Joins("JOIN users ON users.id = comments.user_id").
		Order("comments.created_at DESC").
		Limit(limit).
		Find(&comments).Error

	return comments, err
}

// GetActiveUsers returns users who have logged in recently
func (gdb *GormDB) GetActiveUsers() ([]models.User, error) {
	var users []models.User
	err := gdb.db.Where("last_login > ?", time.Now().Add(-24*time.Hour)).Find(&users).Error
	return users, err
}

// Close closes the database connection
func (gdb *GormDB) Close() error {
	sqlDB, err := gdb.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
