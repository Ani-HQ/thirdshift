package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/auth"
	"github.com/anianroid/thirdshift/internal/coordinator/config"
	"github.com/anianroid/thirdshift/internal/coordinator/httpapi"
	"github.com/anianroid/thirdshift/internal/coordinator/jobs"
	"github.com/anianroid/thirdshift/internal/coordinator/registration"
	"github.com/anianroid/thirdshift/internal/coordinator/sessions"
	"github.com/anianroid/thirdshift/internal/shared/logging"
	"github.com/anianroid/thirdshift/internal/shared/protocol"
	"github.com/anianroid/thirdshift/internal/shared/version"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load(version.Version)
	logger := logging.NewTextLogger(os.Stderr)

	handler := httpapi.NewMux(cfg.Version)
	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		if cfg.AccessTokenSecret == "" {
			return fmt.Errorf("database-backed node endpoints require THIRDSHIFT_ACCESS_TOKEN_SECRET; set a random secret for local development")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var err error
		pool, err = pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("database configured via %s but could not be parsed or pooled: %w; verify the database URL", cfg.DatabaseURLSource, err)
		}
		defer pool.Close()
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("database configured via %s but did not respond to ping: %w; verify the database URL and that PostgreSQL is accepting connections", cfg.DatabaseURLSource, err)
		}
		store := registration.PGStore{Pool: pool}
		jobStore := jobs.PGStore{Pool: pool}
		jobService := &jobs.Service{
			Store:       jobStore,
			Scheduler:   jobs.Scheduler{Weights: cfg.SchedulerWeights},
			RateLimiter: &jobs.RateLimiter{LimitPerMinute: 60},
			StaleAfter:  cfg.SessionStaleAfter,
			LeaseTTL:    10 * time.Second,
			SyncTimeout: 120 * time.Second,
			CreditHold:  cfg.CreditHold,
			Logger:      logger,
		}
		validator, err := protocolValidator()
		if err != nil {
			return err
		}
		handler = httpapi.NewMuxWithOptions(httpapi.Options{
			Version: cfg.Version,
			Registration: registration.Service{
				Repository: store,
			},
			SessionStore:      store,
			TokenSigner:       auth.TokenSigner{Secret: []byte(cfg.AccessTokenSecret), TTL: time.Hour},
			ProtocolValidator: validator,
			JobService:        jobService,
			CatalogDir:        "models/catalog",
			OperatorToken:     cfg.OperatorToken,
			HeartbeatInterval: cfg.HeartbeatInterval,
			Logger:            logger,
		})
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("coordinator listening", "addr", cfg.Addr, "version", cfg.Version)
		errCh <- server.ListenAndServe()
	}()
	if pool != nil {
		store := registration.PGStore{Pool: pool}
		sweeper := sessions.Sweeper{Store: store, StaleAfter: cfg.SessionStaleAfter}
		go func() {
			err := sweeper.Run(ctx, cfg.StaleSweepInterval)
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("stale session sweeper stopped", "error", err)
			}
		}()
	}

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

func protocolValidator() (*protocol.Validator, error) {
	validator, err := protocol.NewValidator("")
	if err != nil {
		return nil, fmt.Errorf("load protocol schemas: %w", err)
	}
	return validator, nil
}
