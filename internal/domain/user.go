package domain

import (
	"encoding/json"
	"time"
)

// Role is a user's authorization level.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// User is an OIDC-authenticated account.
type User struct {
	ID          string
	Issuer      string
	Subject     string
	Email       string
	DisplayName string
	AvatarURL   string
	Role        Role
	CreatedAt   time.Time
	LastLoginAt time.Time
	Prefs       json.RawMessage
}
