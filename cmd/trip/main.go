package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/config"
	"github.com/bytepharaoh/Mobix/pkg/logger"
)


func main() {
	log:=logger.New()
	port:= config.GetString("TRIP_HTTP_PORT","8082")
	mux:=http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter , r *http.Request) {
		w.Header().Set("Content-Type","application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"trip"}`))
	})
	srv:=&http.Server{
		Addr: ":"+port,
		Handler: mux,
		ReadTimeout: 5*time.Second,
		WriteTimeout: 10*time.Second,
		IdleTimeout: 120*time.Second,
	}
	go func () {
		log.Info("trip service starting",slog.String("port",port))
		if err :=srv.ListenAndServe(); err != nil && err != http.ErrServerClosed{
			log.Error("server error", slog.String("error",err.Error()))
			os.Exit(1)
		}
	}()
	quit:=make(chan os.Signal , 1)
	signal.Notify(quit, syscall.SIGINT , syscall.SIGTERM)
	<-quit

	log.Info("shutting down gracefully...")
	ctx, cancel:= context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
