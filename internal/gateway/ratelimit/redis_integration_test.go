package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/authn"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestTwoLimiterInstancesShareQuotaAndKeepOfferResponsesSeparate(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	container, err := testcontainers.Run(ctx, "redis:8.2-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp").WithStartupTimeout(30*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Error(err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	redisURL := fmt.Sprintf("redis://%s:%s/0", host, port.Port())
	config := testConfig()
	config.RedisURL = redisURL
	config.Policies[OfferPoll] = Policy{Capacity: 3, Window: time.Minute}
	config.Policies[OfferResponse] = Policy{Capacity: 1, Window: time.Minute}

	newReplica := func() (*httptest.Server, *RedisBackend) {
		t.Helper()
		backend, err := OpenRedis(redisURL)
		if err != nil {
			t.Fatal(err)
		}
		limiter, err := New(backend, config)
		if err != nil {
			t.Fatal(err)
		}
		endpoint := limiter.Account(OfferPoll, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor := authn.Actor{ID: "driver-shared-across-replicas", Role: authn.Driver}
			endpoint.ServeHTTP(w, r.WithContext(authn.WithActor(r.Context(), actor)))
		}))
		return server, backend
	}
	first, firstBackend := newReplica()
	second, secondBackend := newReplica()
	t.Cleanup(func() {
		first.Close()
		second.Close()
		_ = firstBackend.Close()
		_ = secondBackend.Close()
	})

	for index, address := range []string{first.URL, second.URL, first.URL, second.URL} {
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := http.StatusNoContent
		if index == 3 {
			want = http.StatusTooManyRequests
		}
		if response.StatusCode != want {
			t.Fatalf("shared request %d returned %d, want %d", index+1, response.StatusCode, want)
		}
	}

	responseLimiter, err := New(firstBackend, config)
	if err != nil {
		t.Fatal(err)
	}
	responseEndpoint := responseLimiter.Account(OfferResponse, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/accept", nil)
	request = request.WithContext(authn.WithActor(request.Context(), authn.Actor{ID: "driver-shared-across-replicas", Role: authn.Driver}))
	recorder := httptest.NewRecorder()
	responseEndpoint.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("exhausted polling quota blocked offer acceptance: %d %s", recorder.Code, recorder.Body.String())
	}
}
