package repository

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

type MongoRepository struct {
	collection *mongo.Collection
}

func NewMongoRepository(database *mongo.Database) *MongoRepository {
	return &MongoRepository{
		collection: database.Collection("driver"),
	}
}

func (r *MongoRepository) Create(ctx context.Context, driver *domain.Driver) (*domain.Driver, error) {
	now := time.Now()
	driver.UpdatedAt = now
	driver.CreatedAt = now
	driver.ID = bson.NewObjectID()
	if _, err := r.collection.InsertOne(ctx, driver); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, domain.ErrDriverAlreadyExists
		}
		return nil, fmt.Errorf("insert driver: %w", err)
	}
	return driver, nil
}

func (r *MongoRepository) FindById(ctx context.Context, id string) (*domain.Driver, error) {
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

func (r *MongoRepository) IncreaseTotalTrips(ctx context.Context, id string, tripDelta int) error {
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

func (r *MongoRepository) UpdateStatus(ctx context.Context, id string, status domain.DriverStatus) error {
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

func (r *MongoRepository) UpdateLastLogin(ctx context.Context, id string, loginTime time.Time) error {
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

func (r *MongoRepository) UpdateRating(ctx context.Context, id string, rating float64) error {
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

func (r *MongoRepository) EnsureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{{
		Keys:    bson.D{{Key: "account_id", Value: 1}},
		Options: options.Index().SetName("unique_driver_account").SetUnique(true).SetSparse(true),
	}}
	for _, field := range []string{"email", "phone", "car_number"} {
		indexes = append(indexes, mongo.IndexModel{
			Keys: bson.D{{Key: field, Value: 1}},
			Options: options.Index().SetName("unique_driver_" + field).SetUnique(true).
				SetPartialFilterExpression(bson.M{field: bson.M{"$gt": ""}}).
				SetCollation(&options.Collation{Locale: "en", Strength: 2}),
		})
	}
	_, err := r.collection.Indexes().CreateMany(ctx, indexes)
	return err
}

func (r *MongoRepository) UpdateLocation(ctx context.Context, id string, location domain.Location) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return apperrors.ErrInvalidInput
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": objectID},
		bson.M{"$set": bson.M{"location": location, "updated_at": time.Now().UTC()}})
	if err != nil {
		return fmt.Errorf("update driver location: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrDriverNotFound
	}
	return nil
}

func (r *MongoRepository) FindByAccountID(ctx context.Context, accountID string) (*domain.Driver, error) {
	var driver domain.Driver
	err := r.collection.FindOne(ctx, bson.M{"account_id": accountID}).Decode(&driver)
	if err == mongo.ErrNoDocuments {
		return nil, domain.ErrDriverNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find driver account: %w", err)
	}
	return &driver, nil
}
