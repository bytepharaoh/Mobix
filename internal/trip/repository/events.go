package repository

import (
	"fmt"
	"slices"
	"time"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
)

func newRideEvent(trip *domain.Trip, version int64, eventType messaging.RideEventType, status domain.TripStatus,
	driverAccountID string, offerAccountIDs []string, occurredAt time.Time,
) messaging.RideEvent {
	return messaging.RideEvent{
		ID:              fmt.Sprintf("ride-event:%s:%d", trip.ID.Hex(), version),
		TripID:          trip.ID.Hex(),
		Type:            eventType,
		Status:          string(status),
		RiderAccountID:  trip.RiderAccountID,
		DriverAccountID: driverAccountID,
		OfferAccountIDs: uniqueAccounts(offerAccountIDs),
		Version:         version,
		OccurredAt:      occurredAt.UTC().Truncate(time.Millisecond),
	}
}

func dispatchEvent(trip *domain.Trip, next domain.Dispatch, occurredAt time.Time) (messaging.RideEvent, bool) {
	affected := changedOfferAccounts(trip.Dispatch, &next)
	exhausted := !trip.Dispatch.NextAttemptAt.IsZero() && next.NextAttemptAt.IsZero()
	if len(affected) == 0 && !exhausted {
		return messaging.RideEvent{}, false
	}
	eventType := messaging.RideOffersUpdated
	if exhausted {
		eventType = messaging.RideMatchingExhausted
	}
	version := trip.Version + 1
	return newRideEvent(trip, version, eventType, trip.Status, trip.DriverAccountID, affected, occurredAt), true
}

func activeOfferAccounts(dispatch *domain.Dispatch) []string {
	if dispatch == nil || dispatch.BatchExpiresAt.IsZero() {
		return nil
	}
	accounts := make([]string, 0, len(dispatch.Offers))
	for _, offer := range dispatch.Offers {
		if !offer.Declined && offer.ExpiresAt.Equal(dispatch.BatchExpiresAt) {
			accounts = append(accounts, offer.AccountID)
		}
	}
	return uniqueAccounts(accounts)
}

func changedOfferAccounts(current, next *domain.Dispatch) []string {
	if current == nil || next == nil {
		return nil
	}
	currentOffers := make(map[string]domain.Offer, len(current.Offers))
	nextOffers := make(map[string]domain.Offer, len(next.Offers))
	for _, offer := range current.Offers {
		currentOffers[offer.ID.Hex()] = offer
	}
	for _, offer := range next.Offers {
		nextOffers[offer.ID.Hex()] = offer
	}
	var changed []string
	for id, before := range currentOffers {
		after, exists := nextOffers[id]
		if !exists || before.Declined != after.Declined ||
			activeInBatch(before, current) != activeInBatch(after, next) {
			changed = append(changed, before.AccountID)
		}
	}
	for id, after := range nextOffers {
		if _, exists := currentOffers[id]; !exists {
			changed = append(changed, after.AccountID)
		}
	}
	return uniqueAccounts(changed)
}

func activeInBatch(offer domain.Offer, dispatch *domain.Dispatch) bool {
	return !offer.Declined && !dispatch.BatchExpiresAt.IsZero() && offer.ExpiresAt.Equal(dispatch.BatchExpiresAt)
}

func uniqueAccounts(accounts []string) []string {
	seen := make(map[string]struct{}, len(accounts))
	result := make([]string, 0, len(accounts))
	for _, account := range accounts {
		if account == "" {
			continue
		}
		if _, exists := seen[account]; exists {
			continue
		}
		seen[account] = struct{}{}
		result = append(result, account)
	}
	slices.Sort(result)
	return result
}
