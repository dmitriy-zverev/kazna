package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitriy-zverev/kazna/internal/config"
	"github.com/dmitriy-zverev/kazna/internal/http/respond"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

func newTestRouter(pingErr error, adminToken string) nethttp.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(Deps{
		Logger: logger,
		Config: &config.Config{AppEnv: "test", AdminToken: adminToken, YooKassaAPIURL: "http://127.0.0.1:1"},
		Health: stubPinger{err: pingErr},
		// Applications is intentionally nil: these tests never reach a route
		// that needs it.
	})
}

func do(t *testing.T, h nethttp.Handler, method, path, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) respond.ErrorEnvelope {
	t.Helper()
	var env respond.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %q)", err, rec.Body.String())
	}
	return env
}

func TestLiveIsAlwaysOK(t *testing.T) {
	// Even with a broken database the liveness probe must stay 200.
	rec := do(t, newTestRouter(errors.New("db down"), "token"), nethttp.MethodGet, "/health/live", "")
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyReflectsDatabase(t *testing.T) {
	if rec := do(t, newTestRouter(nil, "token"), nethttp.MethodGet, "/health/ready", ""); rec.Code != nethttp.StatusOK {
		t.Fatalf("healthy db: status = %d, want 200", rec.Code)
	}
	if rec := do(t, newTestRouter(errors.New("db down"), "token"), nethttp.MethodGet, "/health/ready", ""); rec.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("broken db: status = %d, want 503", rec.Code)
	}
}

func TestNotFoundUsesErrorEnvelope(t *testing.T) {
	rec := do(t, newTestRouter(nil, "token"), nethttp.MethodGet, "/nope", "")

	if rec.Code != nethttp.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error.Code != respond.CodeNotFound {
		t.Fatalf("code = %q, want %q", env.Error.Code, respond.CodeNotFound)
	}
	if env.Error.RequestID == "" {
		t.Fatal("request_id must be populated by the middleware chain")
	}
}

// Unauthenticated requests to /v1/* must be rejected before routing resolves,
// so the API surface cannot be enumerated without a key.
func TestV1HidesRoutingWithoutKey(t *testing.T) {
	rec := do(t, newTestRouter(nil, "token"), nethttp.MethodGet, "/v1/payments", "")
	if rec.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMethodNotAllowedUsesErrorEnvelope(t *testing.T) {
	rec := do(t, newTestRouter(nil, "token"), nethttp.MethodPost, "/health/live", "")
	if rec.Code != nethttp.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if env := decodeEnvelope(t, rec); env.Error.Code != respond.CodeInvalidRequest {
		t.Fatalf("code = %q, want %q", env.Error.Code, respond.CodeInvalidRequest)
	}
}

func TestV1RequiresAPIKey(t *testing.T) {
	rec := do(t, newTestRouter(nil, "token"), nethttp.MethodGet, "/v1/me", "")
	if rec.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if env := decodeEnvelope(t, rec); env.Error.Code != respond.CodeUnauthorized {
		t.Fatalf("code = %q, want %q", env.Error.Code, respond.CodeUnauthorized)
	}
}

func TestAdminDisabledWhenTokenMissing(t *testing.T) {
	rec := do(t, newTestRouter(nil, ""), nethttp.MethodGet, "/admin/applications", "")
	if rec.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when ADMIN_TOKEN is unset", rec.Code)
	}
}

func TestAdminRejectsWrongToken(t *testing.T) {
	rec := do(t, newTestRouter(nil, "secret-token"), nethttp.MethodGet, "/admin/applications", "Bearer nope")
	if rec.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
