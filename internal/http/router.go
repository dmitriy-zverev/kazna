// Package httpserver assembles the HTTP handler tree for the API process.
package httpserver

import (
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/dmitriy-zverev/kazna/internal/config"
	"github.com/dmitriy-zverev/kazna/internal/http/handlers"
	"github.com/dmitriy-zverev/kazna/internal/http/middleware"
	"github.com/dmitriy-zverev/kazna/internal/http/respond"
)

// NewRouter builds the API handler tree. Public /v1 endpoints are added in
// later stages; this stage wires the middleware chain and health probes.
func NewRouter(logger *slog.Logger, db handlers.Pinger, cfg *config.Config) nethttp.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.RequestLogger(logger))
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/health/live", handlers.Live)
	r.Get("/health/ready", handlers.Ready(db))

	// Uniform error envelope for unmatched routes and methods.
	r.NotFound(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		respond.Error(w, req, nethttp.StatusNotFound, respond.CodeInvalidRequest, "resource not found", false)
	})
	r.MethodNotAllowed(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		respond.Error(w, req, nethttp.StatusMethodNotAllowed, respond.CodeInvalidRequest, "method not allowed", false)
	})

	return r
}
