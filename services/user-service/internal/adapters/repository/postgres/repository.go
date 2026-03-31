package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type rowScanner interface {
	Scan(...any) error
}

type querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type txStarter interface {
	querier
	Begin(context.Context) (pgx.Tx, error)
}

type UserRepository struct {
	db querier
}

type SessionRepository struct {
	db txStarter
}

func NewUserRepository(db querier) (*UserRepository, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}

	return &UserRepository{db: db}, nil
}

func NewSessionRepository(db txStarter) (*SessionRepository, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}

	return &SessionRepository{db: db}, nil
}

func (r *UserRepository) Create(ctx context.Context, params domain.CreateUserParams) (*domain.UserAccount, error) {
	const query = `
		INSERT INTO users (
			id,
			email,
			password_hash,
			phone,
			first_name,
			last_name,
			status,
			roles,
			email_verified,
			registration_idempotency_key,
			created_at,
			updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
		RETURNING
			id,
			email,
			password_hash,
			phone,
			first_name,
			last_name,
			status,
			roles,
			email_verified,
			created_at,
			updated_at,
			last_login_at
	`

	user, err := scanUser(r.db.QueryRow(
		ctx,
		query,
		params.ID,
		params.Email,
		params.PasswordHash,
		nullableString(params.Phone),
		params.FirstName,
		params.LastName,
		int32(params.Status),
		toRoleInts(params.Roles),
		params.EmailVerified,
		nullableString(params.IdempotencyKey),
		params.CreatedAt.UTC(),
		params.UpdatedAt.UTC(),
	))
	if err == nil {
		return user, nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "users_email_key":
			return nil, domain.NewAlreadyExistsError("user with this email already exists")
		case "users_registration_idempotency_key_key":
			if strings.TrimSpace(params.IdempotencyKey) == "" {
				return nil, domain.NewConflictError("registration idempotency key collision")
			}

			existingUser, lookupErr := r.getByRegistrationKey(ctx, params.IdempotencyKey)
			if lookupErr != nil {
				return nil, lookupErr
			}

			return existingUser, nil
		default:
			return nil, domain.NewConflictError("user already exists")
		}
	}

	return nil, domain.NewInternalError("failed to create user", err)
}

func (r *UserRepository) GetByID(ctx context.Context, userID string) (*domain.UserAccount, error) {
	const query = `
		SELECT
			id,
			email,
			password_hash,
			phone,
			first_name,
			last_name,
			status,
			roles,
			email_verified,
			created_at,
			updated_at,
			last_login_at
		FROM users
		WHERE id = $1
	`

	user, err := scanUser(r.db.QueryRow(ctx, query, userID))
	if err != nil {
		return nil, mapUserReadError(err)
	}

	return user, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.UserAccount, error) {
	const query = `
		SELECT
			id,
			email,
			password_hash,
			phone,
			first_name,
			last_name,
			status,
			roles,
			email_verified,
			created_at,
			updated_at,
			last_login_at
		FROM users
		WHERE email = $1
	`

	user, err := scanUser(r.db.QueryRow(ctx, query, email))
	if err != nil {
		return nil, mapUserReadError(err)
	}

	return user, nil
}

