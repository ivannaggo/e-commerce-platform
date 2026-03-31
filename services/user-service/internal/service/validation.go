package service

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

func normalizeUserID(userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", domain.NewInvalidArgumentError("user_id is required")
	}
	return userID, nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", domain.NewInvalidArgumentError("email is required")
	}

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", domain.NewInvalidArgumentError("email must be a valid email address")
	}
	if len(email) > 254 {
		return "", domain.NewInvalidArgumentError("email is too long")
	}

	return email, nil
}

func normalizeName(fieldName, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", domain.NewInvalidArgumentError(fieldName + " is required")
	}
	if utf8.RuneCountInString(value) > 100 {
		return "", domain.NewInvalidArgumentError(fieldName + " is too long")
	}
	return value, nil
}

func normalizeOptionalName(fieldName string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeName(fieldName, *value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizePhone(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", nil
	}
	if len(phone) < 7 || len(phone) > 20 {
		return "", domain.NewInvalidArgumentError("phone must be between 7 and 20 characters long")
	}
	for _, r := range phone {
		if unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '+', '-', ' ', '(', ')':
			continue
		default:
			return "", domain.NewInvalidArgumentError("phone contains unsupported characters")
		}
	}
	return phone, nil
}

func normalizeOptionalPhone(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizePhone(*value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func validatePassword(password string) error {
	if len(password) < 12 {
		return domain.NewInvalidArgumentError("password must be at least 12 characters long")
	}

	var hasUpper, hasLower, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}

	if !hasUpper || !hasLower || !hasDigit {
		return domain.NewInvalidArgumentError("password must contain upper-case, lower-case, and numeric characters")
	}

	return nil
}

func normalizePageSize(pageSize int32) (int32, error) {
	if pageSize == 0 {
		return defaultPageSize, nil
	}
	if pageSize < 0 || pageSize > maxPageSize {
		return 0, domain.NewInvalidArgumentError("page_size must be between 1 and 100")
	}
	return pageSize, nil
}

func normalizeStatusFilter(status userv1.UserStatus) (*userv1.UserStatus, error) {
	if status == userv1.UserStatus_USER_STATUS_UNSPECIFIED {
		return nil, nil
	}

	switch status {
	case userv1.UserStatus_USER_STATUS_PENDING_VERIFICATION,
		userv1.UserStatus_USER_STATUS_ACTIVE,
		userv1.UserStatus_USER_STATUS_SUSPENDED,
		userv1.UserStatus_USER_STATUS_DEACTIVATED:
		return &status, nil
	default:
		return nil, domain.NewInvalidArgumentError("status filter is invalid")
	}
}

func normalizeReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", domain.NewInvalidArgumentError("reason is required")
	}
	if utf8.RuneCountInString(reason) > 500 {
		return "", domain.NewInvalidArgumentError("reason is too long")
	}
	return reason, nil
}

func validateRefreshToken(refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return domain.NewInvalidArgumentError("refresh_token is required")
	}
	return nil
}

func validateVerificationToken(token string) error {
	if strings.TrimSpace(token) == "" {
		return domain.NewInvalidArgumentError("verification_token is required")
	}
	return nil
}
