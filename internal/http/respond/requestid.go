package respond

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// RequestID returns the correlation id assigned by the chi request-id
// middleware, or an empty string when the middleware is not installed.
func RequestID(r *http.Request) string {
	return chimw.GetReqID(r.Context())
}
