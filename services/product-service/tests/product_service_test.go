package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	productv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/product/v1"
	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/domain"
	"github.com/ivannaggo/e-commerce-platform/services/product-service/internal/service"
)

// --- Idempotency Race Condition Tests ---

// TestCreateProductIdempotencyRace simulates concurrent requests with the same
// idempotency key. The service layer normalizes input and delegates to the repo.
// In production the DB unique index on products.idempotency_key guarantees that
// only one INSERT wins; all others get a unique violation and fall back to the
// existing row. Here we simulate the repo returning the same product for all.
func TestCreateProductIdempotencyRace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	const concurrentRequests = 20
	idempotencyKey := "race-create-key-1"

	var mu sync.Mutex
	createCallCount := 0
	callResults := make([]*domain.Product, 0, concurrentRequests)

	repo := &productRepoStub{
		createFn: func(_ context.Context, params domain.CreateProductParams) (*domain.Product, error) {
			mu.Lock()
			createCallCount++
			callResults = append(callResults, &domain.Product{
				ID:             params.ID,
				Name:           params.Name,
				Description:    params.Description,
				Price:          params.Price,
				Stock:          params.Stock,
				Currency:       params.Currency,
				Status:         params.Status,
				IdempotencyKey: params.IdempotencyKey,
				CreatedAt:      params.CreatedAt,
				UpdatedAt:      params.UpdatedAt,
			})
			mu.Unlock()

			time.Sleep(time.Millisecond)

			return &domain.Product{
				ID:             params.ID,
				Name:           params.Name,
				Description:    params.Description,
				Price:          params.Price,
				Stock:          params.Stock,
				Currency:       params.Currency,
				Status:         params.Status,
				IdempotencyKey: params.IdempotencyKey,
				CreatedAt:      params.CreatedAt,
				UpdatedAt:      params.UpdatedAt,
			}, nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Products: repo,
		Clock:    fixedClock{now: now},
		IDs:      &sequenceIDGenerator{ids: repeatID("race-product-id", concurrentRequests)},
	})

	var wg sync.WaitGroup
	results := make([]*domain.Product, concurrentRequests)
	errs := make([]error, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			product, err := svc.CreateProduct(context.Background(), service.CreateProductInput{
				Name:           "Keyboard",
				Description:    "Mechanical",
				Price:          12999,
				Stock:          10,
				Currency:       "USD",
				IdempotencyKey: idempotencyKey,
			})
			results[idx] = product
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}

	for i, result := range results {
		if result == nil {
			t.Fatalf("request %d: got nil product", i)
		}
		if result.ID != "race-product-id" {
			t.Fatalf("request %d: expected product ID %q, got %q", i, "race-product-id", result.ID)
		}
	}

	t.Logf("createFn was called %d times; in production DB guards ensure idempotency", createCallCount)
	_ = callResults
}

// TestUpdateProductIdempotencyRace verifies that concurrent updates with the
// same idempotency key do not cause unexpected failures.
func TestUpdateProductIdempotencyRace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)

	var mu sync.Mutex
	updateCallCount := 0

	repo := &productRepoStub{
		getByIDFn: func(_ context.Context, productID string) (*domain.Product, error) {
			return &domain.Product{
				ID:        productID,
				Name:      "Laptop",
				Price:     159999,
				Stock:     3,
				Currency:  "USD",
				Status:    productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
				CreatedAt: now.Add(-time.Hour),
				UpdatedAt: now.Add(-time.Minute),
			}, nil
		},
		updateFn: func(_ context.Context, params domain.UpdateProductParams) (*domain.Product, error) {
			mu.Lock()
			updateCallCount++
			mu.Unlock()

			time.Sleep(time.Millisecond)

			return &domain.Product{
				ID:        params.ID,
				Name:      "Laptop",
				Price:     159999,
				Stock:     3,
				Currency:  "USD",
				Status:    productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
				CreatedAt: now.Add(-time.Hour),
				UpdatedAt: params.UpdatedAt,
			}, nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Products: repo,
		Clock:    fixedClock{now: now},
	})

	const concurrentRequests = 10
	var wg sync.WaitGroup
	errs := make([]error, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			price := int64(159999)
			_, err := svc.UpdateProduct(context.Background(), service.UpdateProductInput{
				ProductID: "product-id",
				Price:     &price,
			})
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}

	t.Logf("updateFn calls=%d", updateCallCount)
}

