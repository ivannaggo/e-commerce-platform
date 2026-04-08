package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	productv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/product/v1"
	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/domain"
	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/ports"
)

type Dependencies struct {
	Products ports.ProductRepository
	Clock    ports.Clock
	IDs      ports.IDGenerator
}

type ProductService struct {
	products ports.ProductRepository
	clock    ports.Clock
	ids      ports.IDGenerator
}

type CreateProductInput struct {
	Name           string
	Description    string
	Price          int64
	Stock          int32
	Currency       string
	IdempotencyKey string
}

type ListProductsInput struct {
	PageSize    int32
	PageToken   string
	Query       string
	MinPrice    *int64
	MaxPrice    *int64
	InStockOnly bool
	Status      productv1.ProductStatus
	Currency    string
}

type UpdateProductInput struct {
	ProductID      string
	Name           *string
	Description    *string
	Price          *int64
	Stock          *int32
	Currency       *string
	Status         *productv1.ProductStatus
	IdempotencyKey string
}

type ListProductsOutput struct {
	Products      []*domain.Product
	NextPageToken string
}

type CheckAvailabilityOutput struct {
	ProductID         string
	Available         bool
	RequestedQuantity int32
	AvailableStock    int32
	ReservedStock     int32
}

func New(deps Dependencies) (*ProductService, error) {
	if deps.Products == nil {
		return nil, fmt.Errorf("products repository is required")
	}

	clock := deps.Clock
	if clock == nil {
		clock = systemClock{}
	}

	idGenerator := deps.IDs
	if idGenerator == nil {
		idGenerator = randomIDGenerator{}
	}

	return &ProductService{
		products: deps.Products,
		clock:    clock,
		ids:      idGenerator,
	}, nil
}

