package config

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultCoordinatorAddr = ":8080"

	envCoordinatorAddr    = "THIRDSHIFT_COORDINATOR_ADDR"
	envThirdshiftDatabase = "THIRDSHIFT_DATABASE_URL"
	envStandardDatabase   = "DATABASE_URL"
	envThirdshiftVersion  = "THIRDSHIFT_VERSION"
	envOperatorToken      = "THIRDSHIFT_OPERATOR_TOKEN"
	envAccessTokenSecret  = "THIRDSHIFT_ACCESS_TOKEN_SECRET"
	envHeartbeatSeconds   = "THIRDSHIFT_HEARTBEAT_INTERVAL_SECONDS"
	envStaleAfterSeconds  = "THIRDSHIFT_SESSION_STALE_AFTER_SECONDS"
	envSweepSeconds       = "THIRDSHIFT_STALE_SWEEP_INTERVAL_SECONDS"
)

type Config struct {
	Addr               string
	DatabaseURL        string
	DatabaseURLSource  string
	Version            string
	OperatorToken      string
	AccessTokenSecret  string
	HeartbeatInterval  time.Duration
	SessionStaleAfter  time.Duration
	StaleSweepInterval time.Duration
}

func Load(defaultVersion string) Config {
	cfg := Config{
		Addr:               getenv(envCoordinatorAddr, defaultCoordinatorAddr),
		Version:            getenv(envThirdshiftVersion, defaultVersion),
		OperatorToken:      os.Getenv(envOperatorToken),
		AccessTokenSecret:  os.Getenv(envAccessTokenSecret),
		HeartbeatInterval:  durationSeconds(envHeartbeatSeconds, 15*time.Second),
		SessionStaleAfter:  durationSeconds(envStaleAfterSeconds, 45*time.Second),
		StaleSweepInterval: durationSeconds(envSweepSeconds, 15*time.Second),
	}

	if value := os.Getenv(envThirdshiftDatabase); value != "" {
		cfg.DatabaseURL = value
		cfg.DatabaseURLSource = envThirdshiftDatabase
	} else if value := os.Getenv(envStandardDatabase); value != "" {
		cfg.DatabaseURL = value
		cfg.DatabaseURLSource = envStandardDatabase
	}

	return cfg
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func durationSeconds(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
