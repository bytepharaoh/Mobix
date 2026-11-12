package repository

import (
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestDispatchEventOnlyReportsClientVisibleChanges(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	offer := domain.Offer{
		ID: bson.NewObjectID(), AccountID: "driver-a", DriverID: bson.NewObjectID().Hex(), ExpiresAt: now.Add(time.Minute),
	}
	trip := &domain.Trip{
		ID: bson.NewObjectID(), RiderAccountID: "rider-a", Status: domain.StatusRequested, Version: 4,
		Dispatch: &domain.Dispatch{
			NextAttemptAt: now, BatchExpiresAt: offer.ExpiresAt, Offers: []domain.Offer{offer},
		},
	}

	retry := trip.Dispatch.Clone()
	retry.NextAttemptAt = now.Add(time.Second)
	if event, visible := dispatchEvent(trip, retry, now); visible {
		t.Fatalf("internal retry emitted a client event: %+v", event)
	}

	exhausted := trip.Dispatch.Clone()
	exhausted.NextAttemptAt = time.Time{}
	exhausted.BatchExpiresAt = time.Time{}
	event, visible := dispatchEvent(trip, exhausted, now)
	if !visible || event.Type != messaging.RideMatchingExhausted || event.Version != 5 ||
		len(event.OfferAccountIDs) != 1 || event.OfferAccountIDs[0] != offer.AccountID {
		t.Fatalf("incorrect matching exhaustion event: %+v, visible=%v", event, visible)
	}
}
