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
	// RoleViewer is read-only everywhere: it can see everything a member
	// can see, but every state-changing route refuses it
	// (auth.RequireWriter), except its own account routes (PATCH /me, its
	// API tokens, its Claude token).
	RoleViewer Role = "viewer"
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
