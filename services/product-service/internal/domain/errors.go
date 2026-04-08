package domain

import "errors"

type ErrorCode string

const (
	ErrorCodeInvalidArgument    ErrorCode = "invalid_argument"
	ErrorCodeNotFound           ErrorCode = "not_found"
	ErrorCodeAlreadyExists      ErrorCode = "already_exists"
	ErrorCodePermissionDenied   ErrorCode = "permission_denied"
	ErrorCodeFailedPrecondition ErrorCode = "failed_precondition"
	ErrorCodeConflict           ErrorCode = "conflict"
	ErrorCodeInternal           ErrorCode = "internal"
)

type ServiceError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewError(code ErrorCode, message string, err error) *ServiceError {
	return &ServiceError{Code: code, Message: message, Err: err}
}

func NewInvalidArgumentError(message string) *ServiceError {
	return NewError(ErrorCodeInvalidArgument, message, nil)
}

func NewNotFoundError(message string) *ServiceError {
	return NewError(ErrorCodeNotFound, message, nil)
}

func NewAlreadyExistsError(message string) *ServiceError {
	return NewError(ErrorCodeAlreadyExists, message, nil)
}

func NewPermissionDeniedError(message string) *ServiceError {
	return NewError(ErrorCodePermissionDenied, message, nil)
}

func NewFailedPreconditionError(message string) *ServiceError {
	return NewError(ErrorCodeFailedPrecondition, message, nil)
}

func NewConflictError(message string) *ServiceError {
	return NewError(ErrorCodeConflict, message, nil)
}

func NewInternalError(message string, err error) *ServiceError {
	return NewError(ErrorCodeInternal, message, err)
}

func HasCode(err error, code ErrorCode) bool {
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		return false
	}
	return serviceErr.Code == code
}
