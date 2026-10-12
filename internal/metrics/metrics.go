// Package metrics exposes Prometheus-compatible metrics
// (docs/TECHNICAL_PLAN.md §13.5).
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dmitriy-zverev/kazna/internal/providers"
)

// Metrics holds the service's collectors and the registry they live in.
type Metrics struct {
	registry *prometheus.Registry

	paymentCreations *prometheus.CounterVec
	providerCalls    *prometheus.CounterVec
	providerDuration *prometheus.HistogramVec
	httpDuration     *prometheus.HistogramVec
	unfinished       prometheus.Gauge
	lastWorker       prometheus.Gauge
}

// New builds the collector set. A dedicated registry is used so the exposition
// contains only what this service intends to publish.
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		paymentCreations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kazna_payment_creations_total",
			Help: "Payment creation requests by outcome (created, creation_unknown, replayed, rejected).",
		}, []string{"result"}),
		providerCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kazna_provider_calls_total",
			Help: "Provider API calls by operation and normalized error code (ok when successful).",
		}, []string{"operation", "code"}),
		providerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kazna_provider_call_duration_seconds",
			Help:    "Provider API call latency.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15},
		}, []string{"operation"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kazna_http_request_duration_seconds",
			Help:    "HTTP request latency.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"method", "route", "status"}),
		unfinished: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kazna_unfinished_payments",
			Help: "Payments that are not in a terminal state.",
		}),
		lastWorker: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kazna_last_worker_success_timestamp_seconds",
			Help: "Unix time of the last successful background run. Stays 0 until a worker exists.",
		}),
	}

	m.registry.MustRegister(
		m.paymentCreations,
		m.providerCalls,
		m.providerDuration,
		m.httpDuration,
		m.unfinished,
		m.lastWorker,
	)
	return m
}

// Handler serves the exposition endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// PaymentCreation implements payments.Recorder.
func (m *Metrics) PaymentCreation(result string) {
	m.paymentCreations.WithLabelValues(result).Inc()
}

// ProviderCall implements payments.Recorder.
func (m *Metrics) ProviderCall(operation string, code providers.ErrorCode, duration time.Duration) {
	label := string(code)
	if label == "" {
		label = "ok"
	}
	m.providerCalls.WithLabelValues(operation, label).Inc()
	m.providerDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// ObserveHTTP records one HTTP request. route is the chi route pattern, so
// cardinality stays bounded by the number of routes.
func (m *Metrics) ObserveHTTP(method, route string, status int, duration time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	m.httpDuration.WithLabelValues(method, route, statusClass(status)).Observe(duration.Seconds())
}

// SetUnfinished publishes the current number of unfinished payments.
func (m *Metrics) SetUnfinished(count int64) {
	m.unfinished.Set(float64(count))
}

// statusClass keeps the status label low-cardinality.
func statusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	default:
		return "other"
	}
}
