package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/ports"
)

type Dependencies struct {
	Users        ports.UserRepository
	Sessions     ports.SessionRepository
	Passwords    ports.PasswordManager
	Tokens       ports.TokenManager
	Verification ports.EmailVerificationTokenVerifier
	Clock        ports.Clock
	IDs          ports.IDGenerator
}

type UserService struct {
	users        ports.UserRepository
	sessions     ports.SessionRepository
	passwords    ports.PasswordManager
	tokens       ports.TokenManager
	verification ports.EmailVerificationTokenVerifier
	clock        ports.Clock
	ids          ports.IDGenerator
}

type RegisterUserInput struct {
	Email          string
	Password       string
	Phone          string
	FirstName      string
	LastName       string
	IdempotencyKey string
}

type AuthenticateUserInput struct {
	Email     string
	Password  string
	UserAgent string
	IPAddress string
}

type ListUsersInput struct {
	PageSize  int32
	PageToken string
	Status    userv1.UserStatus
	Email     string
	Name      string
}

type UpdateUserProfileInput struct {
	UserID    string
	Phone     *string
	FirstName *string
	LastName  *string
}

type ChangePasswordInput struct {
	UserID          string
	CurrentPassword string
	NewPassword     string
}

type VerifyEmailInput struct {
	UserID            string
	VerificationToken string
}

type RegisterUserOutput struct {
	User    *domain.UserAccount
	Session domain.IssuedTokens
}

type AuthenticateUserOutput struct {
	User    *domain.UserAccount
	Session domain.IssuedTokens
}

type ListUsersOutput struct {
	Users         []*domain.UserAccount
	NextPageToken string
}

type sessionMetadata struct {
	UserAgent string
	IPAddress string
}

func New(deps Dependencies) (*UserService, error) {
	switch {
	case deps.Users == nil:
		return nil, fmt.Errorf("users repository is required")
	case deps.Sessions == nil:
		return nil, fmt.Errorf("sessions repository is required")
	case deps.Passwords == nil:
		return nil, fmt.Errorf("password manager is required")
	case deps.Tokens == nil:
		return nil, fmt.Errorf("token manager is required")
	case deps.Verification == nil:
		return nil, fmt.Errorf("email verification token verifier is required")
	}

	clock := deps.Clock
	if clock == nil {
		clock = systemClock{}
	}

	idGenerator := deps.IDs
	if idGenerator == nil {
		idGenerator = randomIDGenerator{}
	}

	return &UserService{
		users:        deps.Users,
		sessions:     deps.Sessions,
		passwords:    deps.Passwords,
		tokens:       deps.Tokens,
		verification: deps.Verification,
		clock:        clock,
		ids:          idGenerator,
	}, nil
}

func (s *UserService) RegisterUser(ctx context.Context, input RegisterUserInput) (*RegisterUserOutput, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(input.Password); err != nil {
		return nil, err
	}
	firstName, err := normalizeName("first_name", input.FirstName)
	if err != nil {
		return nil, err
	}
	lastName, err := normalizeName("last_name", input.LastName)
	if err != nil {
		return nil, err
	}
	phone, err := normalizePhone(input.Phone)
	if err != nil {
		return nil, err
	}

	passwordHash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return nil, domain.NewInternalError("failed to hash password", err)
	}

	now := s.clock.Now().UTC()
	user, err := s.users.Create(ctx, domain.CreateUserParams{
		ID:             s.ids.NewID(),
		Email:          email,
		PasswordHash:   passwordHash,
		Phone:          phone,
		FirstName:      firstName,
		LastName:       lastName,
		Status:         userv1.UserStatus_USER_STATUS_PENDING_VERIFICATION,
		Roles:          []userv1.UserRole{userv1.UserRole_USER_ROLE_CUSTOMER},
		EmailVerified:  false,
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return nil, err
	}

	tokens, session, err := s.issueSession(user, sessionMetadata{})
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Save(ctx, session); err != nil {
		return nil, domain.NewInternalError("failed to persist session", err)
	}

	return &RegisterUserOutput{User: user, Session: tokens}, nil
}

