package models

import (
	"time"

	"gorm.io/gorm"
)

// UserHiddenAlert represents a specific alert hidden by a user
type UserHiddenAlert struct {
	ID          string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID      string    `gorm:"type:varchar(32);not null;index:idx_user_hidden,priority:1" json:"user_id"`
	Fingerprint string    `gorm:"type:varchar(255);not null;index:idx_user_hidden,priority:2" json:"fingerprint"`
	AlertName   string    `gorm:"type:varchar(255)" json:"alert_name"`
	Instance    string    `gorm:"type:varchar(255)" json:"instance"`
	Reason      string    `gorm:"type:text" json:"reason"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Relations
	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

// UserHiddenRule represents a label-based rule for hiding alerts
type UserHiddenRule struct {
	ID          string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID      string    `gorm:"type:varchar(32);not null;index" json:"user_id,userId"`
	Name        string    `gorm:"type:varchar(255)" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	LabelKey    string    `gorm:"type:varchar(255);not null" json:"labelKey,label_key"`
	LabelValue  string    `gorm:"type:varchar(255)" json:"labelValue,label_value"`
	IsRegex     bool      `gorm:"default:false" json:"isRegex,is_regex"`
	IsEnabled   bool      `gorm:"default:true" json:"enabled,is_enabled"`
	Priority    int       `gorm:"default:0" json:"priority"` // Higher priority rules are evaluated first
	CreatedAt   time.Time `json:"createdAt,created_at"`
	UpdatedAt   time.Time `json:"updatedAt,updated_at"`

	// Relations
	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

func (u *UserHiddenAlert) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = GenerateID()
	}
	return nil
}

func (u *UserHiddenRule) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = GenerateID()
	}
	return nil
}

// TableName specifies the table name for UserHiddenAlert
func (UserHiddenAlert) TableName() string {
	return "user_hidden_alerts"
}

// TableName specifies the table name for UserHiddenRule
func (UserHiddenRule) TableName() string {
	return "user_hidden_rules"
}