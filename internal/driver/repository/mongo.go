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
	driver.LocationUpdatedAt = now
	point := driver.Location.Point()
	driver.LocationPoint = &point
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
	}, {
		Keys:    bson.D{{Key: "location_point", Value: "2dsphere"}},
		Options: options.Index().SetName("driver_location_sphere"),
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
	now := time.Now().UTC()
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$set": bson.M{
		"location": location, "location_point": location.Point(),
		"location_updated_at": now, "updated_at": now,
	}})
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

func (r *MongoRepository) Candidates(ctx context.Context, search domain.CandidateSearch) ([]domain.Candidate, error) {
	near := bson.M{
		"near": search.Pickup.Point(), "key": "location_point",
		"spherical": true, "distanceField": "distance_meters", "maxDistance": search.RadiusMeters,
		"query": bson.M{
			"status":              domain.StatusAvailable,
			"account_id":          bson.M{"$gt": ""},
			"location_updated_at": bson.M{"$gte": search.FreshAfter},
		},
	}
	pipeline := mongo.Pipeline{bson.D{{Key: "$geoNear", Value: near}}}
	if after := search.After; after != nil {
		near["minDistance"] = after.DistanceMeters
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: bson.M{"$or": bson.A{
			bson.M{"distance_meters": bson.M{"$gt": after.DistanceMeters}},
			bson.M{"distance_meters": after.DistanceMeters, "_id": bson.M{"$gt": after.ID}},
		}}}})
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$sort", Value: bson.D{{Key: "distance_meters", Value: 1}, {Key: "_id", Value: 1}}}},
		bson.D{{Key: "$limit", Value: domain.CandidatePageSize}},
		bson.D{{Key: "$project", Value: bson.M{"account_id": 1, "distance_meters": 1}}},
	)
	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("search nearby drivers: %w", err)
	}
	candidates := make([]domain.Candidate, 0, domain.CandidatePageSize)
	if err := cursor.All(ctx, &candidates); err != nil {
		return nil, fmt.Errorf("decode nearby drivers: %w", err)
	}
	return candidates, nil
}
