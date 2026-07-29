package auth

import "time"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// Permission hierarchy: each role includes all permissions of roles below it.
var roleLevel = map[Role]int{
	RoleViewer: 1,
	RoleEditor: 2,
	RoleAdmin:  3,
	RoleOwner:  4,
}

func (r Role) CanWrite() bool { return roleLevel[r] >= roleLevel[RoleEditor] }
func (r Role) CanAdmin() bool { return roleLevel[r] >= roleLevel[RoleAdmin] }
func (r Role) IsOwner() bool  { return r == RoleOwner }

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         Role      `json:"role"`
	PasswordHash string    `json:"-"` // bcrypt hash
	CreatedAt    time.Time `json:"created_at"`
}

type Claims struct {
	UserID string `json:"sub"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   Role   `json:"role"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds
	TokenType    string `json:"token_type"` // "Bearer"
}
