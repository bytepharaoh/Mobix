package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bytepharaoh/Mobix/internal/driver/db"
	"github.com/bytepharaoh/Mobix/pkg/config"
	"github.com/bytepharaoh/Mobix/pkg/logger"
)

func main() {

	log := logger.New()
	port := config.GetString("DRIVER_HTTP_PORT", "8083")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"driver"}`))
	})
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Mongo Connection
	client, err := db.ConnectMongo()
	if err != nil {
		log.Error("Connection to Mongo failed", slog.String("error", err))
		os.Exit(1)
	}
	log.Info("Connected to Mongo succesfully")


	go func() {
		log.Info("driver service starting", slog.String("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Disconnect mongo on shutdown
	if err := client.Disconnect(ctx); err != nil {
		log.Error("Failed to disconnect from Mongo", slog.String("error", err))
	} else {
		log.Info("Successfully disconnected from Mongo")
	}
	srv.Shutdown(ctx)
}
