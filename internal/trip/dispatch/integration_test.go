package dispatch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	driver "github.com/bytepharaoh/Mobix/internal/driver/domain"
	driverhandler "github.com/bytepharaoh/Mobix/internal/driver/handler"
	driverrepo "github.com/bytepharaoh/Mobix/internal/driver/repository"
	driverservice "github.com/bytepharaoh/Mobix/internal/driver/service"
	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/driverclient"
	"github.com/bytepharaoh/Mobix/internal/trip/repository"
	"github.com/bytepharaoh/Mobix/internal/trip/service"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/geo"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type dispatchFixture struct {
	ctx     context.Context
	db      *mongo.Database
	drivers *driverrepo.MongoRepository
	trips   *repository.MongoRepository
	matcher *Service
	rides   *service.TripService
	rider   context.Context
}

func newDispatchFixture(t *testing.T) dispatchFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("requires Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	container, err := mongodb.Run(ctx, "mongo:8.0", testcontainers.WithEnv(map[string]string{
		"GLIBC_TUNABLES": "glibc.pthread.rseq=1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Error(err)
		}
	})
	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mongoClient.Disconnect(context.Background()); err != nil {
			t.Error(err)
		}
	})
	db := mongoClient.Database("dispatch_test")
	drivers := driverrepo.NewMongoRepository(db)
	trips := repository.NewMongoRepository(db)
	if err := drivers.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	if err := trips.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOBIX_INTERNAL_KEY", "test-dispatch-internal-key-at-least-32-bytes")
	delegation, err := authn.LoadDelegation()
	if err != nil {
		t.Fatal(err)
	}
	driverMux := http.NewServeMux()
	driverhandler.Register(driverMux, driverservice.New(drivers))
	driverServer := httptest.NewServer(delegation.Protect("driver", driverMux))
	t.Cleanup(driverServer.Close)
	base, err := url.Parse(driverServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	matcher := New(trips, driverclient.New(base, delegation), Config{
		BatchSize: 3, RadiusMeters: 8000, LocationMaxAge: 90 * time.Second, OfferTTL: 30 * time.Second,
	})
	return dispatchFixture{
		ctx: ctx, db: db, drivers: drivers, trips: trips, matcher: matcher,
		rides: service.New(trips), rider: authn.WithActor(ctx, authn.Actor{ID: "test-rider", Role: authn.Rider}),
	}
}

