package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/dmitriy-zverev/kazna/internal/http/respond"
)

// Pinger is anything that can verify a dependency is reachable (e.g. *pgxpool.Pool).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Live reports that the process is running. It never touches dependencies.
func Live(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready reports that the process can serve traffic, verifying the database.
func Ready(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			respond.JSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"reason": "database",
			})
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
