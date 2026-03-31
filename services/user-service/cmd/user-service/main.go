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

	postgresadapter "github.com/ivannaggo/e-commerce-platform/services/user-service/internal/adapters/repository/postgres"
	bcryptadapter "github.com/ivannaggo/e-commerce-platform/services/user-service/internal/adapters/security/bcrypt"
	jwtadapter "github.com/ivannaggo/e-commerce-platform/services/user-service/internal/adapters/token/jwt"
	userservice "github.com/ivannaggo/e-commerce-platform/services/user-service/internal/service"
	userservicegrpc "github.com/ivannaggo/e-commerce-platform/services/user-service/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	defaultGRPCPort         = 8081
	defaultShutdownTimeout  = 15 * time.Second
	defaultDBConnectTimeout = 10 * time.Second
)

type config struct {
	Environment             string
	GRPCPort                int
	DBURL                   string
	DBMaxOpenConns          int32
	DBMinIdleConns          int32
	DBMaxConnLifetime       time.Duration
	DBMaxConnIdleTime       time.Duration
	DBHealthCheckPeriod     time.Duration
	BcryptCost              int
	JWTIssuer               string
	JWTAudience             string
	JWTAccessSecret         string
	JWTRefreshSecret        string
	JWTVerificationSecret   string
	JWTAccessTokenTTL       time.Duration
	JWTRefreshTokenTTL      time.Duration
	JWTVerificationTokenTTL time.Duration
	ShutdownTimeout         time.Duration
	DBConnectTimeout        time.Duration
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

	userRepo, err := postgresadapter.NewUserRepository(pool)
	if err != nil {
		log.Fatalf("create user repository: %v", err)
	}

	sessionRepo, err := postgresadapter.NewSessionRepository(pool)
	if err != nil {
		log.Fatalf("create session repository: %v", err)
	}

	passwordManager, err := bcryptadapter.NewPasswordManager(cfg.BcryptCost)
	if err != nil {
		log.Fatalf("create password manager: %v", err)
	}

	tokenManager, err := jwtadapter.NewManager(jwtadapter.Config{
		Issuer:               cfg.JWTIssuer,
		Audience:             cfg.JWTAudience,
		AccessTokenSecret:    []byte(cfg.JWTAccessSecret),
		RefreshTokenSecret:   []byte(cfg.JWTRefreshSecret),
		VerificationSecret:   []byte(cfg.JWTVerificationSecret),
		AccessTokenTTL:       cfg.JWTAccessTokenTTL,
		RefreshTokenTTL:      cfg.JWTRefreshTokenTTL,
		VerificationTokenTTL: cfg.JWTVerificationTokenTTL,
	})
	if err != nil {
		log.Fatalf("create token manager: %v", err)
	}

	coreService, err := userservice.New(userservice.Dependencies{
		Users:        userRepo,
		Sessions:     sessionRepo,
		Passwords:    passwordManager,
		Tokens:       tokenManager,
		Verification: tokenManager,
	})
	if err != nil {
		log.Fatalf("create user service: %v", err)
	}

	grpcServer, err := userservicegrpc.NewServer(coreService)
	if err != nil {
		log.Fatalf("create grpc transport: %v", err)
	}

	server := ggrpc.NewServer()
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		log.Fatalf("listen grpc: %v", err)
	}

	log.Printf("user-service starting: env=%s grpc_port=%d", cfg.Environment, cfg.GRPCPort)

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

	userservicegrpc.Register(server, grpcServer)
	reflection.Register(server)

	if err := server.Serve(grpcListener); err != nil && ctx.Err() == nil {
		log.Fatalf("serve grpc: %v", err)
	}

	log.Printf("user-service stopped")
}

func loadConfig() (config, error) {
	var cfg config

	flag.IntVar(&cfg.GRPCPort, "grpc-port", getEnvInt("USER_SERVICE_GRPC_PORT", defaultGRPCPort), "gRPC port")
	flag.Parse()

	cfg.Environment = getEnv("APP_ENV", "development")
	cfg.DBURL = os.Getenv("USER_SERVICE_DB_URL")
	cfg.DBMaxOpenConns = int32(getEnvInt("USER_SERVICE_DB_MAX_OPEN_CONNS", 10))
	cfg.DBMinIdleConns = int32(getEnvInt("USER_SERVICE_DB_MIN_IDLE_CONNS", 2))
	cfg.DBMaxConnLifetime = getEnvDuration("USER_SERVICE_DB_MAX_CONN_LIFETIME", time.Hour)
	cfg.DBMaxConnIdleTime = getEnvDuration("USER_SERVICE_DB_MAX_CONN_IDLE_TIME", 15*time.Minute)
	cfg.DBHealthCheckPeriod = getEnvDuration("USER_SERVICE_DB_HEALTH_CHECK_PERIOD", time.Minute)
	cfg.BcryptCost = getEnvInt("USER_SERVICE_BCRYPT_COST", bcryptadapter.DefaultCost)
	cfg.JWTIssuer = getEnv("USER_SERVICE_JWT_ISSUER", "ecommerce-user-service")
	cfg.JWTAudience = getEnv("USER_SERVICE_JWT_AUDIENCE", "ecommerce-clients")
	cfg.JWTAccessSecret = os.Getenv("USER_SERVICE_JWT_ACCESS_SECRET")
	cfg.JWTRefreshSecret = os.Getenv("USER_SERVICE_JWT_REFRESH_SECRET")
	cfg.JWTVerificationSecret = os.Getenv("USER_SERVICE_JWT_VERIFICATION_SECRET")
	cfg.JWTAccessTokenTTL = getEnvDuration("USER_SERVICE_JWT_ACCESS_TTL", 15*time.Minute)
	cfg.JWTRefreshTokenTTL = getEnvDuration("USER_SERVICE_JWT_REFRESH_TTL", 7*24*time.Hour)
	cfg.JWTVerificationTokenTTL = getEnvDuration("USER_SERVICE_JWT_VERIFICATION_TTL", 24*time.Hour)
	cfg.ShutdownTimeout = getEnvDuration("USER_SERVICE_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	cfg.DBConnectTimeout = getEnvDuration("USER_SERVICE_DB_CONNECT_TIMEOUT", defaultDBConnectTimeout)

	switch {
	case cfg.DBURL == "":
		return config{}, fmt.Errorf("USER_SERVICE_DB_URL is required")
	case cfg.JWTAccessSecret == "":
		return config{}, fmt.Errorf("USER_SERVICE_JWT_ACCESS_SECRET is required")
	case cfg.JWTRefreshSecret == "":
		return config{}, fmt.Errorf("USER_SERVICE_JWT_REFRESH_SECRET is required")
	case cfg.JWTVerificationSecret == "":
		return config{}, fmt.Errorf("USER_SERVICE_JWT_VERIFICATION_SECRET is required")
	}

	return cfg, nil
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

func getEnvInt(key string, fallback int) int {
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
