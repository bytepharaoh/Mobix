package ratelimit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/authn"
)

type backendFunc func(context.Context, string, Policy) (Decision, error)

func (function backendFunc) Allow(ctx context.Context, key string, policy Policy) (Decision, error) {
	return function(ctx, key, policy)
}

func testConfig() Config {
	policies := make(map[Operation]Policy, 6)
	for _, operation := range []Operation{LoginIP, LoginAccount, RideRequest, Location, OfferPoll, OfferResponse, EventStream} {
		policies[operation] = Policy{Capacity: 3, Window: time.Minute}
	}
	return Config{KeySecret: strings.Repeat("k", 32), Policies: policies}
}

func TestLoginFailsClosedAndAccountTrafficFailsOpen(t *testing.T) {
	backend := backendFunc(func(context.Context, string, Policy) (Decision, error) {
		return Decision{}, errors.New("redis unavailable")
	})
	limiter, err := New(backend, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	loginRequest := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"rider@example.test","password":"password"}`))
	loginRequest.RemoteAddr = "198.51.100.10:1234"
	loginResponse := httptest.NewRecorder()
	limiter.Login(ok).ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusServiceUnavailable || !strings.Contains(loginResponse.Body.String(), "RATE_LIMIT_UNAVAILABLE") {
		t.Fatalf("login outage response = %d %s", loginResponse.Code, loginResponse.Body.String())
	}

	accountRequest := httptest.NewRequest(http.MethodPost, "/v1/trips", nil)
	accountRequest = accountRequest.WithContext(authn.WithActor(accountRequest.Context(), authn.Actor{ID: "rider-1", Role: authn.Rider}))
	accountResponse := httptest.NewRecorder()
	limiter.Account(RideRequest, ok).ServeHTTP(accountResponse, accountRequest)
	if accountResponse.Code != http.StatusNoContent {
		t.Fatalf("authenticated traffic did not fail open: %d %s", accountResponse.Code, accountResponse.Body.String())
	}
}

func TestDeniedRequestReturnsRetryAfterAndDoesNotExposeSubject(t *testing.T) {
	var key string
	backend := backendFunc(func(_ context.Context, actual string, _ Policy) (Decision, error) {
		key = actual
		return Decision{RetryAfter: 1500 * time.Millisecond}, nil
	})
	limiter, err := New(backend, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/drivers/me/offers", nil)
	request = request.WithContext(authn.WithActor(request.Context(), authn.Actor{ID: "sensitive-account-id", Role: authn.Driver}))
	response := httptest.NewRecorder()
	limiter.Account(OfferPoll, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("denied request reached the endpoint")
	})).ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "2" ||
		!strings.Contains(response.Body.String(), "RATE_LIMITED") {
		t.Fatalf("denial response = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if strings.Contains(key, "sensitive-account-id") || !strings.HasPrefix(key, "mobix:rate:v1:offer_poll:") {
		t.Fatalf("unsafe or malformed Redis key %q", key)
	}
}

func TestLoginAccountSubjectRestoresRequestBody(t *testing.T) {
	var calls int
	backend := backendFunc(func(context.Context, string, Policy) (Decision, error) {
		calls++
		return Decision{Allowed: true}, nil
	})
	limiter, err := New(backend, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	body := `{"email":" Rider@Example.Test ","password":"password"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	request.RemoteAddr = "198.51.100.20:1234"
	response := httptest.NewRecorder()
	limiter.Login(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual, readErr := io.ReadAll(r.Body)
		if readErr != nil || string(actual) != body {
			t.Fatalf("body was not restored: %q, %v", actual, readErr)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || calls != 2 {
		t.Fatalf("login response=%d backend calls=%d", response.Code, calls)
	}
}
