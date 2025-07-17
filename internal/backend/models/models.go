package models

import (
	"math/rand"
	"time"

	"gorm.io/gorm"
)

// User represents a user in the system
type User struct {
	ID           string     `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Username     string     `gorm:"uniqueIndex;not null;size:100" json:"username"`
	Email        string     `gorm:"size:255" json:"email"`
	PasswordHash string     `gorm:"not null;size:255" json:"-"` // Never serialize
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLogin    *time.Time `json:"last_login,omitempty"`

	// Relations
	Sessions        []Session        `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Comments        []Comment        `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Acknowledgments []Acknowledgment `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

// BeforeCreate generates a UUID for new users
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = generateID()
	}
	return nil
}

// Session represents a user session
type Session struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	UserID    string    `gorm:"not null;size:32;index" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`

	// Relations
	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// Comment represents a comment on an alert
type Comment struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	AlertKey  string    `gorm:"not null;size:500;index" json:"alert_key"`
	UserID    string    `gorm:"not null;size:32" json:"user_id"`
	Content   string    `gorm:"not null;type:text" json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Relations
	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// BeforeCreate generates a UUID for new comments
func (c *Comment) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = generateID()
	}
	return nil
}

// Acknowledgment represents an alert acknowledgment
type Acknowledgment struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	AlertKey  string    `gorm:"not null;size:500;index" json:"alert_key"`
	UserID    string    `gorm:"not null;size:32" json:"user_id"`
	Reason    string    `gorm:"not null;type:text" json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Relations
	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// BeforeCreate generates a UUID for new acknowledgments
func (a *Acknowledgment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = generateID()
	}
	return nil
}

// Table names (optional - GORM will pluralize automatically)
func (User) TableName() string           { return "users" }
func (Session) TableName() string        { return "sessions" }
func (Comment) TableName() string        { return "comments" }
func (Acknowledgment) TableName() string { return "acknowledgments" }

// CommentWithUser is a view model that includes user information
type CommentWithUser struct {
	Comment
	Username string `json:"username"`
}

// AcknowledgmentWithUser is a view model that includes user information
type AcknowledgmentWithUser struct {
	Acknowledgment
	Username string `json:"username"`
}

// ID generation utility
func generateID() string {
	// Simple implementation - you might want to use UUID library
	return generateRandomString(32)
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
