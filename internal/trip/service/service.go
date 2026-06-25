package service

import (
	"context"
	"time"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

type Repository interface {
	Create(ctx context.Context, trip *domain.Trip) (*domain.Trip, error)
	FindById(ctx context.Context, id string) (*domain.Trip, error)
	UpdateStatus(ctx context.Context, id string, status domain.TripStatus) error
}
type TripService struct {
	repo Repository
}

func New(repo Repository) *TripService {
	return &TripService{
		repo: repo,
	}
}
func (s *TripService) CreateTrip(ctx context.Context, req domain.CreateTripRequest) (*domain.Trip, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	trip := &domain.Trip{
		RiderID:   req.RiderID,
		Pickup:    req.Pickup,
		Dropoff:   req.Pickup,
		Status:    domain.StatusRequested,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return s.repo.Create(ctx, trip)
}
func (s *TripService) GetTrip(ctx context.Context, id string) (*domain.Trip, error) {
	if id == "" {
		return nil, apperrors.ErrInvalidInput
	}
	return s.repo.FindById(ctx, id)
}
func (s *TripService) UpdateTripStatus(ctx context.Context, id string, status domain.TripStatus) error {
	if id == "" {
		return apperrors.ErrInvalidInput
	}
	trip, err := s.repo.FindById(ctx, id)
	if err != nil {
		return err
	}
	if err := validateStatusTransition(trip.Status, status); err != nil {
		return err
	}
	return s.repo.UpdateStatus(ctx, id, status)
}
func validateStatusTransition(current, next domain.TripStatus) error {
	switch current {
	case domain.StatusCompleted, domain.StatusCancelled:
		return domain.ErrInvalidTripStatus
	case domain.StatusRequested:
		if next != domain.StatusAssigned && next != domain.StatusCancelled {
			return domain.ErrInvalidTripStatus
		}
	case domain.StatusAssigned:
		if next != domain.StatusInProgress && next != domain.StatusCancelled {
			return domain.ErrInvalidTripStatus
		}
	case domain.StatusInProgress:
		if next != domain.StatusCompleted {
			return domain.ErrInvalidTripStatus
		}
	}
	return nil
}
