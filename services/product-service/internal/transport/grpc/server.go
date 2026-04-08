package grpc

import (
	"context"
	"errors"
	"time"

	productv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/product/v1"
	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/domain"
	servicesvc "github.com/ivannaggo/e-commerce-platform/services/product-service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

// HealthChecker is a function that returns nil when the service is healthy.
type HealthChecker func(ctx context.Context) error

type Server struct {
	productv1.UnimplementedProductServiceServer

	service       *servicesvc.ProductService
	healthChecker HealthChecker
	timeout       time.Duration
}

type ServerOption func(*Server)

// WithHealthChecker sets the health check function.
func WithHealthChecker(hc HealthChecker) ServerOption {
	return func(s *Server) {
		s.healthChecker = hc
	}
}

// WithRequestTimeout sets the default timeout for RPC handlers.
// Zero or negative means no timeout.
func WithRequestTimeout(d time.Duration) ServerOption {
	return func(s *Server) {
		s.timeout = d
	}
}

func NewServer(service *servicesvc.ProductService, opts ...ServerOption) (*Server, error) {
	if service == nil {
		return nil, errors.New("service is required")
	}

	s := &Server{service: service}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

func (s *Server) CreateProduct(ctx context.Context, req *productv1.CreateProductRequest) (*productv1.CreateProductResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	product, err := s.service.CreateProduct(ctx, servicesvc.CreateProductInput{
		Name:           req.GetName(),
		Description:    req.GetDescription(),
		Price:          req.GetPrice(),
		Stock:          req.GetStock(),
		Currency:       req.GetCurrency(),
		IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &productv1.CreateProductResponse{Product: toProtoProduct(product)}, nil
}

func (s *Server) GetProduct(ctx context.Context, req *productv1.GetProductRequest) (*productv1.GetProductResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	product, err := s.service.GetProduct(ctx, req.GetProductId())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &productv1.GetProductResponse{Product: toProtoProduct(product)}, nil
}

func (s *Server) ListProducts(ctx context.Context, req *productv1.ListProductsRequest) (*productv1.ListProductsResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	var pageSize int32
	var pageToken string
	if req.GetPagination() != nil {
		pageSize = req.GetPagination().GetPageSize()
		pageToken = req.GetPagination().GetPageToken()
	}

	var minPrice, maxPrice *int64
	if req.MinPrice != nil {
		value := req.GetMinPrice().GetValue()
		minPrice = &value
	}
	if req.MaxPrice != nil {
		value := req.GetMaxPrice().GetValue()
		maxPrice = &value
	}

	result, err := s.service.ListProducts(ctx, servicesvc.ListProductsInput{
		PageSize:    pageSize,
		PageToken:   pageToken,
		Query:       req.GetQuery(),
		MinPrice:    minPrice,
		MaxPrice:    maxPrice,
		InStockOnly: req.GetInStockOnly(),
		Status:      req.GetStatus(),
		Currency:    req.GetCurrency(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &productv1.ListProductsResponse{
		Products:      toProtoProducts(result.Products),
		NextPageToken: result.NextPageToken,
	}, nil
}

func (s *Server) UpdateProduct(ctx context.Context, req *productv1.UpdateProductRequest) (*productv1.UpdateProductResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	var name, description, currency *string
	var price *int64
	var stock *int32
	var statusPatch *productv1.ProductStatus
	if req.GetName() != nil {
		value := req.GetName().GetValue()
		name = &value
	}
	if req.GetDescription() != nil {
		value := req.GetDescription().GetValue()
		description = &value
	}
	if req.GetPrice() != nil {
		value := req.GetPrice().GetValue()
		price = &value
	}
	if req.GetStock() != nil {
		value := req.GetStock().GetValue()
		stock = &value
	}
	if req.GetCurrency() != nil {
		value := req.GetCurrency().GetValue()
		currency = &value
	}
	if req.GetStatus() != nil {
		value := productv1.ProductStatus(req.GetStatus().GetValue())
		statusPatch = &value
	}

	product, err := s.service.UpdateProduct(ctx, servicesvc.UpdateProductInput{
		ProductID:      req.GetProductId(),
		Name:           name,
		Description:    description,
		Price:          price,
		Stock:          stock,
		Currency:       currency,
		Status:         statusPatch,
		IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &productv1.UpdateProductResponse{Product: toProtoProduct(product)}, nil
}

func (s *Server) DeleteProduct(ctx context.Context, req *productv1.DeleteProductRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	if err := s.service.DeleteProduct(ctx, req.GetProductId(), req.GetIdempotencyKey()); err != nil {
		return nil, toGRPCError(err)
	}

	return &emptypb.Empty{}, nil
}

func (s *Server) CheckAvailability(ctx context.Context, req *productv1.CheckAvailabilityRequest) (*productv1.CheckAvailabilityResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	result, err := s.service.CheckAvailability(ctx, req.GetProductId(), req.GetRequestedQuantity())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &productv1.CheckAvailabilityResponse{
		ProductId:         result.ProductID,
		Available:         result.Available,
		RequestedQuantity: result.RequestedQuantity,
		AvailableStock:    result.AvailableStock,
		ReservedStock:     result.ReservedStock,
	}, nil
}

func (s *Server) HealthCheck(ctx context.Context, _ *productv1.HealthCheckRequest) (*productv1.HealthCheckResponse, error) {
	if s.healthChecker == nil {
		return &productv1.HealthCheckResponse{Status: productv1.HealthCheckResponse_SERVING}, nil
	}
	if err := s.healthChecker(ctx); err != nil {
		return &productv1.HealthCheckResponse{Status: productv1.HealthCheckResponse_NOT_SERVING}, nil
	}
	return &productv1.HealthCheckResponse{Status: productv1.HealthCheckResponse_SERVING}, nil
}

func toGRPCError(err error) error {
	if err == nil {
		return nil
	}

	var serviceErr *domain.ServiceError
	if !errors.As(err, &serviceErr) {
		return status.Error(codes.Internal, "internal server error")
	}

	switch serviceErr.Code {
	case domain.ErrorCodeInvalidArgument:
		return status.Error(codes.InvalidArgument, serviceErr.Message)
	case domain.ErrorCodeNotFound:
		return status.Error(codes.NotFound, serviceErr.Message)
	case domain.ErrorCodeAlreadyExists:
		return status.Error(codes.AlreadyExists, serviceErr.Message)
	case domain.ErrorCodePermissionDenied:
		return status.Error(codes.PermissionDenied, serviceErr.Message)
	case domain.ErrorCodeFailedPrecondition:
		return status.Error(codes.FailedPrecondition, serviceErr.Message)
	case domain.ErrorCodeConflict:
		return status.Error(codes.Aborted, serviceErr.Message)
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
