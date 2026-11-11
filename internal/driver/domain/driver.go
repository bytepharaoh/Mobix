package domain

import (
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/geo"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Driver statuses
type DriverStatus string

const (
	StatusAvailable DriverStatus = "available"
	StatusOnTrip    DriverStatus = "on_trip"
	StatusOffline   DriverStatus = "offline"
)

// Struct for a location
type Location = geo.Location

// Struct for a driver entity
type Driver struct {
	LocationPoint     *geo.Point    `bson:"location_point,omitempty" json:"-"`
	LocationUpdatedAt time.Time     `bson:"location_updated_at,omitempty" json:"-"`
	AccountID         string        `bson:"account_id,omitempty" json:"-"`
	ID                bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Name              string        `bson:"name" json:"name"`
	Email             string        `bson:"email" json:"email"`
	Phone             string        `bson:"phone" json:"phone"`
	CarNumber         string        `bson:"car_number" json:"carNumber"`
	CarModel          string        `bson:"car_model" json:"carModel"`
	Location          Location      `bson:"location" json:"location"`
	Status            DriverStatus  `bson:"status" json:"status"`
	LastLoginAt       time.Time     `bson:"last_login_at" json:"lastLoginAt"`
	Rating            float64       `bson:"rating" json:"rating"`
	TotalTrips        int           `bson:"total_trips" json:"-"`
	CreatedAt         time.Time     `bson:"created_at" json:"createdAt"`
	UpdatedAt         time.Time     `bson:"updated_at" json:"updatedAt"`
}

type CreateDriverRequest struct {
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Phone     string   `json:"phone"`
	CarNumber string   `json:"carNumber"`
	CarModel  string   `json:"carModel"`
	Location  Location `json:"location"`
}

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
var platePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9 -]{1,19}$`)

func (r *CreateDriverRequest) Validate() error {
	if !r.Location.Valid() {
		return ErrInvalidLocation
	}
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 100 {
		return ErrInvalidName
	}
	if strings.TrimSpace(r.CarModel) == "" || len(r.CarModel) > 100 {
		return ErrInvalidCarModel
	}
	address, err := mail.ParseAddress(r.Email)
	if err != nil || address.Address != r.Email || len(r.Email) > 254 {
		return ErrInvalidEmail
	}
	if !phonePattern.MatchString(r.Phone) {
		return ErrInvalidPhoneNumber
	}
	if !platePattern.MatchString(r.CarNumber) {
		return ErrInvalidCarNumber
	}
	return nil
}

// DispatchProfile is shared with the trip service through its authenticated lookup.
type DispatchProfile struct {
	LocationUpdatedAt time.Time    `json:"location_updated_at"`
	AccountID         string       `json:"account_id"`
	Status            DriverStatus `json:"status"`
}
