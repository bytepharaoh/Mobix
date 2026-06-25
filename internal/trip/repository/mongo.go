package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

type mongoRepository struct {
	collection *mongo.Collection
}

func NewMongoRepository(database *mongo.Database) Repository {
	return &mongoRepository{
		collection: database.Collection("trips"),
	}
}
func (r *mongoRepository) Create(ctx context.Context, trip *domain.Trip) (*domain.Trip, error) {
	trip.CreatedAt = time.Now()
	trip.UpdatedAt = time.Now()
	result, err := r.collection.InsertOne(ctx, trip)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	trip.ID = result.InsertedID.(bson.ObjectID)
	return trip, nil
}
func (r *mongoRepository) FindByID(ctx context.Context, id string) (*domain.Trip, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperrors.ErrInvalidInput
	}
	var trip domain.Trip
	err = r.collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&trip)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, domain.ErrTripNotFound
		}
		return nil, apperrors.ErrInternal

	}
	return &trip, nil
}
func (r *mongoRepository) UpdateStatus(ctx context.Context, id string, status domain.TripStatus) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return apperrors.ErrInvalidInput
	}
	filter := bson.M{"_id": objectID}
	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now(),
		},
	}
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return apperrors.ErrInternal
	}
	if result.MatchedCount == 0 {
		return domain.ErrTripNotFound
	}
	return nil
}
