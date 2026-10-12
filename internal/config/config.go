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
	AppEnv                       string
	PublicURL                    string
	DatabaseURL                  string
	MasterEncryptionKey          string
	MasterEncryptionPreviousKeys string
	AdminToken                   string
	AdminPasswordHash            string
	LogLevel                     string
	HTTPAddr                     string
	YooKassaAPIURL               string
	WorkerConcurrency            int
	PaymentPollInterval          time.Duration
	ProviderIdempotencyWindow    time.Duration
	ShutdownTimeout              time.Duration
}

// IsProduction reports whether the service runs in the production environment.
func (c *Config) IsProduction() bool { return c.AppEnv == "production" }

// Load reads configuration from the environment and applies safe defaults.
// DATABASE_URL and MASTER_ENCRYPTION_KEY are mandatory: the service cannot
// store provider secrets without the encryption key.
func Load() (*Config, error) {
	c := &Config{
		AppEnv:                       getenv("APP_ENV", "development"),
		PublicURL:                    getenv("PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:                  os.Getenv("DATABASE_URL"),
		MasterEncryptionKey:          os.Getenv("MASTER_ENCRYPTION_KEY"),
		MasterEncryptionPreviousKeys: os.Getenv("MASTER_ENCRYPTION_PREVIOUS_KEYS"),
		AdminToken:                   os.Getenv("ADMIN_TOKEN"),
		AdminPasswordHash:            os.Getenv("ADMIN_PASSWORD_HASH"),
		LogLevel:                     getenv("LOG_LEVEL", "info"),
		HTTPAddr:                     getenv("HTTP_ADDR", ":8080"),
		YooKassaAPIURL:               getenv("YOOKASSA_API_URL", "https://api.yookassa.ru"),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}
	if c.MasterEncryptionKey == "" {
		return nil, fmt.Errorf("config: MASTER_ENCRYPTION_KEY is required")
	}

	var err error
	if c.WorkerConcurrency, err = getenvInt("WORKER_CONCURRENCY", 5); err != nil {
		return nil, err
	}
	if c.PaymentPollInterval, err = getenvDuration("PAYMENT_POLL_INTERVAL", 30*time.Second); err != nil {
		return nil, err
	}
	if c.ProviderIdempotencyWindow, err = getenvDuration("PROVIDER_IDEMPOTENCY_WINDOW", 24*time.Hour); err != nil {
		return nil, err
	}
	if c.ShutdownTimeout, err = getenvDuration("SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return nil, err
	}

	return c, nil
}

// AdminEnabled reports whether the administrative API is configured. When it is
// not, /admin/* responds 503 rather than being open (docs/SECURITY.md §3).
func (c *Config) AdminEnabled() bool { return c.AdminToken != "" }

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
