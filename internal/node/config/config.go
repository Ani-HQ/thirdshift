package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	envDataDir        = "THIRDSHIFT_NODE_DATA_DIR"
	envCoordinatorURL = "THIRDSHIFT_COORDINATOR_URL"
	envModelID        = "THIRDSHIFT_MODEL_ID"
	envScheduleFrom   = "THIRDSHIFT_SCHEDULE_FROM"
	envScheduleUntil  = "THIRDSHIFT_SCHEDULE_UNTIL"
	envMaxTempC       = "THIRDSHIFT_MAX_TEMP_C"
	envHardTempC      = "THIRDSHIFT_HARD_TEMP_C"
	envHysteresisC    = "THIRDSHIFT_THERMAL_HYSTERESIS_C"
	envPauseIdleSec   = "THIRDSHIFT_PAUSE_IDLE_TIMEOUT_SECONDS"
)

type Config struct {
	DataDir           string
	CoordinatorURL    string
	ModelID           string
	HeartbeatInterval time.Duration
	ScheduleFrom      string
	ScheduleUntil     string
	MaxTempC          int
	HardTempC         int
	ThermalHysteresis int
	PauseIdleTimeout  time.Duration
}

func DefaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ".thirdshift"
	}
	return filepath.Join(base, "thirdshift")
}

func Load(dataDir string) (Config, error) {
	cfg := Config{
		DataDir:           dataDir,
		ModelID:           "thirdshift-tiny-chat-v1",
		HeartbeatInterval: 15 * time.Second,
		ScheduleFrom:      "00:00",
		ScheduleUntil:     "00:00",
		MaxTempC:          78,
		HardTempC:         88,
		ThermalHysteresis: 5,
		PauseIdleTimeout:  5 * time.Minute,
	}
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	path := filepath.Join(cfg.DataDir, "config.toml")
	if _, err := os.Stat(path); err == nil {
		if err := parseFile(path, &cfg); err != nil {
			return Config{}, err
		}
	} else if !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("stat config file: %w", err)
	}
	applyEnv(&cfg)
	return cfg, nil
}

func Save(cfg Config) error {
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 15 * time.Second
	}
	if cfg.ScheduleFrom == "" {
		cfg.ScheduleFrom = "00:00"
	}
	if cfg.ScheduleUntil == "" {
		cfg.ScheduleUntil = "00:00"
	}
	if cfg.MaxTempC <= 0 {
		cfg.MaxTempC = 78
	}
	if cfg.HardTempC <= 0 {
		cfg.HardTempC = cfg.MaxTempC + 10
	}
	if cfg.HardTempC <= cfg.MaxTempC {
		cfg.HardTempC = cfg.MaxTempC + 10
	}
	if cfg.ThermalHysteresis <= 0 {
		cfg.ThermalHysteresis = 5
	}
	if cfg.PauseIdleTimeout <= 0 {
		cfg.PauseIdleTimeout = 5 * time.Minute
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create node data dir: %w", err)
	}
	path := filepath.Join(cfg.DataDir, "config.toml")
	body := fmt.Sprintf("coordinator_url = %q\nmodel_id = %q\nheartbeat_interval_seconds = %d\nschedule_from = %q\nschedule_until = %q\nmax_temp_c = %d\nhard_temp_c = %d\nthermal_hysteresis_c = %d\npause_idle_timeout_seconds = %d\n",
		cfg.CoordinatorURL,
		cfg.ModelID,
		int(cfg.HeartbeatInterval.Seconds()),
		cfg.ScheduleFrom,
		cfg.ScheduleUntil,
		cfg.MaxTempC,
		cfg.HardTempC,
		cfg.ThermalHysteresis,
		int(cfg.PauseIdleTimeout.Seconds()),
	)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

func HasScheduleOverride(dataDir string) bool {
	if os.Getenv(envScheduleFrom) != "" || os.Getenv(envScheduleUntil) != "" {
		return true
	}
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	path := filepath.Join(dataDir, "config.toml")
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "schedule_from", "schedule_until":
			return true
		}
	}
	return false
}

func parseFile(path string, cfg *Config) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected key = value", path, lineNo)
		}
		key = strings.TrimSpace(key)
		value := strings.Trim(strings.TrimSpace(raw), `"`)
		switch key {
		case "coordinator_url":
			cfg.CoordinatorURL = value
		case "model_id":
			cfg.ModelID = value
		case "heartbeat_interval_seconds":
			seconds, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s:%d: heartbeat_interval_seconds must be an integer", path, lineNo)
			}
			cfg.HeartbeatInterval = time.Duration(seconds) * time.Second
		case "schedule_from":
			cfg.ScheduleFrom = value
		case "schedule_until":
			cfg.ScheduleUntil = value
		case "max_temp_c":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s:%d: max_temp_c must be an integer", path, lineNo)
			}
			cfg.MaxTempC = parsed
		case "hard_temp_c":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s:%d: hard_temp_c must be an integer", path, lineNo)
			}
			cfg.HardTempC = parsed
		case "thermal_hysteresis_c":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s:%d: thermal_hysteresis_c must be an integer", path, lineNo)
			}
			cfg.ThermalHysteresis = parsed
		case "pause_idle_timeout_seconds":
			seconds, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s:%d: pause_idle_timeout_seconds must be an integer", path, lineNo)
			}
			cfg.PauseIdleTimeout = time.Duration(seconds) * time.Second
		default:
			return fmt.Errorf("%s:%d: unknown config key %q", path, lineNo, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	return nil
}

func applyEnv(cfg *Config) {
	if value := os.Getenv(envDataDir); value != "" {
		cfg.DataDir = value
	}
	if value := os.Getenv(envCoordinatorURL); value != "" {
		cfg.CoordinatorURL = value
	}
	if value := os.Getenv(envModelID); value != "" {
		cfg.ModelID = value
	}
	if value := os.Getenv(envScheduleFrom); value != "" {
		cfg.ScheduleFrom = value
	}
	if value := os.Getenv(envScheduleUntil); value != "" {
		cfg.ScheduleUntil = value
	}
	if value := os.Getenv(envMaxTempC); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.MaxTempC = parsed
		}
	}
	if value := os.Getenv(envHardTempC); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.HardTempC = parsed
		}
	}
	if value := os.Getenv(envHysteresisC); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.ThermalHysteresis = parsed
		}
	}
	if value := os.Getenv(envPauseIdleSec); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.PauseIdleTimeout = time.Duration(parsed) * time.Second
		}
	}
}
