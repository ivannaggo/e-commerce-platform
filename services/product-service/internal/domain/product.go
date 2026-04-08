package domain

import (
	"time"

	productv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/product/v1"
)

type Product struct {
	ID             string
	Name           string
	Description    string
	Price          int64
	Stock          int32
	Currency       string
	Status         productv1.ProductStatus
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ProductAvailability struct {
	ProductID      string
	AvailableStock int32
	ReservedStock  int32
	Status         productv1.ProductStatus
}

type CreateProductParams struct {
	ID             string
	Name           string
	Description    string
	Price          int64
	Stock          int32
	Currency       string
	Status         productv1.ProductStatus
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ListProductsFilter struct {
	PageSize    int32
	PageToken   string
	Query       string
	MinPrice    *int64
	MaxPrice    *int64
	InStockOnly bool
	Status      *productv1.ProductStatus
	Currency    string
}

type UpdateProductParams struct {
	ID             string
	Name           *string
	Description    *string
	Price          *int64
	Stock          *int32
	Currency       *string
	Status         *productv1.ProductStatus
	IdempotencyKey string
	UpdatedAt      time.Time
}

type DeleteProductParams struct {
	ID             string
	IdempotencyKey string
	UpdatedAt      time.Time
}
