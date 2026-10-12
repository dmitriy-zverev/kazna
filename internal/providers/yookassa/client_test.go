package yookassa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckConnectionSuccess(t *testing.T) {
	var gotUser, gotPass string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[],"type":"list"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "123456", "test_secret")
	if err := c.CheckConnection(context.Background()); err != nil {
		t.Fatalf("CheckConnection: %v", err)
	}
	if gotUser != "123456" || gotPass != "test_secret" {
		t.Fatalf("basic auth = %q/%q, want 123456/test_secret", gotUser, gotPass)
	}
	if gotPath != "/v3/payments" {
		t.Fatalf("path = %q, want /v3/payments", gotPath)
	}
}

func TestCheckConnectionMapsStatuses(t *testing.T) {
	cases := []struct {
		status    int
		wantCode  string
		retryable bool
	}{
		{http.StatusUnauthorized, CodeAuthFailed, false},
		{http.StatusForbidden, CodeAuthFailed, false},
		{http.StatusTooManyRequests, CodeUnavailable, true},
		{http.StatusInternalServerError, CodeUnavailable, true},
		{http.StatusBadRequest, CodeInvalid, false},
	}

	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))

		err := NewClient(srv.URL, "shop", "secret").CheckConnection(context.Background())
		srv.Close()

		provErr, ok := AsError(err)
		if !ok {
			t.Fatalf("status %d: err = %v, want *Error", tc.status, err)
		}
		if provErr.Code != tc.wantCode {
			t.Fatalf("status %d: code = %q, want %q", tc.status, provErr.Code, tc.wantCode)
		}
		if provErr.Retryable != tc.retryable {
			t.Fatalf("status %d: retryable = %v, want %v", tc.status, provErr.Retryable, tc.retryable)
		}
	}
}

func TestCheckConnectionTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := NewClient(srv.URL, "shop", "secret").CheckConnection(ctx)
	provErr, ok := AsError(err)
	if !ok {
		t.Fatalf("err = %v, want *Error", err)
	}
	if provErr.Code != CodeTimeout {
		t.Fatalf("code = %q, want %q", provErr.Code, CodeTimeout)
	}
}

func TestCheckConnectionUnreachable(t *testing.T) {
	// Nothing listens on this port.
	err := NewClient("http://127.0.0.1:1", "shop", "secret").CheckConnection(context.Background())
	provErr, ok := AsError(err)
	if !ok {
		t.Fatalf("err = %v, want *Error", err)
	}
	if provErr.Code != CodeUnavailable || !provErr.Retryable {
		t.Fatalf("code = %q retryable = %v, want %q/true", provErr.Code, provErr.Retryable, CodeUnavailable)
	}
}

func TestDefaultBaseURL(t *testing.T) {
	if c := NewClient("", "s", "k"); c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c := NewClient("http://x/", "s", "k"); c.baseURL != "http://x" {
		t.Fatalf("trailing slash not trimmed: %q", c.baseURL)
	}
}
