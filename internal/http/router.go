// Package httpserver assembles the HTTP handler tree for the API process.
package httpserver

import (
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/auth"
	"github.com/dmitriy-zverev/kazna/internal/config"
	"github.com/dmitriy-zverev/kazna/internal/http/handlers"
	"github.com/dmitriy-zverev/kazna/internal/http/middleware"
	"github.com/dmitriy-zverev/kazna/internal/http/respond"
	"github.com/dmitriy-zverev/kazna/internal/metrics"
	"github.com/dmitriy-zverev/kazna/internal/payments"
)

// Deps are the collaborators the router needs. Passing them explicitly keeps
// process wiring in main and business wiring testable.
type Deps struct {
	Logger       *slog.Logger
	Config       *config.Config
	Health       handlers.Pinger
	Applications *applications.Service
	Payments     *payments.Service
	Metrics      *metrics.Metrics
}

// NewRouter builds the API handler tree.
//
//	/health/*  probes, unauthenticated
//	/metrics   Prometheus exposition, unauthenticated but not publicly exposed
//	/v1/*      application API, authenticated by API key
//	/admin/*   administrative API, authenticated by the admin token
func NewRouter(d Deps) nethttp.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.RequestLogger(d.Logger))
	if d.Metrics != nil {
		r.Use(middleware.Metrics(d.Metrics))
	}
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	// Uniform error envelope for unmatched routes and methods.
	r.NotFound(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		respond.Error(w, req, nethttp.StatusNotFound, respond.CodeNotFound, "resource not found", false)
	})
	r.MethodNotAllowed(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		respond.Error(w, req, nethttp.StatusMethodNotAllowed, respond.CodeInvalidRequest, "method not allowed", false)
	})

	r.Get("/health/live", handlers.Live)
	r.Get("/health/ready", handlers.Ready(d.Health))
	if d.Metrics != nil {
		r.Method(nethttp.MethodGet, "/metrics", d.Metrics.Handler())
	}

	authenticator := auth.New(d.Applications)

	r.Route("/v1", func(r chi.Router) {
		r.Use(authenticator.RequireApplication)
		r.Get("/me", handlers.Me)
		if d.Payments != nil {
			handlers.NewPayments(d.Payments).Register(r)
		}
	})

	admin := handlers.NewAdmin(d.Applications, d.Config.YooKassaAPIURL)
	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.RequireAdmin(d.Config.AdminToken))
		admin.Register(r)
	})

	return r
}