func (s *UserService) AuthenticateUser(ctx context.Context, input AuthenticateUserInput) (*AuthenticateUserOutput, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Password) == "" {
		return nil, domain.NewInvalidArgumentError("password is required")
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if domain.HasCode(err, domain.ErrorCodeNotFound) {
			return nil, domain.NewUnauthenticatedError("invalid email or password")
		}
		return nil, err
	}
	if err := ensureUserCanSignIn(user); err != nil {
		return nil, err
	}

	matches, err := s.passwords.Compare(user.PasswordHash, input.Password)
	if err != nil {
		return nil, domain.NewInternalError("failed to verify password", err)
	}
	if !matches {
		return nil, domain.NewUnauthenticatedError("invalid email or password")
	}

	now := s.clock.Now().UTC()
	if err := s.users.TouchLastLogin(ctx, user.ID, now); err != nil {
		return nil, err
	}
	user.LastLoginAt = &now

	tokens, session, err := s.issueSession(user, sessionMetadata{
		UserAgent: strings.TrimSpace(input.UserAgent),
		IPAddress: strings.TrimSpace(input.IPAddress),
	})
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Save(ctx, session); err != nil {
		return nil, domain.NewInternalError("failed to persist session", err)
	}

	return &AuthenticateUserOutput{User: user, Session: tokens}, nil
}

func (s *UserService) RefreshSession(ctx context.Context, refreshToken string) (domain.IssuedTokens, error) {
	if err := validateRefreshToken(refreshToken); err != nil {
		return domain.IssuedTokens{}, err
	}

	claims, err := s.tokens.ParseRefreshToken(refreshToken)
	if err != nil {
		return domain.IssuedTokens{}, domain.NewUnauthenticatedError("refresh token is invalid")
	}

	now := s.clock.Now().UTC()
	if !claims.ExpiresAt.IsZero() && now.After(claims.ExpiresAt) {
		return domain.IssuedTokens{}, domain.NewUnauthenticatedError("refresh token has expired")
	}

	storedSession, err := s.sessions.GetByID(ctx, claims.SessionID)
	if err != nil {
		if domain.HasCode(err, domain.ErrorCodeNotFound) {
			return domain.IssuedTokens{}, domain.NewUnauthenticatedError("refresh session was not found")
		}
		return domain.IssuedTokens{}, err
	}

	if storedSession.UserID != claims.UserID {
		return domain.IssuedTokens{}, domain.NewUnauthenticatedError("refresh token is invalid")
	}
	if storedSession.RevokedAt != nil {
		return domain.IssuedTokens{}, domain.NewUnauthenticatedError("refresh session has been revoked")
	}
	if now.After(storedSession.ExpiresAt) {
		return domain.IssuedTokens{}, domain.NewUnauthenticatedError("refresh session has expired")
	}

	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		if domain.HasCode(err, domain.ErrorCodeNotFound) {
			return domain.IssuedTokens{}, domain.NewUnauthenticatedError("user was not found")
		}
		return domain.IssuedTokens{}, err
	}
	if err := ensureUserCanSignIn(user); err != nil {
		return domain.IssuedTokens{}, err
	}

	tokens, replacement, err := s.issueSession(user, sessionMetadata{
		UserAgent: storedSession.UserAgent,
		IPAddress: storedSession.IPAddress,
	})
	if err != nil {
		return domain.IssuedTokens{}, err
	}
	if err := s.sessions.Rotate(ctx, storedSession.ID, replacement); err != nil {
		return domain.IssuedTokens{}, err
	}

	return tokens, nil
}

func (s *UserService) RevokeSession(ctx context.Context, refreshToken string) error {
	if err := validateRefreshToken(refreshToken); err != nil {
		return err
	}

	claims, err := s.tokens.ParseRefreshToken(refreshToken)
	if err != nil {
		return domain.NewUnauthenticatedError("refresh token is invalid")
	}

	if err := s.sessions.Revoke(ctx, claims.SessionID, s.clock.Now().UTC()); err != nil && !domain.HasCode(err, domain.ErrorCodeNotFound) {
		return err
	}
	return nil
}

