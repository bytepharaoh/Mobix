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

type MongoRepository struct {
	collection *mongo.Collection
}

func NewMongoRepository(database *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: database.Collection("trips")}
}

func (r *MongoRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "driver_id", Value: 1}},
			Options: options.Index().SetName("unique_active_driver").SetUnique(true).
				SetPartialFilterExpression(bson.M{
					"driver_id": bson.M{"$gt": ""},
					"status":    bson.M{"$in": []domain.TripStatus{domain.StatusAssigned, domain.StatusInProgress}},
				}),
		},
		{
			Keys:    bson.D{{Key: "driver_id", Value: 1}, {Key: "status", Value: 1}},
			Options: options.Index().SetName("driver_trip_history"),
		},
		{
			Keys: bson.D{{Key: "rider_account_id", Value: 1}, {Key: "scoped_idempotency_key", Value: 1}},
			Options: options.Index().SetName("unique_rider_create_key").SetUnique(true).
				SetPartialFilterExpression(bson.M{"scoped_idempotency_key": bson.M{"$type": "string"}}),
		},
		{
			Keys:    bson.D{{Key: "pending_payment.occurred_at", Value: 1}},
			Options: options.Index().SetName("pending_payments").SetSparse(true),
		},
	})
	return err
}

func (r *MongoRepository) Create(ctx context.Context, trip *domain.Trip) (*domain.Trip, error) {
	trip.ID = bson.NewObjectID()
	trip.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	trip.UpdatedAt = trip.CreatedAt
	_, err := r.collection.InsertOne(ctx, trip)
	if err == nil {
		return trip, nil
	}
	if !mongo.IsDuplicateKeyError(err) || trip.ScopedIdempotencyKey == "" {
		return nil, fmt.Errorf("insert trip: %w", err)
	}
	var existing domain.Trip
	if err := r.collection.FindOne(ctx, bson.M{"rider_account_id": trip.RiderAccountID, "scoped_idempotency_key": trip.ScopedIdempotencyKey}).Decode(&existing); err != nil {
		return nil, fmt.Errorf("load previous trip request: %w", err)
	}
	if existing.Pickup != trip.Pickup || existing.Dropoff != trip.Dropoff {
		return nil, apperrors.New(409, "IDEMPOTENCY_CONFLICT", "The idempotency key was used with different trip details")
	}
	return &existing, nil
}

func (r *MongoRepository) FindByID(ctx context.Context, id string) (*domain.Trip, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperrors.ErrInvalidInput
	}
	var trip domain.Trip
	if err := r.collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&trip); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrTripNotFound
		}
		return nil, fmt.Errorf("find trip: %w", err)
	}
	return &trip, nil
}

func (r *MongoRepository) Transition(ctx context.Context, id string, expected *domain.Trip, change domain.Transition) (*domain.Trip, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperrors.ErrInvalidInput
	}
	fields := bson.M{"status": change.Status, "updated_at": time.Now().UTC()}
	if change.DriverID != "" {
		fields["driver_id"] = change.DriverID
		fields["driver_account_id"] = change.DriverAccountID
	}
	if payment := change.Payment; payment != nil {
		fields["pending_payment"] = payment
		fields["fare_cents"] = payment.AmountCents
		fields["currency"] = payment.Currency
	}
	var updated domain.Trip
	err = r.collection.FindOneAndUpdate(ctx,
		bson.M{
			"_id": objectID, "status": expected.Status,
			"rider_account_id":  storedAccount(expected.RiderAccountID),
			"driver_account_id": storedAccount(expected.DriverAccountID),
		},
		bson.M{"$set": fields},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	switch {
	case mongo.IsDuplicateKeyError(err):
		return nil, domain.ErrDriverAssigned
	case errors.Is(err, mongo.ErrNoDocuments):
		if _, err := r.FindByID(ctx, id); err != nil {
			return nil, err
		}
		return nil, apperrors.ErrConflict
	case err != nil:
		return nil, fmt.Errorf("transition trip: %w", err)
	default:
		return &updated, nil
	}
}

func storedAccount(id string) any {
	if id == "" {
		return nil // BSON null also matches fields missing on v1 documents.
	}
	return id
}

func (r *MongoRepository) NextPayment(ctx context.Context) (*messaging.TripCompleted, error) {
	var trip domain.Trip
	err := r.collection.FindOne(ctx, bson.M{"pending_payment.occurred_at": bson.M{"$exists": true}},
		options.FindOne().SetSort(bson.D{{Key: "pending_payment.occurred_at", Value: 1}})).Decode(&trip)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	return trip.PendingPayment, err
}

func (r *MongoRepository) MarkPaymentPublished(ctx context.Context, event messaging.TripCompleted) error {
	id, err := bson.ObjectIDFromHex(event.TripID)
	if err != nil {
		return err
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": id, "pending_payment.id": event.ID},
		bson.M{"$unset": bson.M{"pending_payment": ""}})
	return err
}

func (r *MongoRepository) DriverActivity(ctx context.Context, driverID string) (domain.DriverActivity, error) {
	active := []domain.TripStatus{domain.StatusAssigned, domain.StatusInProgress}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{
			"driver_id": driverID,
			"status":    bson.M{"$in": []domain.TripStatus{domain.StatusAssigned, domain.StatusInProgress, domain.StatusCompleted}},
		}}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id":             nil,
			"active_trip_id":  bson.M{"$max": bson.M{"$cond": bson.A{bson.M{"$in": bson.A{"$status", active}}, "$_id", nil}}},
			"completed_trips": bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$status", domain.StatusCompleted}}, 1, 0}}},
		}}},
	}
	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return domain.DriverActivity{}, fmt.Errorf("aggregate driver activity: %w", err)
	}
	defer cursor.Close(ctx)
	var result domain.DriverActivity
	if cursor.Next(ctx) {
		err = cursor.Decode(&result)
	} else {
		err = cursor.Err()
	}
	return result, err
}
