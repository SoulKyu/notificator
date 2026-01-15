package models

import (
	"time"

	"gorm.io/gorm"
)

// StatisticsView represents a saved filter configuration for the statistics dashboard
type StatisticsView struct {
	ID          string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID      string    `gorm:"not null;size:32;index" json:"user_id"`
	Name        string    `gorm:"not null;size:255" json:"name"`
	Description string    `gorm:"type:text" json:"description,omitempty"`
	IsShared    bool      `gorm:"default:false;index" json:"is_shared"`
	IsDefault   bool      `gorm:"default:false" json:"is_default"`
	ViewData    JSONB     `gorm:"type:jsonb;not null" json:"view_data"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (sv *StatisticsView) BeforeCreate(tx *gorm.DB) error {
	if sv.ID == "" {
		sv.ID = GenerateID()
	}
	return nil
}

func (StatisticsView) TableName() string { return "statistics_views" }

// UserDefaultStatisticsView represents the default statistics view for a user
// This allows users to set any view (including shared ones) as their default
type UserDefaultStatisticsView struct {
	UserID            string    `gorm:"primaryKey;type:varchar(32);index" json:"user_id"`
	StatisticsViewID  string    `gorm:"not null;type:varchar(32);index" json:"statistics_view_id"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	User           User           `gorm:"foreignKey:UserID" json:"user,omitempty"`
	StatisticsView StatisticsView `gorm:"foreignKey:StatisticsViewID" json:"statistics_view,omitempty"`
}

func (UserDefaultStatisticsView) TableName() string { return "user_default_statistics_views" }

// StatisticsViewData contains the filter state for the statistics dashboard
type StatisticsViewData struct {
	DateRangeType     string `json:"date_range_type,omitempty"`
	StartDate         string `json:"start_date,omitempty"`
	EndDate           string `json:"end_date,omitempty"`
	FilterByTimeOfDay bool   `json:"filter_by_time_of_day"`
	TimeOfDayStart    string `json:"time_of_day_start,omitempty"`
	TimeOfDayEnd      string `json:"time_of_day_end,omitempty"`
	UseOnCallPeriod   bool   `json:"use_on_call_period"`
	IncludeWeekends   bool   `json:"include_weekends"`
	GroupBy           string `json:"group_by,omitempty"`
	PeriodType        string `json:"period_type,omitempty"`
	ApplyRules        bool   `json:"apply_rules"`
	Limit             int    `json:"limit,omitempty"`
}