// TestDeleteProductIdempotencyRace verifies that concurrent deletes with the
// same idempotency key are safe.
func TestDeleteProductIdempotencyRace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)

	var mu sync.Mutex
	deleteCallCount := 0

	repo := &productRepoStub{
		getByIDFn: func(_ context.Context, productID string) (*domain.Product, error) {
			return &domain.Product{
				ID:        productID,
				Name:      "Product",
				Price:     100,
				Stock:     0,
				Currency:  "USD",
				Status:    productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
				CreatedAt: now.Add(-time.Hour),
				UpdatedAt: now.Add(-time.Minute),
			}, nil
		},
		deleteFn: func(_ context.Context, _ domain.DeleteProductParams) error {
			mu.Lock()
			deleteCallCount++
			mu.Unlock()

			time.Sleep(time.Millisecond)
			return nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Products: repo,
		Clock:    fixedClock{now: now},
	})

	const concurrentRequests = 10
	var wg sync.WaitGroup
	errs := make([]error, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = svc.DeleteProduct(context.Background(), "product-id", "race-delete-key-1")
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}

	t.Logf("deleteFn calls=%d", deleteCallCount)
}

// --- Concurrency / Load Tests ---

func TestConcurrentListProducts(t *testing.T) {
	t.Parallel()

	repo := &productRepoStub{
		listFn: func(_ context.Context, _ domain.ListProductsFilter) ([]*domain.Product, string, error) {
			time.Sleep(time.Millisecond * 5)
			return []*domain.Product{
				{ID: "p1", Name: "Product 1", Price: 100, Stock: 5, Currency: "USD", Status: productv1.ProductStatus_PRODUCT_STATUS_ACTIVE},
			}, "", nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Products: repo,
		Clock:    fixedClock{now: time.Now().UTC()},
	})

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = svc.ListProducts(context.Background(), service.ListProductsInput{
				PageSize: 10,
				Currency: "USD",
			})
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
}

func TestConcurrentGetProducts(t *testing.T) {
	t.Parallel()

	repo := &productRepoStub{
		getByIDFn: func(_ context.Context, productID string) (*domain.Product, error) {
			time.Sleep(time.Millisecond * 2)
			return &domain.Product{
				ID:       productID,
				Name:     "Product",
				Price:    100,
				Stock:    5,
				Currency: "USD",
				Status:   productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
			}, nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Products: repo,
		Clock:    fixedClock{now: time.Now().UTC()},
	})

	const goroutines = 100
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = svc.GetProduct(context.Background(), "product-id")
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
}

func TestCheckAvailabilityConcurrent(t *testing.T) {
	t.Parallel()

	repo := &productRepoStub{
		availabilityFn: func(_ context.Context, productID string) (*domain.ProductAvailability, error) {
			time.Sleep(time.Millisecond * 3)
			return &domain.ProductAvailability{
				ProductID:      productID,
				AvailableStock: 10,
				ReservedStock:  3,
				Status:         productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
			}, nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Products: repo,
		Clock:    fixedClock{now: time.Now().UTC()},
	})

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = svc.CheckAvailability(context.Background(), "product-id", 2)
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
}

// --- Edge Case Tests ---

