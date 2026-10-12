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

func newTestRouter(pingErr error) nethttp.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(logger, stubPinger{err: pingErr}, &config.Config{AppEnv: "test"})
}

func do(t *testing.T, h nethttp.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLiveIsAlwaysOK(t *testing.T) {
	// Even with a broken database the liveness probe must stay 200.
	rec := do(t, newTestRouter(errors.New("db down")), nethttp.MethodGet, "/health/live")
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyReflectsDatabase(t *testing.T) {
	if rec := do(t, newTestRouter(nil), nethttp.MethodGet, "/health/ready"); rec.Code != nethttp.StatusOK {
		t.Fatalf("healthy db: status = %d, want 200", rec.Code)
	}
	if rec := do(t, newTestRouter(errors.New("db down")), nethttp.MethodGet, "/health/ready"); rec.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("broken db: status = %d, want 503", rec.Code)
	}
}

func TestNotFoundUsesErrorEnvelope(t *testing.T) {
	rec := do(t, newTestRouter(nil), nethttp.MethodGet, "/v1/payments")

	if rec.Code != nethttp.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var env respond.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %q)", err, rec.Body.String())
	}
	if env.Error.Code != respond.CodeInvalidRequest {
		t.Fatalf("code = %q, want %q", env.Error.Code, respond.CodeInvalidRequest)
	}
	if env.Error.RequestID == "" {
		t.Fatal("request_id must be populated by the middleware chain")
	}
}

func TestMethodNotAllowedUsesErrorEnvelope(t *testing.T) {
	rec := do(t, newTestRouter(nil), nethttp.MethodPost, "/health/live")
	if rec.Code != nethttp.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	var env respond.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Error.Code != respond.CodeInvalidRequest {
		t.Fatalf("code = %q, want %q", env.Error.Code, respond.CodeInvalidRequest)
	}
}
