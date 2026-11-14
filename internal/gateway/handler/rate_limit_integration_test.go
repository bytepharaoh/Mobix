package handler_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/internal/accounts"
	gateway "github.com/bytepharaoh/Mobix/internal/gateway/handler"
	"github.com/bytepharaoh/Mobix/internal/gateway/ratelimit"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestTwoGatewayReplicasShareLoginAccountQuota(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	mongoContainer, err := mongodb.Run(ctx, "mongo:8.0",
		testcontainers.WithEnv(map[string]string{"GLIBC_TUNABLES": "glibc.pthread.rseq=1"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mongoContainer.Terminate(context.Background()); err != nil {
			t.Error(err)
		}
	})
	mongoURL, err := mongoContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(mongoURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mongoClient.Disconnect(context.Background()); err != nil {
			t.Error(err)
		}
	})
	store := accounts.NewStore(mongoClient.Database("gateway_rate_limit_test"))
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	auth, err := accounts.NewService(store)
	if err != nil {
		t.Fatal(err)
	}

	redisContainer, err := testcontainers.Run(ctx, "redis:8.2-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp").WithStartupTimeout(30*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := redisContainer.Terminate(context.Background()); err != nil {
			t.Error(err)
		}
	})
	host, err := redisContainer.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := redisContainer.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	redisURL := fmt.Sprintf("redis://%s:%s/0", host, port.Port())
	policies := make(map[ratelimit.Operation]ratelimit.Policy, 6)
	for _, operation := range []ratelimit.Operation{
		ratelimit.LoginIP, ratelimit.LoginAccount, ratelimit.RideRequest,
		ratelimit.Location, ratelimit.OfferPoll, ratelimit.OfferResponse, ratelimit.EventStream,
	} {
		policies[operation] = ratelimit.Policy{Capacity: 100, Window: time.Minute}
	}
	policies[ratelimit.LoginAccount] = ratelimit.Policy{Capacity: 3, Window: time.Minute}
	config := ratelimit.Config{
		RedisURL: redisURL, KeySecret: strings.Repeat("gateway-test-key", 3), Policies: policies,
	}
	upstream, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	newReplica := func() (*httptest.Server, *ratelimit.RedisBackend) {
		t.Helper()
		backend, err := ratelimit.OpenRedis(redisURL)
		if err != nil {
			t.Fatal(err)
		}
		limits, err := ratelimit.New(backend, config)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(gateway.New(upstream, upstream, upstream, auth, authn.Delegation{}, limits,
			func(context.Context) error { return nil }))
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

	client := &http.Client{Timeout: 5 * time.Second}
	body := []byte(`{"email":"unknown@example.test","password":"wrong-password"}`)
	for index, address := range []string{first.URL, second.URL, first.URL, second.URL} {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, address+"/v1/auth/login", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := http.StatusUnauthorized
		if index == 3 {
			want = http.StatusTooManyRequests
			if response.Header.Get("Retry-After") == "" {
				t.Fatal("shared quota response omitted Retry-After")
			}
		}
		if response.StatusCode != want {
			t.Fatalf("replica request %d returned %d, want %d", index+1, response.StatusCode, want)
		}
	}
}