func (r *UserRepository) List(ctx context.Context, filter domain.ListUsersFilter) ([]*domain.UserAccount, string, error) {
	query := `
		SELECT
			id,
			email,
			password_hash,
			phone,
			first_name,
			last_name,
			status,
			roles,
			email_verified,
			created_at,
			updated_at,
			last_login_at
		FROM users
		WHERE 1 = 1
	`

	args := make([]any, 0, 6)
	placeholder := 1

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", placeholder)
		args = append(args, int32(*filter.Status))
		placeholder++
	}
	if filter.Email != "" {
		query += fmt.Sprintf(" AND email = $%d", placeholder)
		args = append(args, filter.Email)
		placeholder++
	}
	if filter.Name != "" {
		pattern := "%" + filter.Name + "%"
		query += fmt.Sprintf(" AND (first_name ILIKE $%d OR last_name ILIKE $%d OR CONCAT_WS(' ', first_name, last_name) ILIKE $%d)", placeholder, placeholder, placeholder)
		args = append(args, pattern)
		placeholder++
	}
	if filter.PageToken != "" {
		cursor, err := decodeUserCursor(filter.PageToken)
		if err != nil {
			return nil, "", err
		}

		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", placeholder, placeholder+1)
		args = append(args, cursor.CreatedAt.UTC(), cursor.ID)
		placeholder += 2
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", placeholder)
	args = append(args, filter.PageSize+1)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", domain.NewInternalError("failed to list users", err)
	}
	defer rows.Close()

	users := make([]*domain.UserAccount, 0, filter.PageSize+1)
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, "", domain.NewInternalError("failed to scan user", scanErr)
		}
		users = append(users, user)
	}

	if rows.Err() != nil {
		return nil, "", domain.NewInternalError("failed to iterate users", rows.Err())
	}

	var nextPageToken string
	if len(users) > int(filter.PageSize) {
		lastVisible := users[int(filter.PageSize)-1]
		nextPageToken = encodeUserCursor(userCursor{
			CreatedAt: lastVisible.CreatedAt,
			ID:        lastVisible.ID,
		})
		users = users[:int(filter.PageSize)]
	}

	return users, nextPageToken, nil
}

