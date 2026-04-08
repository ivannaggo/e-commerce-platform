package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	productv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/product/v1"
	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/domain"
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

type ProductRepository struct {
	db txStarter
}

type operationType string

const (
	operationTypeCreate operationType = "create"
	operationTypeUpdate operationType = "update"
	operationTypeDelete operationType = "delete"
)

func NewProductRepository(db txStarter) (*ProductRepository, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}

	return &ProductRepository{db: db}, nil
}

func (r *ProductRepository) Create(ctx context.Context, params domain.CreateProductParams) (*domain.Product, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, domain.NewInternalError("failed to begin product creation transaction", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	idempotencyKey := strings.TrimSpace(params.IdempotencyKey)

	// If an idempotency key is provided, try to claim it atomically.
	// If the key was already used, return the previously created product.
	if idempotencyKey != "" {
		existing, err := r.lookupByGlobalIdempotencyKey(ctx, tx, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}

	const query = `
		INSERT INTO products (
			id,
			name,
			description,
			price,
			stock,
			reserved_stock,
			currency,
			status,
			created_at,
			updated_at,
			idempotency_key
		) VALUES (
			$1, $2, $3, $4, $5, 0, $6, $7, $8, $9, $10
		)
		RETURNING
			id,
			name,
			description,
			price,
			stock,
			currency,
			status,
			created_at,
			updated_at,
			idempotency_key
	`

	product, err := scanProduct(tx.QueryRow(
		ctx,
		query,
		params.ID,
		params.Name,
		params.Description,
		params.Price,
		params.Stock,
		params.Currency,
		int32(params.Status),
		params.CreatedAt.UTC(),
		params.UpdatedAt.UTC(),
		nullString(idempotencyKey),
	))
	if err != nil {
		if isUniqueViolation(err, "idx_products_idempotency_key") {
			// Concurrent request claimed the key; return the existing product.
			existing, lookupErr := r.lookupByGlobalIdempotencyKey(ctx, r.db, idempotencyKey)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if existing != nil {
				return existing, nil
			}
			return nil, domain.NewConflictError("idempotency key collision")
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, domain.NewConflictError("product already exists")
		}
		return nil, domain.NewInternalError("failed to create product", err)
	}

	// Persist idempotency audit record (secondary — the real guard is the unique index).
	if idempotencyKey != "" {
		if _, insertErr := insertOperationMapping(ctx, tx, operationTypeCreate, idempotencyKey, product.ID, params.CreatedAt.UTC()); insertErr != nil {
			// Non-fatal: the unique index already protects against duplicates.
			// Log in production; we swallow the error here to avoid breaking the flow.
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, domain.NewInternalError("failed to commit product creation transaction", err)
	}

	return product, nil
}

func (r *ProductRepository) GetByID(ctx context.Context, productID string) (*domain.Product, error) {
	return r.getByIDWithQuerier(ctx, r.db, productID)
}

// listQueryBuilder provides a safe way to build dynamic SQL queries
// with parameterized arguments. It tracks placeholder indices automatically.
type listQueryBuilder struct {
	baseQuery   string
	args        []any
	placeholder int
	conditions  []string
}

func newListQueryBuilder(baseQuery string, startPlaceholder int) *listQueryBuilder {
	return &listQueryBuilder{
		baseQuery:   baseQuery,
		placeholder: startPlaceholder,
	}
}

func (b *listQueryBuilder) And(condition string, args ...any) {
	b.conditions = append(b.conditions, condition)
	b.args = append(b.args, args...)
	b.placeholder++
}

func (b *listQueryBuilder) Andf(conditionFmt string, args ...any) {
	// conditionFmt should contain exactly one $N placeholder.
	// We replace it with the current placeholder value.
	condition := fmt.Sprintf(conditionFmt, fmt.Sprintf("$%d", b.placeholder))
	b.conditions = append(b.conditions, condition)
	b.args = append(b.args, args...)
	b.placeholder++
}

func (b *listQueryBuilder) AndMulti(conditionFmt string, argCount int, args ...any) {
	// Builds a condition with multiple placeholders (e.g. "$1 AND $2").
	placeholders := make([]string, argCount)
	for i := 0; i < argCount; i++ {
		placeholders[i] = fmt.Sprintf("$%d", b.placeholder+i)
	}
	condition := fmt.Sprintf(conditionFmt, anySlice(placeholders))
	b.conditions = append(b.conditions, condition)
	b.args = append(b.args, args...)
	b.placeholder += argCount
}

func (b *listQueryBuilder) Build() (string, []any) {
	query := b.baseQuery
	if len(b.conditions) > 0 {
		query += "\n\t\tAND " + strings.Join(b.conditions, "\n\t\tAND ")
	}
	return query, b.args
}

// anySlice formats a slice of placeholder references for use in fmt.Sprintf.
func anySlice(items []string) string {
	return strings.Join(items, ", ")
}

func (r *ProductRepository) List(ctx context.Context, filter domain.ListProductsFilter) ([]*domain.Product, string, error) {
	// Use a structured query builder to prevent placeholder index bugs.
	qb := newListQueryBuilder(`
		SELECT
			id,
			name,
			description,
			price,
			stock,
			currency,
			status,
			created_at,
			updated_at
		FROM products
		WHERE 1 = 1
	`, 1)

	if filter.Query != "" {
		pattern := "%" + filter.Query + "%"
		qb.AndMulti(
			"(name ILIKE %s OR description ILIKE %s)", 2, pattern,
		)
	}
	if filter.MinPrice != nil {
		qb.And("price >= "+fmt.Sprintf("$%d", qb.placeholder), *filter.MinPrice)
	}
	if filter.MaxPrice != nil {
		qb.And("price <= "+fmt.Sprintf("$%d", qb.placeholder), *filter.MaxPrice)
	}
	if filter.InStockOnly {
		qb.And("stock > 0 AND status = "+fmt.Sprintf("$%d", qb.placeholder), int32(productv1.ProductStatus_PRODUCT_STATUS_ACTIVE))
	}
	if filter.Status != nil {
		qb.And("status = "+fmt.Sprintf("$%d", qb.placeholder), int32(*filter.Status))
	}
	if filter.Currency != "" {
		qb.And("currency = "+fmt.Sprintf("$%d", qb.placeholder), filter.Currency)
	}
	if filter.PageToken != "" {
		cursor, err := decodeProductCursor(filter.PageToken)
		if err != nil {
			return nil, "", err
		}
		// Composite cursor: (created_at, id) — need two placeholders
		qb.AndMulti(
			"(created_at, id) < (%s, %s)", 2, cursor.CreatedAt.UTC(), cursor.ID,
		)
	}

	query, args := qb.Build()
	query += fmt.Sprintf("\n\t\tORDER BY created_at DESC, id DESC LIMIT $%d", qb.placeholder)
	args = append(args, filter.PageSize+1)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", domain.NewInternalError("failed to list products", err)
	}
	defer rows.Close()

	products := make([]*domain.Product, 0, filter.PageSize+1)
	for rows.Next() {
		product, scanErr := scanProduct(rows)
		if scanErr != nil {
			return nil, "", domain.NewInternalError("failed to scan product", scanErr)
		}
		products = append(products, product)
	}

	if rows.Err() != nil {
		return nil, "", domain.NewInternalError("failed to iterate products", rows.Err())
	}

	var nextPageToken string
	if len(products) > int(filter.PageSize) {
		lastVisible := products[int(filter.PageSize)-1]
		nextPageToken = encodeProductCursor(productCursor{
			CreatedAt: lastVisible.CreatedAt,
			ID:        lastVisible.ID,
		})
		products = products[:int(filter.PageSize)]
	}

	return products, nextPageToken, nil
}

func (r *ProductRepository) Update(ctx context.Context, params domain.UpdateProductParams) (*domain.Product, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, domain.NewInternalError("failed to begin product update transaction", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	idempotencyKey := strings.TrimSpace(params.IdempotencyKey)

	// For updates, check idempotency by looking up the key on the target product.
	// If the same key exists on a different product, reject as conflict.
	if idempotencyKey != "" {
		existing, err := r.lookupByGlobalIdempotencyKey(ctx, tx, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.ID != params.ID {
				return nil, domain.NewConflictError("idempotency key is already associated with another product")
			}
			return existing, nil
		}
	}

	assignments := make([]string, 0, 7)
	args := make([]any, 0, 9)
	placeholder := 1

	if params.Name != nil {
		assignments = append(assignments, fmt.Sprintf("name = $%d", placeholder))
		args = append(args, *params.Name)
		placeholder++
	}
	if params.Description != nil {
		assignments = append(assignments, fmt.Sprintf("description = $%d", placeholder))
		args = append(args, *params.Description)
		placeholder++
	}
	if params.Price != nil {
		assignments = append(assignments, fmt.Sprintf("price = $%d", placeholder))
		args = append(args, *params.Price)
		placeholder++
	}
	if params.Stock != nil {
		assignments = append(assignments, fmt.Sprintf("stock = $%d", placeholder))
		args = append(args, *params.Stock)
		placeholder++
	}
	if params.Currency != nil {
		assignments = append(assignments, fmt.Sprintf("currency = $%d", placeholder))
		args = append(args, *params.Currency)
		placeholder++
	}
	if params.Status != nil {
		assignments = append(assignments, fmt.Sprintf("status = $%d", placeholder))
		args = append(args, int32(*params.Status))
		placeholder++
	}
	if idempotencyKey != "" {
		assignments = append(assignments, fmt.Sprintf("idempotency_key = $%d", placeholder))
		args = append(args, nullString(idempotencyKey))
		placeholder++
	}

	assignments = append(assignments, fmt.Sprintf("updated_at = $%d", placeholder))
	args = append(args, params.UpdatedAt.UTC())
	placeholder++

	args = append(args, params.ID)
	query := fmt.Sprintf(`
		UPDATE products
		SET %s
		WHERE id = $%d
		RETURNING
			id,
			name,
			description,
			price,
			stock,
			currency,
			status,
			created_at,
			updated_at,
			idempotency_key
	`, strings.Join(assignments, ", "), placeholder)

	product, err := scanProduct(tx.QueryRow(ctx, query, args...))
	if err != nil {
		return nil, mapProductWriteError(err)
	}

	// Persist idotency audit record.
	if idempotencyKey != "" {
		if _, insertErr := insertOperationMapping(ctx, tx, operationTypeUpdate, idempotencyKey, product.ID, params.UpdatedAt.UTC()); insertErr != nil {
			// Non-fatal: the unique index on products already guards.
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, domain.NewInternalError("failed to commit product update transaction", err)
	}

	return product, nil
}

func (r *ProductRepository) Delete(ctx context.Context, params domain.DeleteProductParams) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.NewInternalError("failed to begin product delete transaction", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	idempotencyKey := strings.TrimSpace(params.IdempotencyKey)

	if idempotencyKey != "" {
		existing, err := r.lookupByGlobalIdempotencyKey(ctx, tx, idempotencyKey)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.ID != params.ID {
				return domain.NewConflictError("idempotency key is already associated with another product")
			}
			return nil
		}
	}

	tag, err := tx.Exec(
		ctx,
		`UPDATE products SET status = $1, updated_at = $2, idempotency_key = $3 WHERE id = $4 AND status <> $1`,
		int32(productv1.ProductStatus_PRODUCT_STATUS_DISABLED),
		params.UpdatedAt.UTC(),
		nullString(idempotencyKey),
		params.ID,
	)
	if err != nil {
		return domain.NewInternalError("failed to delete product", err)
	}
	if tag.RowsAffected() == 0 {
		exists, existsErr := productExists(ctx, tx, params.ID)
		if existsErr != nil {
			return existsErr
		}
		if !exists {
			return domain.NewNotFoundError("product was not found")
		}
		// Product already disabled — idempotent.
	}

	if idempotencyKey != "" {
		if _, insertErr := insertOperationMapping(ctx, tx, operationTypeDelete, idempotencyKey, params.ID, params.UpdatedAt.UTC()); insertErr != nil {
			// Non-fatal: the unique index guards.
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.NewInternalError("failed to commit product delete transaction", err)
	}

	return nil
}

func (r *ProductRepository) GetAvailability(ctx context.Context, productID string) (*domain.ProductAvailability, error) {
	const query = `
		SELECT
			id,
			stock - reserved_stock AS available_stock,
			reserved_stock,
			status
		FROM products
		WHERE id = $1
	`

	availability, err := scanAvailability(r.db.QueryRow(ctx, query, productID))
	if err != nil {
		return nil, mapProductReadError(err)
	}

	return availability, nil
}

// lookupByGlobalIdempotencyKey looks up a product by the idempotency_key column on the products table.
// Returns nil (no error) if not found.
func (r *ProductRepository) lookupByGlobalIdempotencyKey(ctx context.Context, q querier, idempotencyKey string) (*domain.Product, error) {
	const query = `
		SELECT
			id,
			name,
			description,
			price,
			stock,
			currency,
			status,
			created_at,
			updated_at,
			COALESCE(idempotency_key, '')
		FROM products
		WHERE idempotency_key = $1
	`
	product, err := scanProduct(q.QueryRow(ctx, query, idempotencyKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapProductReadError(err)
	}
	return product, nil
}

func (r *ProductRepository) getByIDWithQuerier(ctx context.Context, q querier, productID string) (*domain.Product, error) {
	const query = `
		SELECT
			id,
			name,
			description,
			price,
			stock,
			currency,
			status,
			created_at,
			updated_at,
			COALESCE(idempotency_key, '')
		FROM products
		WHERE id = $1
	`

	product, err := scanProduct(q.QueryRow(ctx, query, productID))
	if err != nil {
		return nil, mapProductReadError(err)
	}

	return product, nil
}

func (r *ProductRepository) getProductByOperationKey(ctx context.Context, q querier, operation operationType, idempotencyKey string) (*domain.Product, error) {
	productID, err := lookupOperationProductID(ctx, q, operation, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if productID == "" {
		return nil, domain.NewConflictError("idempotency key collision")
	}
	return r.getByIDWithQuerier(ctx, q, productID)
}

func scanProduct(scanner rowScanner) (*domain.Product, error) {
	var (
		product domain.Product
		status  int32
	)

	if err := scanner.Scan(
		&product.ID,
		&product.Name,
		&product.Description,
		&product.Price,
		&product.Stock,
		&product.Currency,
		&status,
		&product.CreatedAt,
		&product.UpdatedAt,
		&product.IdempotencyKey,
	); err != nil {
		return nil, err
	}

	product.Status = productv1.ProductStatus(status)
	product.Currency = strings.ToUpper(product.Currency)

	return &product, nil
}

func scanAvailability(scanner rowScanner) (*domain.ProductAvailability, error) {
	var (
		availability domain.ProductAvailability
		status       int32
	)

	if err := scanner.Scan(
		&availability.ProductID,
		&availability.AvailableStock,
		&availability.ReservedStock,
		&status,
	); err != nil {
		return nil, err
	}

	availability.Status = productv1.ProductStatus(status)
	return &availability, nil
}

func lookupOperationProductID(ctx context.Context, q querier, operation operationType, idempotencyKey string) (string, error) {
	var productID string
	err := q.QueryRow(
		ctx,
		`SELECT product_id FROM product_operation_idempotency WHERE operation_type = $1 AND idempotency_key = $2`,
		string(operation),
		idempotencyKey,
	).Scan(&productID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", domain.NewInternalError("failed to load product idempotency mapping", err)
	}
	return productID, nil
}

func insertOperationMapping(ctx context.Context, q querier, operation operationType, idempotencyKey, productID string, createdAt time.Time) (bool, error) {
	tag, err := q.Exec(
		ctx,
		`
			INSERT INTO product_operation_idempotency (
				operation_type,
				idempotency_key,
				product_id,
				created_at
			) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING
		`,
		string(operation),
		idempotencyKey,
		productID,
		createdAt.UTC(),
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func productExists(ctx context.Context, q querier, productID string) (bool, error) {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1)`, productID).Scan(&exists); err != nil {
		return false, domain.NewInternalError("failed to check product existence", err)
	}
	return exists, nil
}

func mapProductReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewNotFoundError("product was not found")
	}
	return domain.NewInternalError("failed to load product", err)
}

func mapProductWriteError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewNotFoundError("product was not found")
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" {
		return domain.NewFailedPreconditionError("product data violates persistence constraints")
	}

	return domain.NewInternalError("failed to persist product", err)
}

// isUniqueViolation checks if the error is a PostgreSQL unique violation on the specified index.
func isUniqueViolation(err error, indexName string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return strings.Contains(pgErr.ConstraintName, indexName) ||
			strings.Contains(pgErr.Message, indexName)
	}
	return false
}

// nullString returns a sql.NullString for the given value.
// Empty string becomes NULL (for partial unique index compatibility).
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

type productCursor struct {
	CreatedAt time.Time
	ID        string
}

func encodeProductCursor(cursor productCursor) string {
	payload := cursor.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + cursor.ID
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeProductCursor(token string) (productCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return productCursor{}, domain.NewInvalidArgumentError("page_token is invalid")
	}

	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return productCursor{}, domain.NewInvalidArgumentError("page_token is invalid")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return productCursor{}, domain.NewInvalidArgumentError("page_token is invalid")
	}

	return productCursor{
		CreatedAt: createdAt.UTC(),
		ID:        parts[1],
	}, nil
}
