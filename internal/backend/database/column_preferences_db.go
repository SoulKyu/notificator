package database

import (
	"fmt"
	"log"

	"notificator/internal/backend/models"
)

// GetUserColumnPreferences retrieves the column preferences for a user
func (gdb *GormDB) GetUserColumnPreferences(userID string) (*models.UserColumnPreference, error) {
	var pref models.UserColumnPreference

	result := gdb.db.Where("user_id = ?", userID).First(&pref)
	if result.Error != nil {
		// If not found, return nil (no error) to allow graceful fallback to defaults
		if result.Error.Error() == "record not found" {
			log.Printf("No column preferences found for user %s", userID)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get column preferences: %w", result.Error)
	}

	return &pref, nil
}

// SaveUserColumnPreferences saves or updates the column preferences for a user
func (gdb *GormDB) SaveUserColumnPreferences(pref *models.UserColumnPreference) error {
	// Check if preference exists
	var existing models.UserColumnPreference
	result := gdb.db.Where("user_id = ?", pref.UserID).First(&existing)

	if result.Error != nil {
		// If not found, create new
		if result.Error.Error() == "record not found" {
			log.Printf("Creating new column preferences for user %s", pref.UserID)
			if err := gdb.db.Create(pref).Error; err != nil {
				return fmt.Errorf("failed to create column preferences: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to check existing column preferences: %w", result.Error)
	}

	// Update existing preference (UserID is the primary key)
	pref.CreatedAt = existing.CreatedAt // Preserve creation time

	if err := gdb.db.Save(pref).Error; err != nil {
		return fmt.Errorf("failed to update column preferences: %w", err)
	}

	log.Printf("Updated column preferences for user %s", pref.UserID)
	return nil
}

// DeleteUserColumnPreferences deletes the column preferences for a user
func (gdb *GormDB) DeleteUserColumnPreferences(userID string) error {
	result := gdb.db.Where("user_id = ?", userID).Delete(&models.UserColumnPreference{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete column preferences: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		log.Printf("No column preferences found to delete for user %s", userID)
		return nil
	}

	log.Printf("Deleted column preferences for user %s", userID)
	return nil
}