func (r *UserRepository) UpdateProfile(ctx context.Context, params domain.UpdateUserProfileParams) (*domain.UserAccount, error) {
	assignments := make([]string, 0, 4)
	args := make([]any, 0, 6)
	placeholder := 1

	if params.Phone != nil {
		assignments = append(assignments, fmt.Sprintf("phone = $%d", placeholder))
		args = append(args, nullableString(*params.Phone))
		placeholder++
	}
	if params.FirstName != nil {
		assignments = append(assignments, fmt.Sprintf("first_name = $%d", placeholder))
		args = append(args, *params.FirstName)
		placeholder++
	}
	if params.LastName != nil {
		assignments = append(assignments, fmt.Sprintf("last_name = $%d", placeholder))
		args = append(args, *params.LastName)
		placeholder++
	}

	assignments = append(assignments, fmt.Sprintf("updated_at = $%d", placeholder))
	args = append(args, params.UpdatedAt.UTC())
	placeholder++

	args = append(args, params.UserID)
	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE id = $%d
		RETURNING
			id,
			email,
			password_hash,
			phone,
			first_name,
			last_name,
			status,
			roles,
			email_verified,
			created_at,
			updated_at,
			last_login_at
	`, strings.Join(assignments, ", "), placeholder)

	user, err := scanUser(r.db.QueryRow(ctx, query, args...))
	if err != nil {
		return nil, mapUserWriteError(err)
	}

	return user, nil
}

func (r *UserRepository) UpdatePassword(ctx context.Context, params domain.UpdatePasswordParams) error {
	const query = `
		UPDATE users
		SET password_hash = $1, updated_at = $2
		WHERE id = $3
	`

	tag, err := r.db.Exec(ctx, query, params.PasswordHash, params.UpdatedAt.UTC(), params.UserID)
	if err != nil {
		return domain.NewInternalError("failed to update password", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NewNotFoundError("user was not found")
	}

	return nil
}

func (r *UserRepository) MarkEmailVerified(ctx context.Context, userID string, verifiedAt time.Time) error {
	const query = `
		UPDATE users
		SET
			email_verified = TRUE,
			status = CASE
				WHEN status = $1 THEN $2
				ELSE status
			END,
			updated_at = $3
		WHERE id = $4
	`

	tag, err := r.db.Exec(
		ctx,
		query,
		int32(userv1.UserStatus_USER_STATUS_PENDING_VERIFICATION),
		int32(userv1.UserStatus_USER_STATUS_ACTIVE),
		verifiedAt.UTC(),
		userID,
	)
	if err != nil {
		return domain.NewInternalError("failed to mark email verified", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NewNotFoundError("user was not found")
	}

	return nil
}

func (r *UserRepository) Deactivate(ctx context.Context, params domain.DeactivateUserParams) error {
	const query = `
		UPDATE users
		SET
			status = $1,
			deactivated_at = $2,
			deactivation_reason = $3,
			updated_at = $2
		WHERE id = $4
	`

	tag, err := r.db.Exec(
		ctx,
		query,
		int32(userv1.UserStatus_USER_STATUS_DEACTIVATED),
		params.DeactivatedAt.UTC(),
		params.Reason,
		params.UserID,
	)
	if err != nil {
		return domain.NewInternalError("failed to deactivate user", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NewNotFoundError("user was not found")
	}

	return nil
}

func (r *UserRepository) TouchLastLogin(ctx context.Context, userID string, at time.Time) error {
	const query = `
		UPDATE users
		SET last_login_at = $1, updated_at = $1
		WHERE id = $2
	`

	tag, err := r.db.Exec(ctx, query, at.UTC(), userID)
	if err != nil {
		return domain.NewInternalError("failed to update last login", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NewNotFoundError("user was not found")
	}

	return nil
}

func (r *UserRepository) getByRegistrationKey(ctx context.Context, idempotencyKey string) (*domain.UserAccount, error) {
	const query = `
		SELECT
			id,
			email,
			password_hash,
			phone,
			first_name,
			last_name,
			status,
			roles,
			email_verified,
			created_at,
			updated_at,
			last_login_at
		FROM users
		WHERE registration_idempotency_key = $1
	`

	user, err := scanUser(r.db.QueryRow(ctx, query, idempotencyKey))
	if err != nil {
		return nil, mapUserReadError(err)
	}

	return user, nil
}

func (r *SessionRepository) Save(ctx context.Context, session domain.StoredSession) error {
	const query = `
		INSERT INTO user_sessions (
			id,
			user_id,
			user_agent,
			ip_address,
			created_at,
			expires_at,
			revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	if _, err := r.db.Exec(
		ctx,
		query,
		session.ID,
		session.UserID,
		nullableString(session.UserAgent),
		nullableString(session.IPAddress),
		session.CreatedAt.UTC(),
		session.ExpiresAt.UTC(),
		session.RevokedAt,
	); err != nil {
		return domain.NewInternalError("failed to persist session", err)
	}

	return nil
}

func (r *SessionRepository) GetByID(ctx context.Context, sessionID string) (*domain.StoredSession, error) {
	const query = `
		SELECT
			id,
			user_id,
			user_agent,
			ip_address,
			created_at,
			expires_at,
			revoked_at
		FROM user_sessions
		WHERE id = $1
	`

	session, err := scanSession(r.db.QueryRow(ctx, query, sessionID))
	if err != nil {
		return nil, mapSessionError(err)
	}

	return session, nil
}

func (r *SessionRepository) Rotate(ctx context.Context, currentSessionID string, replacement domain.StoredSession) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.NewInternalError("failed to begin session rotation transaction", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	tag, err := tx.Exec(
		ctx,
		`UPDATE user_sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`,
		currentSessionID,
		replacement.CreatedAt.UTC(),
	)
	if err != nil {
		return domain.NewInternalError("failed to revoke previous session", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NewConflictError("session cannot be rotated")
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO user_sessions (id, user_id, user_agent, ip_address, created_at, expires_at, revoked_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		replacement.ID,
		replacement.UserID,
		nullableString(replacement.UserAgent),
		nullableString(replacement.IPAddress),
		replacement.CreatedAt.UTC(),
		replacement.ExpiresAt.UTC(),
		replacement.RevokedAt,
	); err != nil {
		return domain.NewInternalError("failed to persist replacement session", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.NewInternalError("failed to commit session rotation transaction", err)
	}

	return nil
}

func (r *SessionRepository) Revoke(ctx context.Context, sessionID string, revokedAt time.Time) error {
	tag, err := r.db.Exec(
		ctx,
		`UPDATE user_sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`,
		sessionID,
		revokedAt.UTC(),
	)
	if err != nil {
		return domain.NewInternalError("failed to revoke session", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NewNotFoundError("session was not found")
	}

	return nil
}

func (r *SessionRepository) RevokeByUser(ctx context.Context, userID string, revokedAt time.Time) error {
	if _, err := r.db.Exec(
		ctx,
		`UPDATE user_sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
		revokedAt.UTC(),
	); err != nil {
		return domain.NewInternalError("failed to revoke user sessions", err)
	}

	return nil
}

func scanUser(scanner rowScanner) (*domain.UserAccount, error) {
	var (
		user        domain.UserAccount
		phone       *string
		status      int32
		roleInts    []int32
		lastLoginAt *time.Time
	)

	if err := scanner.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&phone,
		&user.FirstName,
		&user.LastName,
		&status,
		&roleInts,
		&user.EmailVerified,
		&user.CreatedAt,
		&user.UpdatedAt,
		&lastLoginAt,
	); err != nil {
		return nil, err
	}

	if phone != nil {
		user.Phone = *phone
	}
	if lastLoginAt != nil {
		normalized := lastLoginAt.UTC()
		user.LastLoginAt = &normalized
	}
	user.Status = userv1.UserStatus(status)
	user.Roles = fromRoleInts(roleInts)

	return &user, nil
}

func scanSession(scanner rowScanner) (*domain.StoredSession, error) {
	var (
		session   domain.StoredSession
		userAgent *string
		ipAddress *string
		revokedAt *time.Time
	)

	if err := scanner.Scan(
		&session.ID,
		&session.UserID,
		&userAgent,
		&ipAddress,
		&session.CreatedAt,
		&session.ExpiresAt,
		&revokedAt,
	); err != nil {
		return nil, err
	}

	if userAgent != nil {
		session.UserAgent = *userAgent
	}
	if ipAddress != nil {
		session.IPAddress = *ipAddress
	}
	if revokedAt != nil {
		normalized := revokedAt.UTC()
		session.RevokedAt = &normalized
	}

	return &session, nil
}

func mapUserReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewNotFoundError("user was not found")
	}
	return domain.NewInternalError("failed to load user", err)
}

func mapUserWriteError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewNotFoundError("user was not found")
	}
	return domain.NewInternalError("failed to persist user", err)
}

func mapSessionError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewNotFoundError("session was not found")
	}
	return domain.NewInternalError("failed to load session", err)
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func toRoleInts(roles []userv1.UserRole) []int32 {
	if len(roles) == 0 {
		return nil
	}

	values := make([]int32, len(roles))
	for i, role := range roles {
		values[i] = int32(role)
	}

	return values
}

func fromRoleInts(values []int32) []userv1.UserRole {
	if len(values) == 0 {
		return nil
	}

	roles := make([]userv1.UserRole, len(values))
	for i, value := range values {
		roles[i] = userv1.UserRole(value)
	}

	return roles
}

type userCursor struct {
	CreatedAt time.Time
	ID        string
}

func encodeUserCursor(cursor userCursor) string {
	payload := cursor.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + cursor.ID
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeUserCursor(token string) (userCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return userCursor{}, domain.NewInvalidArgumentError("page_token is invalid")
	}

	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return userCursor{}, domain.NewInvalidArgumentError("page_token is invalid")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return userCursor{}, domain.NewInvalidArgumentError("page_token is invalid")
	}

	return userCursor{
		CreatedAt: createdAt.UTC(),
		ID:        parts[1],
	}, nil
}
