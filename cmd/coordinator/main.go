package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/config"
	"github.com/anianroid/thirdshift/internal/coordinator/httpapi"
	"github.com/anianroid/thirdshift/internal/shared/version"
	"github.com/jackc/pgx/v5"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load(version.Version)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}))

	if cfg.DatabaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("database configured via %s but unreachable: %w; verify the database URL and that PostgreSQL is accepting connections", cfg.DatabaseURLSource, err)
		}
		defer conn.Close(context.Background())
		if err := conn.Ping(ctx); err != nil {
			return fmt.Errorf("database configured via %s but did not respond to ping: %w; verify the database URL and that PostgreSQL is accepting connections", cfg.DatabaseURLSource, err)
		}
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewMux(cfg.Version),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("coordinator listening", "addr", cfg.Addr, "version", cfg.Version)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		logger.Info("coordinator stopped")
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("coordinator server failed: %w", err)
	}
}
