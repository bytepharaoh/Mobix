package domain

import (
	"time"

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

type Location struct {
	Lat float64 `bson:"lat" json:"lat"`
	Lng float64 `bson:"lng" json:"lng"`
}

// Trip is the core domain object for the trip service.
// One Trip = one document in MongoDB = one ride from request to completion.
type Trip struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	RiderID   string        `bson:"rider_id"      json:"rider_id"`
	DriverID  string        `bson:"driver_id"     json:"driver_id,omitempty"`
	Pickup    Location      `bson:"pickup"        json:"pickup"`
	Dropoff   Location      `bson:"dropoff"       json:"dropoff"`
	Status    TripStatus    `bson:"status"        json:"status"`
	CreatedAt time.Time     `bson:"created_at"    json:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"    json:"updated_at"`
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
	if r.RiderID == "" {
		return ErrMissingRiderID
	}
	if r.Pickup.Lat == 0 && r.Pickup.Lng == 0 {
		return ErrInvalidPickup
	}
	if r.Dropoff.Lat == 0 && r.Dropoff.Lng == 0 {
		return ErrInvalidDropoff
	}
	return nil
}
