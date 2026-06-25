package errors

import (
	"errors"
	"fmt"
	"net/http"
)

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *AppError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}
func New(status int, code, message string) *AppError {
	return &AppError{
		Status:  status,
		Code:    code,
		Message: message,
	}
}

// Is allows error.Is() to work with the AppError
func (e *AppError) Is(target error) bool {

	t, ok := target.(*AppError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// AsAppError extracts an AppError  from any error vlaue , handler call this
func AsAppError(err error) (*AppError, bool) {
	var appError *AppError
	if errors.As(err, &appError) {
		return appError, true
	}
	return ErrInternal, false
}

var (
	// Generic errors — any service can use these

	ErrNotFound = &AppError{
		Code:    "NOT_FOUND",
		Message: "The requested resource was not found",
		Status:  http.StatusNotFound,
	}

	ErrInvalidInput = &AppError{
		Code:    "INVALID_INPUT",
		Message: "The request contains invalid or missing fields",
		Status:  http.StatusBadRequest,
	}

	ErrInternal = &AppError{
		Code:    "INTERNAL_ERROR",
		Message: "Something went wrong on our end",
		Status:  http.StatusInternalServerError,
	}

	ErrAlreadyExists = &AppError{
		Code:    "ALREADY_EXISTS",
		Message: "This resource already exists",
		Status:  http.StatusConflict,
	}

	ErrUnauthorized = &AppError{
		Code:    "UNAUTHORIZED",
		Message: "Authentication is required",
		Status:  http.StatusUnauthorized,
	}

	// Trip-specific errors

	ErrTripNotFound = &AppError{
		Code:    "TRIP_NOT_FOUND",
		Message: "Trip not found",
		Status:  http.StatusNotFound,
	}

	ErrInvalidTripStatus = &AppError{
		Code:    "INVALID_TRIP_STATUS",
		Message: "This action is not allowed for the current trip status",
		Status:  http.StatusUnprocessableEntity,
	}

	// Driver-specific errors

	ErrDriverNotFound = &AppError{
		Code:    "DRIVER_NOT_FOUND",
		Message: "Driver not found",
		Status:  http.StatusNotFound,
	}

	ErrDriverNotAvailable = &AppError{
		Code:    "DRIVER_NOT_AVAILABLE",
		Message: "No drivers are currently available",
		Status:  http.StatusServiceUnavailable,
	}
)
