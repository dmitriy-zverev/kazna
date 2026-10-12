package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitriy-zverev/kazna/internal/database/sqlcgen"
)

// Checker verifies database reachability through the generated query layer.
// It satisfies the handlers.Pinger interface used by /health/ready.
type Checker struct {
	q *sqlcgen.Queries
}

// NewChecker builds a Checker backed by the given pool.
func NewChecker(pool *pgxpool.Pool) *Checker {
	return &Checker{q: sqlcgen.New(pool)}
}

// Ping runs a trivial query to confirm the connection and generated code path.
func (c *Checker) Ping(ctx context.Context) error {
	if _, err := c.q.Ping(ctx); err != nil {
		return fmt.Errorf("database: ping: %w", err)
	}
	return nil
}
