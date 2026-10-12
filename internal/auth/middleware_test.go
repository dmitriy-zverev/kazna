package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/http/respond"
)

type stubAuthenticator struct {
	result applications.Authenticated
	err    error
}

func (s stubAuthenticator) Authenticate(context.Context, string) (applications.Authenticated, error) {
	return s.result, s.err
}

func TestRequireApplicationAcceptsValidKey(t *testing.T) {
	want := applications.Authenticated{
		Application: applications.Application{ID: uuid.New(), Name: "SaaS", Slug: "saas", Status: applications.StatusActive},
		KeyID:       uuid.New(),
	}

	var got applications.Authenticated
	handler := New(stubAuthenticator{result: want}).RequireApplication(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			got, _ = From(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer app_live_abc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.Application.ID != want.Application.ID {
		t.Fatalf("context application = %v, want %v", got.Application.ID, want.Application.ID)
	}
}

func TestRequireApplicationRejectsBadCredentials(t *testing.T) {
	cases := []struct {
		name   string
		header string
		err    error
		want   int
	}{
		{"missing header", "", nil, http.StatusUnauthorized},
		{"not bearer", "Basic abc", nil, http.StatusUnauthorized},
		{"empty bearer", "Bearer ", nil, http.StatusUnauthorized},
		{"invalid key", "Bearer app_live_x", applications.ErrUnauthorized, http.StatusUnauthorized},
		{"backend failure", "Bearer app_live_x", errors.New("db down"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := New(stubAuthenticator{err: tc.err}).RequireApplication(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("next handler must not be called")
				}))

			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}

			var env respond.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			wantCode := respond.CodeUnauthorized
			if tc.want == http.StatusInternalServerError {
				wantCode = respond.CodeInternalError
			}
			if env.Error.Code != wantCode {
				t.Fatalf("code = %q, want %q", env.Error.Code, wantCode)
			}
		})
	}
}

func TestRequireAdmin(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		header     string
		want       int
	}{
		{"disabled", "", "Bearer x", http.StatusServiceUnavailable},
		{"missing header", "secret", "", http.StatusUnauthorized},
		{"wrong token", "secret", "Bearer nope", http.StatusUnauthorized},
		{"correct token", "secret", "Bearer secret", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := RequireAdmin(tc.configured)(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))

			req := httptest.NewRequest(http.MethodGet, "/admin/applications", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
