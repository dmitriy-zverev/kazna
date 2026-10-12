// Package yookassa is the adapter for the YooKassa API.
//
// Stage 2 only needs a connectivity check (docs/TECHNICAL_PLAN.md §11.2):
// a request that proves the credentials work without creating a payment.
// Payment creation, status reads and refunds arrive in Stage 3.
package yookassa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the production endpoint. It is overridden by
// YOOKASSA_API_URL so the verification stand can point at an emulator
// (docs/VERIFICATION_HARNESS.md).
const DefaultBaseURL = "https://api.yookassa.ru"

// Normalized error codes, aligned with the API error envelope.
const (
	CodeAuthFailed  = "PROVIDER_AUTH_FAILED"
	CodeTimeout     = "PROVIDER_TIMEOUT"
	CodeUnavailable = "PROVIDER_UNAVAILABLE"
	CodeInvalid     = "INVALID_REQUEST"
)

// Error is a provider failure translated into our own vocabulary.
type Error struct {
	Code      string
	HTTPState int
	Message   string
	Retryable bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("yookassa: %s (http %d): %s", e.Code, e.HTTPState, e.Message)
}

// AsError extracts a *Error from err, if present.
func AsError(err error) (*Error, bool) {
	var provErr *Error
	if errors.As(err, &provErr) {
		return provErr, true
	}
	return nil, false
}

// Client calls the YooKassa API with HTTP Basic Auth.
type Client struct {
	baseURL   string
	shopID    string
	secretKey string
	http      *http.Client
}

// NewClient builds a client. An empty baseURL falls back to DefaultBaseURL.
func NewClient(baseURL, shopID, secretKey string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		shopID:    shopID,
		secretKey: secretKey,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CheckConnection verifies the credentials with a read-only request that never
// creates a payment.
//
// GET /v3/payments is used because it accepts shop credentials (shop_id +
// secret_key) via HTTP Basic Auth and has no side effects. /v3/payment_methods
// is deliberately not used: it is POST-only and answers 405 to a GET.
func (c *Client) CheckConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v3/payments?limit=1", nil)
	if err != nil {
		return &Error{Code: CodeInvalid, Message: err.Error()}
	}
	req.SetBasicAuth(c.shopID, c.secretKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return &Error{Code: CodeTimeout, Message: err.Error(), Retryable: true}
		}
		var netErr interface{ Timeout() bool }
		if errors.As(err, &netErr) && netErr.Timeout() {
			return &Error{Code: CodeTimeout, Message: err.Error(), Retryable: true}
		}
		return &Error{Code: CodeUnavailable, Message: err.Error(), Retryable: true}
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain a bounded amount so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &Error{
			Code:      CodeAuthFailed,
			HTTPState: resp.StatusCode,
			Message:   "credentials rejected by the provider",
		}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &Error{
			Code:      CodeUnavailable,
			HTTPState: resp.StatusCode,
			Message:   "provider rate limit reached",
			Retryable: true,
		}
	case resp.StatusCode >= 500:
		return &Error{
			Code:      CodeUnavailable,
			HTTPState: resp.StatusCode,
			Message:   "provider is unavailable",
			Retryable: true,
		}
	default:
		return &Error{
			Code:      CodeInvalid,
			HTTPState: resp.StatusCode,
			Message:   fmt.Sprintf("unexpected provider status %d", resp.StatusCode),
		}
	}
}
