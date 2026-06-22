package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Driver statuses
type DriverStatus string

const (
	StatusAvailable DriverStatus = "available"
	StatusOnTrip DriverStatus = "on_trip"
	StatusOffline DriverStatus = "offline"
)

// Struct for a location
type Location struct {
	Lat float64 `bson:"lat" json:"lat"`
	Lng float64 `bson:"lng" json:"lng"`
}

// Struct for a driver entity
type Driver struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name string `bson:"name" json:"name"`
	Email string `bson:"email" json:"email"`
	Phone string `bson:"phone" json:"phone"`
	CarNumber string `bson:"car_number" json:"carNumber"`
	CarModel string `bson:"car_model" json:"carModel"`
	Location Location `bson:"location" json:"location"`
	Status DriverStatus `bson:"status" json:"status"` 
	LastLoginAt time.Time `bson:"last_login_at" json:"lastLoginAt"`
	Rating float64  `bson:"rating" json:"rating"`
	TotalTrips int `bson:"total_trips" json:"totalTrips"`
	CreatedAt time.Time `bson:"created_at,omitempty" json:"createdAt"`	
	UpdatedAt time.Time `bson:"updated_at,omitempty" json:"updatedAt"`
}

type CreateDriverRequest struct {
	Name string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	CarNumber string `json:"carNumber"`
	CarModel string `json:"carModel"`
	Location Location `json:"location"`
}

func (r *CreateDriverRequest) Validate() error {
	// Location validation
	if r.Location.Lat == 0 && r.Location.Lng == 0 {
		return ErrInvalidLocation
	}
	// Name validation
	if r.Name == "" {
		return ErrInvalidName
	}
	// Car model validation
	if r.CarModel == "" {
		return ErrInvalidCarModel
	}
	// Email validation
	if r.Email == "" {
		return ErrInvalidEmail
	}
	// Phone validation
	if r.Phone == "" {
		return ErrInvalidPhoneNumber
	}
	// Car number validation
	if r.CarNumber == "" {
		return ErrInvalidCarNumber
	}
	
	return nil
}