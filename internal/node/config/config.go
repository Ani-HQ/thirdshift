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
)

type Config struct {
	DataDir           string
	CoordinatorURL    string
	ModelID           string
	HeartbeatInterval time.Duration
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
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create node data dir: %w", err)
	}
	path := filepath.Join(cfg.DataDir, "config.toml")
	body := fmt.Sprintf("coordinator_url = %q\nmodel_id = %q\nheartbeat_interval_seconds = %d\n",
		cfg.CoordinatorURL,
		cfg.ModelID,
		int(cfg.HeartbeatInterval.Seconds()),
	)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
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
}
