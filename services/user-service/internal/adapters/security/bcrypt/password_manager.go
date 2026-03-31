package bcrypt

import (
	"errors"
	"fmt"

	xbcrypt "golang.org/x/crypto/bcrypt"
)

const (
	DefaultCost = 12
	MinCost     = 10
	MaxCost     = 14
)

type PasswordManager struct {
	cost int
}

func NewPasswordManager(cost int) (*PasswordManager, error) {
	if cost == 0 {
		cost = DefaultCost
	}
	if cost < MinCost || cost > MaxCost {
		return nil, fmt.Errorf("bcrypt cost must be between %d and %d", MinCost, MaxCost)
	}

	return &PasswordManager{cost: cost}, nil
}

func (m *PasswordManager) Hash(password string) (string, error) {
	hash, err := xbcrypt.GenerateFromPassword([]byte(password), m.cost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func (m *PasswordManager) Compare(hash, password string) (bool, error) {
	err := xbcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, xbcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}

	return false, err
}
