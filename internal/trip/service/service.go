package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/repository"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/geo"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type TripService struct {
	repo repository.Repository
}

func New(repo repository.Repository) *TripService {
	return &TripService{repo: repo}
}

func (s *TripService) CreateTrip(ctx context.Context, req domain.CreateTripRequest, key string) (*domain.Trip, error) {
	actor := authn.From(ctx)
	if actor.Role != authn.Rider || actor.ID == "" {
		return nil, apperrors.ErrForbidden
	}
	req.RiderID = strings.TrimSpace(actor.ID)
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if len(key) > 128 {
		return nil, apperrors.ErrInvalidInput
	}
	trip := &domain.Trip{
		RiderID:        req.RiderID,
		RiderAccountID: actor.ID,
		Pickup:         req.Pickup,
		Dropoff:        req.Dropoff,
		Status:         domain.StatusRequested,
		FareCents:      fare(req.Pickup, req.Dropoff),
		Currency:       "USD",
	}
	if key != "" {
		digest := sha256.Sum256([]byte(key))
		trip.ScopedIdempotencyKey = hex.EncodeToString(digest[:])
	}
	return s.repo.Create(ctx, trip)
}

func (s *TripService) GetTrip(ctx context.Context, id string) (*domain.Trip, error) {
	if id == "" {
		return nil, apperrors.ErrInvalidInput
	}
	trip, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !canRead(authn.From(ctx), trip) {
		return nil, domain.ErrTripNotFound
	}
	return withFare(trip), nil
}

func (s *TripService) UpdateTripStatus(ctx context.Context, id string, status domain.TripStatus, driverID string, checkDriver func(context.Context, string) (string, error)) (*domain.Trip, error) {
	trip, err := s.GetTrip(ctx, id)
	if err != nil {
		return nil, err
	}
	if !canTransition(authn.From(ctx), trip, status) {
		return nil, apperrors.ErrForbidden
	}
	if status == domain.StatusAssigned {
		parsed, err := bson.ObjectIDFromHex(driverID)
		if err != nil || parsed.IsZero() || checkDriver == nil {
			return nil, apperrors.ErrInvalidInput
		}
		driverID = parsed.Hex()
	} else if driverID != "" {
		return nil, apperrors.ErrInvalidInput
	}
	if sameTransition(trip, status, driverID) {
		return trip, nil
	}
	if err := validateStatusTransition(trip.Status, status); err != nil {
		return nil, err
	}
	driverAccountID := ""
	if status == domain.StatusAssigned {
		driverAccountID, err = checkDriver(ctx, driverID)
		if err != nil {
			return nil, err
		}
		if driverAccountID == "" {
			return nil, apperrors.New(409, "DRIVER_NOT_ENROLLED", "The driver needs an individual account before assignment")
		}
	}
	var payment *messaging.TripCompleted
	if status == domain.StatusCompleted {
		amount := trip.FareCents
		if amount <= 0 {
			amount = fare(trip.Pickup, trip.Dropoff)
		}
		payment = &messaging.TripCompleted{
			ID: "trip-completed:" + trip.ID.Hex(), TripID: trip.ID.Hex(),
			AmountCents: amount, Currency: "USD", OccurredAt: time.Now().UTC(),
		}
	}
	updated, err := s.repo.Transition(ctx, id, trip, domain.Transition{
		Status: status, DriverID: driverID, DriverAccountID: driverAccountID, Payment: payment,
	})
	if errors.Is(err, apperrors.ErrConflict) {
		current, readErr := s.GetTrip(ctx, id)
		if readErr != nil {
			return nil, readErr
		}
		if !canTransition(authn.From(ctx), current, status) {
			return nil, apperrors.ErrForbidden
		}
		if sameTransition(current, status, driverID) {
			return withFare(current), nil
		}
	}
	return withFare(updated), err
}

func sameTransition(trip *domain.Trip, status domain.TripStatus, driverID string) bool {
	switch status {
	case domain.StatusAssigned:
		return trip.Status == status && trip.DriverID == driverID
	case domain.StatusInProgress, domain.StatusCompleted, domain.StatusCancelled:
		return trip.Status == status
	default:
		return false
	}
}

func validateStatusTransition(current, next domain.TripStatus) error {
	if current == domain.StatusRequested && (next == domain.StatusAssigned || next == domain.StatusCancelled) ||
		current == domain.StatusAssigned && (next == domain.StatusInProgress || next == domain.StatusCancelled) ||
		current == domain.StatusInProgress && next == domain.StatusCompleted {
		return nil
	}
	return domain.ErrInvalidTripStatus
}

func withFare(trip *domain.Trip) *domain.Trip {
	if trip != nil && trip.FareCents <= 0 {
		trip.FareCents = fare(trip.Pickup, trip.Dropoff)
		trip.Currency = "USD"
	}
	return trip
}

func fare(pickup, dropoff domain.Location) int64 {
	return 350 + int64(math.Round(120*geo.Kilometers(pickup, dropoff)))
}

func canRead(actor authn.Actor, trip *domain.Trip) bool {
	switch actor.Role {
	case authn.Operator:
		return actor.ID != ""
	case authn.Rider:
		return actor.ID != "" && trip.RiderAccountID == actor.ID
	case authn.Driver:
		return actor.ID != "" && trip.DriverAccountID == actor.ID
	default:
		return false
	}
}

func canTransition(actor authn.Actor, trip *domain.Trip, status domain.TripStatus) bool {
	if actor.ID == "" {
		return false
	}
	switch actor.Role {
	case authn.Operator:
		return status == domain.StatusAssigned || status == domain.StatusCancelled
	case authn.Rider:
		return actor.ID == trip.RiderAccountID && status == domain.StatusCancelled
	case authn.Driver:
		return actor.ID == trip.DriverAccountID && (status == domain.StatusInProgress || status == domain.StatusCompleted)
	default:
		return false
	}
}
