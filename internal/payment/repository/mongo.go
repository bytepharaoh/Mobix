package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrPaymentNotFound = apperrors.New(http.StatusNotFound, "PAYMENT_NOT_FOUND", "No payment has been recorded for this trip")

type Receipt struct {
	TripID      string    `bson:"_id" json:"trip_id"`
	EventID     string    `bson:"event_id" json:"event_id"`
	AmountCents int64     `bson:"amount_cents" json:"amount_cents"`
	Currency    string    `bson:"currency" json:"currency"`
	Status      string    `bson:"status" json:"status"`
	RecordedAt  time.Time `bson:"recorded_at" json:"recorded_at"`
}

type Repository struct{ collection *mongo.Collection }

func New(database *mongo.Database) *Repository {
	return &Repository{collection: database.Collection("payments")}
}

func (r *Repository) Record(ctx context.Context, event messaging.TripCompleted) error {
	id, err := bson.ObjectIDFromHex(event.TripID)
	if err != nil || id.IsZero() || id.Hex() != event.TripID ||
		event.ID != "trip-completed:"+event.TripID ||
		event.AmountCents <= 0 || event.AmountCents > 3_000_000 || event.Currency != "USD" || event.OccurredAt.IsZero() {
		return messaging.ErrInvalidEvent
	}
	receipt := Receipt{
		TripID: event.TripID, EventID: event.ID, AmountCents: event.AmountCents,
		Currency: event.Currency, Status: "simulated_paid", RecordedAt: time.Now().UTC(),
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": receipt.TripID},
		bson.M{"$setOnInsert": receipt}, options.UpdateOne().SetUpsert(true))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("record simulated payment: %w", err)
	}
	return nil
}

func (r *Repository) FindByTrip(ctx context.Context, id string) (*Receipt, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperrors.ErrInvalidInput
	}
	var receipt Receipt
	err = r.collection.FindOne(ctx, bson.M{"_id": objectID.Hex()}).Decode(&receipt)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPaymentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find payment: %w", err)
	}
	return &receipt, nil
}
