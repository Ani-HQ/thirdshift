package config

import "os"

const (
	defaultCoordinatorAddr = ":8080"

	envCoordinatorAddr    = "THIRDSHIFT_COORDINATOR_ADDR"
	envThirdshiftDatabase = "THIRDSHIFT_DATABASE_URL"
	envStandardDatabase   = "DATABASE_URL"
	envThirdshiftVersion  = "THIRDSHIFT_VERSION"
)

type Config struct {
	Addr              string
	DatabaseURL       string
	DatabaseURLSource string
	Version           string
}

func Load(defaultVersion string) Config {
	cfg := Config{
		Addr:    getenv(envCoordinatorAddr, defaultCoordinatorAddr),
		Version: getenv(envThirdshiftVersion, defaultVersion),
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
