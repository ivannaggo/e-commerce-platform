package ports

import (
	"context"
	"time"
)

type PasswordManager interface {
	Hash(string) (string, error)
	Compare(hash, password string) (bool, error)
}

type EmailVerificationTokenVerifier interface {
	VerifyEmailToken(context.Context, string, string) error
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID() string
}
