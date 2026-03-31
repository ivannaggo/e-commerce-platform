package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/service"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRegisterUserSuccess(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	repo := &userRepoStub{
		createFn: func(_ context.Context, params domain.CreateUserParams) (*domain.UserAccount, error) {
			if params.Email != "user@example.com" {
				t.Fatalf("expected normalized email, got %q", params.Email)
			}
			if params.PasswordHash != "hashed-password" {
				t.Fatalf("expected hashed password, got %q", params.PasswordHash)
			}
			return &domain.UserAccount{
				ID:            params.ID,
				Email:         params.Email,
				Phone:         params.Phone,
				FirstName:     params.FirstName,
				LastName:      params.LastName,
				Status:        params.Status,
				Roles:         append([]userv1.UserRole(nil), params.Roles...),
				EmailVerified: params.EmailVerified,
				CreatedAt:     params.CreatedAt,
				UpdatedAt:     params.UpdatedAt,
			}, nil
		},
	}
	sessions := &sessionRepoStub{
		saveFn: func(_ context.Context, session domain.StoredSession) error {
			if session.ID != "session-id" || session.UserID != "user-id" {
				t.Fatalf("unexpected saved session: %+v", session)
			}
			return nil
		},
	}
	passwords := &passwordManagerStub{
		hashFn: func(password string) (string, error) {
			if password != "StrongPassword1" {
				t.Fatalf("unexpected password %q", password)
			}
			return "hashed-password", nil
		},
	}
	tokens := &tokenManagerStub{
		issueFn: func(subject domain.SessionTokenSubject) (domain.IssuedTokens, error) {
			if subject.UserID != "user-id" || subject.SessionID != "session-id" {
				t.Fatalf("unexpected token subject: %+v", subject)
			}
			return domain.IssuedTokens{
				AccessToken:           "access-token",
				RefreshToken:          "refresh-token",
				TokenType:             "Bearer",
				AccessTokenExpiresAt:  now.Add(15 * time.Minute),
				RefreshTokenExpiresAt: now.Add(24 * time.Hour),
			}, nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Users:        repo,
		Sessions:     sessions,
		Passwords:    passwords,
		Tokens:       tokens,
		Verification: verificationStub{},
		Clock:        fixedClock{now: now},
		IDs:          &sequenceIDGenerator{ids: []string{"user-id", "session-id"}},
	})

	resp, err := svc.RegisterUser(context.Background(), service.RegisterUserInput{
		Email:          " User@Example.com ",
		Password:       "StrongPassword1",
		Phone:          "+1 555 123 4567",
		FirstName:      "Ivan",
		LastName:       "Naggo",
		IdempotencyKey: "register-user-1",
	})
	if err != nil {
		t.Fatalf("RegisterUser returned error: %v", err)
	}
	if resp.User.ID != "user-id" || resp.Session.RefreshToken != "refresh-token" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAuthenticateUserMasksMissingUser(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, service.Dependencies{
		Users: &userRepoStub{
			getByEmailFn: func(context.Context, string) (*domain.UserAccount, error) {
				return nil, domain.NewNotFoundError("user not found")
			},
		},
		Sessions:     &sessionRepoStub{},
		Passwords:    &passwordManagerStub{},
		Tokens:       &tokenManagerStub{},
		Verification: verificationStub{},
		Clock:        fixedClock{now: time.Now().UTC()},
	})

	_, err := svc.AuthenticateUser(context.Background(), service.AuthenticateUserInput{
		Email:    "missing@example.com",
		Password: "StrongPassword1",
	})
	if !domain.HasCode(err, domain.ErrorCodeUnauthenticated) {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}
}

func TestRefreshSessionRotatesSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, service.Dependencies{
		Users: &userRepoStub{
			getByIDFn: func(_ context.Context, userID string) (*domain.UserAccount, error) {
				return &domain.UserAccount{
					ID:            userID,
					Email:         "user@example.com",
					Status:        userv1.UserStatus_USER_STATUS_ACTIVE,
					Roles:         []userv1.UserRole{userv1.UserRole_USER_ROLE_CUSTOMER},
					EmailVerified: true,
					CreatedAt:     now.Add(-time.Hour),
					UpdatedAt:     now.Add(-time.Minute),
				}, nil
			},
		},
		Sessions: &sessionRepoStub{
			getByIDFn: func(_ context.Context, sessionID string) (*domain.StoredSession, error) {
				return &domain.StoredSession{
					ID:        sessionID,
					UserID:    "user-id",
					UserAgent: "agent",
					IPAddress: "127.0.0.1",
					CreatedAt: now.Add(-time.Hour),
					ExpiresAt: now.Add(time.Hour),
				}, nil
			},
			rotateFn: func(_ context.Context, previousSessionID string, replacement domain.StoredSession) error {
				if previousSessionID != "old-session" || replacement.ID != "new-session" {
					t.Fatalf("unexpected rotation: %q %+v", previousSessionID, replacement)
				}
				return nil
			},
		},
		Passwords: &passwordManagerStub{},
		Tokens: &tokenManagerStub{
			parseFn: func(token string) (domain.RefreshTokenClaims, error) {
				if token != "refresh-token" {
					t.Fatalf("unexpected refresh token %q", token)
				}
				return domain.RefreshTokenClaims{
					SessionID: "old-session",
					UserID:    "user-id",
					ExpiresAt: now.Add(time.Hour),
				}, nil
			},
			issueFn: func(subject domain.SessionTokenSubject) (domain.IssuedTokens, error) {
				if subject.SessionID != "new-session" {
					t.Fatalf("expected new-session, got %q", subject.SessionID)
				}
				return domain.IssuedTokens{
					AccessToken:           "new-access",
					RefreshToken:          "new-refresh",
					TokenType:             "Bearer",
					AccessTokenExpiresAt:  now.Add(30 * time.Minute),
					RefreshTokenExpiresAt: now.Add(24 * time.Hour),
				}, nil
			},
		},
		Verification: verificationStub{},
		Clock:        fixedClock{now: now},
		IDs:          &sequenceIDGenerator{ids: []string{"new-session"}},
	})

	session, err := svc.RefreshSession(context.Background(), "refresh-token")
	if err != nil {
		t.Fatalf("RefreshSession returned error: %v", err)
	}
	if session.AccessToken != "new-access" {
		t.Fatalf("expected new-access, got %q", session.AccessToken)
	}
}

func TestUpdateUserProfileRejectsEmptyPatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, service.Dependencies{
		Users: &userRepoStub{
			getByIDFn: func(_ context.Context, userID string) (*domain.UserAccount, error) {
				return &domain.UserAccount{
					ID:        userID,
					Email:     "user@example.com",
					Status:    userv1.UserStatus_USER_STATUS_ACTIVE,
					CreatedAt: now.Add(-time.Hour),
					UpdatedAt: now.Add(-time.Minute),
				}, nil
			},
		},
		Sessions:     &sessionRepoStub{},
		Passwords:    &passwordManagerStub{},
		Tokens:       &tokenManagerStub{},
		Verification: verificationStub{},
		Clock:        fixedClock{now: now},
	})

	_, err := svc.UpdateUserProfile(context.Background(), service.UpdateUserProfileInput{UserID: "user-id"})
	if !domain.HasCode(err, domain.ErrorCodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestChangePasswordRejectsIncorrectCurrentPassword(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, service.Dependencies{
		Users: &userRepoStub{
			getByIDFn: func(_ context.Context, userID string) (*domain.UserAccount, error) {
				return &domain.UserAccount{
					ID:           userID,
					Email:        "user@example.com",
					PasswordHash: "stored-hash",
					Status:       userv1.UserStatus_USER_STATUS_ACTIVE,
					CreatedAt:    now.Add(-time.Hour),
					UpdatedAt:    now.Add(-time.Minute),
				}, nil
			},
		},
		Sessions: &sessionRepoStub{},
		Passwords: &passwordManagerStub{
			compareFn: func(hash, _ string) (bool, error) {
				if hash != "stored-hash" {
					t.Fatalf("unexpected hash %q", hash)
				}
				return false, nil
			},
		},
		Tokens:       &tokenManagerStub{},
		Verification: verificationStub{},
		Clock:        fixedClock{now: now},
	})

	err := svc.ChangePassword(context.Background(), service.ChangePasswordInput{
		UserID:          "user-id",
		CurrentPassword: "WrongPassword1",
		NewPassword:     "BetterPassword2",
	})
	if !domain.HasCode(err, domain.ErrorCodePermissionDenied) {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestDeactivateUserRevokesAllSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	var revoked bool
	svc := newTestService(t, service.Dependencies{
		Users: &userRepoStub{
			getByIDFn: func(_ context.Context, userID string) (*domain.UserAccount, error) {
				return &domain.UserAccount{
					ID:        userID,
					Email:     "user@example.com",
					Status:    userv1.UserStatus_USER_STATUS_ACTIVE,
					CreatedAt: now.Add(-time.Hour),
					UpdatedAt: now.Add(-time.Minute),
				}, nil
			},
			deactivateFn: func(_ context.Context, params domain.DeactivateUserParams) error {
				if params.Reason != "fraud detected" {
					t.Fatalf("unexpected reason %q", params.Reason)
				}
				return nil
			},
		},
		Sessions: &sessionRepoStub{
			revokeByUserFn: func(_ context.Context, userID string, _ time.Time) error {
				revoked = true
				if userID != "user-id" {
					t.Fatalf("expected user-id, got %q", userID)
				}
				return nil
			},
		},
		Passwords:    &passwordManagerStub{},
		Tokens:       &tokenManagerStub{},
		Verification: verificationStub{},
		Clock:        fixedClock{now: now},
	})

	if err := svc.DeactivateUser(context.Background(), "user-id", "fraud detected"); err != nil {
		t.Fatalf("DeactivateUser returned error: %v", err)
	}
	if !revoked {
		t.Fatal("expected sessions to be revoked")
	}
}

func TestUpdateUserProfileSuccess(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, service.Dependencies{
		Users: &userRepoStub{
			getByIDFn: func(_ context.Context, userID string) (*domain.UserAccount, error) {
				return &domain.UserAccount{
					ID:            userID,
					Email:         "user@example.com",
					Phone:         "+1 111 222 3333",
					FirstName:     "Ivan",
					LastName:      "Naggo",
					Status:        userv1.UserStatus_USER_STATUS_ACTIVE,
					EmailVerified: true,
					CreatedAt:     now.Add(-24 * time.Hour),
					UpdatedAt:     now.Add(-time.Hour),
				}, nil
			},
			updateProfileFn: func(_ context.Context, params domain.UpdateUserProfileParams) (*domain.UserAccount, error) {
				if params.FirstName == nil || *params.FirstName != "John" {
					t.Fatalf("expected first name update, got %+v", params.FirstName)
				}
				return &domain.UserAccount{
					ID:            params.UserID,
					Email:         "user@example.com",
					Phone:         "+1 111 222 3333",
					FirstName:     "John",
					LastName:      "Naggo",
					Status:        userv1.UserStatus_USER_STATUS_ACTIVE,
					EmailVerified: true,
					Roles:         []userv1.UserRole{userv1.UserRole_USER_ROLE_CUSTOMER},
					CreatedAt:     now.Add(-24 * time.Hour),
					UpdatedAt:     params.UpdatedAt,
				}, nil
			},
		},
		Sessions:     &sessionRepoStub{},
		Passwords:    &passwordManagerStub{},
		Tokens:       &tokenManagerStub{},
		Verification: verificationStub{},
		Clock:        fixedClock{now: now},
	})

	firstName := wrapperspb.String("John").Value
	resp, err := svc.UpdateUserProfile(context.Background(), service.UpdateUserProfileInput{
		UserID:    "user-id",
		FirstName: &firstName,
	})
	if err != nil {
		t.Fatalf("UpdateUserProfile returned error: %v", err)
	}
	if resp.FirstName != "John" {
		t.Fatalf("expected updated first name, got %q", resp.FirstName)
	}
}