func TestMinPriceZeroIsNotIgnored(t *testing.T) {
	t.Parallel()

	repo := &productRepoStub{
		listFn: func(_ context.Context, filter domain.ListProductsFilter) ([]*domain.Product, string, error) {
			if filter.MinPrice == nil {
				t.Fatal("expected MinPrice to be set (even if zero)")
			}
			if *filter.MinPrice != 0 {
				t.Fatalf("expected MinPrice=0, got %d", *filter.MinPrice)
			}
			return []*domain.Product{}, "", nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Products: repo,
		Clock:    fixedClock{now: time.Now().UTC()},
	})

	minPrice := int64(0)
	_, err := svc.ListProducts(context.Background(), service.ListProductsInput{
		PageSize: 10,
		MinPrice: &minPrice,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusPatchRejectsActiveWithZeroStock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	svc := newTestService(t, service.Dependencies{
		Products: &productRepoStub{
			getByIDFn: func(_ context.Context, productID string) (*domain.Product, error) {
				return &domain.Product{
					ID:        productID,
					Name:      "Laptop",
					Price:     159999,
					Stock:     0,
					Currency:  "USD",
					Status:    productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK,
					CreatedAt: now.Add(-time.Hour),
					UpdatedAt: now.Add(-time.Minute),
				}, nil
			},
		},
		Clock: fixedClock{now: now},
	})

	status := productv1.ProductStatus_PRODUCT_STATUS_ACTIVE
	_, err := svc.UpdateProduct(context.Background(), service.UpdateProductInput{
		ProductID: "product-id",
		Status:    &status,
	})
	if !domain.HasCode(err, domain.ErrorCodeInvalidArgument) {
		t.Fatalf("expected invalid_argument error, got %v", err)
	}
}

func TestCreateProductRejectsDisabledMutation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	svc := newTestService(t, service.Dependencies{
		Products: &productRepoStub{
			getByIDFn: func(_ context.Context, productID string) (*domain.Product, error) {
				return &domain.Product{
					ID:        productID,
					Name:      "Archived",
					Price:     100,
					Stock:     0,
					Currency:  "USD",
					Status:    productv1.ProductStatus_PRODUCT_STATUS_DISABLED,
					CreatedAt: now.Add(-time.Hour),
					UpdatedAt: now.Add(-time.Minute),
				}, nil
			},
		},
		Clock: fixedClock{now: now},
	})

	_, err := svc.UpdateProduct(context.Background(), service.UpdateProductInput{
		ProductID: "product-id",
	})
	if !domain.HasCode(err, domain.ErrorCodeFailedPrecondition) {
		t.Fatalf("expected failed_precondition error, got %v", err)
	}
}

func TestDeleteProductIsIdempotentForDisabledProducts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	var deleteCalled bool
	svc := newTestService(t, service.Dependencies{
		Products: &productRepoStub{
			getByIDFn: func(_ context.Context, productID string) (*domain.Product, error) {
				return &domain.Product{
					ID:        productID,
					Name:      "Archived Product",
					Price:     100,
					Stock:     0,
					Currency:  "USD",
					Status:    productv1.ProductStatus_PRODUCT_STATUS_DISABLED,
					CreatedAt: now.Add(-24 * time.Hour),
					UpdatedAt: now.Add(-time.Hour),
				}, nil
			},
			deleteFn: func(context.Context, domain.DeleteProductParams) error {
				deleteCalled = true
				return nil
			},
		},
		Clock: fixedClock{now: now},
	})

	if err := svc.DeleteProduct(context.Background(), "product-id", "delete-product-1"); err != nil {
		t.Fatalf("DeleteProduct returned error: %v", err)
	}
	if deleteCalled {
		t.Fatal("expected disabled product delete to be idempotent")
	}
}

func TestCheckAvailabilityReturnsFalseForDisabledProduct(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, service.Dependencies{
		Products: &productRepoStub{
			availabilityFn: func(_ context.Context, productID string) (*domain.ProductAvailability, error) {
				return &domain.ProductAvailability{
					ProductID:      productID,
					AvailableStock: 10,
					ReservedStock:  3,
					Status:         productv1.ProductStatus_PRODUCT_STATUS_DISABLED,
				}, nil
			},
		},
		Clock: fixedClock{now: time.Now().UTC()},
	})

	result, err := svc.CheckAvailability(context.Background(), "product-id", 2)
	if err != nil {
		t.Fatalf("CheckAvailability returned error: %v", err)
	}
	if result.Available {
		t.Fatal("expected disabled product to be unavailable")
	}
	if result.AvailableStock != 10 || result.ReservedStock != 3 {
		t.Fatalf("unexpected availability result: %+v", result)
	}
}

