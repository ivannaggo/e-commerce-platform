package jwt

import (
	"context"
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
)

const (
	defaultAccessTokenTTL       = 15 * time.Minute
	defaultRefreshTokenTTL      = 7 * 24 * time.Hour
	defaultVerificationTokenTTL = 24 * time.Hour
)

type Config struct {
	Issuer               string
	Audience             string
	AccessTokenSecret    []byte
	RefreshTokenSecret   []byte
	VerificationSecret   []byte
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	VerificationTokenTTL time.Duration
	Clock                func() time.Time
}

type Manager struct {
	issuer               string
	audience             string
	accessTokenSecret    []byte
	refreshTokenSecret   []byte
	verificationSecret   []byte
	accessTokenTTL       time.Duration
	refreshTokenTTL      time.Duration
	verificationTokenTTL time.Duration
	clock                func() time.Time
}

type accessClaims struct {
	TokenType string  `json:"typ"`
	Email     string  `json:"email,omitempty"`
	Roles     []int32 `json:"roles,omitempty"`
	jwtlib.RegisteredClaims
}

type refreshClaims struct {
	TokenType string `json:"typ"`
	jwtlib.RegisteredClaims
}

type verificationClaims struct {
	TokenType string `json:"typ"`
	jwtlib.RegisteredClaims
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("issuer is required")
	}
	if cfg.Audience == "" {
		return nil, errors.New("audience is required")
	}
	if len(cfg.AccessTokenSecret) < 32 {
		return nil, errors.New("access token secret must be at least 32 bytes")
	}
	if len(cfg.RefreshTokenSecret) < 32 {
		return nil, errors.New("refresh token secret must be at least 32 bytes")
	}
	if len(cfg.VerificationSecret) < 32 {
		return nil, errors.New("verification secret must be at least 32 bytes")
	}

	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = defaultAccessTokenTTL
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = defaultRefreshTokenTTL
	}
	if cfg.VerificationTokenTTL <= 0 {
		cfg.VerificationTokenTTL = defaultVerificationTokenTTL
	}
	if cfg.Clock == nil {
		cfg.Clock = func() time.Time { return time.Now().UTC() }
	}

	return &Manager{
		issuer:               cfg.Issuer,
		audience:             cfg.Audience,
		accessTokenSecret:    append([]byte(nil), cfg.AccessTokenSecret...),
		refreshTokenSecret:   append([]byte(nil), cfg.RefreshTokenSecret...),
		verificationSecret:   append([]byte(nil), cfg.VerificationSecret...),
		accessTokenTTL:       cfg.AccessTokenTTL,
		refreshTokenTTL:      cfg.RefreshTokenTTL,
		verificationTokenTTL: cfg.VerificationTokenTTL,
		clock:                cfg.Clock,
	}, nil
}

func (m *Manager) IssueSessionTokens(subject domain.SessionTokenSubject) (domain.IssuedTokens, error) {
	now := m.clock().UTC()
	accessExpiresAt := now.Add(m.accessTokenTTL)
	refreshExpiresAt := now.Add(m.refreshTokenTTL)

	accessToken, err := m.signAccessToken(subject, now, accessExpiresAt)
	if err != nil {
		return domain.IssuedTokens{}, err
	}

	refreshToken, err := m.signRefreshToken(subject, now, refreshExpiresAt)
	if err != nil {
		return domain.IssuedTokens{}, err
	}

	return domain.IssuedTokens{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		TokenType:             "Bearer",
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

func (m *Manager) ParseRefreshToken(token string) (domain.RefreshTokenClaims, error) {
	claims := &refreshClaims{}
	parsedToken, err := jwtlib.ParseWithClaims(token, claims, func(parsed *jwtlib.Token) (any, error) {
		if parsed.Method != jwtlib.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", parsed.Method.Alg())
		}
		return m.refreshTokenSecret, nil
	}, jwtlib.WithAudience(m.audience), jwtlib.WithIssuer(m.issuer))
	if err != nil || !parsedToken.Valid {
		return domain.RefreshTokenClaims{}, errors.New("invalid refresh token")
	}
	if claims.TokenType != "refresh" {
		return domain.RefreshTokenClaims{}, errors.New("invalid refresh token type")
	}

	expiresAt := time.Time{}
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time.UTC()
	}

	return domain.RefreshTokenClaims{
		SessionID: claims.ID,
		UserID:    claims.Subject,
		ExpiresAt: expiresAt,
	}, nil
}

func (m *Manager) VerifyEmailToken(_ context.Context, userID, token string) error {
	claims := &verificationClaims{}
	parsedToken, err := jwtlib.ParseWithClaims(token, claims, func(parsed *jwtlib.Token) (any, error) {
		if parsed.Method != jwtlib.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", parsed.Method.Alg())
		}
		return m.verificationSecret, nil
	}, jwtlib.WithAudience(m.audience), jwtlib.WithIssuer(m.issuer))
	if err != nil || !parsedToken.Valid {
		return domain.NewUnauthenticatedError("verification token is invalid")
	}
	if claims.TokenType != "email_verification" || claims.Subject != userID {
		return domain.NewUnauthenticatedError("verification token is invalid")
	}

	return nil
}

func (m *Manager) IssueEmailVerificationToken(userID string) (string, time.Time, error) {
	now := m.clock().UTC()
	expiresAt := now.Add(m.verificationTokenTTL)

	claims := verificationClaims{
		TokenType: "email_verification",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID,
			Audience:  jwtlib.ClaimStrings{m.audience},
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(expiresAt),
		},
	}

	token, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(m.verificationSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign email verification token: %w", err)
	}

	return token, expiresAt, nil
}

func (m *Manager) signAccessToken(subject domain.SessionTokenSubject, issuedAt, expiresAt time.Time) (string, error) {
	claims := accessClaims{
		TokenType: "access",
		Email:     subject.Email,
		Roles:     toRoleInts(subject.Roles),
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   subject.UserID,
			Audience:  jwtlib.ClaimStrings{m.audience},
			IssuedAt:  jwtlib.NewNumericDate(issuedAt),
			ExpiresAt: jwtlib.NewNumericDate(expiresAt),
		},
	}

	token, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(m.accessTokenSecret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}

	return token, nil
}

func (m *Manager) signRefreshToken(subject domain.SessionTokenSubject, issuedAt, expiresAt time.Time) (string, error) {
	claims := refreshClaims{
		TokenType: "refresh",
		RegisteredClaims: jwtlib.RegisteredClaims{
			ID:        subject.SessionID,
			Issuer:    m.issuer,
			Subject:   subject.UserID,
			Audience:  jwtlib.ClaimStrings{m.audience},
			IssuedAt:  jwtlib.NewNumericDate(issuedAt),
			ExpiresAt: jwtlib.NewNumericDate(expiresAt),
		},
	}

	token, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(m.refreshTokenSecret)
	if err != nil {
		return "", fmt.Errorf("sign refresh token: %w", err)
	}

	return token, nil
}

func toRoleInts(roles []userv1.UserRole) []int32 {
	if len(roles) == 0 {
		return nil
	}

	cloned := make([]int32, len(roles))
	for i, role := range roles {
		cloned[i] = int32(role)
	}
	return cloned
}
