package main

import (
	"context"
	"fmt"

	"github.com/bytepharaoh/Mobix/internal/gateway/handler"
	"github.com/bytepharaoh/Mobix/pkg/config"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
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
	return httpapi.Run(ctx, config.GetString("GATEWAY_PORT", "8080"), handler.New(trip, driver, payment, config.GetString("MOBIX_API_KEY", "")))
}