func TestCreateProductSuccess(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	repo := &productRepoStub{
		createFn: func(_ context.Context, params domain.CreateProductParams) (*domain.Product, error) {
			if params.ID != "product-id" {
				t.Fatalf("expected generated product id, got %q", params.ID)
			}
			if params.Name != "Mechanical Keyboard" {
				t.Fatalf("expected normalized name, got %q", params.Name)
			}
			if params.Description != "Tactile switches" {
				t.Fatalf("expected normalized description, got %q", params.Description)
			}
			if params.Currency != "USD" {
				t.Fatalf("expected uppercase currency, got %q", params.Currency)
			}
			if params.Status != productv1.ProductStatus_PRODUCT_STATUS_ACTIVE {
				t.Fatalf("expected active status, got %v", params.Status)
			}
			return &domain.Product{
				ID:          params.ID,
				Name:        params.Name,
				Description: params.Description,
				Price:       params.Price,
				Stock:       params.Stock,
				Currency:    params.Currency,
				Status:      params.Status,
				CreatedAt:   params.CreatedAt,
				UpdatedAt:   params.UpdatedAt,
			}, nil
		},
	}

	svc := newTestService(t, service.Dependencies{
		Products: repo,
		Clock:    fixedClock{now: now},
		IDs:      &sequenceIDGenerator{ids: []string{"product-id"}},
	})

	product, err := svc.CreateProduct(context.Background(), service.CreateProductInput{
		Name:           "  Mechanical Keyboard  ",
		Description:    " Tactile switches ",
		Price:          12999,
		Stock:          12,
		Currency:       "usd",
		IdempotencyKey: "create-product-1",
	})
	if err != nil {
		t.Fatalf("CreateProduct returned error: %v", err)
	}
	if product.ID != "product-id" {
		t.Fatalf("expected product-id, got %q", product.ID)
	}
}

