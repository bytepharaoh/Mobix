package domain

import (
	"net/http"

	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

var(
	ErrMissingRiderID=&apperrors.AppError{
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
)