package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

type mongoRepository struct {
	collection *mongo.Collection
}

func NewMongoRepository(database *mongo.Database) Repository {
	return &mongoRepository{
		collection: database.Collection("driver"),
	}
}

func (r *mongoRepository) Create(ctx context.Context, driver *domain.Driver) (*domain.Driver, error) {
	now := time.Now()
	driver.UpdatedAt = now
	driver.CreatedAt = now
	result, err := r.collection.InsertOne(ctx, driver)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	driver.ID = result.InsertedID.(bson.ObjectID)
	return driver, nil
}

func (r *mongoRepository) FindById(ctx context.Context, id string) (*domain.Driver, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperrors.ErrInvalidInput
	}
	var driver domain.Driver
	err = r.collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&driver)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, domain.ErrDriverNotFound
		}
		return nil, apperrors.ErrInternal

	}
	return &driver, nil
}

func (r *mongoRepository) IncreaseTotalTrips(ctx context.Context, id string, tripDelta int) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return apperrors.ErrInvalidInput
	}

	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": objectID},
		bson.M{
			"$inc": bson.M{
				"total_trips": tripDelta,
			},
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		},
	)
	if err != nil {
		return apperrors.ErrInternal
	}
	if result.MatchedCount == 0 {
		return domain.ErrDriverNotFound
	}

	return nil
}

func (r *mongoRepository) UpdateStatus(ctx context.Context, id string, status domain.DriverStatus) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return apperrors.ErrInvalidInput
	}

	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": objectID},
		bson.M{
			"$set": bson.M{
				"status":     status,
				"updated_at": time.Now(),
			},
		},
	)
	if err != nil {
		return apperrors.ErrInternal
	}
	if result.MatchedCount == 0 {
		return domain.ErrDriverNotFound
	}

	return nil
}

func (r *mongoRepository) UpdateLastLogin(ctx context.Context, id string, loginTime time.Time) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return apperrors.ErrInvalidInput
	}

	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": objectID},
		bson.M{
			"$set": bson.M{
				"last_login_at": loginTime,
				"updated_at":    time.Now(),
			},
		},
	)
	if err != nil {
		return apperrors.ErrInternal
	}
	if result.MatchedCount == 0 {
		return domain.ErrDriverNotFound
	}

	return nil
}

func (r *mongoRepository) UpdateRating(ctx context.Context, id string, rating float64) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return apperrors.ErrInvalidInput
	}

	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": objectID},
		bson.M{
			"$set": bson.M{
				"rating":     rating,
				"updated_at": time.Now(),
			},
		},
	)
	if err != nil {
		return apperrors.ErrInternal
	}
	if result.MatchedCount == 0 {
		return domain.ErrDriverNotFound
	}

	return nil
}
