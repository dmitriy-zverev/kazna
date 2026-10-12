// Command migrate applies, reverts and inspects database migrations.
//
// Usage:
//
//	migrate up            apply all pending migrations
//	migrate down          revert the most recent migration
//	migrate version       print the current migration version
//	migrate force <v>     force the version (recover from a dirty state)
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"

	"github.com/dmitriy-zverev/kazna/internal/config"
	"github.com/dmitriy-zverev/kazna/internal/database"
	"github.com/dmitriy-zverev/kazna/internal/logging"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("migrate: fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logging.Setup(cfg.LogLevel)

	if len(args) == 0 {
		return fmt.Errorf("usage: migrate <up|down|version|force <v>>")
	}

	m, err := database.Migrate(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer m.Close()

	switch args[0] {
	case "up":
		return ignoreNoChange(m.Up())
	case "down":
		return ignoreNoChange(m.Down())
	case "version":
		return printVersion(m)
	case "force":
		if len(args) != 2 {
			return fmt.Errorf("usage: migrate force <version>")
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("migrate: invalid version %q: %w", args[1], err)
		}
		return m.Force(v)
	default:
		return fmt.Errorf("migrate: unknown command %q", args[0])
	}
}

func ignoreNoChange(err error) error {
	if errors.Is(err, migrate.ErrNoChange) {
		slog.Info("migrate: no change")
		return nil
	}
	if err != nil {
		return err
	}
	slog.Info("migrate: done")
	return nil
}

func printVersion(m *migrate.Migrate) error {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		slog.Info("migrate: no version applied")
		return nil
	}
	if err != nil {
		return err
	}
	slog.Info("migrate: version", slog.Uint64("version", uint64(v)), slog.Bool("dirty", dirty))
	return nil
}
