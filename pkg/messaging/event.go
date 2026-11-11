package messaging

import "time"

const (
	CompletedKind  = "mobix.trip.completed.v1"
	CompletedQueue = "mobix.payment.trip-completed.v1"
)

type TripCompleted struct {
	ID          string    `bson:"id" json:"id"`
	TripID      string    `bson:"trip_id" json:"trip_id"`
	AmountCents int64     `bson:"amount_cents" json:"amount_cents"`
	Currency    string    `bson:"currency" json:"currency"`
	OccurredAt  time.Time `bson:"occurred_at" json:"occurred_at"`
}
