package domain

import "time"

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type User struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	Role         Role
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	AvatarPath   *string
	UITheme      string
	// SessionVersion is part of every session token; raising it signs the user out everywhere.
	SessionVersion int
}

func (r Role) IsAdmin() bool { return r == RoleAdmin }
