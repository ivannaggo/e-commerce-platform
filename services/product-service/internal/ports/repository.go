package ports

import (
	"context"

	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/domain"
)

type ProductRepository interface {
	Create(context.Context, domain.CreateProductParams) (*domain.Product, error)
	GetByID(context.Context, string) (*domain.Product, error)
	List(context.Context, domain.ListProductsFilter) ([]*domain.Product, string, error)
	Update(context.Context, domain.UpdateProductParams) (*domain.Product, error)
	// Delete is expected to perform a logical delete by disabling the product.
	Delete(context.Context, domain.DeleteProductParams) error
	GetAvailability(context.Context, string) (*domain.ProductAvailability, error)
}
