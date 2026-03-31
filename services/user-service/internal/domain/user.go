package domain

import (
	"time"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
)

type UserAccount struct {
	ID            string
	Email         string
	PasswordHash  string
	Phone         string
	FirstName     string
	LastName      string
	Status        userv1.UserStatus
	Roles         []userv1.UserRole
	EmailVerified bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastLoginAt   *time.Time
}

type CreateUserParams struct {
	ID             string
	Email          string
	PasswordHash   string
	Phone          string
	FirstName      string
	LastName       string
	Status         userv1.UserStatus
	Roles          []userv1.UserRole
	EmailVerified  bool
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ListUsersFilter struct {
	PageSize  int32
	PageToken string
	Status    *userv1.UserStatus
	Email     string
	Name      string
}

type UpdateUserProfileParams struct {
	UserID    string
	Phone     *string
	FirstName *string
	LastName  *string
	UpdatedAt time.Time
}

type UpdatePasswordParams struct {
	UserID       string
	PasswordHash string
	UpdatedAt    time.Time
}

type DeactivateUserParams struct {
	UserID        string
	Reason        string
	DeactivatedAt time.Time
}
