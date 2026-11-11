package handler

import (
	"net/http"

	"github.com/bytepharaoh/Mobix/internal/payment/repository"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
)

func Register(mux *http.ServeMux, payments *repository.Repository) {
	mux.HandleFunc("GET /v1/payments/{tripID}", func(w http.ResponseWriter, r *http.Request) {
		receipt, err := payments.FindByTrip(r.Context(), r.PathValue("tripID"))
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, receipt)
	})
}