func newTestService(t *testing.T, deps service.Dependencies) *service.UserService {
	t.Helper()

	svc, err := service.New(deps)
	if err != nil {
		t.Fatalf("service.New returned error: %v", err)
	}

	return svc
}

type userRepoStub struct {
	createFn        func(context.Context, domain.CreateUserParams) (*domain.UserAccount, error)
	getByIDFn       func(context.Context, string) (*domain.UserAccount, error)
	getByEmailFn    func(context.Context, string) (*domain.UserAccount, error)
	listFn          func(context.Context, domain.ListUsersFilter) ([]*domain.UserAccount, string, error)
	updateProfileFn func(context.Context, domain.UpdateUserProfileParams) (*domain.UserAccount, error)
	updatePwdFn     func(context.Context, domain.UpdatePasswordParams) error
	verifyFn        func(context.Context, string, time.Time) error
	deactivateFn    func(context.Context, domain.DeactivateUserParams) error
	touchLoginFn    func(context.Context, string, time.Time) error
}

func (s *userRepoStub) Create(ctx context.Context, params domain.CreateUserParams) (*domain.UserAccount, error) {
	if s.createFn == nil {
		return nil, errors.New("unexpected Create call")
	}
	return s.createFn(ctx, params)
}

func (s *userRepoStub) GetByID(ctx context.Context, userID string) (*domain.UserAccount, error) {
	if s.getByIDFn == nil {
		return nil, errors.New("unexpected GetByID call")
	}
	return s.getByIDFn(ctx, userID)
}

func (s *userRepoStub) GetByEmail(ctx context.Context, email string) (*domain.UserAccount, error) {
	if s.getByEmailFn == nil {
		return nil, errors.New("unexpected GetByEmail call")
	}
	return s.getByEmailFn(ctx, email)
}

func (s *userRepoStub) List(ctx context.Context, filter domain.ListUsersFilter) ([]*domain.UserAccount, string, error) {
	if s.listFn == nil {
		return nil, "", errors.New("unexpected List call")
	}
	return s.listFn(ctx, filter)
}

