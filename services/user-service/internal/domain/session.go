package domain

import (
	"time"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
)

type StoredSession struct {
	ID        string
	UserID    string
	UserAgent string
	IPAddress string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type IssuedTokens struct {
	AccessToken           string
	RefreshToken          string
	TokenType             string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
}

type RefreshTokenClaims struct {
	SessionID string
	UserID    string
	ExpiresAt time.Time
}

type SessionTokenSubject struct {
	UserID    string
	SessionID string
	Email     string
	Roles     []userv1.UserRole
}
