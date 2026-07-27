// Package apierror defines standardized API errors with an HTTP status and a
// stable numeric code, plus helpers to translate transport/upstream errors
// (including gRPC status codes) into a uniform shape for HTTP responses.
//
// Code layout mirrors agent-frame: {httpStatus}{moduleCode}{errorCode}.
package apierror

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// APIError is a transport-agnostic error carrying an HTTP status and code.
type APIError struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *APIError) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Message) }

// New builds an APIError.
func New(httpStatus, code int, message string) *APIError {
	return &APIError{Code: code, Message: message, HTTPStatus: httpStatus}
}

// WithMessage returns a copy carrying a custom message (keeps code + status).
func (e *APIError) WithMessage(message string) *APIError {
	return &APIError{Code: e.Code, Message: message, HTTPStatus: e.HTTPStatus}
}

// WithMessagef is WithMessage with formatting.
func (e *APIError) WithMessagef(format string, args ...any) *APIError {
	return e.WithMessage(fmt.Sprintf(format, args...))
}

// Common errors. Module code 001 is reserved for the runtime-invocation context.
var (
	ErrBadRequest           = New(400, 400001000, "bad request")
	ErrUnauthorized         = New(401, 401001000, "unauthorized")
	ErrForbidden            = New(403, 403001000, "forbidden")
	ErrNotFound             = New(404, 404001000, "not found")
	ErrTimeout              = New(504, 504001000, "request timed out")
	ErrUpstream             = New(502, 502001000, "upstream error")
	ErrRuntimeUnavailable   = New(503, 503001000, "agent-runtime unavailable")
	ErrNotImplemented       = New(501, 501001000, "not implemented")
	ErrInternal             = New(500, 500001000, "internal server error")
	ErrModelBindingRequired = New(409, 409001001, "请先绑定模型后再使用 Agent")
)

// FromError normalizes any error into an APIError. Already-APIErrors pass
// through; gRPC status codes are mapped to the closest HTTP semantics.
func FromError(err error) *APIError {
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	if st, ok := status.FromError(err); ok {
		return fromGRPC(st)
	}
	return ErrInternal.WithMessage(err.Error())
}

func fromGRPC(st *status.Status) *APIError {
	msg := st.Message()
	switch st.Code() {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		return ErrBadRequest.WithMessage(msg)
	case codes.Unauthenticated:
		return ErrUnauthorized.WithMessage(msg)
	case codes.PermissionDenied:
		return ErrForbidden.WithMessage(msg)
	case codes.NotFound:
		return ErrNotFound.WithMessage(msg)
	case codes.DeadlineExceeded:
		return ErrTimeout.WithMessage(msg)
	case codes.Unavailable:
		return ErrRuntimeUnavailable.WithMessage(msg)
	case codes.Unimplemented:
		return ErrNotImplemented.WithMessage(msg)
	default:
		return ErrUpstream.WithMessage(msg)
	}
}
