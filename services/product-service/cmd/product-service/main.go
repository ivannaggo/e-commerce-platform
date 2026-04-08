package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	postgresadapter "github.com/ivannaggo/e-commerce-platform/services/product-service/internal/adapters/repository/postgres"
	productservice "github.com/ivannaggo/e-commerce-platform/services/product-service/internal/service"
	productgrpc "github.com/ivannaggo/e-commerce-platform/services/product-service/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	defaultGRPCPort         = 8082
	defaultShutdownTimeout  = 15 * time.Second
	defaultDBConnectTimeout = 10 * time.Second
	defaultRPCTimeout       = 30 * time.Second
)

type config struct {
	Environment         string
	GRPCPort            int
	DBURL               string
	DBMaxOpenConns      int32
	DBMinIdleConns      int32
	DBMaxConnLifetime   time.Duration
	DBMaxConnIdleTime   time.Duration
	DBHealthCheckPeriod time.Duration
	ShutdownTimeout     time.Duration
	DBConnectTimeout    time.Duration
	RPCTimeout          time.Duration
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	pool, err := newPostgresPool(ctx, cfg)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	productRepo, err := postgresadapter.NewProductRepository(pool)
	if err != nil {
		log.Fatalf("create product repository: %v", err)
	}

	coreService, err := productservice.New(productservice.Dependencies{
		Products: productRepo,
	})
	if err != nil {
		log.Fatalf("create product service: %v", err)
	}

	grpcHandler, err := productgrpc.NewServer(coreService,
		productgrpc.WithHealthChecker(makeHealthChecker(pool)),
		productgrpc.WithRequestTimeout(cfg.RPCTimeout),
	)
	if err != nil {
		log.Fatalf("create grpc transport: %v", err)
	}

	server := ggrpc.NewServer()
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		log.Fatalf("listen grpc: %v", err)
	}

	log.Printf("product-service starting: env=%s grpc_port=%d", cfg.Environment, cfg.GRPCPort)

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		stopped := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
		case <-shutdownCtx.Done():
			server.Stop()
		}

		_ = grpcListener.Close()
	}()

	productgrpc.Register(server, grpcHandler)
	reflection.Register(server)

	if err := server.Serve(grpcListener); err != nil && ctx.Err() == nil {
		log.Fatalf("serve grpc: %v", err)
	}

	log.Printf("product-service stopped")
}

func loadConfig() (config, error) {
	var cfg config

	grpcPort, err := getEnvInt("PRODUCT_SERVICE_GRPC_PORT", defaultGRPCPort)
	if err != nil {
		return config{}, err
	}
	flag.IntVar(&cfg.GRPCPort, "grpc-port", grpcPort, "gRPC port")
	flag.Parse()

	cfg.Environment = getEnv("APP_ENV", "development")
	cfg.DBURL = os.Getenv("PRODUCT_SERVICE_DB_URL")
	cfg.DBMaxOpenConns = int32(getEnvIntOrDie("PRODUCT_SERVICE_DB_MAX_OPEN_CONNS", 10))
	cfg.DBMinIdleConns = int32(getEnvIntOrDie("PRODUCT_SERVICE_DB_MIN_IDLE_CONNS", 2))
	cfg.DBMaxConnLifetime = getEnvDuration("PRODUCT_SERVICE_DB_MAX_CONN_LIFETIME", time.Hour)
	cfg.DBMaxConnIdleTime = getEnvDuration("PRODUCT_SERVICE_DB_MAX_CONN_IDLE_TIME", 15*time.Minute)
	cfg.DBHealthCheckPeriod = getEnvDuration("PRODUCT_SERVICE_DB_HEALTH_CHECK_PERIOD", time.Minute)
	cfg.ShutdownTimeout = getEnvDuration("PRODUCT_SERVICE_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	cfg.DBConnectTimeout = getEnvDuration("PRODUCT_SERVICE_DB_CONNECT_TIMEOUT", defaultDBConnectTimeout)
	cfg.RPCTimeout = getEnvDuration("PRODUCT_SERVICE_RPC_TIMEOUT", defaultRPCTimeout)

	if cfg.DBURL == "" {
		return config{}, fmt.Errorf("PRODUCT_SERVICE_DB_URL is required")
	}

	return cfg, nil
}

// getEnvInt parses an environment variable as an integer, returning fallback if unset.
// Returns an error if the value is set but not a valid integer.
func getEnvInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid integer value for %s: %w", key, err)
	}
	return parsed, nil
}

// getEnvIntOrDie parses an environment variable as an integer, falling back to the default.
// Unlike getEnvInt, this one logs and exits on parse failure (used for non-critical config).
func getEnvIntOrDie(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalf("invalid integer value for %s: %v", key, err)
	}
	return parsed
}

func newPostgresPool(ctx context.Context, cfg config) (*pgxpool.Pool, error) {
	connectCtx, cancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(cfg.DBURL)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}

	poolConfig.MaxConns = cfg.DBMaxOpenConns
	poolConfig.MinConns = cfg.DBMinIdleConns
	poolConfig.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.DBMaxConnIdleTime
	poolConfig.HealthCheckPeriod = cfg.DBHealthCheckPeriod

	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		log.Fatalf("invalid duration value for %s: %v", key, err)
	}

	return parsed
}

func makeHealthChecker(pool *pgxpool.Pool) func(context.Context) error {
	return func(ctx context.Context) error {
		return pool.Ping(ctx)
	}
}
