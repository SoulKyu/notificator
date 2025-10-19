package database

import (
	"fmt"
	"log"

	"notificator/internal/backend/models"
)

// GetUserNotificationPreference retrieves the notification preferences for a user
func (gdb *GormDB) GetUserNotificationPreference(userID string) (*models.NotificationPreference, error) {
	var pref models.NotificationPreference

	result := gdb.db.Where("user_id = ?", userID).First(&pref)
	if result.Error != nil {
		// If not found, return default preferences (but don't save them yet)
		if result.Error.Error() == "record not found" {
			log.Printf("No notification preference found for user %s, returning defaults", userID)
			return models.DefaultNotificationPreference(userID), nil
		}
		return nil, fmt.Errorf("failed to get notification preference: %w", result.Error)
	}

	return &pref, nil
}

// SaveUserNotificationPreference saves or updates the notification preferences for a user
func (gdb *GormDB) SaveUserNotificationPreference(pref *models.NotificationPreference) error {
	// Check if preference exists
	var existing models.NotificationPreference
	result := gdb.db.Where("user_id = ?", pref.UserID).First(&existing)

	if result.Error != nil {
		// If not found, create new
		if result.Error.Error() == "record not found" {
			log.Printf("Creating new notification preference for user %s", pref.UserID)
			if err := gdb.db.Create(pref).Error; err != nil {
				return fmt.Errorf("failed to create notification preference: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to check existing notification preference: %w", result.Error)
	}

	// Update existing preference
	pref.ID = existing.ID // Keep the same ID
	pref.CreatedAt = existing.CreatedAt // Preserve creation time

	if err := gdb.db.Save(pref).Error; err != nil {
		return fmt.Errorf("failed to update notification preference: %w", err)
	}

	log.Printf("Updated notification preference for user %s", pref.UserID)
	return nil
}

// DeleteUserNotificationPreference deletes the notification preferences for a user
func (gdb *GormDB) DeleteUserNotificationPreference(userID string) error {
	result := gdb.db.Where("user_id = ?", userID).Delete(&models.NotificationPreference{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete notification preference: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		log.Printf("No notification preference found to delete for user %s", userID)
		return nil
	}

	log.Printf("Deleted notification preference for user %s", userID)
	return nil
}
