package config

import (
	"os"
	"strconv"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/jobs"
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
	envSchedulerWarm      = "THIRDSHIFT_SCHEDULER_WEIGHT_WARM_MODEL"
	envSchedulerSuccess   = "THIRDSHIFT_SCHEDULER_WEIGHT_SUCCESS_RATE"
	envSchedulerTPS       = "THIRDSHIFT_SCHEDULER_WEIGHT_TOKENS_PER_SECOND"
	envSchedulerFailures  = "THIRDSHIFT_SCHEDULER_WEIGHT_RECENT_FAILURE"
	envSchedulerThermal   = "THIRDSHIFT_SCHEDULER_WEIGHT_THERMAL"
	envSchedulerFairness  = "THIRDSHIFT_SCHEDULER_WEIGHT_FAIRNESS"
	envSchedulerRegion    = "THIRDSHIFT_SCHEDULER_WEIGHT_REGION"
	envCreditHoldSeconds  = "THIRDSHIFT_CREDIT_HOLD_SECONDS"
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
	SchedulerWeights   jobs.SchedulerWeights
	CreditHold         time.Duration
}

func Load(defaultVersion string) Config {
	weights := jobs.DefaultSchedulerWeights()
	cfg := Config{
		Addr:               getenv(envCoordinatorAddr, defaultCoordinatorAddr),
		Version:            getenv(envThirdshiftVersion, defaultVersion),
		OperatorToken:      os.Getenv(envOperatorToken),
		AccessTokenSecret:  os.Getenv(envAccessTokenSecret),
		HeartbeatInterval:  durationSeconds(envHeartbeatSeconds, 15*time.Second),
		SessionStaleAfter:  durationSeconds(envStaleAfterSeconds, 45*time.Second),
		StaleSweepInterval: durationSeconds(envSweepSeconds, 15*time.Second),
		CreditHold:         durationSeconds(envCreditHoldSeconds, 24*time.Hour),
		SchedulerWeights: jobs.SchedulerWeights{
			WarmModelBonus:            floatEnv(envSchedulerWarm, weights.WarmModelBonus),
			RollingSuccessRate:        floatEnv(envSchedulerSuccess, weights.RollingSuccessRate),
			NormalizedTokensPerSecond: floatEnv(envSchedulerTPS, weights.NormalizedTokensPerSecond),
			LowRecentFailureBonus:     floatEnv(envSchedulerFailures, weights.LowRecentFailureBonus),
			ThermalHeadroom:           floatEnv(envSchedulerThermal, weights.ThermalHeadroom),
			HostFairness:              floatEnv(envSchedulerFairness, weights.HostFairness),
			RegionalPreference:        floatEnv(envSchedulerRegion, weights.RegionalPreference),
		},
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

func floatEnv(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
