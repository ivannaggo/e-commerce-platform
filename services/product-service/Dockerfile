FROM golang:1.25 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/product-service ./services/product-service/cmd/product-service

FROM gcr.io/distroless/static-debian12

WORKDIR /app

COPY --from=builder /out/product-service /app/product-service
COPY --from=builder /src/services/product-service/internal/adapters/repository/postgres/schema.sql /app/schema.sql

EXPOSE 8082

ENTRYPOINT ["/app/product-service"]
