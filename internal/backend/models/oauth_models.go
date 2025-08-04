package models

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type OAuthUser struct {
	User
	OAuthProvider *string     `gorm:"size:50" json:"oauth_provider,omitempty"`
	OAuthID       *string     `gorm:"size:255;index" json:"oauth_id,omitempty"`
	OAuthEmail    *string     `gorm:"size:255" json:"oauth_email,omitempty"`
	EmailVerified bool        `gorm:"default:false" json:"email_verified"`
	Groups        []UserGroup `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"groups,omitempty"`
}

type UserGroup struct {
	ID          string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID      string    `gorm:"not null;size:32;index" json:"user_id"`
	Provider    string    `gorm:"not null;size:50;index" json:"provider"`
	GroupName   string    `gorm:"not null;size:255" json:"group_name"`
	GroupID     string    `gorm:"size:255" json:"group_id,omitempty"`
	GroupType   string    `gorm:"size:50" json:"group_type,omitempty"`
	Permissions JSONB     `gorm:"type:jsonb" json:"permissions,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (ug *UserGroup) BeforeCreate(tx *gorm.DB) error {
	if ug.ID == "" {
		ug.ID = GenerateID()
	}
	return nil
}

func (UserGroup) TableName() string { return "user_groups" }

type OAuthToken struct {
	ID           string     `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID       string     `gorm:"not null;size:32;index" json:"user_id"`
	Provider     string     `gorm:"not null;size:50" json:"provider"`
	AccessToken  string     `gorm:"not null;type:text" json:"-"`
	RefreshToken string     `gorm:"type:text" json:"-"`
	TokenType    string     `gorm:"size:50" json:"token_type"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Scopes       string     `gorm:"type:text" json:"scopes"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (ot *OAuthToken) BeforeCreate(tx *gorm.DB) error {
	if ot.ID == "" {
		ot.ID = GenerateID()
	}
	return nil
}

func (OAuthToken) TableName() string { return "oauth_tokens" }

func (ot *OAuthToken) IsExpired() bool {
	if ot.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*ot.ExpiresAt)
}

func (ot *OAuthToken) GetScopes() []string {
	if ot.Scopes == "" {
		return []string{}
	}
	var scopes []string
	if err := json.Unmarshal([]byte(ot.Scopes), &scopes); err != nil {
		return []string{ot.Scopes}
	}
	return scopes
}

func (ot *OAuthToken) SetScopes(scopes []string) error {
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	ot.Scopes = string(scopesJSON)
	return nil
}

type OAuthState struct {
	ID        string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	Provider  string    `gorm:"not null;size:50" json:"provider"`
	State     string    `gorm:"not null;size:255;uniqueIndex" json:"state"`
	SessionID string    `gorm:"size:64" json:"session_id,omitempty"`
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (OAuthState) TableName() string { return "oauth_states" }

func (os *OAuthState) IsExpired() bool {
	return time.Now().After(os.ExpiresAt)
}

type OAuthUserInfo struct {
	ID            string                 `json:"id"`
	Email         string                 `json:"email"`
	Name          string                 `json:"name"`
	Username      string                 `json:"username"`
	AvatarURL     string                 `json:"avatar_url,omitempty"`
	EmailVerified bool                   `json:"email_verified"`
	Groups        []OAuthGroupInfo       `json:"groups,omitempty"`
	CustomClaims  map[string]interface{} `json:"custom_claims,omitempty"`
	Provider      string                 `json:"provider"`
}

type OAuthGroupInfo struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Role        string                 `json:"role,omitempty"`
	Permissions map[string]interface{} `json:"permissions,omitempty"`
	AvatarURL   string                 `json:"avatar_url,omitempty"`
}

type GroupMembership struct {
	UserID     string                 `json:"user_id"`
	Provider   string                 `json:"provider"`
	Groups     []OAuthGroupInfo       `json:"groups"`
	SyncedAt   time.Time              `json:"synced_at"`
	CustomData map[string]interface{} `json:"custom_data,omitempty"`
}

type UserGroupWithDetails struct {
	UserGroup
	UserEmail    string `json:"user_email"`
	UserUsername string `json:"user_username"`
}

type OAuthProviderStats struct {
	Provider      string     `json:"provider"`
	TotalUsers    int64      `json:"total_users"`
	ActiveUsers   int64      `json:"active_users"`
	TotalGroups   int64      `json:"total_groups"`
	LastSync      *time.Time `json:"last_sync,omitempty"`
	ErrorRate     float64    `json:"error_rate"`
	AverageGroups float64    `json:"average_groups_per_user"`
}

