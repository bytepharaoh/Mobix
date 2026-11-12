package messaging

import "time"

const (
	CompletedKind  = "mobix.trip.completed.v1"
	CompletedQueue = "mobix.payment.trip-completed.v1"

	RideEventKind   = "mobix.ride.event.v1"
	RideExchange    = "mobix.ride.events.v1"
	RideBufferQueue = "mobix.realtime.ride-events.v1.buffer"
)

type TripCompleted struct {
	ID          string    `bson:"id" json:"id"`
	TripID      string    `bson:"trip_id" json:"trip_id"`
	AmountCents int64     `bson:"amount_cents" json:"amount_cents"`
	Currency    string    `bson:"currency" json:"currency"`
	OccurredAt  time.Time `bson:"occurred_at" json:"occurred_at"`
}

type RideEventType string

const (
	RideTripCreated       RideEventType = "trip.created"
	RideTripStatusChanged RideEventType = "trip.status_changed"
	RideOffersUpdated     RideEventType = "offers.updated"
	RideMatchingExhausted RideEventType = "matching.exhausted"
)

// RideEvent is a versioned notification, not a second copy of trip state.
// Authorized clients fetch a current snapshot and use these events to know
// when that snapshot changed. Stable IDs make at-least-once delivery safe.
type RideEvent struct {
	ID              string        `bson:"id" json:"id"`
	TripID          string        `bson:"trip_id" json:"trip_id"`
	Type            RideEventType `bson:"type" json:"type"`
	Status          string        `bson:"status" json:"status"`
	RiderAccountID  string        `bson:"rider_account_id,omitempty" json:"rider_account_id,omitempty"`
	DriverAccountID string        `bson:"driver_account_id,omitempty" json:"driver_account_id,omitempty"`
	OfferAccountIDs []string      `bson:"offer_account_ids,omitempty" json:"offer_account_ids,omitempty"`
	Version         int64         `bson:"version" json:"version"`
	OccurredAt      time.Time     `bson:"occurred_at" json:"occurred_at"`
}
