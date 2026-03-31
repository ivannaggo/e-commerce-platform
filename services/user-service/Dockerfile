FROM golang:1.25 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/user-service ./services/user-service/cmd/user-service

FROM gcr.io/distroless/static-debian12

WORKDIR /app

COPY --from=builder /out/user-service /app/user-service
COPY --from=builder /src/services/user-service/internal/adapters/repository/postgres/schema.sql /app/schema.sql

EXPOSE 8081

ENTRYPOINT ["/app/user-service"]
