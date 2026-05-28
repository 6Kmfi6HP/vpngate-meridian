package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	TotalRequests     int
	WorkerCount       int
	OutputDir         string
	StatePath         string
	ActiveMissLimit   int
	PruneMissLimit    int
	RequestTimeoutMs  int
	NoProgress        bool
	IsCI              bool
}

func Load() *Config {
	cfg := &Config{
		TotalRequests:    getEnvInt("TOTAL_REQUESTS", 1500),
		WorkerCount:      getEnvInt("WORKER_COUNT", 8),
		OutputDir:        getEnvStr("OUTPUT_DIR", "public"),
		StatePath:        getEnvStr("STATE_PATH", "public/state/servers.json"),
		ActiveMissLimit:  getEnvInt("ACTIVE_MISS_LIMIT", 12),
		PruneMissLimit:   getEnvInt("PRUNE_MISS_LIMIT", 48),
		RequestTimeoutMs: getEnvInt("REQUEST_TIMEOUT_MS", 30000),
		NoProgress:       getEnvStr("NO_PROGRESS", "") == "1",
		IsCI:             getEnvStr("CI", "") != "",
	}
	return cfg
}

func (c *Config) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutMs) * time.Millisecond
}

func getEnvStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