func (s *UserService) GetUser(ctx context.Context, userID string) (*domain.UserAccount, error) {
	normalizedUserID, err := normalizeUserID(userID)
	if err != nil {
		return nil, err
	}
	return s.users.GetByID(ctx, normalizedUserID)
}

func (s *UserService) ListUsers(ctx context.Context, input ListUsersInput) (*ListUsersOutput, error) {
	pageSize, err := normalizePageSize(input.PageSize)
	if err != nil {
		return nil, err
	}
	statusFilter, err := normalizeStatusFilter(input.Status)
	if err != nil {
		return nil, err
	}

	var emailFilter string
	if trimmed := strings.TrimSpace(input.Email); trimmed != "" {
		emailFilter, err = normalizeEmail(trimmed)
		if err != nil {
			return nil, err
		}
	}

	nameFilter := strings.TrimSpace(input.Name)
	if len(nameFilter) > 100 {
		return nil, domain.NewInvalidArgumentError("name filter is too long")
	}

	users, nextPageToken, err := s.users.List(ctx, domain.ListUsersFilter{
		PageSize:  pageSize,
		PageToken: strings.TrimSpace(input.PageToken),
		Status:    statusFilter,
		Email:     emailFilter,
		Name:      nameFilter,
	})
	if err != nil {
		return nil, err
	}

	return &ListUsersOutput{Users: users, NextPageToken: nextPageToken}, nil
}

