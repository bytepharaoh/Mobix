package handler

import (
	"net/http"
	"net/url"
	"time"

	"github.com/bytepharaoh/Mobix/internal/payment/repository"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func Register(mux *http.ServeMux, payments *repository.Repository, tripURL *url.URL, delegation authn.Delegation) {
	client := &http.Client{Timeout: 3 * time.Second}
	mux.HandleFunc("GET /v1/payments/{tripID}", func(w http.ResponseWriter, r *http.Request) {
		actor := authn.From(r.Context())
		if actor.Role != authn.Rider && actor.Role != authn.Operator {
			httpapi.Error(w, apperrors.ErrForbidden)
			return
		}
		id, err := bson.ObjectIDFromHex(r.PathValue("tripID"))
		if err != nil {
			httpapi.Error(w, apperrors.ErrInvalidInput)
			return
		}
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, tripURL.JoinPath("v1", "trips", id.Hex()).String(), nil)
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		delegation.Sign(request, "trip", actor)
		response, err := client.Do(request)
		if err != nil {
			httpapi.Error(w, apperrors.ErrUnavailable)
			return
		}
		response.Body.Close()
		switch response.StatusCode {
		case http.StatusOK:
		case http.StatusNotFound:
			httpapi.Error(w, apperrors.ErrTripNotFound)
			return
		default:
			httpapi.Error(w, apperrors.ErrUnavailable)
			return
		}
		receipt, err := payments.FindByTrip(r.Context(), id.Hex())
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, receipt)
	})
}
