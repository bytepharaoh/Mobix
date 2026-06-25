package repository

import (
	"context"
	"time"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
)

// Repository defines what the trip service needs from the database.
// The service layer depends on this interface, not on MongoDB directly.
type Repository interface {
	Create(ctx context.Context, trip *domain.Driver) (*domain.Driver, error)
	FindById(ctx context.Context, id string) (*domain.Driver, error)
	// For portential batch processing (for example refresh amoutn of trips for drivers once per day)
	IncreaseTotalTrips(ctx context.Context, id string, tripDelta int) error
	UpdateStatus(ctx context.Context, id string, status domain.DriverStatus) error
	UpdateLastLogin(ctx context.Context, id string, time time.Time) error
	UpdateRating(ctx context.Context, id string, rating float64) error
}