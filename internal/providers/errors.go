package providers

import (
	"errors"
	"fmt"
)

// ErrorCode is a normalized provider failure, aligned with the API error
// envelope (docs/TECHNICAL_PLAN.md §5.5).
type ErrorCode string

// Normalized provider error codes.
const (
	CodeInvalidRequest ErrorCode = "INVALID_REQUEST"
	CodeAuthFailed     ErrorCode = "PROVIDER_AUTH_FAILED"
	CodeUnavailable    ErrorCode = "PROVIDER_UNAVAILABLE"
	CodeTimeout        ErrorCode = "PROVIDER_TIMEOUT"
	// CodeStateUnknown means the outcome of the call could not be established.
	// It must never be treated as a failure of the underlying operation.
	CodeStateUnknown ErrorCode = "PAYMENT_STATE_UNKNOWN"
)

// Error is a provider failure translated into our own vocabulary.
type Error struct {
	Code       ErrorCode
	HTTPStatus int
	Message    string
	Retryable  bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("provider: %s (http %d): %s", e.Code, e.HTTPStatus, e.Message)
}

// AsError extracts a *Error from err, if present.
func AsError(err error) (*Error, bool) {
	var providerErr *Error
	if errors.As(err, &providerErr) {
		return providerErr, true
	}
	return nil, false
}

// CodeOf returns the normalized code for err, or CodeStateUnknown when the
// error did not come from a provider adapter (a local failure leaves the
// outcome of any external call unresolved).
func CodeOf(err error) ErrorCode {
	if providerErr, ok := AsError(err); ok {
		return providerErr.Code
	}
	return CodeStateUnknown
}

// Definitive reports whether the provider refused the operation outright, so
// repeating it cannot help and the local state may be settled.
func Definitive(code ErrorCode) bool {
	return code == CodeInvalidRequest || code == CodeAuthFailed
}
