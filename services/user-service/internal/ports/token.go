package ports

import "github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"

type TokenManager interface {
	IssueSessionTokens(domain.SessionTokenSubject) (domain.IssuedTokens, error)
	ParseRefreshToken(string) (domain.RefreshTokenClaims, error)
}
