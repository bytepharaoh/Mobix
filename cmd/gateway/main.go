package main

import (
	"context"
	"fmt"

	"github.com/bytepharaoh/Mobix/internal/accounts"
	"github.com/bytepharaoh/Mobix/internal/gateway/handler"
	"github.com/bytepharaoh/Mobix/internal/gateway/ratelimit"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	"github.com/bytepharaoh/Mobix/pkg/config"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
	"github.com/bytepharaoh/Mobix/pkg/mongox"
)

func main() { httpapi.Main(run) }

func run(ctx context.Context) error {
	trip, err := httpapi.Upstream(config.GetString("TRIP_URL", "http://localhost:8082"))
	if err != nil {
		return fmt.Errorf("TRIP_URL: %w", err)
	}
	driver, err := httpapi.Upstream(config.GetString("DRIVER_URL", "http://localhost:8083"))
	if err != nil {
		return fmt.Errorf("DRIVER_URL: %w", err)
	}
	payment, err := httpapi.Upstream(config.GetString("PAYMENT_URL", "http://localhost:8084"))
	if err != nil {
		return fmt.Errorf("PAYMENT_URL: %w", err)
	}
	delegation, err := authn.LoadDelegation()
	if err != nil {
		return err
	}
	database, err := mongox.Open(ctx)
	if err != nil {
		return err
	}
	defer mongox.Close(database.Client())
	store := accounts.NewStore(database)
	if err := store.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("prepare account indexes: %w", err)
	}
	auth, err := accounts.NewService(store)
	if err != nil {
		return err
	}
	if err := auth.Bootstrap(ctx, config.GetString("MOBIX_BOOTSTRAP_EMAIL", ""), config.GetString("MOBIX_BOOTSTRAP_PASSWORD", "")); err != nil {
		return err
	}
	limitConfig, err := ratelimit.LoadConfig()
	if err != nil {
		return fmt.Errorf("rate limit configuration: %w", err)
	}
	limitBackend, err := ratelimit.OpenRedis(limitConfig.RedisURL)
	if err != nil {
		return err
	}
	defer limitBackend.Close()
	limits, err := ratelimit.New(limitBackend, limitConfig)
	if err != nil {
		return fmt.Errorf("initialize rate limiter: %w", err)
	}
	ready := func(ctx context.Context) error { return database.Client().Ping(ctx, nil) }
	return httpapi.Run(ctx, config.GetString("GATEWAY_PORT", "8080"), handler.New(trip, driver, payment, auth, delegation, limits, ready))
}