func (s *ProductService) CreateProduct(ctx context.Context, input CreateProductInput) (*domain.Product, error) {
	name, err := normalizeProductName(input.Name)
	if err != nil {
		return nil, err
	}
	description, err := normalizeDescription(input.Description)
	if err != nil {
		return nil, err
	}
	price, err := normalizePrice(input.Price)
	if err != nil {
		return nil, err
	}
	stock, err := normalizeStock(input.Stock)
	if err != nil {
		return nil, err
	}
	currency, err := normalizeCurrency(input.Currency)
	if err != nil {
		return nil, err
	}
	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now().UTC()
	product, err := s.products.Create(ctx, domain.CreateProductParams{
		ID:             s.ids.NewID(),
		Name:           name,
		Description:    description,
		Price:          price,
		Stock:          stock,
		Currency:       currency,
		Status:         deriveStatusFromStock(stock),
		IdempotencyKey: idempotencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return nil, err
	}

	return product, nil
}

func (s *ProductService) GetProduct(ctx context.Context, productID string) (*domain.Product, error) {
	normalizedProductID, err := normalizeProductID(productID)
	if err != nil {
		return nil, err
	}
	return s.products.GetByID(ctx, normalizedProductID)
}

func (s *ProductService) ListProducts(ctx context.Context, input ListProductsInput) (*ListProductsOutput, error) {
	pageSize, err := normalizePageSize(input.PageSize)
	if err != nil {
		return nil, err
	}
	query, err := normalizeQuery(input.Query)
	if err != nil {
		return nil, err
	}
	statusFilter, err := normalizeProductStatusFilter(input.Status)
	if err != nil {
		return nil, err
	}
	var currency string
	if strings.TrimSpace(input.Currency) != "" {
		currency, err = normalizeCurrency(input.Currency)
		if err != nil {
			return nil, err
		}
	}
	minPrice, err := normalizeOptionalPrice(input.MinPrice)
	if err != nil {
		return nil, err
	}
	maxPrice, err := normalizeOptionalPrice(input.MaxPrice)
	if err != nil {
		return nil, err
	}
	if err := validatePriceRange(minPrice, maxPrice); err != nil {
		return nil, err
	}
	if input.InStockOnly && statusFilter != nil && *statusFilter == productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK {
		return nil, domain.NewInvalidArgumentError("in_stock_only cannot be combined with out_of_stock status")
	}

	products, nextPageToken, err := s.products.List(ctx, domain.ListProductsFilter{
		PageSize:    pageSize,
		PageToken:   strings.TrimSpace(input.PageToken),
		Query:       query,
		MinPrice:    minPrice,
		MaxPrice:    maxPrice,
		InStockOnly: input.InStockOnly,
		Status:      statusFilter,
		Currency:    currency,
	})
	if err != nil {
		return nil, err
	}

	return &ListProductsOutput{
		Products:      products,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *ProductService) UpdateProduct(ctx context.Context, input UpdateProductInput) (*domain.Product, error) {
	productID, err := normalizeProductID(input.ProductID)
	if err != nil {
		return nil, err
	}
	current, err := s.products.GetByID(ctx, productID)
	if err != nil {
		return nil, err
	}
	if err := ensureProductCanBeMutated(current); err != nil {
		return nil, err
	}

	name, err := normalizeOptionalProductName(input.Name)
	if err != nil {
		return nil, err
	}
	description, err := normalizeOptionalDescription(input.Description)
	if err != nil {
		return nil, err
	}
	price, err := normalizeOptionalPrice(input.Price)
	if err != nil {
		return nil, err
	}
	stock, err := normalizeOptionalStock(input.Stock)
	if err != nil {
		return nil, err
	}
	currency, err := normalizeOptionalCurrency(input.Currency)
	if err != nil {
		return nil, err
	}
	statusPatch, err := normalizeOptionalProductStatus(input.Status)
	if err != nil {
		return nil, err
	}
	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if name == nil && description == nil && price == nil && stock == nil && currency == nil && statusPatch == nil {
		return nil, domain.NewInvalidArgumentError("at least one field must be provided for update")
	}

	resolvedStatus, err := resolveStatusPatch(current, stock, statusPatch)
	if err != nil {
		return nil, err
	}

	return s.products.Update(ctx, domain.UpdateProductParams{
		ID:             productID,
		Name:           name,
		Description:    description,
		Price:          price,
		Stock:          stock,
		Currency:       currency,
		Status:         resolvedStatus,
		IdempotencyKey: idempotencyKey,
		UpdatedAt:      s.clock.Now().UTC(),
	})
}

func (s *ProductService) DeleteProduct(ctx context.Context, productID, idempotencyKey string) error {
	normalizedProductID, err := normalizeProductID(productID)
	if err != nil {
		return err
	}
	normalizedIdempotencyKey, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return err
	}

	product, err := s.products.GetByID(ctx, normalizedProductID)
	if err != nil {
		return err
	}
	if product.Status == productv1.ProductStatus_PRODUCT_STATUS_DISABLED {
		return nil
	}

	return s.products.Delete(ctx, domain.DeleteProductParams{
		ID:             normalizedProductID,
		IdempotencyKey: normalizedIdempotencyKey,
		UpdatedAt:      s.clock.Now().UTC(),
	})
}

func (s *ProductService) CheckAvailability(ctx context.Context, productID string, requestedQuantity int32) (*CheckAvailabilityOutput, error) {
	normalizedProductID, err := normalizeProductID(productID)
	if err != nil {
		return nil, err
	}
	if err := validateRequestedQuantity(requestedQuantity); err != nil {
		return nil, err
	}

	availability, err := s.products.GetAvailability(ctx, normalizedProductID)
	if err != nil {
		return nil, err
	}

	availableStock := availability.AvailableStock
	if availableStock < 0 {
		availableStock = 0
	}
	reservedStock := availability.ReservedStock
	if reservedStock < 0 {
		reservedStock = 0
	}

	return &CheckAvailabilityOutput{
		ProductID:         normalizedProductID,
		Available:         isAvailableForPurchase(availability.Status, availableStock, requestedQuantity),
		RequestedQuantity: requestedQuantity,
		AvailableStock:    availableStock,
		ReservedStock:     reservedStock,
	}, nil
}

func deriveStatusFromStock(stock int32) productv1.ProductStatus {
	if stock == 0 {
		return productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK
	}
	return productv1.ProductStatus_PRODUCT_STATUS_ACTIVE
}

func ensureProductCanBeMutated(product *domain.Product) error {
	if product == nil {
		return domain.NewNotFoundError("product was not found")
	}

	switch product.Status {
	case productv1.ProductStatus_PRODUCT_STATUS_ACTIVE, productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK:
		return nil
	case productv1.ProductStatus_PRODUCT_STATUS_DISABLED:
		return domain.NewFailedPreconditionError("product is disabled")
	default:
		return domain.NewFailedPreconditionError("product is not in a valid state")
	}
}

func resolveStatusPatch(current *domain.Product, stockPatch *int32, explicitStatus *productv1.ProductStatus) (*productv1.ProductStatus, error) {
	finalStock := current.Stock
	if stockPatch != nil {
		finalStock = *stockPatch
	}

	if explicitStatus != nil {
		resolved := *explicitStatus
		if err := validateStatusAgainstStock(resolved, finalStock); err != nil {
			return nil, err
		}
		return &resolved, nil
	}

	if stockPatch == nil {
		return nil, nil
	}

	resolved := deriveStatusFromStock(finalStock)
	if resolved == current.Status {
		return nil, nil
	}
	return &resolved, nil
}

func isAvailableForPurchase(status productv1.ProductStatus, availableStock, requestedQuantity int32) bool {
	switch status {
	case productv1.ProductStatus_PRODUCT_STATUS_DISABLED, productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK:
		return false
	case productv1.ProductStatus_PRODUCT_STATUS_ACTIVE:
		return availableStock >= requestedQuantity
	default:
		return false
	}
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
