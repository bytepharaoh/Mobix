package domain

import (
	"time"

	"github.com/bytepharaoh/Mobix/pkg/geo"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const CandidatePageSize = 32
const MaxDispatchRadiusMeters = 50_000

type CandidateSearch struct {
	Pickup       geo.Location     `json:"pickup"`
	FreshAfter   time.Time        `json:"fresh_after"`
	RadiusMeters int              `json:"radius_meters"`
	After        *CandidateCursor `json:"after,omitempty"`
}

type CandidateCursor struct {
	ID             bson.ObjectID `json:"driver_id" bson:"_id"`
	DistanceMeters float64       `json:"distance_meters" bson:"distance_meters"`
}

type Candidate struct {
	CandidateCursor `bson:",inline"`
	AccountID       string `json:"account_id" bson:"account_id"`
}