func (s *UserService) UpdateUserProfile(ctx context.Context, input UpdateUserProfileInput) (*domain.UserAccount, error) {
	userID, err := normalizeUserID(input.UserID)
	if err != nil {
		return nil, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureUserCanBeMutated(user); err != nil {
		return nil, err
	}

	phone, err := normalizeOptionalPhone(input.Phone)
	if err != nil {
		return nil, err
	}
	firstName, err := normalizeOptionalName("first_name", input.FirstName)
	if err != nil {
		return nil, err
	}
	lastName, err := normalizeOptionalName("last_name", input.LastName)
	if err != nil {
		return nil, err
	}

	if phone == nil && firstName == nil && lastName == nil {
		return nil, domain.NewInvalidArgumentError("at least one profile field must be provided")
	}

	return s.users.UpdateProfile(ctx, domain.UpdateUserProfileParams{
		UserID:    userID,
		Phone:     phone,
		FirstName: firstName,
		LastName:  lastName,
		UpdatedAt: s.clock.Now().UTC(),
	})
}

func (s *UserService) ChangePassword(ctx context.Context, input ChangePasswordInput) error {
	userID, err := normalizeUserID(input.UserID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.CurrentPassword) == "" {
		return domain.NewInvalidArgumentError("current_password is required")
	}
	if input.CurrentPassword == input.NewPassword {
		return domain.NewInvalidArgumentError("new_password must differ from current_password")
	}
	if err := validatePassword(input.NewPassword); err != nil {
		return err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := ensureUserCanBeMutated(user); err != nil {
		return err
	}

	matches, err := s.passwords.Compare(user.PasswordHash, input.CurrentPassword)
	if err != nil {
		return domain.NewInternalError("failed to verify current password", err)
	}
	if !matches {
		return domain.NewPermissionDeniedError("current password is incorrect")
	}

	passwordHash, err := s.passwords.Hash(input.NewPassword)
	if err != nil {
		return domain.NewInternalError("failed to hash new password", err)
	}

	return s.users.UpdatePassword(ctx, domain.UpdatePasswordParams{
		UserID:       userID,
		PasswordHash: passwordHash,
		UpdatedAt:    s.clock.Now().UTC(),
	})
}

func (s *UserService) VerifyEmail(ctx context.Context, input VerifyEmailInput) error {
	userID, err := normalizeUserID(input.UserID)
	if err != nil {
		return err
	}
	if err := validateVerificationToken(input.VerificationToken); err != nil {
		return err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.EmailVerified {
		return nil
	}
	if err := ensureUserCanBeMutated(user); err != nil {
		return err
	}
	if err := s.verification.VerifyEmailToken(ctx, userID, strings.TrimSpace(input.VerificationToken)); err != nil {
		return err
	}
	return s.users.MarkEmailVerified(ctx, userID, s.clock.Now().UTC())
}

func (s *UserService) DeactivateUser(ctx context.Context, userID, reason string) error {
	normalizedUserID, err := normalizeUserID(userID)
	if err != nil {
		return err
	}
	normalizedReason, err := normalizeReason(reason)
	if err != nil {
		return err
	}

	user, err := s.users.GetByID(ctx, normalizedUserID)
	if err != nil {
		return err
	}
	if user.Status == userv1.UserStatus_USER_STATUS_DEACTIVATED {
		return nil
	}

	now := s.clock.Now().UTC()
	if err := s.users.Deactivate(ctx, domain.DeactivateUserParams{
		UserID:        normalizedUserID,
		Reason:        normalizedReason,
		DeactivatedAt: now,
	}); err != nil {
		return err
	}

	return s.sessions.RevokeByUser(ctx, normalizedUserID, now)
}

func (s *UserService) issueSession(user *domain.UserAccount, metadata sessionMetadata) (domain.IssuedTokens, domain.StoredSession, error) {
	sessionID := s.ids.NewID()
	tokens, err := s.tokens.IssueSessionTokens(domain.SessionTokenSubject{
		UserID:    user.ID,
		SessionID: sessionID,
		Email:     user.Email,
		Roles:     cloneRoles(user.Roles),
	})
	if err != nil {
		return domain.IssuedTokens{}, domain.StoredSession{}, domain.NewInternalError("failed to issue session tokens", err)
	}

	return tokens, domain.StoredSession{
		ID:        sessionID,
		UserID:    user.ID,
		UserAgent: metadata.UserAgent,
		IPAddress: metadata.IPAddress,
		CreatedAt: s.clock.Now().UTC(),
		ExpiresAt: tokens.RefreshTokenExpiresAt.UTC(),
	}, nil
}

func ensureUserCanSignIn(user *domain.UserAccount) error {
	if user == nil {
		return domain.NewNotFoundError("user was not found")
	}

	switch user.Status {
	case userv1.UserStatus_USER_STATUS_ACTIVE, userv1.UserStatus_USER_STATUS_PENDING_VERIFICATION:
		return nil
	case userv1.UserStatus_USER_STATUS_SUSPENDED:
		return domain.NewPermissionDeniedError("user account is suspended")
	case userv1.UserStatus_USER_STATUS_DEACTIVATED:
		return domain.NewFailedPreconditionError("user account is deactivated")
	default:
		return domain.NewFailedPreconditionError("user account is not in a valid state")
	}
}

func ensureUserCanBeMutated(user *domain.UserAccount) error {
	if user == nil {
		return domain.NewNotFoundError("user was not found")
	}

	switch user.Status {
	case userv1.UserStatus_USER_STATUS_ACTIVE, userv1.UserStatus_USER_STATUS_PENDING_VERIFICATION:
		return nil
	case userv1.UserStatus_USER_STATUS_SUSPENDED:
		return domain.NewPermissionDeniedError("user account is suspended")
	case userv1.UserStatus_USER_STATUS_DEACTIVATED:
		return domain.NewFailedPreconditionError("user account is deactivated")
	default:
		return domain.NewFailedPreconditionError("user account is not in a valid state")
	}
}

func cloneRoles(roles []userv1.UserRole) []userv1.UserRole {
	if len(roles) == 0 {
		return nil
	}
	cloned := make([]userv1.UserRole, len(roles))
	copy(cloned, roles)
	return cloned
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type randomIDGenerator struct{}

func (randomIDGenerator) NewID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("id-%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(bytes[:])
}
