package accounts

import (
	"context"
	"errors"
	"net/http"

	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
)

type accountKey struct{}

func (s *Service) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, err := s.Authenticate(r.Context(), authn.Bearer(r))
		if err != nil {
			if errors.Is(err, apperrors.ErrUnauthorized) {
				w.Header().Set("WWW-Authenticate", "Bearer")
			}
			httpapi.Error(w, err)
			return
		}
		ctx := authn.WithActor(r.Context(), account.Actor())
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, accountKey{}, account)))
	})
}

func (s *Service) RegisterHTTP(mux *http.ServeMux) {
	s.registerHTTP(mux, nil)
}

func (s *Service) RegisterHTTPWithLoginMiddleware(mux *http.ServeMux, middleware func(http.Handler) http.Handler) {
	s.registerHTTP(mux, middleware)
}

func (s *Service) registerHTTP(mux *http.ServeMux, loginMiddleware func(http.Handler) http.Handler) {
	var login http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := httpapi.Decode(w, r, &body); err != nil {
			httpapi.Error(w, err)
			return
		}
		result, err := s.Login(r.Context(), body.Email, body.Password)
		if err != nil {
			if errors.Is(err, errBusy) {
				w.Header().Set("Retry-After", "1")
			}
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, result)
	})
	if loginMiddleware != nil {
		login = loginMiddleware(login)
	}
	mux.Handle("POST /v1/auth/login", login)
	mux.Handle("GET /v1/auth/me", s.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, _ := r.Context().Value(accountKey{}).(Account)
		httpapi.JSON(w, http.StatusOK, account)
	})))
	mux.Handle("POST /v1/auth/logout", s.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Logout(r.Context(), authn.Bearer(r)); err != nil {
			httpapi.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	mux.Handle("POST /v1/accounts", s.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authn.From(r.Context()).Role != authn.Operator {
			httpapi.Error(w, apperrors.ErrForbidden)
			return
		}
		var body struct {
			Email    string     `json:"email"`
			Password string     `json:"password"`
			Role     authn.Role `json:"role"`
		}
		if err := httpapi.Decode(w, r, &body); err != nil {
			httpapi.Error(w, err)
			return
		}
		account, err := s.Create(r.Context(), body.Email, body.Password, body.Role)
		if err != nil {
			httpapi.Error(w, err)
			return
		}
		httpapi.JSON(w, http.StatusCreated, account)
	})))
}
