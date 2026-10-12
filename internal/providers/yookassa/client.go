package yookassa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmitriy-zverev/kazna/internal/providers"
)

// DefaultBaseURL is the production endpoint. It is overridden by
// YOOKASSA_API_URL so the verification stand can point at an emulator
// (docs/VERIFICATION_HARNESS.md).
const DefaultBaseURL = "https://api.yookassa.ru"

const (
	maxErrorBodyBytes = 64 << 10
	httpTimeout       = 15 * time.Second
)

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
		http:      &http.Client{Timeout: httpTimeout},
	}
}

// CheckConnection verifies the credentials with a read-only request that never
// creates a payment.
//
// GET /v3/payments is used because it accepts shop credentials (shop_id +
// secret_key) via HTTP Basic Auth and has no side effects. /v3/payment_methods
// is deliberately not used: it is POST-only and answers 405 to a GET.
func (c *Client) CheckConnection(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/v3/payments?limit=1", "", nil, nil)
	return err
}

// do performs one authenticated request and normalizes the failure modes.
// A nil out means the response body is discarded.
func (c *Client) do(ctx context.Context, method, path, idempotencyKey string, body any, out any) (int, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, &providers.Error{Code: providers.CodeInvalidRequest, Message: err.Error()}
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return 0, &providers.Error{Code: providers.CodeInvalidRequest, Message: err.Error()}
	}
	req.SetBasicAuth(c.shopID, c.secretKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotence-Key", idempotencyKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, networkError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
			return resp.StatusCode, nil
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
			return resp.StatusCode, &providers.Error{
				Code:       providers.CodeStateUnknown,
				HTTPStatus: resp.StatusCode,
				Message:    "cannot decode the provider response: " + err.Error(),
			}
		}
		return resp.StatusCode, nil
	}

	return resp.StatusCode, statusError(resp)
}

// networkError classifies a transport failure. A request that may have reached
// the provider is never reported as a definitive failure.
func networkError(err error) *providers.Error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &providers.Error{Code: providers.CodeTimeout, Message: err.Error(), Retryable: true}
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &providers.Error{Code: providers.CodeTimeout, Message: err.Error(), Retryable: true}
	}
	return &providers.Error{Code: providers.CodeUnavailable, Message: err.Error(), Retryable: true}
}

// statusError maps a non-2xx response to a normalized error, using the provider
// error code from the body when it is available.
func statusError(resp *http.Response) *providers.Error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	description := strings.TrimSpace(string(raw))
	bodyCode := ""

	var parsed struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	}
	if json.Unmarshal(raw, &parsed) == nil {
		bodyCode = parsed.Code
		if parsed.Description != "" {
			description = parsed.Description
		}
	}
	if len(description) > 512 {
		description = description[:512]
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &providers.Error{
			Code:       providers.CodeAuthFailed,
			HTTPStatus: resp.StatusCode,
			Message:    "credentials rejected by the provider",
		}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &providers.Error{
			Code:       providers.CodeUnavailable,
			HTTPStatus: resp.StatusCode,
			Message:    "provider rate limit reached",
			Retryable:  true,
		}
	case resp.StatusCode >= 500:
		return &providers.Error{
			Code:       providers.CodeUnavailable,
			HTTPStatus: resp.StatusCode,
			Message:    "provider is unavailable",
			Retryable:  true,
		}
	case bodyCode == "invalid_credentials":
		// YooKassa reports malformed credentials as 400, not 401.
		return &providers.Error{
			Code:       providers.CodeAuthFailed,
			HTTPStatus: resp.StatusCode,
			Message:    description,
		}
	default:
		return &providers.Error{
			Code:       providers.CodeInvalidRequest,
			HTTPStatus: resp.StatusCode,
			Message:    fmt.Sprintf("provider rejected the request: %s", description),
		}
	}
}

// formatAmountMinor renders minor units as the provider's decimal string.
func formatAmountMinor(minor int64) string {
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	return sign + strconv.FormatInt(minor/100, 10) + "." + fmt.Sprintf("%02d", minor%100)
}

// parseAmountMinor reads the provider's decimal string back into minor units.
func parseAmountMinor(value string) (int64, error) {
	parts := strings.SplitN(strings.TrimSpace(value), ".", 2)
	rubles, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	var kopecks int64
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		if len(fraction) > 2 {
			fraction = fraction[:2]
		}
		kopecks, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid amount %q", value)
		}
	}
	return rubles*100 + kopecks, nil
}
