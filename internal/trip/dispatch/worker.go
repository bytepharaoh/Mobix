package dispatch

import (
	"context"
	"errors"
	"log/slog"
	"time"

	driver "github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const sweepInterval = 500 * time.Millisecond
const retryDelay = 3 * time.Second
const maxOffersPerTrip = 200

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *Service) sweep(ctx context.Context) {
	trips, err := s.store.DueDispatches(ctx, time.Now().UTC(), 32)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("load dispatch jobs", "error", err)
		}
		return
	}
	for _, trip := range trips {
		if ctx.Err() != nil {
			return
		}
		if err := s.advance(ctx, trip); err != nil && !errors.Is(err, apperrors.ErrConflict) && ctx.Err() == nil {
			slog.Warn("advance dispatch", "trip_id", trip.ID.Hex(), "error", err)
		}
	}
}

func (s *Service) advance(ctx context.Context, trip domain.Trip) error {
	next := *trip.Dispatch
	now := time.Now().UTC()
	limit := min(s.config.BatchSize, maxOffersPerTrip-len(next.Offers))
	if !now.Before(next.Deadline) || limit <= 0 {
		next.NextAttemptAt = time.Time{}
		return s.store.SaveDispatch(ctx, trip.ID, next, false)
	}
	searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	candidates, searchErr := s.nearest(searchCtx, trip, now, limit)
	now = time.Now().UTC().Truncate(time.Millisecond)
	if !now.Before(next.Deadline) {
		next.NextAttemptAt = time.Time{}
	} else if len(candidates) == 0 || searchErr != nil {
		next.NextAttemptAt = earlier(now.Add(retryDelay), next.Deadline)
	} else {
		next.BatchExpiresAt = earlier(now.Add(s.config.OfferTTL), next.Deadline)
		next.NextAttemptAt = next.BatchExpiresAt
		for _, candidate := range candidates {
			next.Offers = append(next.Offers, domain.Offer{
				ID: bson.NewObjectID(), DriverID: candidate.ID.Hex(), AccountID: candidate.AccountID,
				ExpiresAt: next.BatchExpiresAt,
			})
		}
	}
	if err := s.store.SaveDispatch(ctx, trip.ID, next, false); err != nil {
		return err
	}
	return searchErr
}

func (s *Service) nearest(ctx context.Context, trip domain.Trip, now time.Time, limit int) ([]driver.Candidate, error) {
	tried := make(map[string]bool, len(trip.Dispatch.Offers))
	for _, offer := range trip.Dispatch.Offers {
		tried[offer.DriverID] = true
	}
	search := driver.CandidateSearch{
		Pickup: trip.Pickup, FreshAfter: now.Add(-s.config.LocationMaxAge), RadiusMeters: s.config.RadiusMeters,
	}
	selected := make([]driver.Candidate, 0, limit)
	for {
		page, err := s.drivers.Candidates(ctx, search)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(page))
		for _, candidate := range page {
			if !tried[candidate.ID.Hex()] {
				ids = append(ids, candidate.ID.Hex())
			}
		}
		if len(ids) > 0 {
			busy, err := s.store.ActiveDrivers(ctx, ids)
			if err != nil {
				return nil, err
			}
			for _, candidate := range page {
				id := candidate.ID.Hex()
				if !tried[id] && !busy[id] {
					selected = append(selected, candidate)
					tried[id] = true
					if len(selected) == limit {
						return selected, nil
					}
				}
			}
		}
		if len(page) < driver.CandidatePageSize {
			return selected, nil
		}
		last := page[len(page)-1].CandidateCursor
		search.After = &last
	}
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
