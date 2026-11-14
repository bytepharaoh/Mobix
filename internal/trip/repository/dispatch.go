package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (r *MongoRepository) DueDispatches(ctx context.Context, now time.Time, limit int64) ([]domain.Trip, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"status": domain.StatusRequested, "dispatch.next_attempt_at": bson.M{"$type": "date", "$lte": now},
	}, options.Find().SetSort(bson.D{{Key: "dispatch.next_attempt_at", Value: 1}}).SetLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("find due dispatches: %w", err)
	}
	var trips []domain.Trip
	if err := cursor.All(ctx, &trips); err != nil {
		return nil, fmt.Errorf("read due dispatches: %w", err)
	}
	return trips, nil
}

func (r *MongoRepository) ActiveDrivers(ctx context.Context, driverIDs []string) (map[string]bool, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"driver_id": bson.M{"$in": driverIDs},
		"status":    bson.M{"$in": []domain.TripStatus{domain.StatusAssigned, domain.StatusInProgress}},
	}, options.Find().SetProjection(bson.M{"driver_id": 1, "_id": 0}))
	if err != nil {
		return nil, fmt.Errorf("find drivers on active rides: %w", err)
	}
	var trips []struct {
		DriverID string `bson:"driver_id"`
	}
	if err := cursor.All(ctx, &trips); err != nil {
		return nil, fmt.Errorf("read active driver IDs: %w", err)
	}
	result := make(map[string]bool, len(trips))
	for _, trip := range trips {
		result[trip.DriverID] = true
	}
	return result, nil
}

// SaveDispatch compares the revision shared by the matcher and declining drivers.
// The database clock decides whether a batch is still open or due to advance.
func (r *MongoRepository) SaveDispatch(ctx context.Context, trip *domain.Trip, next domain.Dispatch, decline bool) error {
	filter := bson.M{
		"_id": trip.ID, "status": domain.StatusRequested, "dispatch.revision": next.Revision,
	}
	if trip.Version > 0 {
		filter["version"] = trip.Version
	} else {
		filter["$or"] = bson.A{bson.M{"version": 0}, bson.M{"version": bson.M{"$exists": false}}}
	}
	if decline {
		filter["$expr"] = bson.M{"$gt": bson.A{"$dispatch.batch_expires_at", "$$NOW"}}
	} else {
		filter["dispatch.next_attempt_at"] = bson.M{"$type": "date"}
		filter["$expr"] = bson.M{"$lte": bson.A{"$dispatch.next_attempt_at", "$$NOW"}}
	}
	next.Revision++
	update := bson.M{"$set": bson.M{"dispatch": next}}
	if event, visible := dispatchEvent(trip, next, time.Now()); visible {
		update["$set"].(bson.M)["version"] = event.Version
		update["$push"] = bson.M{"pending_ride_events": event}
	}
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("save dispatch: %w", err)
	}
	if result.MatchedCount == 0 {
		return apperrors.ErrConflict
	}
	return nil
}

func (r *MongoRepository) OfferInbox(ctx context.Context, accountID string, now time.Time) ([]domain.DriverOffer, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"status":                    domain.StatusRequested,
		"dispatch.batch_expires_at": bson.M{"$gt": now},
		"dispatch.offers": bson.M{"$elemMatch": bson.M{
			"account_id": accountID, "expires_at": bson.M{"$gt": now}, "declined": false,
		}},
	}, options.Find().SetSort(bson.D{{Key: "dispatch.batch_expires_at", Value: 1}}).SetLimit(50))
	if err != nil {
		return nil, fmt.Errorf("find driver offers: %w", err)
	}
	defer cursor.Close(ctx)
	offers := make([]domain.DriverOffer, 0)
	for cursor.Next(ctx) {
		var trip domain.Trip
		if err := cursor.Decode(&trip); err != nil {
			return nil, fmt.Errorf("decode driver offer: %w", err)
		}
		for _, offer := range trip.Dispatch.Offers {
			if offer.AccountID == accountID && !offer.Declined && offer.ExpiresAt.After(now) {
				offers = append(offers, domain.DriverOffer{
					ID: offer.ID, TripID: trip.ID, Version: trip.Version, Pickup: trip.Pickup,
					FareCents: trip.FareCents, Currency: trip.Currency, ExpiresAt: offer.ExpiresAt,
				})
			}
		}
	}
	return offers, cursor.Err()
}

func (r *MongoRepository) AcceptOffer(ctx context.Context, trip *domain.Trip, offer domain.Offer) (*domain.Trip, error) {
	filter := bson.M{
		"_id": trip.ID, "status": domain.StatusRequested,
		"dispatch.batch_expires_at": offer.ExpiresAt,
		"$expr":                     bson.M{"$gt": bson.A{"$dispatch.batch_expires_at", "$$NOW"}},
		"dispatch.offers": bson.M{"$elemMatch": bson.M{
			"id": offer.ID, "account_id": offer.AccountID, "driver_id": offer.DriverID,
			"expires_at": offer.ExpiresAt, "declined": false,
		}},
	}
	if trip.Version > 0 {
		filter["version"] = trip.Version
	} else {
		filter["$or"] = bson.A{bson.M{"version": 0}, bson.M{"version": bson.M{"$exists": false}}}
	}
	changedAt := time.Now().UTC().Truncate(time.Millisecond)
	nextVersion := trip.Version + 1
	event := newRideEvent(trip, nextVersion, messaging.RideTripStatusChanged, domain.StatusAssigned,
		offer.AccountID, activeOfferAccounts(trip.Dispatch), changedAt)
	var assigned domain.Trip
	err := r.collection.FindOneAndUpdate(ctx, filter, bson.M{
		"$set": bson.M{
			"status": domain.StatusAssigned, "driver_id": offer.DriverID, "driver_account_id": offer.AccountID,
			"dispatch.accepted_offer_id": offer.ID, "updated_at": changedAt, "version": nextVersion,
		},
		"$unset": bson.M{"dispatch.next_attempt_at": ""},
		"$push":  bson.M{"pending_ride_events": event},
	}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&assigned)
	switch {
	case mongo.IsDuplicateKeyError(err):
		// The unique active-driver index rejects this write in full.
		return nil, domain.ErrDriverAssigned
	case errors.Is(err, mongo.ErrNoDocuments):
		return nil, apperrors.ErrConflict
	case err != nil:
		return nil, fmt.Errorf("accept driver offer: %w", err)
	default:
		return &assigned, nil
	}
}
