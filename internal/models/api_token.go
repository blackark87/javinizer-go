package models

import "time"

// ApiToken represents a persisted API authentication token.
type ApiToken struct {
	ID          string     `json:"id" gorm:"primaryKey"`
	Name        string     `json:"name"`
	TokenHash   string     `json:"-" gorm:"uniqueIndex;not null"`
	TokenPrefix string     `json:"token_prefix" gorm:"index;not null"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at" gorm:"index"`
}

// TableName implements gorm.Tabler, mapping ApiToken to the api_tokens table.
func (ApiToken) TableName() string {
	return "api_tokens"
}
