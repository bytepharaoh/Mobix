package main

import (
	"context"
	"fmt"

	"github.com/bytepharaoh/Mobix/internal/trip/handler"
	"github.com/bytepharaoh/Mobix/internal/trip/repository"
	"github.com/bytepharaoh/Mobix/internal/trip/service"
	"github.com/bytepharaoh/Mobix/pkg/config"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"github.com/bytepharaoh/Mobix/pkg/mongox"
)

func main() { httpapi.Main(run) }

func run(ctx context.Context) error {
	driverURL, err := httpapi.Upstream(config.GetString("DRIVER_URL", "http://localhost:8083"))
	if err != nil {
		return fmt.Errorf("DRIVER_URL: %w", err)
	}
	database, err := mongox.Open(ctx)
	if err != nil {
		return err
	}
	defer mongox.Close(database.Client())
	repo := repository.NewMongoRepository(database)
	if err := repo.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("prepare trip indexes: %w", err)
	}
	worker, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		messaging.RunPublisher(worker, config.RabbitURL(), repo)
	}()
	defer func() {
		cancel()
		<-done
	}()
	mux := httpapi.Mux("trip", func(ctx context.Context) error { return database.Client().Ping(ctx, nil) })
	handler.Register(mux, service.New(repo), repo, driverURL)
	return httpapi.Run(ctx, config.GetString("TRIP_HTTP_PORT", "8082"), mux)
}
