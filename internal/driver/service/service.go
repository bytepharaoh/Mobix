package service

import (
	"context"
	"time"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/internal/driver/repository"
)

type Service interface {
	Create(ctx context.Context, driver *domain.Driver) (*domain.Driver, error)
	GetByID(ctx context.Context, id string) (*domain.Driver, error)
	IncreaseTotalTrips(ctx context.Context, id string, tripDelta int) error
	UpdateStatus(ctx context.Context, id string, status domain.DriverStatus) error
	UpdateLastLogin(ctx context.Context, id string, loginTime time.Time) error
	UpdateRating(ctx context.Context, id string, rating float64) error
}

type service struct {
	repository repository.Repository
}

func New(repository repository.Repository) Service {
	return &service{
		repository: repository,
	}
}

func (s *service) Create(
	ctx context.Context,
	driver *domain.Driver,
) (*domain.Driver, error) {
	return s.repository.Create(ctx, driver)
}

func (s *service) GetByID(
	ctx context.Context,
	id string,
) (*domain.Driver, error) {
	return s.repository.FindById(ctx, id)
}

func (s *service) IncreaseTotalTrips(
	ctx context.Context,
	id string,
	tripDelta int,
) error {
	return s.repository.IncreaseTotalTrips(ctx, id, tripDelta)
}

func (s *service) UpdateStatus(
	ctx context.Context,
	id string,
	status domain.DriverStatus,
) error {
	return s.repository.UpdateStatus(ctx, id, status)
}

func (s *service) UpdateLastLogin(
	ctx context.Context,
	id string,
	loginTime time.Time,
) error {
	return s.repository.UpdateLastLogin(ctx, id, loginTime)
}

func (s *service) UpdateRating(
	ctx context.Context,
	id string,
	rating float64,
) error {
	return s.repository.UpdateRating(ctx, id, rating)
}
