package domain

import (
	"net/http"

	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

var (
	ErrMissingRiderID = &apperrors.AppError{
		Code:    "MISSING_RIDER_ID",
		Message: "rider_id is required",
		Status:  http.StatusBadRequest,
	}
	ErrInvalidPickup = &apperrors.AppError{
		Code:    "INVALID_PICKUP",
		Message: "pickup coordinates are required",
		Status:  http.StatusBadRequest,
	}
	ErrInvalidDropoff = &apperrors.AppError{
		Code:    "INVALID_DROPOFF",
		Message: "dropoff coordinates are required",
		Status:  http.StatusBadRequest,
	}
	ErrTripNotFound = &apperrors.AppError{
		Code:    "TRIP_NOT_FOUND",
		Message: "Trip not found",
		Status:  http.StatusNotFound,
	}
	ErrInvalidTripStatus = &apperrors.AppError{
		Code:    "INVALID_TRIP_STATUS",
		Message: "this action is not allowed for the current trip status",
		Status:  http.StatusUnprocessableEntity,
	}
)