func (f dispatchFixture) driver(t *testing.T, id string, where geo.Location, status driver.DriverStatus) *driver.Driver {
	t.Helper()
	d, err := f.drivers.Create(f.ctx, &driver.Driver{AccountID: id, Location: where, Status: status})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (f dispatchFixture) trip(t *testing.T, pickup geo.Location) *domain.Trip {
	t.Helper()
	trip, err := f.rides.CreateTrip(f.rider, domain.CreateTripRequest{Pickup: pickup, Dropoff: geo.Location{Lat: pickup.Lat + .04, Lng: pickup.Lng}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return trip
}

func (f dispatchFixture) read(t *testing.T, id bson.ObjectID) *domain.Trip {
	t.Helper()
	trip, err := f.trips.FindByID(f.ctx, id.Hex())
	if err != nil {
		t.Fatal(err)
	}
	return trip
}

func (f dispatchFixture) publish(t *testing.T, trip *domain.Trip) []domain.Offer {
	t.Helper()
	if err := f.matcher.advance(f.ctx, *trip); err != nil {
		t.Fatal(err)
	}
	return f.read(t, trip.ID).Dispatch.Offers
}

func (f dispatchFixture) asDriver(account string) context.Context {
	return authn.WithActor(f.ctx, authn.Actor{ID: account, Role: authn.Driver})
}

func (f dispatchFixture) accept(trip *domain.Trip, offer domain.Offer) (*domain.Trip, error) {
	return f.matcher.Accept(f.asDriver(offer.AccountID), trip.ID.Hex(), offer.ID.Hex())
}

func TestBatchedDispatch(t *testing.T) {
	f := newDispatchFixture(t)

	t.Run("nearest fresh drivers get one batch and the next nearest get expired rides", func(t *testing.T) {
		pickup := geo.Location{Lat: 40, Lng: -73}
		stale := f.driver(t, "stale", pickup, driver.StatusAvailable)
		if _, err := f.db.Collection("driver").UpdateOne(f.ctx, bson.M{"_id": stale.ID}, bson.M{
			"$set": bson.M{"location_updated_at": time.Now().Add(-10 * time.Minute)},
		}); err != nil {
			t.Fatal(err)
		}
		busy := f.driver(t, "busy", geo.Location{Lat: 40.001, Lng: -73}, driver.StatusAvailable)
		if _, err := f.trips.Create(f.ctx, &domain.Trip{
			Status: domain.StatusAssigned, DriverID: busy.ID.Hex(), DriverAccountID: busy.AccountID,
		}); err != nil {
			t.Fatal(err)
		}
		f.driver(t, "offline", geo.Location{Lat: 40.002, Lng: -73}, driver.StatusOffline)
		f.driver(t, "out-of-radius", geo.Location{Lat: 41, Lng: -73}, driver.StatusAvailable)
		var available []*driver.Driver
		for i := range 5 {
			available = append(available, f.driver(t, fmt.Sprintf("near-%d", i),
				geo.Location{Lat: 40.01 + float64(i)*.01, Lng: -73}, driver.StatusAvailable))
		}
		trip := f.trip(t, pickup)
		first := f.publish(t, trip)
		if len(first) != 3 {
			t.Fatalf("first batch has %d offers, want 3", len(first))
		}
		for i, offer := range first {
			if offer.DriverID != available[i].ID.Hex() || !offer.ExpiresAt.Equal(first[0].ExpiresAt) {
				t.Fatalf("offer %d did not target the expected driver with the shared deadline: %+v", i, offer)
			}
			inbox, err := f.matcher.Inbox(f.asDriver(offer.AccountID))
			if err != nil || len(inbox) != 1 || inbox[0].ID != offer.ID || inbox[0].TripID != trip.ID {
				t.Fatalf("wrong inbox for driver %d: %+v, %v", i, inbox, err)
			}
		}
		for _, account := range []string{"stale", "busy", "offline", "out-of-radius", available[3].AccountID} {
			inbox, err := f.matcher.Inbox(f.asDriver(account))
			if err != nil || len(inbox) != 0 {
				t.Fatalf("ineligible or uninvited %s got offers: %+v, %v", account, inbox, err)
			}
		}
		if _, err := f.matcher.Accept(f.asDriver(available[1].AccountID), trip.ID.Hex(), first[0].ID.Hex()); !errors.Is(err, domain.ErrOfferNotFound) {
			t.Fatalf("another driver can see or accept an offer: %v", err)
		}
		if _, err := f.matcher.Accept(f.rider, trip.ID.Hex(), first[0].ID.Hex()); !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatalf("rider was allowed to accept an offer: %v", err)
		}
		if err := f.matcher.advance(f.ctx, *f.read(t, trip.ID)); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("a matcher advanced the batch before its deadline: %v", err)
		}

		expired := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
		_, err := f.db.Collection("trips").UpdateOne(f.ctx, bson.M{"_id": trip.ID}, bson.M{"$set": bson.M{
			"dispatch.next_attempt_at": expired, "dispatch.batch_expires_at": expired, "dispatch.offers.$[].expires_at": expired,
		}})
		if err != nil {
			t.Fatal(err)
		}
		first[0].ExpiresAt = expired
		if _, err := f.trips.AcceptOffer(f.ctx, f.read(t, trip.ID), first[0]); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("MongoDB accepted an expired offer: %v", err)
		}
		f.matcher.sweep(f.ctx)
		offers := f.read(t, trip.ID).Dispatch.Offers
		if len(offers) != 5 || offers[3].DriverID != available[3].ID.Hex() || offers[4].DriverID != available[4].ID.Hex() {
			t.Fatalf("second batch must target the next nearest drivers: %+v", offers)
		}
		if _, err := f.accept(trip, first[0]); !errors.Is(err, domain.ErrOfferExpired) {
			t.Fatalf("expired offer returned %v", err)
		}
		assigned, err := f.accept(trip, offers[3])
		if err != nil || assigned.Status != domain.StatusAssigned || assigned.DriverAccountID != offers[3].AccountID {
			t.Fatalf("next driver could not accept: %+v, %v", assigned, err)
		}
		if _, err := f.accept(trip, offers[4]); !errors.Is(err, domain.ErrOfferTaken) {
			t.Fatalf("other open offer did not close: %v", err)
		}
	})

	t.Run("two offered drivers race for one ride", func(t *testing.T) {
		pickup := geo.Location{Lat: 45, Lng: -73}
		f.driver(t, "two-drivers-a", pickup, driver.StatusAvailable)
		f.driver(t, "two-drivers-b", pickup, driver.StatusAvailable)
		trip := f.trip(t, pickup)
		offers := f.publish(t, trip)
		if len(offers) != 2 {
			t.Fatalf("got %d offers, want two", len(offers))
		}
		results := raceOffers(f, trip, offers[0], trip, offers[1])
		winner := requireOneWinner(t, results, domain.ErrOfferTaken)
		assigned := f.read(t, trip.ID)
		if assigned.DriverAccountID != winner.AccountID || assigned.Dispatch.AcceptedOfferID != winner.ID {
			t.Fatalf("wrong winner persisted: %+v", assigned)
		}
		if retry, err := f.accept(trip, winner); err != nil || retry.ID != trip.ID {
			t.Fatalf("winning acceptance retry returned %+v, %v", retry, err)
		}
	})

	t.Run("concurrent matchers publish one batch", func(t *testing.T) {
		pickup := geo.Location{Lat: 46, Lng: -73}
		for i := range 4 {
			f.driver(t, fmt.Sprintf("workers-%d", i), pickup, driver.StatusAvailable)
		}
		trip := f.trip(t, pickup)
		a, b := f.read(t, trip.ID), f.read(t, trip.ID)
		results := make(chan error, 2)
		go func() { results <- f.matcher.advance(f.ctx, *a) }()
		go func() { results <- f.matcher.advance(f.ctx, *b) }()
		first, second := <-results, <-results
		if !(first == nil && errors.Is(second, apperrors.ErrConflict) || second == nil && errors.Is(first, apperrors.ErrConflict)) {
			t.Fatalf("want one successful publication, got %v and %v", first, second)
		}
		state := f.read(t, trip.ID).Dispatch
		if state.Revision != 1 || len(state.Offers) != 3 {
			t.Fatalf("parallel matchers published an unexpected batch: %+v", state)
		}
	})

	t.Run("one driver cannot accept two rides and other drivers can still accept", func(t *testing.T) {
		pickup := geo.Location{Lat: 48, Lng: -73}
		f.driver(t, "two-rides-a", pickup, driver.StatusAvailable)
		f.driver(t, "two-rides-b", geo.Location{Lat: 48.01, Lng: -73}, driver.StatusAvailable)
		first := f.trip(t, pickup)
		second := f.trip(t, pickup)
		a := f.publish(t, first)
		b := f.publish(t, second)
		if len(a) != 2 || len(b) != 2 || a[0].DriverID != b[0].DriverID {
			t.Fatalf("both rides should offer to the same two drivers: %+v, %+v", a, b)
		}
		results := raceOffers(f, first, a[0], second, b[0])
		winner := requireOneWinner(t, results, domain.ErrDriverAssigned)
		remaining, next := second, b[1]
		if winner.ID == b[0].ID {
			remaining, next = first, a[1]
		}
		if current := f.read(t, remaining.ID); current.Status != domain.StatusRequested {
			t.Fatalf("failed acceptance changed the competing ride: %s", current.Status)
		}
		inbox, err := f.matcher.Inbox(f.asDriver(next.AccountID))
		if err != nil || len(inbox) != 1 || inbox[0].ID != next.ID {
			t.Fatalf("the remaining driver should still have an offer: %+v, %v", inbox, err)
		}
		if assigned, err := f.accept(remaining, next); err != nil || assigned.DriverID != next.DriverID {
			t.Fatalf("the other driver could not accept the remaining ride: %+v, %v", assigned, err)
		}
	})

	t.Run("declines advance a batch and rider cancellation closes every offer", func(t *testing.T) {
		pickup := geo.Location{Lat: 51, Lng: -73}
		for i := range 5 {
			f.driver(t, fmt.Sprintf("cancel-%d", i), geo.Location{Lat: 51 + float64(i)*.01, Lng: -73}, driver.StatusAvailable)
		}
		trip := f.trip(t, pickup)
		first := f.publish(t, trip)
		if len(first) != 3 {
			t.Fatalf("got %d initial offers, want three", len(first))
		}
		for _, offer := range first {
			if err := f.matcher.Decline(f.asDriver(offer.AccountID), trip.ID.Hex(), offer.ID.Hex()); err != nil {
				t.Fatal(err)
			}
		}
		f.matcher.sweep(f.ctx)
		offers := f.read(t, trip.ID).Dispatch.Offers
		if len(offers) != 5 || !time.Now().Before(first[0].ExpiresAt) {
			t.Fatalf("all declines should advance before the previous deadline: %+v", offers)
		}
		if _, err := f.rides.UpdateTripStatus(f.rider, trip.ID.Hex(), domain.StatusCancelled, "", nil); err != nil {
			t.Fatal(err)
		}
		for _, offer := range offers[3:] {
			if _, err := f.accept(trip, offer); !errors.Is(err, domain.ErrOfferClosed) {
				t.Fatalf("late accept after cancellation returned %v", err)
			}
			inbox, err := f.matcher.Inbox(f.asDriver(offer.AccountID))
			if err != nil || len(inbox) != 0 {
				t.Fatalf("cancelled offers remain visible: %+v, %v", inbox, err)
			}
		}
		if !f.read(t, trip.ID).Dispatch.NextAttemptAt.IsZero() {
			t.Fatal("cancelled trip still has a scheduled dispatch")
		}
	})

	t.Run("equal-distance candidates beyond the first page are considered", func(t *testing.T) {
		pickup := geo.Location{Lat: 56, Lng: -73}
		for i := range driver.CandidatePageSize + 3 {
			d := f.driver(t, fmt.Sprintf("page-%02d", i), pickup, driver.StatusAvailable)
			if i < driver.CandidatePageSize {
				if _, err := f.trips.Create(f.ctx, &domain.Trip{Status: domain.StatusAssigned, DriverID: d.ID.Hex()}); err != nil {
					t.Fatal(err)
				}
			}
		}
		offers := f.publish(t, f.trip(t, pickup))
		if len(offers) != 3 || offers[0].AccountID != "page-32" || offers[2].AccountID != "page-34" {
			t.Fatalf("did not find all idle drivers beyond the first geospatial page: %+v", offers)
		}
	})
}

type offerResult struct {
	offer domain.Offer
	err   error
}

func raceOffers(f dispatchFixture, first *domain.Trip, a domain.Offer, second *domain.Trip, b domain.Offer) [2]offerResult {
	start := make(chan struct{})
	results := make(chan offerResult, 2)
	for _, call := range []struct {
		trip  *domain.Trip
		offer domain.Offer
	}{{first, a}, {second, b}} {
		go func() {
			<-start
			_, err := f.accept(call.trip, call.offer)
			results <- offerResult{offer: call.offer, err: err}
		}()
	}
	close(start)
	return [2]offerResult{<-results, <-results}
}

func requireOneWinner(t *testing.T, results [2]offerResult, rejection error) domain.Offer {
	t.Helper()
	if results[0].err == nil && errors.Is(results[1].err, rejection) {
		return results[0].offer
	}
	if results[1].err == nil && errors.Is(results[0].err, rejection) {
		return results[1].offer
	}
	t.Fatalf("want one winner and %v, got %v and %v", rejection, results[0].err, results[1].err)
	return domain.Offer{}
}
