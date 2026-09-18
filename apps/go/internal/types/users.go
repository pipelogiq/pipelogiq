package types

import "strings"

const (
	UserRoleAdmin    = "admin"
	UserRoleOperator = "operator"
	UserRoleViewer   = "viewer"
)

// NormalizeUserRole recognizes legacy names while preserving unknown values so
// callers can reject invalid roles rather than silently grant permissions.
func NormalizeUserRole(role string) string {
	switch role = strings.ToLower(strings.TrimSpace(role)); role {
	case "administrator", "superadmin":
		return UserRoleAdmin
	case "user":
		return UserRoleOperator
	default:
		return role
	}
}

func IsValidUserRole(role string) bool {
	switch NormalizeUserRole(role) {
	case UserRoleAdmin, UserRoleOperator, UserRoleViewer:
		return true
	default:
		return false
	}
}

// ManagedUser exposes account details and membership, never password hashes or
// application credentials.
type ManagedUser struct {
	UserResponse
	ApplicationIDs []int `json:"applicationIds"`
}

type SaveUserRequest struct {
	FirstName      string  `json:"firstName"`
	LastName       *string `json:"lastName"`
	Email          string  `json:"email"`
	Role           string  `json:"role"`
	IsActive       bool    `json:"isActive"`
	ApplicationIDs []int   `json:"applicationIds"`
}

// UserApplication is the membership picker representation. In particular it
// does not contain API keys, even for administrators who manage all accounts.
type UserApplication struct {
	ID          int     `json:"id" db:"id"`
	Name        string  `json:"name" db:"name"`
	Description *string `json:"description,omitempty" db:"description"`
}
