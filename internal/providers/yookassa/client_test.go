package yookassa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitriy-zverev/kazna/internal/providers"
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

func TestStatusErrorsAreNormalized(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantCode   providers.ErrorCode
		retryable  bool
		definitive bool
	}{
		{"unauthorized", http.StatusUnauthorized, `{}`, providers.CodeAuthFailed, false, true},
		{"forbidden", http.StatusForbidden, `{}`, providers.CodeAuthFailed, false, true},
		{"rate limited", http.StatusTooManyRequests, `{}`, providers.CodeUnavailable, true, false},
		{"server error", http.StatusInternalServerError, `{}`, providers.CodeUnavailable, true, false},
		{"bad request", http.StatusBadRequest, `{"code":"invalid_request"}`, providers.CodeInvalidRequest, false, true},
		// YooKassa reports malformed credentials as 400 with this code.
		{"invalid credentials as 400", http.StatusBadRequest, `{"code":"invalid_credentials"}`, providers.CodeAuthFailed, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			err := NewClient(srv.URL, "shop", "secret").CheckConnection(context.Background())
			provErr, ok := providers.AsError(err)
			if !ok {
				t.Fatalf("err = %v, want *providers.Error", err)
			}
			if provErr.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", provErr.Code, tc.wantCode)
			}
			if provErr.Retryable != tc.retryable {
				t.Fatalf("retryable = %v, want %v", provErr.Retryable, tc.retryable)
			}
			if providers.Definitive(provErr.Code) != tc.definitive {
				t.Fatalf("Definitive(%q) = %v, want %v", provErr.Code, providers.Definitive(provErr.Code), tc.definitive)
			}
		})
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
	provErr, ok := providers.AsError(err)
	if !ok {
		t.Fatalf("err = %v, want *providers.Error", err)
	}
	if provErr.Code != providers.CodeTimeout {
		t.Fatalf("code = %q, want %q", provErr.Code, providers.CodeTimeout)
	}
}

func TestCheckConnectionUnreachable(t *testing.T) {
	// Nothing listens on this port.
	err := NewClient("http://127.0.0.1:1", "shop", "secret").CheckConnection(context.Background())
	provErr, ok := providers.AsError(err)
	if !ok {
		t.Fatalf("err = %v, want *providers.Error", err)
	}
	if provErr.Code != providers.CodeUnavailable || !provErr.Retryable {
		t.Fatalf("code = %q retryable = %v, want %q/true", provErr.Code, provErr.Retryable, providers.CodeUnavailable)
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

func TestAmountFormatting(t *testing.T) {
	cases := []struct {
		minor int64
		want  string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{10, "0.10"},
		{99, "0.99"},
		{100, "1.00"},
		{49900, "499.00"},
		{123456789, "1234567.89"},
	}

	for _, tc := range cases {
		if got := formatAmountMinor(tc.minor); got != tc.want {
			t.Fatalf("formatAmountMinor(%d) = %q, want %q", tc.minor, got, tc.want)
		}
		// Round trip must be exact: money must never lose a kopeck.
		back, err := parseAmountMinor(tc.want)
		if err != nil {
			t.Fatalf("parseAmountMinor(%q): %v", tc.want, err)
		}
		if back != tc.minor {
			t.Fatalf("round trip %d → %q → %d", tc.minor, tc.want, back)
		}
	}
}

func TestParseAmountMinorRejectsJunk(t *testing.T) {
	for _, bad := range []string{"", "abc", "1.2.3x", "RUB"} {
		if _, err := parseAmountMinor(bad); err == nil {
			t.Fatalf("parseAmountMinor(%q) must fail", bad)
		}
	}
}

func TestMapStatus(t *testing.T) {
	cases := map[string]providers.PaymentStatus{
		"pending":             providers.StatusPending,
		"waiting_for_capture": providers.StatusPending,
		"succeeded":           providers.StatusSucceeded,
		"canceled":            providers.StatusCanceled,
	}
	for input, want := range cases {
		got, err := mapStatus(input)
		if err != nil {
			t.Fatalf("mapStatus(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("mapStatus(%q) = %q, want %q", input, got, want)
		}
	}

	// An unknown status must be an error, never a guess.
	if _, err := mapStatus("something_new"); err == nil {
		t.Fatal("mapStatus must reject an unrecognized status")
	} else if provErr, ok := providers.AsError(err); !ok || provErr.Code != providers.CodeStateUnknown {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePaymentSendsIdempotencyKeyAndMapsResponse(t *testing.T) {
	var gotKey, gotBody, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotKey = r.Header.Get("Idempotence-Key")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "2c0f6a2e-000f-5000-8000-1a2b3c4d5e6f",
			"status": "pending",
			"paid": false,
			"amount": {"value": "499.00", "currency": "RUB"},
			"confirmation": {"type": "redirect", "confirmation_url": "https://yoomoney.ru/checkout/x"}
		}`))
	}))
	defer srv.Close()

	result, err := NewClient(srv.URL, "shop", "secret").CreatePayment(context.Background(), providers.CreatePaymentRequest{
		AmountMinor:    49900,
		Currency:       "RUB",
		Description:    "Subscription Pro",
		ReturnURL:      "https://app.example.com/billing",
		Metadata:       map[string]string{"order_id": "order_1"},
		IdempotencyKey: "9f1b0d3a-idem",
	})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotKey != "9f1b0d3a-idem" {
		t.Fatalf("Idempotence-Key = %q, want 9f1b0d3a-idem", gotKey)
	}
	for _, want := range []string{`"value":"499.00"`, `"currency":"RUB"`, `"capture":true`, `"type":"redirect"`, `"order_id":"order_1"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("request body %s is missing %s", gotBody, want)
		}
	}
	// No receipt may be sent (docs/ARCHITECTURE.md §9).
	if strings.Contains(gotBody, "receipt") {
		t.Fatalf("request body must not contain a receipt: %s", gotBody)
	}

	if result.ProviderPaymentID != "2c0f6a2e-000f-5000-8000-1a2b3c4d5e6f" {
		t.Fatalf("provider id = %q", result.ProviderPaymentID)
	}
	if result.Status != providers.StatusPending {
		t.Fatalf("status = %q, want pending", result.Status)
	}
	if result.CheckoutURL != "https://yoomoney.ru/checkout/x" {
		t.Fatalf("checkout url = %q", result.CheckoutURL)
	}
	if result.AmountMinor != 49900 {
		t.Fatalf("amount = %d, want 49900", result.AmountMinor)
	}
}

func TestGetPayment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/payments/2c0f6a2e" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Idempotence-Key") != "" {
			t.Error("GET must not send an idempotency key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "2c0f6a2e",
			"status": "succeeded",
			"paid": true,
			"amount": {"value": "499.00", "currency": "RUB"}
		}`))
	}))
	defer srv.Close()

	result, err := NewClient(srv.URL, "shop", "secret").GetPayment(context.Background(), "2c0f6a2e")
	if err != nil {
		t.Fatalf("GetPayment: %v", err)
	}
	if result.Status != providers.StatusSucceeded || !result.Paid {
		t.Fatalf("result = %+v, want succeeded/paid", result)
	}
}

func TestGetPaymentRequiresID(t *testing.T) {
	_, err := NewClient("http://127.0.0.1:1", "s", "k").GetPayment(context.Background(), "")
	if provErr, ok := providers.AsError(err); !ok || provErr.Code != providers.CodeInvalidRequest {
		t.Fatalf("err = %v, want CodeInvalidRequest", err)
	}
}
