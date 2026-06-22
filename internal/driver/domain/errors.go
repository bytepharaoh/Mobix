package domain

import (
	"net/http"

	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

var (
	ErrDriverNotFound = &apperrors.AppError{
		Code:    "DRIVER_NOT_FOUND",
		Message: "Driver not found",
		Status:  http.StatusNotFound,
	}
	ErrDriverAlreadyExists = &apperrors.AppError{
		Code:    "DRIVER_ALREADY_EXISTS",
		Message: "Driver already exists",
		Status:  http.StatusConflict,
	}
	ErrDriverBusy = &apperrors.AppError{
		Code:    "DRIVER_BUSY",
		Message: "Driver is currently busy",
		Status:  http.StatusConflict,
	}
)