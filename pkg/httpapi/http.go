package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/logger"
)

const MaxBody = 64 << 10

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Error(w http.ResponseWriter, err error) {
	app, known := apperrors.AsAppError(err)
	if !known || app.Status >= 500 && app.Status != http.StatusServiceUnavailable {
		slog.Error("request failed", "error", err)
	}
	JSON(w, app.Status, app)
}

func Decode(w http.ResponseWriter, r *http.Request, dest any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(dest)
	if err == nil {
		if trailing := decoder.Decode(new(any)); trailing == io.EOF {
			return nil
		} else {
			err = trailing
		}
	}
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		return apperrors.New(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Request body exceeds 64 KiB")
	}
	return apperrors.ErrInvalidInput
}

func Mux(name string, ready func(context.Context) error) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": name})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if ready != nil && ready(ctx) != nil {
			Error(w, apperrors.ErrUnavailable)
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ready", "service": name})
	})
	return mux
}

func Run(ctx context.Context, port string, handler http.Handler) error {
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       time.Minute,
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	slog.Info("HTTP server starting", "port", port)
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		return err
	}
	return nil
}

func Main(run func(context.Context) error) {
	slog.SetDefault(logger.New())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
