package handler

import (
	"net/http"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/internal/driver/service"
	"github.com/bytepharaoh/Mobix/pkg/geo"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
)

func Register(mux *http.ServeMux, drivers *service.Service) {
	mux.HandleFunc("GET /internal/drivers/{id}/dispatch-profile", func(w http.ResponseWriter, r *http.Request) {
		profile, err := drivers.DispatchProfile(r.Context(), r.PathValue("id"))
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, profile)
	})
	mux.HandleFunc("GET /v1/drivers/me", func(w http.ResponseWriter, r *http.Request) {
		profile, err := drivers.Me(r.Context())
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, profile)
	})
	mux.HandleFunc("POST /v1/drivers", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name      string    `json:"name"`
			Email     string    `json:"email"`
			Phone     string    `json:"phone"`
			CarNumber string    `json:"carNumber"`
			CarModel  string    `json:"carModel"`
			Location  geo.Input `json:"location"`
		}
		if err := httpapi.Decode(w, r, &body); err != nil {
			httpapi.Error(w, err)
			return
		}
		location, ok := body.Location.Location()
		if !ok {
			httpapi.Error(w, domain.ErrInvalidLocation)
			return
		}
		driver, err := drivers.Register(r.Context(), domain.CreateDriverRequest{
			Name: body.Name, Email: body.Email, Phone: body.Phone, CarNumber: body.CarNumber,
			CarModel: body.CarModel, Location: location,
		})
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		w.Header().Set("Location", "/v1/drivers/"+driver.ID.Hex())
		httpapi.JSON(w, http.StatusCreated, driver)
	})
	mux.HandleFunc("GET /v1/drivers/{id}", func(w http.ResponseWriter, r *http.Request) {
		driver, err := drivers.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, driver)
	})
	mux.HandleFunc("PATCH /v1/drivers/{id}/availability", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status domain.DriverStatus `json:"status"`
		}
		if err := httpapi.Decode(w, r, &body); err != nil {
			httpapi.Error(w, err)
			return
		}
		if err := drivers.SetAvailability(r.Context(), r.PathValue("id"), body.Status); err != nil {
			httpapi.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PATCH /v1/drivers/{id}/location", func(w http.ResponseWriter, r *http.Request) {
		var body geo.Input
		if err := httpapi.Decode(w, r, &body); err != nil {
			httpapi.Error(w, err)
			return
		}
		location, ok := body.Location()
		if !ok {
			httpapi.Error(w, domain.ErrInvalidLocation)
			return
		}
		if err := drivers.SetLocation(r.Context(), r.PathValue("id"), location); err != nil {
			httpapi.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
