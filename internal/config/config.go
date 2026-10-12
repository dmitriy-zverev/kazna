package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the runtime configuration loaded from the environment.
// See docs/SECURITY.md and docs/ARCHITECTURE.md for the meaning of each value.
type Config struct {
	AppEnv              string
	PublicURL           string
	DatabaseURL         string
	MasterEncryptionKey string
	AdminPasswordHash   string
	LogLevel            string
	HTTPAddr            string
	WorkerConcurrency   int
	PaymentPollInterval time.Duration
	ShutdownTimeout     time.Duration
}

// IsProduction reports whether the service runs in the production environment.
func (c *Config) IsProduction() bool { return c.AppEnv == "production" }

// Load reads configuration from the environment and applies safe defaults.
// Only DATABASE_URL is mandatory; everything else has a development default.
func Load() (*Config, error) {
	c := &Config{
		AppEnv:              getenv("APP_ENV", "development"),
		PublicURL:           getenv("PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		MasterEncryptionKey: os.Getenv("MASTER_ENCRYPTION_KEY"),
		AdminPasswordHash:   os.Getenv("ADMIN_PASSWORD_HASH"),
		LogLevel:            getenv("LOG_LEVEL", "info"),
		HTTPAddr:            getenv("HTTP_ADDR", ":8080"),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}

	var err error
	if c.WorkerConcurrency, err = getenvInt("WORKER_CONCURRENCY", 5); err != nil {
		return nil, err
	}
	if c.PaymentPollInterval, err = getenvDuration("PAYMENT_POLL_INTERVAL", 30*time.Second); err != nil {
		return nil, err
	}
	if c.ShutdownTimeout, err = getenvDuration("SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return nil, err
	}

	if c.IsProduction() && c.MasterEncryptionKey == "" {
		return nil, fmt.Errorf("config: MASTER_ENCRYPTION_KEY is required in production")
	}

	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer: %w", key, err)
	}
	return n, nil
}

func getenvDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a duration: %w", key, err)
	}
	return d, nil
}