func (s *userRepoStub) UpdateProfile(ctx context.Context, params domain.UpdateUserProfileParams) (*domain.UserAccount, error) {
	if s.updateProfileFn == nil {
		return nil, errors.New("unexpected UpdateProfile call")
	}
	return s.updateProfileFn(ctx, params)
}

func (s *userRepoStub) UpdatePassword(ctx context.Context, params domain.UpdatePasswordParams) error {
	if s.updatePwdFn == nil {
		return errors.New("unexpected UpdatePassword call")
	}
	return s.updatePwdFn(ctx, params)
}

func (s *userRepoStub) MarkEmailVerified(ctx context.Context, userID string, verifiedAt time.Time) error {
	if s.verifyFn == nil {
		return errors.New("unexpected MarkEmailVerified call")
	}
	return s.verifyFn(ctx, userID, verifiedAt)
}

func (s *userRepoStub) Deactivate(ctx context.Context, params domain.DeactivateUserParams) error {
	if s.deactivateFn == nil {
		return errors.New("unexpected Deactivate call")
	}
	return s.deactivateFn(ctx, params)
}

func (s *userRepoStub) TouchLastLogin(ctx context.Context, userID string, at time.Time) error {
	if s.touchLoginFn == nil {
		return errors.New("unexpected TouchLastLogin call")
	}
	return s.touchLoginFn(ctx, userID, at)
}

type sessionRepoStub struct {
	saveFn         func(context.Context, domain.StoredSession) error
	getByIDFn      func(context.Context, string) (*domain.StoredSession, error)
	rotateFn       func(context.Context, string, domain.StoredSession) error
	revokeFn       func(context.Context, string, time.Time) error
	revokeByUserFn func(context.Context, string, time.Time) error
}

func (s *sessionRepoStub) Save(ctx context.Context, session domain.StoredSession) error {
	if s.saveFn == nil {
		return errors.New("unexpected Save call")
	}
	return s.saveFn(ctx, session)
}

func (s *sessionRepoStub) GetByID(ctx context.Context, sessionID string) (*domain.StoredSession, error) {
	if s.getByIDFn == nil {
		return nil, errors.New("unexpected GetByID call")
	}
	return s.getByIDFn(ctx, sessionID)
}

func (s *sessionRepoStub) Rotate(ctx context.Context, currentSessionID string, replacement domain.StoredSession) error {
	if s.rotateFn == nil {
		return errors.New("unexpected Rotate call")
	}
	return s.rotateFn(ctx, currentSessionID, replacement)
}

func (s *sessionRepoStub) Revoke(ctx context.Context, sessionID string, revokedAt time.Time) error {
	if s.revokeFn == nil {
		return errors.New("unexpected Revoke call")
	}
	return s.revokeFn(ctx, sessionID, revokedAt)
}

func (s *sessionRepoStub) RevokeByUser(ctx context.Context, userID string, revokedAt time.Time) error {
	if s.revokeByUserFn == nil {
		return errors.New("unexpected RevokeByUser call")
	}
	return s.revokeByUserFn(ctx, userID, revokedAt)
}

type passwordManagerStub struct {
	hashFn    func(string) (string, error)
	compareFn func(string, string) (bool, error)
}

func (s *passwordManagerStub) Hash(password string) (string, error) {
	if s.hashFn == nil {
		return "", errors.New("unexpected Hash call")
	}
	return s.hashFn(password)
}

func (s *passwordManagerStub) Compare(hash, password string) (bool, error) {
	if s.compareFn == nil {
		return false, errors.New("unexpected Compare call")
	}
	return s.compareFn(hash, password)
}

type tokenManagerStub struct {
	issueFn func(domain.SessionTokenSubject) (domain.IssuedTokens, error)
	parseFn func(string) (domain.RefreshTokenClaims, error)
}

func (s *tokenManagerStub) IssueSessionTokens(subject domain.SessionTokenSubject) (domain.IssuedTokens, error) {
	if s.issueFn == nil {
		return domain.IssuedTokens{}, errors.New("unexpected IssueSessionTokens call")
	}
	return s.issueFn(subject)
}

func (s *tokenManagerStub) ParseRefreshToken(token string) (domain.RefreshTokenClaims, error) {
	if s.parseFn == nil {
		return domain.RefreshTokenClaims{}, errors.New("unexpected ParseRefreshToken call")
	}
	return s.parseFn(token)
}

type verificationStub struct {
	verifyFn func(context.Context, string, string) error
}

func (s verificationStub) VerifyEmailToken(ctx context.Context, userID, token string) error {
	if s.verifyFn == nil {
		return nil
	}
	return s.verifyFn(ctx, userID, token)
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type sequenceIDGenerator struct {
	ids   []string
	index int
}

func (g *sequenceIDGenerator) NewID() string {
	if g.index >= len(g.ids) {
		return "generated-id"
	}
	id := g.ids[g.index]
	g.index++
	return id
}
