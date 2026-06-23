package repository

import (
	"context"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
)

// Repository defines what the trip service needs from the database.
// The service layer depends on this interface, not on MongoDB directly.
type Repository interface {
	Create(ctx context.Context, trip *domain.Trip) (*domain.Trip, error)
	FindByID(ctx context.Context, id string) (*domain.Trip, error)
	UpdateStatus(ctx context.Context, id string, status domain.TripStatus) error
}
