package repository

import (
	"context"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
)

type Repository interface {
	Create(ctx context.Context, trip *domain.Trip) (*domain.Trip, error)
	FindByID(ctx context.Context, id string) (*domain.Trip, error)
	Transition(ctx context.Context, id string, expected *domain.Trip, change domain.Transition) (*domain.Trip, error)
}
