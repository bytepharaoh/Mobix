package handler

import (
	"net/http"

	"github.com/bytepharaoh/Mobix/internal/trip/dispatch"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
)

func RegisterOffers(mux *http.ServeMux, offers *dispatch.Service) {
	mux.HandleFunc("GET /v1/drivers/me/offers", func(w http.ResponseWriter, r *http.Request) {
		inbox, err := offers.Inbox(r.Context())
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, inbox)
	})
	mux.HandleFunc("POST /v1/trips/{id}/offers/{offerID}/accept", func(w http.ResponseWriter, r *http.Request) {
		trip, err := offers.Accept(r.Context(), r.PathValue("id"), r.PathValue("offerID"))
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, trip)
	})
	mux.HandleFunc("POST /v1/trips/{id}/offers/{offerID}/decline", func(w http.ResponseWriter, r *http.Request) {
		if err := offers.Decline(r.Context(), r.PathValue("id"), r.PathValue("offerID")); err != nil {
			httpapi.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
