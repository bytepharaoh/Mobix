package domain

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestDispatchCloneOwnsItsOfferSlice(t *testing.T) {
	dispatch := &Dispatch{Offers: []Offer{{ID: bson.NewObjectID(), AccountID: "driver-a"}}}
	clone := dispatch.Clone()
	clone.Offers[0].Declined = true
	clone.Offers = append(clone.Offers, Offer{ID: bson.NewObjectID(), AccountID: "driver-b"})

	if dispatch.Offers[0].Declined || len(dispatch.Offers) != 1 {
		t.Fatalf("mutating clone changed the original dispatch: %+v", dispatch)
	}
}