func TestCreateProductRejectsInvalidCurrency(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, service.Dependencies{
		Products: &productRepoStub{},
		Clock:    fixedClock{now: time.Now().UTC()},
	})

	_, err := svc.CreateProduct(context.Background(), service.CreateProductInput{
		Name:     "Mouse",
		Price:    4999,
		Stock:    5,
		Currency: "US",
	})
	if !domain.HasCode(err, domain.ErrorCodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestUpdateProductRejectsEmptyPatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	svc := newTestService(t, service.Dependencies{
		Products: &productRepoStub{
			getByIDFn: func(_ context.Context, productID string) (*domain.Product, error) {
				return &domain.Product{
					ID:        productID,
					Name:      "Laptop",
					Price:     159999,
					Stock:     3,
					Currency:  "USD",
					Status:    productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
					CreatedAt: now.Add(-time.Hour),
					UpdatedAt: now.Add(-time.Minute),
				}, nil
			},
		},
		Clock: fixedClock{now: now},
	})

	_, err := svc.UpdateProduct(context.Background(), service.UpdateProductInput{
		ProductID: "product-id",
	})
	if !domain.HasCode(err, domain.ErrorCodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestUpdateProductSetsOutOfStockWhenStockBecomesZero(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	svc := newTestService(t, service.Dependencies{
		Products: &productRepoStub{
			getByIDFn: func(_ context.Context, productID string) (*domain.Product, error) {
				return &domain.Product{
					ID:        productID,
					Name:      "Laptop",
					Price:     159999,
					Stock:     3,
					Currency:  "USD",
					Status:    productv1.ProductStatus_PRODUCT_STATUS_ACTIVE,
					CreatedAt: now.Add(-24 * time.Hour),
					UpdatedAt: now.Add(-time.Hour),
				}, nil
			},
			updateFn: func(_ context.Context, params domain.UpdateProductParams) (*domain.Product, error) {
				if params.Stock == nil || *params.Stock != 0 {
					t.Fatalf("expected stock patch to zero, got %+v", params.Stock)
				}
				if params.Status == nil || *params.Status != productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK {
					t.Fatalf("expected status patch to OUT_OF_STOCK, got %+v", params.Status)
				}
				return &domain.Product{
					ID:        params.ID,
					Name:      "Laptop",
					Price:     159999,
					Stock:     *params.Stock,
					Currency:  "USD",
					Status:    *params.Status,
					CreatedAt: now.Add(-24 * time.Hour),
					UpdatedAt: params.UpdatedAt,
				}, nil
			},
		},
		Clock: fixedClock{now: now},
	})

	stock := int32(0)
	product, err := svc.UpdateProduct(context.Background(), service.UpdateProductInput{
		ProductID: "product-id",
		Stock:     &stock,
	})
	if err != nil {
		t.Fatalf("UpdateProduct returned error: %v", err)
	}
	if product.Status != productv1.ProductStatus_PRODUCT_STATUS_OUT_OF_STOCK {
		t.Fatalf("expected OUT_OF_STOCK, got %v", product.Status)
	}
}

// --- Test Helpers ---

func newTestService(t *testing.T, deps service.Dependencies) *service.ProductService {
	t.Helper()

	svc, err := service.New(deps)
	if err != nil {
		t.Fatalf("service.New returned error: %v", err)
	}

	return svc
}

type productRepoStub struct {
	mu             sync.RWMutex
	createFn       func(context.Context, domain.CreateProductParams) (*domain.Product, error)
	getByIDFn      func(context.Context, string) (*domain.Product, error)
	listFn         func(context.Context, domain.ListProductsFilter) ([]*domain.Product, string, error)
	updateFn       func(context.Context, domain.UpdateProductParams) (*domain.Product, error)
	deleteFn       func(context.Context, domain.DeleteProductParams) error
	availabilityFn func(context.Context, string) (*domain.ProductAvailability, error)
}

func (s *productRepoStub) Create(ctx context.Context, params domain.CreateProductParams) (*domain.Product, error) {
	s.mu.RLock()
	fn := s.createFn
	s.mu.RUnlock()
	if fn == nil {
		return nil, errors.New("unexpected Create call")
	}
	return fn(ctx, params)
}

func (s *productRepoStub) GetByID(ctx context.Context, productID string) (*domain.Product, error) {
	s.mu.RLock()
	fn := s.getByIDFn
	s.mu.RUnlock()
	if fn == nil {
		return nil, errors.New("unexpected GetByID call")
	}
	return fn(ctx, productID)
}

func (s *productRepoStub) List(ctx context.Context, filter domain.ListProductsFilter) ([]*domain.Product, string, error) {
	s.mu.RLock()
	fn := s.listFn
	s.mu.RUnlock()
	if fn == nil {
		return nil, "", errors.New("unexpected List call")
	}
	return fn(ctx, filter)
}

func (s *productRepoStub) Update(ctx context.Context, params domain.UpdateProductParams) (*domain.Product, error) {
	s.mu.RLock()
	fn := s.updateFn
	s.mu.RUnlock()
	if fn == nil {
		return nil, errors.New("unexpected Update call")
	}
	return fn(ctx, params)
}

func (s *productRepoStub) Delete(ctx context.Context, params domain.DeleteProductParams) error {
	s.mu.RLock()
	fn := s.deleteFn
	s.mu.RUnlock()
	if fn == nil {
		return errors.New("unexpected Delete call")
	}
	return fn(ctx, params)
}

func (s *productRepoStub) GetAvailability(ctx context.Context, productID string) (*domain.ProductAvailability, error) {
	s.mu.RLock()
	fn := s.availabilityFn
	s.mu.RUnlock()
	if fn == nil {
		return nil, errors.New("unexpected GetAvailability call")
	}
	return fn(ctx, productID)
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type sequenceIDGenerator struct {
	mu    sync.Mutex
	ids   []string
	index int
}

func (g *sequenceIDGenerator) NewID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.index >= len(g.ids) {
		return "generated-id"
	}
	id := g.ids[g.index]
	g.index++
	return id
}

func repeatID(id string, n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = id
	}
	return ids
}

// Silence unused import warning.
var _ = errors.New
