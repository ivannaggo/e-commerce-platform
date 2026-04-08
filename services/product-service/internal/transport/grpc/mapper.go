package grpc

import (
	"time"

	productv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/product/v1"
	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func toProtoProduct(product *domain.Product) *productv1.Product {
	if product == nil {
		return nil
	}

	return &productv1.Product{
		Id:          product.ID,
		Name:        product.Name,
		Description: product.Description,
		Price:       product.Price,
		Stock:       product.Stock,
		Currency:    product.Currency,
		Status:      product.Status,
		CreatedAt:   timestampPointer(product.CreatedAt),
		UpdatedAt:   timestampPointer(product.UpdatedAt),
	}
}

func toProtoProducts(products []*domain.Product) []*productv1.Product {
	items := make([]*productv1.Product, 0, len(products))
	for _, product := range products {
		items = append(items, toProtoProduct(product))
	}
	return items
}

func timestampPointer(ts time.Time) *timestamppb.Timestamp {
	if ts.IsZero() {
		return nil
	}
	return timestamppb.New(ts.UTC())
}
