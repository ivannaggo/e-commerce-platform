package grpc

import (
	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	ggrpc "google.golang.org/grpc"
)

func Register(server ggrpc.ServiceRegistrar, handler userv1.UserServiceServer) {
	userv1.RegisterUserServiceServer(server, handler)
}
