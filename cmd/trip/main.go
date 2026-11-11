package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/bytepharaoh/Mobix/internal/trip/dispatch"
	"github.com/bytepharaoh/Mobix/internal/trip/driverclient"
	"github.com/bytepharaoh/Mobix/internal/trip/handler"
	"github.com/bytepharaoh/Mobix/internal/trip/repository"
	"github.com/bytepharaoh/Mobix/internal/trip/service"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	"github.com/bytepharaoh/Mobix/pkg/config"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"github.com/bytepharaoh/Mobix/pkg/mongox"
)

func main() { httpapi.Main(run) }

func run(ctx context.Context) error {
	delegation, err := authn.LoadDelegation()
	if err != nil {
		return err
	}
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
	matchingConfig, err := dispatch.LoadConfig()
	if err != nil {
		return err
	}
	drivers := driverclient.New(driverURL, delegation)
	matcher := dispatch.New(repo, drivers, matchingConfig)
	worker, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Go(func() { messaging.RunPublisher(worker, config.RabbitURL(), repo) })
	workers.Go(func() { matcher.Run(worker) })
	defer func() {
		cancel()
		workers.Wait()
	}()
	mux := httpapi.Mux("trip", func(ctx context.Context) error { return database.Client().Ping(ctx, nil) })
	handler.Register(mux, service.New(repo), repo, drivers)
	handler.RegisterOffers(mux, matcher)
	return httpapi.Run(ctx, config.GetString("TRIP_HTTP_PORT", "8082"), delegation.Protect("trip", mux))
}
