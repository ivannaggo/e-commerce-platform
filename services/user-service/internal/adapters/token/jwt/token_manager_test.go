package jwt

import (
	"context"
	"testing"
	"time"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
)

func TestManagerIssueAndParseRefreshToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	manager := newTestManager(t, now)

	tokens, err := manager.IssueSessionTokens(domain.SessionTokenSubject{
		UserID:    "user-id",
		SessionID: "session-id",
		Email:     "user@example.com",
		Roles:     []userv1.UserRole{userv1.UserRole_USER_ROLE_CUSTOMER},
	})
	if err != nil {
		t.Fatalf("IssueSessionTokens returned error: %v", err)
	}

	claims, err := manager.ParseRefreshToken(tokens.RefreshToken)
	if err != nil {
		t.Fatalf("ParseRefreshToken returned error: %v", err)
	}
	if claims.UserID != "user-id" || claims.SessionID != "session-id" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestManagerVerifyEmailToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	manager := newTestManager(t, now)

	token, _, err := manager.IssueEmailVerificationToken("user-id")
	if err != nil {
		t.Fatalf("IssueEmailVerificationToken returned error: %v", err)
	}

	if err := manager.VerifyEmailToken(context.Background(), "user-id", token); err != nil {
		t.Fatalf("VerifyEmailToken returned error: %v", err)
	}
}

func newTestManager(t *testing.T, now time.Time) *Manager {
	t.Helper()

	manager, err := NewManager(Config{
		Issuer:               "ecommerce-user-service",
		Audience:             "ecommerce-clients",
		AccessTokenSecret:    []byte("0123456789abcdef0123456789abcdef"),
		RefreshTokenSecret:   []byte("abcdef0123456789abcdef0123456789"),
		VerificationSecret:   []byte("fedcba9876543210fedcba9876543210"),
		AccessTokenTTL:       15 * time.Minute,
		RefreshTokenTTL:      24 * time.Hour,
		VerificationTokenTTL: 12 * time.Hour,
		Clock:                func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	return manager
}
