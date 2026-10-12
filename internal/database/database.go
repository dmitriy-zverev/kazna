package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// scheme
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitriy-zverev/kazna/migrations"
)

// Pool wraps pgxpool and exposes the operations the application needs.
type Pool = pgxpool.Pool

// NewPool opens a connection pool and verifies connectivity before returning.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}

// RunMigrations applies all pending embedded migrations. It is safe to call on
// every startup: an already up-to-date database returns ErrNoChange, ignored here.
func RunMigrations(databaseURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("database: open migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, pgxURL(databaseURL))
	if err != nil {
		return fmt.Errorf("database: init migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("database: apply migrations: %w", err)
	}
	return nil
}

// Migrate returns a configured migrator for explicit up/down/version operations
// (used by cmd/migrate). The caller must Close it.
func Migrate(databaseURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("database: open migration source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, pgxURL(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("database: init migrator: %w", err)
	}
	return m, nil
}

// pgxURL rewrites a postgres:// DSN into the pgx5:// scheme expected by the
// golang-migrate database driver.
func pgxURL(databaseURL string) string {
	switch {
	case strings.HasPrefix(databaseURL, "postgres://"):
		return "pgx5://" + strings.TrimPrefix(databaseURL, "postgres://")
	case strings.HasPrefix(databaseURL, "postgresql://"):
		return "pgx5://" + strings.TrimPrefix(databaseURL, "postgresql://")
	default:
		return databaseURL
	}
}
