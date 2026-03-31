package grpc

import (
	"context"
	"errors"

	userv1 "github.com/ivannaggo/e-commerce-platform/proto/gen/go/ecommerce/user/v1"
	"github.com/ivannaggo/e-commerce-platform/services/user-service/internal/domain"
	servicesvc "github.com/ivannaggo/e-commerce-platform/services/user-service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

type Server struct {
	userv1.UnimplementedUserServiceServer

	service *servicesvc.UserService
}

func NewServer(service *servicesvc.UserService) (*Server, error) {
	if service == nil {
		return nil, errors.New("service is required")
	}

	return &Server{service: service}, nil
}

func (s *Server) RegisterUser(ctx context.Context, req *userv1.RegisterUserRequest) (*userv1.RegisterUserResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	result, err := s.service.RegisterUser(ctx, servicesvc.RegisterUserInput{
		Email:          req.GetEmail(),
		Password:       req.GetPassword(),
		Phone:          req.GetPhone(),
		FirstName:      req.GetFirstName(),
		LastName:       req.GetLastName(),
		IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &userv1.RegisterUserResponse{
		User:    toProtoUser(result.User),
		Session: toProtoSession(result.Session),
	}, nil
}

func (s *Server) AuthenticateUser(ctx context.Context, req *userv1.AuthenticateUserRequest) (*userv1.AuthenticateUserResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	result, err := s.service.AuthenticateUser(ctx, servicesvc.AuthenticateUserInput{
		Email:     req.GetEmail(),
		Password:  req.GetPassword(),
		UserAgent: req.GetUserAgent(),
		IPAddress: req.GetIpAddress(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &userv1.AuthenticateUserResponse{
		User:    toProtoUser(result.User),
		Session: toProtoSession(result.Session),
	}, nil
}

func (s *Server) RefreshSession(ctx context.Context, req *userv1.RefreshSessionRequest) (*userv1.RefreshSessionResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	session, err := s.service.RefreshSession(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &userv1.RefreshSessionResponse{Session: toProtoSession(session)}, nil
}

func (s *Server) RevokeSession(ctx context.Context, req *userv1.RevokeSessionRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	if err := s.service.RevokeSession(ctx, req.GetRefreshToken()); err != nil {
		return nil, toGRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.GetUserResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	user, err := s.service.GetUser(ctx, req.GetUserId())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &userv1.GetUserResponse{User: toProtoUserDetails(user)}, nil
}

func (s *Server) ListUsers(ctx context.Context, req *userv1.ListUsersRequest) (*userv1.ListUsersResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	var pageSize int32
	var pageToken string
	if req.GetPagination() != nil {
		pageSize = req.GetPagination().GetPageSize()
		pageToken = req.GetPagination().GetPageToken()
	}

	result, err := s.service.ListUsers(ctx, servicesvc.ListUsersInput{
		PageSize:  pageSize,
		PageToken: pageToken,
		Status:    req.GetStatus(),
		Email:     req.GetEmail(),
		Name:      req.GetName(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &userv1.ListUsersResponse{
		Users:         toProtoUsers(result.Users),
		NextPageToken: result.NextPageToken,
	}, nil
}

func (s *Server) UpdateUserProfile(ctx context.Context, req *userv1.UpdateUserProfileRequest) (*userv1.UpdateUserProfileResponse, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	var phone, firstName, lastName *string
	if req.GetPhone() != nil {
		value := req.GetPhone().GetValue()
		phone = &value
	}
	if req.GetFirstName() != nil {
		value := req.GetFirstName().GetValue()
		firstName = &value
	}
	if req.GetLastName() != nil {
		value := req.GetLastName().GetValue()
		lastName = &value
	}

	user, err := s.service.UpdateUserProfile(ctx, servicesvc.UpdateUserProfileInput{
		UserID:    req.GetUserId(),
		Phone:     phone,
		FirstName: firstName,
		LastName:  lastName,
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &userv1.UpdateUserProfileResponse{User: toProtoUserDetails(user)}, nil
}

func (s *Server) ChangePassword(ctx context.Context, req *userv1.ChangePasswordRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	if err := s.service.ChangePassword(ctx, servicesvc.ChangePasswordInput{
		UserID:          req.GetUserId(),
		CurrentPassword: req.GetCurrentPassword(),
		NewPassword:     req.GetNewPassword(),
	}); err != nil {
		return nil, toGRPCError(err)
	}

	return &emptypb.Empty{}, nil
}

func (s *Server) VerifyEmail(ctx context.Context, req *userv1.VerifyEmailRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	if err := s.service.VerifyEmail(ctx, servicesvc.VerifyEmailInput{
		UserID:            req.GetUserId(),
		VerificationToken: req.GetVerificationToken(),
	}); err != nil {
		return nil, toGRPCError(err)
	}

	return &emptypb.Empty{}, nil
}

func (s *Server) DeactivateUser(ctx context.Context, req *userv1.DeactivateUserRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, toGRPCError(domain.NewInvalidArgumentError("request is required"))
	}

	if err := s.service.DeactivateUser(ctx, req.GetUserId(), req.GetReason()); err != nil {
		return nil, toGRPCError(err)
	}

	return &emptypb.Empty{}, nil
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
	case domain.ErrorCodeUnauthenticated:
		return status.Error(codes.Unauthenticated, serviceErr.Message)
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
