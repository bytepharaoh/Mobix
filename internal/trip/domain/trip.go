package domain

import (
	"strings"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/geo"
	"github.com/bytepharaoh/Mobix/pkg/messaging"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type TripStatus string

const (
	StatusRequested  TripStatus = "requested"   // rider created the trip
	StatusAssigned   TripStatus = "assigned"    // driver has been assigned
	StatusInProgress TripStatus = "in_progress" // ride is happening
	StatusCompleted  TripStatus = "completed"   // ride finished normally
	StatusCancelled  TripStatus = "cancelled"   // cancelled before completion

)

// Location holds a GPS coordinate pair.
// Used for both pickup and dropoff points.

type Location = geo.Location

// Trip is the core domain object for the trip service.
// One Trip = one document in MongoDB = one ride from request to completion.
type Trip struct {
	Dispatch             *Dispatch                `bson:"dispatch,omitempty" json:"-"`
	RiderAccountID       string                   `bson:"rider_account_id,omitempty" json:"-"`
	DriverAccountID      string                   `bson:"driver_account_id,omitempty" json:"-"`
	ScopedIdempotencyKey string                   `bson:"scoped_idempotency_key,omitempty" json:"-"`
	PendingPayment       *messaging.TripCompleted `bson:"pending_payment,omitempty" json:"-"`
	FareCents            int64                    `bson:"fare_cents,omitempty" json:"fare_cents"`
	Currency             string                   `bson:"currency,omitempty" json:"currency"`
	ID                   bson.ObjectID            `bson:"_id,omitempty" json:"id"`
	RiderID              string                   `bson:"rider_id"      json:"rider_id"`
	DriverID             string                   `bson:"driver_id"     json:"driver_id,omitempty"`
	Pickup               Location                 `bson:"pickup"        json:"pickup"`
	Dropoff              Location                 `bson:"dropoff"       json:"dropoff"`
	Status               TripStatus               `bson:"status"        json:"status"`
	CreatedAt            time.Time                `bson:"created_at"    json:"created_at"`
	UpdatedAt            time.Time                `bson:"updated_at"    json:"updated_at"`
}

// CreateTripRequest is what the HTTP handler receives from the client.
type CreateTripRequest struct {
	RiderID string   `json:"rider_id"`
	Pickup  Location `json:"pickup"`
	Dropoff Location `json:"dropoff"`
}

// Validate checks that the request has all required fields.
// Called by the service before doing anything with the request.
func (r *CreateTripRequest) Validate() error {
	if strings.TrimSpace(r.RiderID) == "" || len(r.RiderID) > 128 {
		return ErrMissingRiderID
	}
	if !r.Pickup.Valid() {
		return ErrInvalidPickup
	}
	if !r.Dropoff.Valid() {
		return ErrInvalidDropoff
	}
	return nil
}

type DriverActivity struct {
	ActiveTripID   *bson.ObjectID `bson:"active_trip_id" json:"active_trip_id,omitempty"`
	CompletedTrips int64          `bson:"completed_trips" json:"completed_trips"`
}

type Transition struct {
	Status          TripStatus
	DriverID        string
	DriverAccountID string
	Payment         *messaging.TripCompleted
}
