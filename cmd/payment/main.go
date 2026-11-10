package main

import (
	"context"

	"github.com/bytepharaoh/Mobix/internal/payment/handler"
	"github.com/bytepharaoh/Mobix/internal/payment/repository"
	"github.com/bytepharaoh/Mobix/pkg/config"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"github.com/bytepharaoh/Mobix/pkg/mongox"
)

func main() { httpapi.Main(run) }

func run(ctx context.Context) error {
	database, err := mongox.Open(ctx)
	if err != nil {
		return err
	}
	defer mongox.Close(database.Client())
	repo := repository.New(database)
	worker, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		messaging.RunConsumer(worker, config.RabbitURL(), repo.Record)
	}()
	defer func() {
		cancel()
		<-done
	}()
	mux := httpapi.Mux("payment", func(ctx context.Context) error { return database.Client().Ping(ctx, nil) })
	handler.Register(mux, repo)
	return httpapi.Run(ctx, config.GetString("PAYMENT_HTTP_PORT", "8084"), mux)
}