type OAuthSession struct {
	ID                  string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	Provider            string    `gorm:"not null;size:50" json:"provider"`
	State               string    `gorm:"not null;size:255" json:"state"`
	CodeChallenge       string    `gorm:"size:255" json:"code_challenge,omitempty"`
	CodeChallengeMethod string    `gorm:"size:20" json:"code_challenge_method,omitempty"`
	RedirectURI         string    `gorm:"size:500" json:"redirect_uri"`
	Scopes              string    `gorm:"type:text" json:"scopes"`
	UserID              *string   `gorm:"size:32" json:"user_id,omitempty"`
	ExpiresAt           time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (OAuthSession) TableName() string { return "oauth_sessions" }

func (os *OAuthSession) BeforeCreate(tx *gorm.DB) error {
	if os.ID == "" {
		os.ID = GenerateID()
	}
	return nil
}

func (os *OAuthSession) IsExpired() bool {
	return time.Now().After(os.ExpiresAt)
}

func (os *OAuthSession) GetScopes() []string {
	if os.Scopes == "" {
		return []string{}
	}
	var scopes []string
	if err := json.Unmarshal([]byte(os.Scopes), &scopes); err != nil {
		return []string{os.Scopes}
	}
	return scopes
}

func (os *OAuthSession) SetScopes(scopes []string) error {
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	os.Scopes = string(scopesJSON)
	return nil
}

type OAuthAuditLog struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID    *string   `gorm:"size:32;index" json:"user_id,omitempty"`
	Provider  string    `gorm:"not null;size:50;index" json:"provider"`
	Action    string    `gorm:"not null;size:100" json:"action"`
	Success   bool      `gorm:"not null;index" json:"success"`
	Error     string    `gorm:"type:text" json:"error,omitempty"`
	IPAddress string    `gorm:"size:45" json:"ip_address,omitempty"`
	UserAgent string    `gorm:"type:text" json:"user_agent,omitempty"`
	Metadata  JSONB     `gorm:"type:jsonb" json:"metadata,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (oal *OAuthAuditLog) BeforeCreate(tx *gorm.DB) error {
	if oal.ID == "" {
		oal.ID = GenerateID()
	}
	return nil
}

func (OAuthAuditLog) TableName() string { return "oauth_audit_logs" }

type OAuthGroupCache struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	UserID    string    `gorm:"not null;size:32;index" json:"user_id"`
	Provider  string    `gorm:"not null;size:50" json:"provider"`
	Groups    JSONB     `gorm:"type:jsonb;not null" json:"groups"`
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (ogc *OAuthGroupCache) BeforeCreate(tx *gorm.DB) error {
	if ogc.ID == "" {
		ogc.ID = GenerateID()
	}
	return nil
}

func (OAuthGroupCache) TableName() string { return "oauth_group_cache" }

func (ogc *OAuthGroupCache) IsExpired() bool {
	return time.Now().After(ogc.ExpiresAt)
}

func (ogc *OAuthGroupCache) GetGroups() ([]OAuthGroupInfo, error) {
	var groups []OAuthGroupInfo
	if err := json.Unmarshal(ogc.Groups, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (ogc *OAuthGroupCache) SetGroups(groups []OAuthGroupInfo) error {
	groupsJSON, err := json.Marshal(groups)
	if err != nil {
		return err
	}
	ogc.Groups = JSONB(groupsJSON)
	return nil
}

type UserRole struct {
	UserID     string    `json:"user_id"`
	Provider   string    `json:"provider"`
	Role       string    `json:"role"`
	Source     string    `json:"source"`
	ComputedAt time.Time `json:"computed_at"`
}

func AddOAuthFieldsToUser() []string {
	return []string{
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS oauth_provider VARCHAR(50)",
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS oauth_id VARCHAR(255)",
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS oauth_email VARCHAR(255)",
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN DEFAULT false",
		"CREATE INDEX IF NOT EXISTS idx_users_oauth_id ON users(oauth_id)",
		"CREATE INDEX IF NOT EXISTS idx_users_oauth_provider ON users(oauth_provider)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oauth_provider_id ON users(oauth_provider, oauth_id) WHERE oauth_provider IS NOT NULL AND oauth_id IS NOT NULL",
	}
}

func GetOAuthTables() []interface{} {
	return []interface{}{
		&UserGroup{},
		&OAuthToken{},
		&OAuthState{},
		&OAuthSession{},
		&OAuthAuditLog{},
		&OAuthGroupCache{},
	}
}
