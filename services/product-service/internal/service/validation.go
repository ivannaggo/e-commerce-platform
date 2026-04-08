package service

import (
	"strings"
	"unicode/utf8"

	productv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/product/v1"
	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/domain"
)

const (
	defaultPageSize         = 20
	maxPageSize             = 100
	maxProductNameLength    = 200
	maxDescriptionLength    = 4000
	maxQueryLength          = 200
	maxIdempotencyKeyLength = 200
)

func normalizeProductID(productID string) (string, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return "", domain.NewInvalidArgumentError("product_id is required")
	}
	return productID, nil
}

func normalizeProductName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", domain.NewInvalidArgumentError("name is required")
	}
	if utf8.RuneCountInString(value) > maxProductNameLength {
		return "", domain.NewInvalidArgumentError("name is too long")
	}
	return value, nil
}

func normalizeOptionalProductName(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeProductName(*value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizeDescription(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxDescriptionLength {
		return "", domain.NewInvalidArgumentError("description is too long")
	}
	return value, nil
}

func normalizeOptionalDescription(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeDescription(*value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizePrice(value int64) (int64, error) {
	if value < 0 {
		return 0, domain.NewInvalidArgumentError("price must be greater than or equal to zero")
	}
	return value, nil
}

func normalizeOptionalPrice(value *int64) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizePrice(*value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizeStock(value int32) (int32, error) {
	if value < 0 {
		return 0, domain.NewInvalidArgumentError("stock must be greater than or equal to zero")
	}
	return value, nil
}

func normalizeOptionalStock(value *int32) (*int32, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeStock(*value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizeCurrency(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "", domain.NewInvalidArgumentError("currency is required")
	}
	if len(value) != 3 {
		return "", domain.NewInvalidArgumentError("currency must be a 3-letter ISO code")
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return "", domain.NewInvalidArgumentError("currency must be a 3-letter ISO code")
		}
	}
	return value, nil
}

func normalizeOptionalCurrency(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeCurrency(*value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
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

func normalizeQuery(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxQueryLength {
		return "", domain.NewInvalidArgumentError("query is too long")
	}
	return value, nil
}

func normalizeIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxIdempotencyKeyLength {
		return "", domain.NewInvalidArgumentError("idempotency_key is too long")
	}
	return value, nil
}

func normalizeProductStatusFilter(status productv1.ProductStatus) (*productv1.ProductStatus, error) {
	if status == productv1.ProductStatus_PRODUCT_STATUS_UNSPECIFIED {
		return nil, nil
	}

	switch status {
	case productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
		productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK,
		productv1.ProductStatus_PRODUCT_STATUS_DISABLED:
		return &status, nil
	default:
		return nil, domain.NewInvalidArgumentError("status filter is invalid")
	}
}

func normalizeOptionalProductStatus(status *productv1.ProductStatus) (*productv1.ProductStatus, error) {
	if status == nil {
		return nil, nil
	}
	if *status == productv1.ProductStatus_PRODUCT_STATUS_UNSPECIFIED {
		return nil, domain.NewInvalidArgumentError("status is invalid")
	}

	switch *status {
	case productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
		productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK,
		productv1.ProductStatus_PRODUCT_STATUS_DISABLED:
		return status, nil
	default:
		return nil, domain.NewInvalidArgumentError("status is invalid")
	}
}

func validatePriceRange(minPrice, maxPrice *int64) error {
	if minPrice != nil && maxPrice != nil && *minPrice > *maxPrice {
		return domain.NewInvalidArgumentError("min_price cannot be greater than max_price")
	}
	return nil
}

func validateRequestedQuantity(quantity int32) error {
	if quantity <= 0 {
		return domain.NewInvalidArgumentError("requested_quantity must be greater than zero")
	}
	return nil
}

func validateStatusAgainstStock(status productv1.ProductStatus, stock int32) error {
	switch status {
	case productv1.ProductStatus_PRODUCT_STATUS_ACTIVE:
		if stock <= 0 {
			return domain.NewInvalidArgumentError("active products must have stock greater than zero")
		}
	case productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK:
		if stock != 0 {
			return domain.NewInvalidArgumentError("out_of_stock products must have zero stock")
		}
	case productv1.ProductStatus_PRODUCT_STATUS_DISABLED:
		return nil
	default:
		return domain.NewInvalidArgumentError("status is invalid")
	}
	return nil
}
