package models

import (
	"gorm.io/gorm"
)

// UserSentryConfig stores user-specific Sentry configuration
type UserSentryConfig struct {
	gorm.Model
	UserID        string `gorm:"type:varchar(32);uniqueIndex;not null" json:"user_id"`
	PersonalToken string `gorm:"type:text" json:"-"` // Encrypted, never sent to client
	SentryBaseURL string `gorm:"type:varchar(255);default:'https://sentry.io'" json:"sentry_base_url"`
	
	// Relationship
	User User `gorm:"foreignKey:UserID" json:"-"`
}