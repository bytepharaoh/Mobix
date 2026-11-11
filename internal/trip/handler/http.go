package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	driver "github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/domain"
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

func Register(mux *http.ServeMux, trips *service.TripService, activity DriverActivityReader, driverURL *url.URL, delegation authn.Delegation) {
	client := &http.Client{Timeout: 3 * time.Second}
	driverStatus := func(ctx context.Context, id string) (driver.DispatchProfile, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, driverURL.JoinPath("internal", "drivers", id, "dispatch-profile").String(), nil)
		if err != nil {
			return driver.DispatchProfile{}, fmt.Errorf("build driver request: %w", err)
		}
		delegation.Sign(request, "driver", authn.Actor{Role: authn.Service})
		response, err := client.Do(request)
		if err != nil {
			return driver.DispatchProfile{}, apperrors.ErrUnavailable
		}
		defer response.Body.Close()
		switch response.StatusCode {
		case http.StatusNotFound:
			return driver.DispatchProfile{}, driver.ErrDriverNotFound
		case http.StatusOK:
			var value driver.DispatchProfile
			if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
				return driver.DispatchProfile{}, apperrors.ErrUnavailable
			}
			return value, nil
		default:
			return driver.DispatchProfile{}, apperrors.ErrUnavailable
		}
	}
	checkDriver := func(ctx context.Context, id string) (string, error) {
		status, err := driverStatus(ctx, id)
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
		availability, err := driverStatus(r.Context(), id.Hex())
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
