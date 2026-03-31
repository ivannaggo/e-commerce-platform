package ports

import (
	"context"
	"time"

	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
)

type UserRepository interface {
	Create(context.Context, domain.CreateUserParams) (*domain.UserAccount, error)
	GetByID(context.Context, string) (*domain.UserAccount, error)
	GetByEmail(context.Context, string) (*domain.UserAccount, error)
	List(context.Context, domain.ListUsersFilter) ([]*domain.UserAccount, string, error)
	UpdateProfile(context.Context, domain.UpdateUserProfileParams) (*domain.UserAccount, error)
	UpdatePassword(context.Context, domain.UpdatePasswordParams) error
	MarkEmailVerified(context.Context, string, time.Time) error
	Deactivate(context.Context, domain.DeactivateUserParams) error
	TouchLastLogin(context.Context, string, time.Time) error
}

type SessionRepository interface {
	Save(context.Context, domain.StoredSession) error
	GetByID(context.Context, string) (*domain.StoredSession, error)
	Rotate(context.Context, string, domain.StoredSession) error
	Revoke(context.Context, string, time.Time) error
	RevokeByUser(context.Context, string, time.Time) error
}
