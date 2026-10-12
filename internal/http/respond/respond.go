// Package respond writes the uniform JSON responses and the normalized error
// envelope defined in the technical plan §5.5.
package respond

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Normalized error codes. Keep this list in sync with api/openapi.yaml.
const (
	CodeInvalidRequest          = "INVALID_REQUEST"
	CodeUnauthorized            = "UNAUTHORIZED"
	CodeNotFound                = "NOT_FOUND"
	CodeConflict                = "CONFLICT"
	CodePaymentNotFound         = "PAYMENT_NOT_FOUND"
	CodeIdempotencyConflict     = "IDEMPOTENCY_CONFLICT"
	CodePaymentAlreadySucceeded = "PAYMENT_ALREADY_SUCCEEDED"
	CodeProviderUnavailable     = "PROVIDER_UNAVAILABLE"
	CodeProviderTimeout         = "PROVIDER_TIMEOUT"
	CodeProviderAuthFailed      = "PROVIDER_AUTH_FAILED"
	CodePaymentStateUnknown     = "PAYMENT_STATE_UNKNOWN"
	CodeInternalError           = "INTERNAL_ERROR"
)

// ErrorBody is the inner object of the error envelope.
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"request_id,omitempty"`
}

// ErrorEnvelope is the standard top-level error response.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// JSON writes v as JSON with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("respond: encode json", slog.Any("error", err))
	}
}

// Error writes a normalized error envelope. The request id is taken from the
// chi request-id middleware when present.
func Error(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool) {
	JSON(w, status, ErrorEnvelope{Error: ErrorBody{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		RequestID: RequestID(r),
	}})
}
