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
	ErrInvalidLocation = &apperrors.AppError{
		Code:    "INVALID_LOCATION",
		Message: "Invalid location",
		Status:  http.StatusBadRequest,
	}
	ErrInvalidEmail = &apperrors.AppError{
		Code:    "INVALID_EMAIL",
		Message: "Invalid email",
		Status:  http.StatusBadRequest,
	}
	ErrInvalidPhoneNumber = &apperrors.AppError{
		Code:    "INVALID_PHONE_NUMBER",
		Message: "Invalid phone number",
		Status:  http.StatusBadRequest,
	}
	ErrInvalidCarNumber = &apperrors.AppError{
		Code:    "INVALID_CAR_NUMBER",
		Message: "Invalid car number",
		Status:  http.StatusBadRequest,
	}
	ErrInvalidName = &apperrors.AppError{
		Code:    "INVALID_NAME",
		Message: "Invalid name",
		Status:  http.StatusBadRequest,
	}
	ErrInvalidCarModel = &apperrors.AppError{
		Code:    "INVALID_CAR_MODEL",
		Message: "Invalid car model",
		Status:  http.StatusBadRequest,
	}
)