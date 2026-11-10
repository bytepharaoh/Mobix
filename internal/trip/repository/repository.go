package repository

import (
	"context"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
)

type Repository interface {
	Create(ctx context.Context, trip *domain.Trip) (*domain.Trip, error)
	FindByID(ctx context.Context, id string) (*domain.Trip, error)
	Transition(ctx context.Context, id string, from, to domain.TripStatus, driverID string, payment *messaging.TripCompleted) (*domain.Trip, error)
}
