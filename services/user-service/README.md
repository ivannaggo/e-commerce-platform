# User Service

## Run With Docker Compose

From the repository root:

```bash
docker compose -f deployments/docker-compose.yml up --build
```

This starts:

- PostgreSQL on `localhost:5433`
- gRPC user-service on `localhost:8081`

The database schema is initialized automatically from:

- `services/user-service/internal/adapters/repository/postgres/schema.sql`

To stop services:

```bash
docker compose -f deployments/docker-compose.yml down
```

To stop services and remove database data:

```bash
docker compose -f deployments/docker-compose.yml down -v
```

## Check Service Status

See running containers:

```bash
docker compose -f deployments/docker-compose.yml ps
```

See logs:

```bash
docker compose -f deployments/docker-compose.yml logs -f user-service
docker compose -f deployments/docker-compose.yml logs -f postgres
```

## Test Locally With grpcurl

The service enables gRPC reflection, so `grpcurl` can inspect it directly.

List services:

```bash
grpcurl -plaintext localhost:8081 list
```

Describe the user service:

```bash
grpcurl -plaintext localhost:8081 describe ecommerce.user.v1.UserService
```

### Register User

```bash
grpcurl -plaintext \
  -d '{
    "email": "john@example.com",
    "password": "StrongPassword1",
    "phone": "+375291112233",
    "firstName": "John",
    "lastName": "Doe",
    "idempotencyKey": "register-john-1"
  }' \
  localhost:8081 \
  ecommerce.user.v1.UserService/RegisterUser
```

Expected result:

- created user object
- access token
- refresh token

Save the returned `refreshToken` and `user.id` for the next calls.

### Authenticate User

```bash
grpcurl -plaintext \
  -d '{
    "email": "john@example.com",
    "password": "StrongPassword1",
    "userAgent": "grpcurl",
    "ipAddress": "127.0.0.1"
  }' \
  localhost:8081 \
  ecommerce.user.v1.UserService/AuthenticateUser
```

### Get User

Replace `USER_ID` with the value returned from registration:

```bash
grpcurl -plaintext \
  -d '{
    "userId": "USER_ID"
  }' \
  localhost:8081 \
  ecommerce.user.v1.UserService/GetUser
```

### List Users

```bash
grpcurl -plaintext \
  -d '{
    "pagination": {
      "pageSize": 10
    }
  }' \
  localhost:8081 \
  ecommerce.user.v1.UserService/ListUsers
```

### Refresh Session

Replace `REFRESH_TOKEN` with the value returned from registration or login:

```bash
grpcurl -plaintext \
  -d '{
    "refreshToken": "REFRESH_TOKEN"
  }' \
  localhost:8081 \
  ecommerce.user.v1.UserService/RefreshSession
```

### Revoke Session

```bash
grpcurl -plaintext \
  -d '{
    "refreshToken": "REFRESH_TOKEN"
  }' \
  localhost:8081 \
  ecommerce.user.v1.UserService/RevokeSession
```

## Check Database Data

Open PostgreSQL shell:

```bash
docker exec -it user-service-postgres psql -U user_service -d user_service
```

If `5433` or `8081` are already busy on your machine, override host ports at startup:

```bash
USER_SERVICE_POSTGRES_HOST_PORT=55432 USER_SERVICE_GRPC_HOST_PORT=18081 docker compose -f deployments/docker-compose.yml up --build
```

Useful queries:

```sql
SELECT id, email, status, email_verified, created_at
FROM users
ORDER BY created_at DESC;
```

```sql
SELECT id, user_id, created_at, expires_at, revoked_at
FROM user_sessions
ORDER BY created_at DESC;
```

## Run Tests Without Docker

From the repository root:

```bash
go test ./...
```

## Notes

- The JWT secrets in `deployments/docker-compose.yml` are only for local development.
- For production, use strong secrets from a secret manager and do not commit them.
- If the database schema changes, recreate the database volume or apply migrations manually.
