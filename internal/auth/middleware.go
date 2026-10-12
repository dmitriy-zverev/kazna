// Package auth turns credentials into an authenticated request context.
package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/http/respond"
)

type contextKey int

const authenticatedKey contextKey = iota

// Authenticator resolves a presented API key. *applications.Service satisfies it.
type Authenticator interface {
	Authenticate(ctx context.Context, presented string) (applications.Authenticated, error)
}

// Middleware authenticates application API keys and the admin token.
type Middleware struct {
	apps Authenticator
}

// New builds the middleware on top of an application authenticator.
func New(apps Authenticator) *Middleware {
	return &Middleware{apps: apps}
}

// RequireApplication rejects requests without a valid application API key and
// puts the resolved application into the request context.
func (m *Middleware) RequireApplication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			respond.Error(w, r, http.StatusUnauthorized, respond.CodeUnauthorized,
				"missing or malformed Authorization header", false)
			return
		}

		authenticated, err := m.apps.Authenticate(r.Context(), token)
		if err != nil {
			if errors.Is(err, applications.ErrUnauthorized) {
				respond.Error(w, r, http.StatusUnauthorized, respond.CodeUnauthorized, "invalid API key", false)
				return
			}
			respond.Error(w, r, http.StatusInternalServerError, respond.CodeInternalError,
				"authentication failed", true)
			return
		}

		next.ServeHTTP(w, r.WithContext(withAuthenticated(r.Context(), authenticated)))
	})
}

// RequireAdmin enforces the provisional admin token. An unconfigured token
// disables the admin API entirely instead of leaving it open.
func RequireAdmin(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.TrimSpace(token) == "" {
				respond.Error(w, r, http.StatusServiceUnavailable, respond.CodeInternalError,
					"admin API is disabled: ADMIN_TOKEN is not configured", false)
				return
			}

			presented, ok := bearerToken(r)
			if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
				respond.Error(w, r, http.StatusUnauthorized, respond.CodeUnauthorized,
					"invalid admin token", false)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// From returns the authenticated application attached by RequireApplication.
func From(ctx context.Context) (applications.Authenticated, bool) {
	authenticated, ok := ctx.Value(authenticatedKey).(applications.Authenticated)
	return authenticated, ok
}

func withAuthenticated(ctx context.Context, authenticated applications.Authenticated) context.Context {
	return context.WithValue(ctx, authenticatedKey, authenticated)
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}
