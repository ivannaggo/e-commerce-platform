# Product Service

## Run With Docker Compose

From the repository root:

```bash
docker compose -f deployments/docker-compose.yml up --build product-postgres product-service
```

This starts:

- PostgreSQL on `localhost:5434`
- gRPC product-service on `localhost:8082`

The database schema is initialized automatically from:

- `services/product-service/internal/adapters/repository/postgres/schema.sql`

To stop services:

```bash
docker compose -f deployments/docker-compose.yml stop product-service product-postgres
```

To stop services and remove product database data:

```bash
docker compose -f deployments/docker-compose.yml rm -sf product-service product-postgres
docker volume rm deployments_product-service-postgres-data
```

## Check Service Status

See running containers:

```bash
docker compose -f deployments/docker-compose.yml ps product-service product-postgres
```

See logs:

```bash
docker compose -f deployments/docker-compose.yml logs -f product-service
docker compose -f deployments/docker-compose.yml logs -f product-postgres
```

## Test Locally With grpcurl

The service enables gRPC reflection, so `grpcurl` can inspect it directly.

List services:

```bash
grpcurl -plaintext localhost:8082 list
```

Describe the product service:

```bash
grpcurl -plaintext localhost:8082 describe ecommerce.product.v1.ProductService
```

### Create Product

```bash
grpcurl -plaintext \
  -d '{
    "name": "Mechanical Keyboard",
    "description": "Hot-swappable keyboard",
    "price": 12999,
    "stock": 15,
    "currency": "USD",
    "idempotencyKey": "create-keyboard-1"
  }' \
  localhost:8082 \
  ecommerce.product.v1.ProductService/CreateProduct
```

### Get Product

Replace `PRODUCT_ID` with the value returned from creation:

```bash
grpcurl -plaintext \
  -d '{
    "productId": "PRODUCT_ID"
  }' \
  localhost:8082 \
  ecommerce.product.v1.ProductService/GetProduct
```

### List Products

```bash
grpcurl -plaintext \
  -d '{
    "pagination": {
      "pageSize": 10
    },
    "currency": "USD"
  }' \
  localhost:8082 \
  ecommerce.product.v1.ProductService/ListProducts
```

### Update Product

```bash
grpcurl -plaintext \
  -d '{
    "productId": "PRODUCT_ID",
    "stock": 0,
    "idempotencyKey": "update-keyboard-1"
  }' \
  localhost:8082 \
  ecommerce.product.v1.ProductService/UpdateProduct
```

### Delete Product

```bash
grpcurl -plaintext \
  -d '{
    "productId": "PRODUCT_ID",
    "idempotencyKey": "delete-keyboard-1"
  }' \
  localhost:8082 \
  ecommerce.product.v1.ProductService/DeleteProduct
```

### Check Availability

```bash
grpcurl -plaintext \
  -d '{
    "productId": "PRODUCT_ID",
    "requestedQuantity": 2
  }' \
  localhost:8082 \
  ecommerce.product.v1.ProductService/CheckAvailability
```

## Check Database Data

Open PostgreSQL shell:

```bash
docker exec -it product-service-postgres psql -U product_service -d product_service
```

Useful queries:

```sql
SELECT id, name, price, stock, reserved_stock, currency, status, created_at
FROM products
ORDER BY created_at DESC;
```

```sql
SELECT operation_type, idempotency_key, product_id, created_at
FROM product_operation_idempotency
ORDER BY created_at DESC;
```

## Run Tests Without Docker

From the repository root:

```bash
go test ./...
```

## Notes

- `DeleteProduct` is implemented as a logical delete by setting status to `DISABLED`.
- `reserved_stock` is persisted in PostgreSQL for future inventory reservation flows; it defaults to `0` in the current service.
- If `5434` or `8082` are already busy on your machine, override host ports:

```bash
PRODUCT_SERVICE_POSTGRES_HOST_PORT=55434 PRODUCT_SERVICE_GRPC_HOST_PORT=18082 docker compose -f deployments/docker-compose.yml up --build product-postgres product-service
```
