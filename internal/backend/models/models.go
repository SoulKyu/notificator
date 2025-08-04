package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID           string     `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Username     string     `gorm:"uniqueIndex;not null;size:100" json:"username"`
	Email        string     `gorm:"size:255" json:"email"`
	PasswordHash string     `gorm:"size:255" json:"-"` // Never serialize
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLogin    *time.Time `json:"last_login,omitempty"`

	// OAuth fields
	OAuthProvider *string `gorm:"size:50" json:"oauth_provider,omitempty"`
	OAuthID       *string `gorm:"size:255;index" json:"oauth_id,omitempty"`
	OAuthEmail    *string `gorm:"size:255" json:"oauth_email,omitempty"`
	EmailVerified bool    `gorm:"default:false" json:"email_verified"`

	Sessions        []Session        `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Comments        []Comment        `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Acknowledgments []Acknowledgment `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = GenerateID()
	}
	return nil
}

func (u *User) IsOAuthUser() bool {
	return u.OAuthProvider != nil && u.OAuthID != nil
}

func (u *User) HasPassword() bool {
	return u.PasswordHash != ""
}

func (u *User) CanLogin() bool {
	return u.HasPassword() || u.IsOAuthUser()
}

func (u *User) GetAuthMethod() string {
	if u.IsOAuthUser() {
		return "oauth:" + *u.OAuthProvider
	}
	if u.HasPassword() {
		return "password"
	}
	return "none"
}

type Session struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	UserID    string    `gorm:"not null;size:32;index" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

type Comment struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	AlertKey  string    `gorm:"not null;size:500;index" json:"alert_key"`
	UserID    string    `gorm:"not null;size:32" json:"user_id"`
	Content   string    `gorm:"not null;type:text" json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (c *Comment) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = GenerateID()
	}
	return nil
}

type Acknowledgment struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	AlertKey  string    `gorm:"not null;size:500;index" json:"alert_key"`
	UserID    string    `gorm:"not null;size:32" json:"user_id"`
	Reason    string    `gorm:"not null;type:text" json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (a *Acknowledgment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = GenerateID()
	}
	return nil
}

func (User) TableName() string           { return "users" }
func (Session) TableName() string        { return "sessions" }
func (Comment) TableName() string        { return "comments" }
func (Acknowledgment) TableName() string { return "acknowledgments" }

type CommentWithUser struct {
	Comment
	Username string `json:"username"`
}

type AcknowledgmentWithUser struct {
	Acknowledgment
	Username string `json:"username"`
}

type JSONB json.RawMessage

func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*j = JSONB(v)
	case string:
		*j = JSONB(v)
	default:
		return fmt.Errorf("cannot scan %T into JSONB", value)
	}
	return nil
}

type ResolvedAlert struct {
	ID          string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Fingerprint string    `gorm:"not null;size:500;index" json:"fingerprint"`
	
	AlertData   JSONB     `gorm:"type:jsonb;not null" json:"alert_data"`
	
	// Preserved relationships
	Comments        JSONB     `gorm:"type:jsonb" json:"comments,omitempty"`
	Acknowledgments JSONB     `gorm:"type:jsonb" json:"acknowledgments,omitempty"`
	
	ResolvedAt  time.Time `gorm:"not null;index" json:"resolved_at"`
	ExpiresAt   time.Time `gorm:"not null;index" json:"expires_at"`
	Source      string    `gorm:"not null;size:255" json:"source"`
	
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ra *ResolvedAlert) BeforeCreate(tx *gorm.DB) error {
	if ra.ID == "" {
		ra.ID = GenerateID()
	}
	return nil
}

func (ResolvedAlert) TableName() string { return "resolved_alerts" }

func GenerateID() string {
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
