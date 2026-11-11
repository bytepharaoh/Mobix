package authn

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDelegationRejectsReusedOrForgedIdentity(t *testing.T) {
	delegation := Delegation{key: []byte(strings.Repeat("s", 32))}
	actor := Actor{ID: "rider-account", Role: Rider}
	original := httptest.NewRequest(http.MethodGet, "/v1/trips/example", nil)
	delegation.Sign(original, "trip", actor)
	if got, ok := delegation.verify(original, "trip", time.Now()); !ok || got != actor {
		t.Fatalf("signed identity rejected: actor=%+v accepted=%v", got, ok)
	}

	tests := []struct {
		name     string
		method   string
		path     string
		audience string
		change   func(*http.Request)
	}{
		{name: "different method", method: http.MethodDelete, path: "/v1/trips/example", audience: "trip"},
		{name: "different resource", method: http.MethodGet, path: "/v1/trips/another", audience: "trip"},
		{name: "different audience", method: http.MethodGet, path: "/v1/trips/example", audience: "payment"},
		{name: "duplicate headers", method: http.MethodGet, path: "/v1/trips/example", audience: "trip", change: func(r *http.Request) {
			r.Header.Add(IdentityHeader, r.Header.Get(IdentityHeader))
		}},
		{name: "payload changed", method: http.MethodGet, path: "/v1/trips/example", audience: "trip", change: func(r *http.Request) {
			payload, signature, _ := strings.Cut(r.Header.Get(IdentityHeader), ".")
			decoded, err := base64.RawURLEncoding.DecodeString(payload)
			if err != nil {
				t.Fatal(err)
			}
			decoded = []byte(strings.ReplaceAll(string(decoded), "rider-account", "other-account"))
			r.Header.Set(IdentityHeader, base64.RawURLEncoding.EncodeToString(decoded)+"."+signature)
		}},
		{name: "another signing key", method: http.MethodGet, path: "/v1/trips/example", audience: "trip", change: func(r *http.Request) {
			Delegation{key: []byte(strings.Repeat("x", 32))}.Sign(r, "trip", actor)
		}},
		{name: "malformed header", method: http.MethodGet, path: "/v1/trips/example", audience: "trip", change: func(r *http.Request) {
			r.Header.Set(IdentityHeader, "invalid.payload.signature")
		}},
		{name: "oversized header", method: http.MethodGet, path: "/v1/trips/example", audience: "trip", change: func(r *http.Request) {
			r.Header.Set(IdentityHeader, strings.Repeat("x", 2049))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header = original.Header.Clone()
			if test.change != nil {
				test.change(request)
			}
			if got, ok := delegation.verify(request, test.audience, time.Now()); ok {
				t.Fatalf("accepted invalid assertion as %+v", got)
			}
		})
	}
}

func TestDelegationValidatesExpiryAndActorClaims(t *testing.T) {
	delegation := Delegation{key: []byte(strings.Repeat("s", 32))}
	now := time.Now().Truncate(time.Second)
	tests := []struct {
		name   string
		actor  Actor
		expiry time.Time
		accept bool
	}{
		{name: "rider", actor: Actor{ID: "rider", Role: Rider}, expiry: now.Add(30 * time.Second), accept: true},
		{name: "driver", actor: Actor{ID: "driver", Role: Driver}, expiry: now.Add(30 * time.Second), accept: true},
		{name: "operator", actor: Actor{ID: "operator", Role: Operator}, expiry: now.Add(30 * time.Second), accept: true},
		{name: "service", actor: Actor{Role: Service}, expiry: now.Add(30 * time.Second), accept: true},
		{name: "expired", actor: Actor{ID: "rider", Role: Rider}, expiry: now.Add(-time.Second)},
		{name: "expires now", actor: Actor{ID: "rider", Role: Rider}, expiry: now},
		{name: "excessive lifetime", actor: Actor{ID: "rider", Role: Rider}, expiry: now.Add(time.Hour)},
		{name: "missing account", actor: Actor{Role: Driver}, expiry: now.Add(30 * time.Second)},
		{name: "service with account", actor: Actor{ID: "rider", Role: Service}, expiry: now.Add(30 * time.Second)},
		{name: "unknown role", actor: Actor{ID: "rider", Role: Role("admin")}, expiry: now.Add(30 * time.Second)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPatch, "/v1/trips/example/status", nil)
			data, err := json.Marshal(assertion{
				Actor: test.actor, Audience: "trip", Method: request.Method,
				Path: request.URL.EscapedPath(), Expiry: test.expiry.Unix(),
			})
			if err != nil {
				t.Fatal(err)
			}
			payload := base64.RawURLEncoding.EncodeToString(data)
			request.Header.Set(IdentityHeader, payload+"."+base64.RawURLEncoding.EncodeToString(delegation.signature(payload)))
			got, accepted := delegation.verify(request, "trip", now)
			if accepted != test.accept || accepted && got != test.actor {
				t.Fatalf("actor=%+v accepted=%v, want actor=%+v accepted=%v", got, accepted, test.actor, test.accept)
			}
		})
	}
}

func TestProtectExemptsOnlyReadOnlyExactProbePaths(t *testing.T) {
	delegation := Delegation{key: []byte(strings.Repeat("s", 32))}
	tests := []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/health", http.StatusNoContent},
		{http.MethodHead, "/health", http.StatusNoContent},
		{http.MethodGet, "/ready", http.StatusNoContent},
		{http.MethodHead, "/ready", http.StatusNoContent},
		{http.MethodPost, "/health", http.StatusUnauthorized},
		{http.MethodPatch, "/ready", http.StatusUnauthorized},
		{http.MethodGet, "/health/", http.StatusUnauthorized},
		{http.MethodGet, "/ready/anything", http.StatusUnauthorized},
		{http.MethodGet, "/%68ealth", http.StatusUnauthorized},
		{http.MethodGet, "/v1/trips/example", http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			called := false
			handler := delegation.Protect("trip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.status || called != (test.status == http.StatusNoContent) {
				t.Fatalf("status=%d next called=%v, expected status=%d", response.Code, called, test.status)
			}
		})
	}

	actor := Actor{ID: "assigned-driver", Role: Driver}
	request := httptest.NewRequest(http.MethodGet, "/v1/trips/example", nil)
	delegation.Sign(request, "trip", actor)
	response := httptest.NewRecorder()
	delegation.Protect("trip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := From(r.Context()); got != actor {
			t.Errorf("handler received %+v, want %+v", got, actor)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("signed request rejected with %d", response.Code)
	}
}

func TestDelegationRequiresConfiguredKey(t *testing.T) {
	for _, key := range []string{"", "short-secret"} {
		t.Setenv("MOBIX_INTERNAL_KEY", key)
		if _, err := LoadDelegation(); err == nil {
			t.Fatal("accepted missing or short internal key")
		}
	}
	t.Setenv("MOBIX_INTERNAL_KEY", strings.Repeat("k", 32))
	if _, err := LoadDelegation(); err != nil {
		t.Fatal(err)
	}
}
