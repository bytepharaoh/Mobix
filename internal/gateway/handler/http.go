package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/bytepharaoh/Mobix/internal/accounts"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
)

func New(trip, driver, payment *url.URL, auth *accounts.Service, delegation authn.Delegation, databaseReady func(context.Context) error) http.Handler {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	mux := httpapi.Mux("gateway", func(ctx context.Context) error {
		if err := databaseReady(ctx); err != nil {
			return err
		}
		for _, upstream := range []*url.URL{trip, driver, payment} {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.JoinPath("ready").String(), nil)
			if err != nil {
				return err
			}
			response, err := client.Do(req)
			if err != nil {
				return err
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("upstream %s returned %d", upstream.Host, response.StatusCode)
			}
		}
		return nil
	})

	register := func(audience string, upstream *url.URL, routes ...string) {
		proxy := &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(upstream)
				r.Out.Header.Del("Authorization")
				r.Out.Header.Del(authn.IdentityHeader)
				delegation.Sign(r.Out, audience, authn.From(r.In.Context()))
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				httpapi.Error(w, apperrors.ErrUnavailable)
			},
		}
		for _, route := range routes {
			mux.Handle(route, auth.Require(proxy))
		}
	}
	register("driver", driver, "POST /v1/drivers", "GET /v1/drivers/me", "GET /v1/drivers/{id}",
		"PATCH /v1/drivers/{id}/availability", "PATCH /v1/drivers/{id}/location")
	register("trip", trip, "GET /v1/drivers/{id}/activity", "POST /v1/trips", "GET /v1/trips/{id}", "PATCH /v1/trips/{id}/status")
	register("payment", payment, "GET /v1/payments/{tripID}")
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(w, apperrors.ErrNotFound)
	})
	auth.RegisterHTTP(mux)
	RegisterDocs(mux)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.ContentLength > httpapi.MaxBody {
			httpapi.Error(w, apperrors.New(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Request body exceeds 64 KiB"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
