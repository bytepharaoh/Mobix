package handler

import (
	"context"
	"net/http"

	driver "github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/driverclient"
	"github.com/bytepharaoh/Mobix/internal/trip/service"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/geo"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type DriverActivityReader interface {
	DriverActivity(context.Context, string) (domain.DriverActivity, error)
}

func Register(mux *http.ServeMux, trips *service.TripService, activity DriverActivityReader, drivers *driverclient.Client) {
	checkDriver := func(ctx context.Context, id string) (string, error) {
		status, err := drivers.Profile(ctx, id)
		if err != nil {
			return "", err
		}
		if status.Status != driver.StatusAvailable {
			return "", driver.ErrDriverBusy
		}
		return status.AccountID, nil
	}
	mux.HandleFunc("GET /v1/drivers/{id}/activity", func(w http.ResponseWriter, r *http.Request) {
		id, err := bson.ObjectIDFromHex(r.PathValue("id"))
		if err != nil {
			httpapi.Error(w, apperrors.ErrInvalidInput)
			return
		}
		availability, err := drivers.Profile(r.Context(), id.Hex())
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		actor := authn.From(r.Context())
		if actor.Role != authn.Operator && (actor.Role != authn.Driver || actor.ID == "" || availability.AccountID != actor.ID) {
			httpapi.Error(w, driver.ErrDriverNotFound)
			return
		}
		trips, err := activity.DriverActivity(r.Context(), id.Hex())
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		status := availability.Status
		if trips.ActiveTripID != nil {
			status = driver.StatusOnTrip
		}
		httpapi.JSON(w, http.StatusOK, struct {
			domain.DriverActivity
			Availability driver.DriverStatus `json:"availability"`
			Status       driver.DriverStatus `json:"status"`
		}{trips, availability.Status, status})
	})
	mux.HandleFunc("POST /v1/trips", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Pickup  geo.Input `json:"pickup"`
			Dropoff geo.Input `json:"dropoff"`
		}
		if err := httpapi.Decode(w, r, &body); err != nil {
			httpapi.Error(w, err)
			return
		}
		pickup, ok := body.Pickup.Location()
		if !ok {
			httpapi.Error(w, domain.ErrInvalidPickup)
			return
		}
		dropoff, ok := body.Dropoff.Location()
		if !ok {
			httpapi.Error(w, domain.ErrInvalidDropoff)
			return
		}
		trip, err := trips.CreateTrip(r.Context(), domain.CreateTripRequest{
			Pickup: pickup, Dropoff: dropoff,
		}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		w.Header().Set("Location", "/v1/trips/"+trip.ID.Hex())
		httpapi.JSON(w, http.StatusCreated, trip)
	})
	mux.HandleFunc("GET /v1/trips/{id}", func(w http.ResponseWriter, r *http.Request) {
		trip, err := trips.GetTrip(r.Context(), r.PathValue("id"))
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, trip)
	})
	mux.HandleFunc("PATCH /v1/trips/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status   domain.TripStatus `json:"status"`
			DriverID string            `json:"driver_id"`
		}
		if err := httpapi.Decode(w, r, &body); err != nil {
			httpapi.Error(w, err)
			return
		}
		trip, err := trips.UpdateTripStatus(r.Context(), r.PathValue("id"), body.Status, body.DriverID, checkDriver)
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, trip)
	})
}
