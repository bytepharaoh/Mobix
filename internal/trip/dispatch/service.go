package dispatch

import (
	"context"
	"errors"
	"time"

	driver "github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Store interface {
	FindByID(context.Context, string) (*domain.Trip, error)
	DueDispatches(context.Context, time.Time, int64) ([]domain.Trip, error)
	ActiveDrivers(context.Context, []string) (map[string]bool, error)
	SaveDispatch(context.Context, *domain.Trip, domain.Dispatch, bool) error
	OfferInbox(context.Context, string, time.Time) ([]domain.DriverOffer, error)
	AcceptOffer(context.Context, *domain.Trip, domain.Offer) (*domain.Trip, error)
}

type Drivers interface {
	Profile(context.Context, string) (driver.DispatchProfile, error)
	Candidates(context.Context, driver.CandidateSearch) ([]driver.Candidate, error)
}

type Service struct {
	store   Store
	drivers Drivers
	config  Config
}

func New(store Store, drivers Drivers, config Config) *Service {
	return &Service{store: store, drivers: drivers, config: config}
}

func (s *Service) Inbox(ctx context.Context) ([]domain.DriverOffer, error) {
	actor := authn.From(ctx)
	if actor.Role != authn.Driver || actor.ID == "" {
		return nil, apperrors.ErrForbidden
	}
	return s.store.OfferInbox(ctx, actor.ID, time.Now().UTC())
}

func (s *Service) Accept(ctx context.Context, tripID, offerID string) (*domain.Trip, error) {
	trip, offer, err := s.findOffer(ctx, tripID, offerID)
	if err != nil {
		return nil, err
	}
	if wonOffer(trip, offer) {
		return trip, nil
	}
	if err := openOffer(trip, offer, time.Now()); err != nil {
		return nil, err
	}
	profile, err := s.drivers.Profile(ctx, offer.DriverID)
	if err != nil {
		return nil, err
	}
	if profile.AccountID != offer.AccountID || profile.Status != driver.StatusAvailable ||
		profile.LocationUpdatedAt.Before(time.Now().Add(-s.config.LocationMaxAge)) {
		return nil, driver.ErrDriverBusy
	}
	assigned, err := s.store.AcceptOffer(ctx, trip, offer)
	if !errors.Is(err, apperrors.ErrConflict) {
		return assigned, err
	}

	// A competing acceptance, cancellation or expiry may have won the write.
	current, offer, err := s.findOffer(ctx, tripID, offerID)
	if err != nil {
		return nil, err
	}
	if wonOffer(current, offer) {
		return current, nil
	}
	if err := openOffer(current, offer, time.Now()); err != nil {
		return nil, err
	}
	return nil, apperrors.ErrConflict
}

func (s *Service) Decline(ctx context.Context, tripID, offerID string) error {
	for range 5 {
		trip, offer, err := s.findOffer(ctx, tripID, offerID)
		if err != nil {
			return err
		}
		if trip.Status == domain.StatusRequested && offer.Declined {
			return nil
		}
		now := time.Now().UTC()
		if err := openOffer(trip, offer, now); err != nil {
			return err
		}
		next := trip.Dispatch.Clone()
		hasPending := false
		for i := range next.Offers {
			item := &next.Offers[i]
			if item.ID == offer.ID {
				item.Declined = true
			}
			hasPending = hasPending || !item.Declined && item.ExpiresAt.Equal(next.BatchExpiresAt)
		}
		if !hasPending {
			next.NextAttemptAt = now
		}
		err = s.store.SaveDispatch(ctx, trip, next, true)
		if !errors.Is(err, apperrors.ErrConflict) {
			return err
		}
	}
	return apperrors.ErrConflict
}

func (s *Service) findOffer(ctx context.Context, tripID, offerID string) (*domain.Trip, domain.Offer, error) {
	actor := authn.From(ctx)
	if actor.Role != authn.Driver || actor.ID == "" {
		return nil, domain.Offer{}, apperrors.ErrForbidden
	}
	id, err := bson.ObjectIDFromHex(offerID)
	if err != nil {
		return nil, domain.Offer{}, apperrors.ErrInvalidInput
	}
	trip, err := s.store.FindByID(ctx, tripID)
	if errors.Is(err, domain.ErrTripNotFound) {
		return nil, domain.Offer{}, domain.ErrOfferNotFound
	}
	if err != nil {
		return nil, domain.Offer{}, err
	}
	if trip.Dispatch != nil {
		for _, offer := range trip.Dispatch.Offers {
			if offer.ID == id && offer.AccountID == actor.ID {
				return trip, offer, nil
			}
		}
	}
	return nil, domain.Offer{}, domain.ErrOfferNotFound
}

func wonOffer(trip *domain.Trip, offer domain.Offer) bool {
	return trip.Dispatch.AcceptedOfferID == offer.ID && trip.DriverAccountID == offer.AccountID &&
		(trip.Status == domain.StatusAssigned || trip.Status == domain.StatusInProgress || trip.Status == domain.StatusCompleted)
}

func openOffer(trip *domain.Trip, offer domain.Offer, now time.Time) error {
	switch trip.Status {
	case domain.StatusAssigned, domain.StatusInProgress, domain.StatusCompleted:
		return domain.ErrOfferTaken
	case domain.StatusRequested:
		if offer.Declined {
			return domain.ErrOfferClosed
		}
		if !offer.ExpiresAt.After(now) || !offer.ExpiresAt.Equal(trip.Dispatch.BatchExpiresAt) {
			return domain.ErrOfferExpired
		}
		return nil
	default:
		return domain.ErrOfferClosed
	}
}
