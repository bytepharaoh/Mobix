package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const MatchingWindow = 2 * time.Minute

// Dispatch is persisted with the trip so offers and the winning assignment can
// share a conditional write, including on a standalone MongoDB server.
type Dispatch struct {
	Revision        int64         `bson:"revision"`
	Deadline        time.Time     `bson:"deadline"`
	NextAttemptAt   time.Time     `bson:"next_attempt_at,omitempty"`
	BatchExpiresAt  time.Time     `bson:"batch_expires_at,omitempty"`
	Offers          []Offer       `bson:"offers,omitempty"`
	AcceptedOfferID bson.ObjectID `bson:"accepted_offer_id,omitempty"`
}

type Offer struct {
	ID        bson.ObjectID `bson:"id"`
	DriverID  string        `bson:"driver_id"`
	AccountID string        `bson:"account_id"`
	ExpiresAt time.Time     `bson:"expires_at"`
	Declined  bool          `bson:"declined"`
}

// DriverOffer is the view available to the driver invited to this ride.
type DriverOffer struct {
	ID        bson.ObjectID `json:"id"`
	TripID    bson.ObjectID `json:"trip_id"`
	Version   int64         `json:"version"`
	Pickup    Location      `json:"pickup"`
	FareCents int64         `json:"fare_cents"`
	Currency  string        `json:"currency"`
	ExpiresAt time.Time     `json:"expires_at"`
}

func NewDispatch(now time.Time) *Dispatch {
	now = now.UTC().Truncate(time.Millisecond)
	return &Dispatch{Deadline: now.Add(MatchingWindow), NextAttemptAt: now}
}

func (dispatch *Dispatch) Clone() Dispatch {
	if dispatch == nil {
		return Dispatch{}
	}
	clone := *dispatch
	clone.Offers = append([]Offer(nil), dispatch.Offers...)
	return clone
}
