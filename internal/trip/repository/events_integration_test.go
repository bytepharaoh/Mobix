package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestRideEventsAreVersionedWithTripWritesAndDrainInOrder(t *testing.T) {
	repository, cleanup := setupTestDB(t)
	defer cleanup()
	repo := repository.(*MongoRepository)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	trip, err := repo.Create(ctx, &domain.Trip{
		Dispatch: domain.NewDispatch(now), RiderID: "rider-a", RiderAccountID: "rider-a",
		Status: domain.StatusRequested, Pickup: domain.Location{Lat: 40, Lng: -73},
		Dropoff: domain.Location{Lat: 40.1, Lng: -73.1}, FareCents: 900, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRideEvent(t, trip, 1, messaging.RideTripCreated, nil)

	first := domain.Offer{
		ID: bson.NewObjectID(), DriverID: bson.NewObjectID().Hex(), AccountID: "driver-a", ExpiresAt: now.Add(time.Minute),
	}
	second := domain.Offer{
		ID: bson.NewObjectID(), DriverID: bson.NewObjectID().Hex(), AccountID: "driver-b", ExpiresAt: first.ExpiresAt,
	}
	next := trip.Dispatch.Clone()
	next.BatchExpiresAt = first.ExpiresAt
	next.NextAttemptAt = first.ExpiresAt
	next.Offers = append(next.Offers, first, second)
	if err := repo.SaveDispatch(ctx, trip, next, false); err != nil {
		t.Fatal(err)
	}
	trip = readTrip(t, ctx, repo, trip.ID)
	assertRideEvent(t, trip, 2, messaging.RideOffersUpdated, []string{"driver-a", "driver-b"})

	next = trip.Dispatch.Clone()
	next.Offers[0].Declined = true
	if err := repo.SaveDispatch(ctx, trip, next, true); err != nil {
		t.Fatal(err)
	}
	trip = readTrip(t, ctx, repo, trip.ID)
	assertRideEvent(t, trip, 3, messaging.RideOffersUpdated, []string{"driver-a"})

	trip, err = repo.AcceptOffer(ctx, trip, second)
	if err != nil {
		t.Fatal(err)
	}
	assertRideEvent(t, trip, 4, messaging.RideTripStatusChanged, []string{"driver-b"})
	if trip.DriverAccountID != "driver-b" || trip.Status != domain.StatusAssigned {
		t.Fatalf("offer assignment was not persisted: %+v", trip)
	}

	trip, err = repo.Transition(ctx, trip.ID.Hex(), trip, domain.Transition{Status: domain.StatusInProgress})
	if err != nil {
		t.Fatal(err)
	}
	assertRideEvent(t, trip, 5, messaging.RideTripStatusChanged, []string{"driver-b"})
	payment := &messaging.TripCompleted{
		ID: "trip-completed:" + trip.ID.Hex(), TripID: trip.ID.Hex(), AmountCents: trip.FareCents,
		Currency: "USD", OccurredAt: now.Add(2 * time.Minute),
	}
	trip, err = repo.Transition(ctx, trip.ID.Hex(), trip, domain.Transition{Status: domain.StatusCompleted, Payment: payment})
	if err != nil {
		t.Fatal(err)
	}
	assertRideEvent(t, trip, 6, messaging.RideTripStatusChanged, []string{"driver-b"})
	if trip.PendingPayment == nil || trip.PendingPayment.ID != payment.ID {
		t.Fatal("completion did not atomically retain the payment event")
	}

	for version := int64(1); version <= 6; version++ {
		event, err := repo.NextRideEvent(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event == nil || event.Version != version || event.ID != fmt.Sprintf("ride-event:%s:%d", trip.ID.Hex(), version) {
			t.Fatalf("outbox event %d is out of order: %+v", version, event)
		}
		if err := repo.MarkRideEventPublished(ctx, *event); err != nil {
			t.Fatal(err)
		}
	}
	event, err := repo.NextRideEvent(ctx)
	if err != nil || event != nil {
		t.Fatalf("drained outbox returned %+v, %v", event, err)
	}
	persisted := readTrip(t, ctx, repo, trip.ID)
	if persisted.Version != 6 || len(persisted.PendingRideEvents) != 0 || persisted.PendingPayment == nil {
		t.Fatalf("draining ride events changed canonical or payment state: %+v", persisted)
	}
}

func assertRideEvent(t *testing.T, trip *domain.Trip, version int64, eventType messaging.RideEventType, offers []string) {
	t.Helper()
	if trip.Version != version || len(trip.PendingRideEvents) != int(version) {
		t.Fatalf("trip version/outbox mismatch: version=%d events=%d", trip.Version, len(trip.PendingRideEvents))
	}
	event := trip.PendingRideEvents[len(trip.PendingRideEvents)-1]
	if event.Version != version || event.Type != eventType || event.TripID != trip.ID.Hex() ||
		event.Status != string(trip.Status) || event.RiderAccountID != trip.RiderAccountID {
		t.Fatalf("incorrect ride event: %+v", event)
	}
	if fmt.Sprint(event.OfferAccountIDs) != fmt.Sprint(offers) {
		t.Fatalf("event offer audience = %v, want %v", event.OfferAccountIDs, offers)
	}
}

func readTrip(t *testing.T, ctx context.Context, repo *MongoRepository, id bson.ObjectID) *domain.Trip {
	t.Helper()
	trip, err := repo.FindByID(ctx, id.Hex())
	if err != nil {
		t.Fatal(err)
	}
	return trip
}
