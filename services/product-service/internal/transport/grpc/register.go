package grpc

import (
	productv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/product/v1"
	ggrpc "google.golang.org/grpc"
)

func Register(server *ggrpc.Server, handler productv1.ProductServiceServer) {
	productv1.RegisterProductServiceServer(server, handler)
}
