package grpc

import (
	"time"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func toProtoUser(user *domain.UserAccount) *userv1.User {
	if user == nil {
		return nil
	}

	return &userv1.User{
		Id:            user.ID,
		Email:         user.Email,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		Status:        user.Status,
		EmailVerified: user.EmailVerified,
		CreatedAt:     timestampPointer(user.CreatedAt),
		UpdatedAt:     timestampPointer(user.UpdatedAt),
	}
}

func toProtoUserDetails(user *domain.UserAccount) *userv1.UserDetails {
	if user == nil {
		return nil
	}

	return &userv1.UserDetails{
		User:        toProtoUser(user),
		Phone:       user.Phone,
		Roles:       cloneRoles(user.Roles),
		LastLoginAt: timestampPointerPtr(user.LastLoginAt),
	}
}

func toProtoSession(tokens domain.IssuedTokens) *userv1.Session {
	expiresInSeconds := int64(time.Until(tokens.AccessTokenExpiresAt).Seconds())
	if expiresInSeconds < 0 {
		expiresInSeconds = 0
	}

	return &userv1.Session{
		AccessToken:      tokens.AccessToken,
		RefreshToken:     tokens.RefreshToken,
		TokenType:        nonEmptyOrDefault(tokens.TokenType, "Bearer"),
		ExpiresInSeconds: expiresInSeconds,
		ExpiresAt:        timestampPointer(tokens.AccessTokenExpiresAt),
	}
}

func toProtoUsers(users []*domain.UserAccount) []*userv1.User {
	items := make([]*userv1.User, 0, len(users))
	for _, user := range users {
		items = append(items, toProtoUser(user))
	}
	return items
}

func cloneRoles(roles []userv1.UserRole) []userv1.UserRole {
	if len(roles) == 0 {
		return nil
	}

	cloned := make([]userv1.UserRole, len(roles))
	copy(cloned, roles)
	return cloned
}

func timestampPointer(ts time.Time) *timestamppb.Timestamp {
	if ts.IsZero() {
		return nil
	}
	return timestamppb.New(ts.UTC())
}

func timestampPointerPtr(ts *time.Time) *timestamppb.Timestamp {
	if ts == nil {
		return nil
	}
	return timestampPointer(*ts)
}

func nonEmptyOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
