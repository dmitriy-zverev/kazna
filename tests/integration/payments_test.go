//go:build integration

// Stage 3 acceptance: the full create → pay → status cycle against an emulated
// provider, plus the idempotency and concurrency guarantees that make it safe.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func newPaymentHarness(t *testing.T) (*harness, *fakeYooKassa) {
	t.Helper()
	fake := newFakeYooKassa()
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	return newHarnessWith(t, harnessOptions{providerURL: server.URL}), fake
}

func (h *harness) createPayment(t *testing.T, apiKey, idempotencyKey string, payload map[string]any) (*http.Response, map[string]any) {
	t.Helper()
	return h.requestWithHeaders(t, http.MethodPost, "/v1/payments", apiKey, payload,
		map[string]string{"Idempotency-Key": idempotencyKey})
}

func (h *harness) createAttempt(t *testing.T, apiKey, orderID, idempotencyKey string, payload map[string]any) (*http.Response, map[string]any) {
	t.Helper()
	return h.requestWithHeaders(t, http.MethodPost, "/v1/orders/"+orderID+"/payment-attempts", apiKey, payload,
		map[string]string{"Idempotency-Key": idempotencyKey})
}

func paymentPayload(orderID string, amount int64) map[string]any {
	return map[string]any{
		"order_id":    orderID,
		"amount":      amount,
		"currency":    "RUB",
		"description": "Subscription Pro",
		"metadata":    map[string]string{"user_id": "u1"},
	}
}

func (h *harness) paymentStatus(t *testing.T, paymentID string) string {
	t.Helper()
	var status string
	if err := h.pool.QueryRow(context.Background(),
		`SELECT status FROM payments WHERE id = $1`, paymentID).Scan(&status); err != nil {
		t.Fatalf("read payment status: %v", err)
	}
	return status
}

func (h *harness) countPayments(t *testing.T, appID, orderID string) int {
	t.Helper()
	var count int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payments WHERE application_id = $1 AND order_id = $2`,
		appID, orderID).Scan(&count); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	return count
}

func (h *harness) operationRows(t *testing.T, paymentID string) []struct{ Status, Key string } {
	t.Helper()
	rows, err := h.pool.Query(context.Background(),
		`SELECT status, provider_idempotency_key FROM payment_operations WHERE payment_id = $1`, paymentID)
	if err != nil {
		t.Fatalf("read operations: %v", err)
	}
	defer rows.Close()

	var out []struct{ Status, Key string }
	for rows.Next() {
		var row struct{ Status, Key string }
		if err := rows.Scan(&row.Status, &row.Key); err != nil {
			t.Fatalf("scan operation: %v", err)
		}
		out = append(out, row)
	}
	return out
}

func TestCreatePaymentReturnsCheckoutURL(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	resp, body := h.createPayment(t, app.apiKey, "key-1", paymentPayload("order_1", 49900))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %v", resp.StatusCode, body)
	}
	if got := stringField(body, "status"); got != "pending" {
		t.Fatalf("status = %q, want pending", got)
	}
	if stringField(body, "checkout_url") == "" {
		t.Fatalf("checkout_url is missing: %v", body)
	}
	if got := numberField(body, "attempt_number"); got != 1 {
		t.Fatalf("attempt_number = %v, want 1", got)
	}
	if got := numberField(body, "amount"); got != 49900 {
		t.Fatalf("amount = %v, want 49900", got)
	}

	create, _ := fake.counts()
	if create != 1 {
		t.Fatalf("provider create calls = %d, want 1", create)
	}

	paymentID := stringField(body, "id")
	ops := h.operationRows(t, paymentID)
	if len(ops) != 1 {
		t.Fatalf("operation rows = %d, want 1", len(ops))
	}
	if ops[0].Status != "succeeded" {
		t.Fatalf("operation status = %q, want succeeded", ops[0].Status)
	}
	if ops[0].Key == "" || len(ops[0].Key) > 64 {
		t.Fatalf("provider idempotency key %q is missing or longer than the 64-char provider limit", ops[0].Key)
	}
}

func TestIdempotentReplayDoesNotCreateSecondPayment(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")
	payload := paymentPayload("order_1", 49900)

	first, firstBody := h.createPayment(t, app.apiKey, "same-key", payload)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first create: status %d, body %v", first.StatusCode, firstBody)
	}

	second, secondBody := h.createPayment(t, app.apiKey, "same-key", payload)
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("replay: status %d, body %v", second.StatusCode, secondBody)
	}
	if stringField(firstBody, "id") != stringField(secondBody, "id") {
		t.Fatalf("replay returned a different payment: %v vs %v", firstBody["id"], secondBody["id"])
	}

	create, _ := fake.counts()
	if create != 1 {
		t.Fatalf("provider create calls = %d, want 1: a replay must not reach the provider", create)
	}
	if ids := fake.providerPaymentIDs(); len(ids) != 1 {
		t.Fatalf("provider payments = %d, want 1", len(ids))
	}
}

func TestIdempotencyConflictOnDifferentBody(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	if resp, body := h.createPayment(t, app.apiKey, "key-x", paymentPayload("order_1", 49900)); resp.StatusCode != http.StatusCreated {
		t.Fatalf("first create: status %d, body %v", resp.StatusCode, body)
	}

	resp, body := h.createPayment(t, app.apiKey, "key-x", paymentPayload("order_1", 100))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %v", resp.StatusCode, body)
	}
	if code := errorCode(body); code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("code = %q, want IDEMPOTENCY_CONFLICT", code)
	}

	create, _ := fake.counts()
	if create != 1 {
		t.Fatalf("provider create calls = %d, want 1", create)
	}
}

func TestConcurrentCreatesLeaveOneActiveAttempt(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	const workers = 4
	var wg sync.WaitGroup
	codes := make([]int, workers)
	raw := make([]string, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			// Called from a goroutine: no testing.T helpers that can fail here.
			body, err := json.Marshal(paymentPayload("order_race", 49900))
			if err != nil {
				raw[index] = "marshal: " + err.Error()
				return
			}
			req, err := http.NewRequest(http.MethodPost, h.server.URL+"/v1/payments", bytes.NewReader(body))
			if err != nil {
				raw[index] = "request: " + err.Error()
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+app.apiKey)
			req.Header.Set("Idempotency-Key", "concurrent-"+strconv.Itoa(index))

			resp, err := h.server.Client().Do(req)
			if err != nil {
				raw[index] = "do: " + err.Error()
				return
			}
			defer func() { _ = resp.Body.Close() }()
			payload, _ := io.ReadAll(resp.Body)
			codes[index] = resp.StatusCode
			raw[index] = string(payload)
		}(i)
	}
	wg.Wait()

	created := 0
	conflicted := 0
	for i, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicted++
		default:
			t.Fatalf("worker %d: status %d, body %s", i, code, raw[i])
		}
	}
	if created != 1 {
		t.Fatalf("created = %d, want exactly 1: %v", created, codes)
	}
	if conflicted != workers-1 {
		t.Fatalf("conflicted = %d, want %d: %v", conflicted, workers-1, codes)
	}

	if got := h.countPayments(t, app.id, "order_race"); got != 1 {
		t.Fatalf("payments stored = %d, want 1", got)
	}
	if create, _ := fake.counts(); create != 1 {
		t.Fatalf("provider create calls = %d, want 1: only the winning attempt may reach the provider", create)
	}
}

func TestUnknownResultReturns202AndNeverCreatesSecondPayment(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	fake.mu.Lock()
	fake.hangUntilCanceled = true
	fake.mu.Unlock()

	payload := paymentPayload("order_unknown", 49900)

	resp, body := h.createPayment(t, app.apiKey, "key-unknown", payload)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body %v", resp.StatusCode, body)
	}
	if got := stringField(body, "status"); got != "creation_unknown" {
		t.Fatalf("status = %q, want creation_unknown", got)
	}
	if stringField(body, "id") == "" {
		t.Fatalf("202 must still return the payment id: %v", body)
	}
	if numberField(body, "retry_after") <= 0 {
		t.Fatalf("202 must suggest a retry delay: %v", body)
	}

	paymentID := stringField(body, "id")
	if got := h.paymentStatus(t, paymentID); got != "creation_unknown" {
		t.Fatalf("stored status = %q, want creation_unknown", got)
	}

	// The provider becomes reachable again. Repeating the SAME key must replay
	// the existing payment, never create a second provider payment.
	fake.mu.Lock()
	fake.hangUntilCanceled = false
	fake.mu.Unlock()

	resp, replayBody := h.createPayment(t, app.apiKey, "key-unknown", payload)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("replay status = %d, want 202; body %v", resp.StatusCode, replayBody)
	}
	if stringField(replayBody, "id") != paymentID {
		t.Fatalf("replay returned a different payment: %v vs %v", replayBody["id"], paymentID)
	}
	if create, _ := fake.counts(); create != 1 {
		t.Fatalf("provider create calls = %d, want 1: an unknown outcome must not be retried automatically", create)
	}
	if got := h.countPayments(t, app.id, "order_unknown"); got != 1 {
		t.Fatalf("payments stored = %d, want 1", got)
	}
}

func TestProviderRefusalCancelsPayment(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	fake.mu.Lock()
	fake.createStatus = http.StatusBadRequest
	fake.mu.Unlock()

	resp, body := h.createPayment(t, app.apiKey, "key-refused", paymentPayload("order_refused", 49900))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %v", resp.StatusCode, body)
	}

	var paymentID string
	if err := h.pool.QueryRow(context.Background(),
		`SELECT id FROM payments WHERE application_id = $1 AND order_id = 'order_refused'`, app.id).Scan(&paymentID); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if got := h.paymentStatus(t, paymentID); got != "canceled" {
		t.Fatalf("stored status = %q, want canceled after a definitive refusal", got)
	}

	// The refusal is recorded as a failed operation, not an unknown one.
	ops := h.operationRows(t, paymentID)
	if len(ops) != 1 || ops[0].Status != "failed" {
		t.Fatalf("operations = %v, want a single failed operation", ops)
	}
}

func TestGetPaymentRefreshesFromProviderAndNeverRollsBack(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	resp, body := h.createPayment(t, app.apiKey, "key-get", paymentPayload("order_get", 49900))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d, body %v", resp.StatusCode, body)
	}
	paymentID := stringField(body, "id")
	providerID := fake.providerPaymentIDs()[0]

	fake.setStatus(providerID, "succeeded")

	resp, body = h.request(t, http.MethodGet, "/v1/payments/"+paymentID, app.apiKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status %d, body %v", resp.StatusCode, body)
	}
	if got := stringField(body, "status"); got != "succeeded" {
		t.Fatalf("status = %q, want succeeded", got)
	}
	if stringField(body, "paid_at") == "" {
		t.Fatalf("paid_at must be set once a payment succeeds: %v", body)
	}

	// A late provider read reporting an older state must not undo the result.
	fake.setStatus(providerID, "pending")
	_, body = h.request(t, http.MethodGet, "/v1/payments/"+paymentID, app.apiKey, nil)
	if got := stringField(body, "status"); got != "succeeded" {
		t.Fatalf("status = %q after a stale provider read, want succeeded", got)
	}
}

func TestPaymentsAreIsolatedBetweenApplications(t *testing.T) {
	h, _ := newPaymentHarness(t)
	appA := h.provisionApp(t, "SaaS A")
	appB := h.provisionApp(t, "SaaS B")

	_, body := h.createPayment(t, appA.apiKey, "key-a", paymentPayload("order_a", 49900))
	paymentID := stringField(body, "id")

	resp, bodyB := h.request(t, http.MethodGet, "/v1/payments/"+paymentID, appB.apiKey, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when reading another application's payment", resp.StatusCode)
	}
	if code := errorCode(bodyB); code != "PAYMENT_NOT_FOUND" {
		t.Fatalf("code = %q, want PAYMENT_NOT_FOUND", code)
	}

	// The list endpoint must not leak it either.
	_, listBody := h.request(t, http.MethodGet, "/v1/payments", appB.apiKey, nil)
	if data, _ := listBody["data"].([]any); len(data) != 0 {
		t.Fatalf("application B sees %d payments, want 0", len(data))
	}
}

func TestListPaymentsByOrderID(t *testing.T) {
	h, _ := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	h.createPayment(t, app.apiKey, "list-1", paymentPayload("order_one", 100))
	h.createPayment(t, app.apiKey, "list-2", paymentPayload("order_two", 200))

	_, body := h.request(t, http.MethodGet, "/v1/payments?order_id=order_one", app.apiKey, nil)
	data, _ := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("filtered list returned %d payments, want 1", len(data))
	}
	first, _ := data[0].(map[string]any)
	if got := stringField(first, "order_id"); got != "order_one" {
		t.Fatalf("order_id = %q, want order_one", got)
	}

	_, allBody := h.request(t, http.MethodGet, "/v1/payments", app.apiKey, nil)
	all, _ := allBody["data"].([]any)
	if len(all) != 2 {
		t.Fatalf("unfiltered list returned %d payments, want 2", len(all))
	}
}

func TestCreatePaymentRequiresProviderAccount(t *testing.T) {
	h, _ := newPaymentHarness(t)
	app := h.createApplication(t, "No Provider")

	resp, body := h.createPayment(t, app.apiKey, "key-noprovider", paymentPayload("order_np", 100))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %v", resp.StatusCode, body)
	}
	if code := errorCode(body); code != "PROVIDER_NOT_CONFIGURED" {
		t.Fatalf("code = %q, want PROVIDER_NOT_CONFIGURED", code)
	}
}

func TestIdempotencyKeyIsRequired(t *testing.T) {
	h, _ := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	resp, body := h.request(t, http.MethodPost, "/v1/payments", app.apiKey, paymentPayload("order_nokey", 100))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %v", resp.StatusCode, body)
	}
}

func TestProductionRejectsUnsafeReturnURL(t *testing.T) {
	fake := newFakeYooKassa()
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	h := newHarnessWith(t, harnessOptions{production: true, providerURL: server.URL})
	app := h.provisionApp(t, "Prod SaaS")

	payload := paymentPayload("order_ssrf", 100)
	payload["return_url"] = "https://169.254.169.254/latest/meta-data"

	resp, body := h.createPayment(t, app.apiKey, "key-ssrf", payload)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an internal return_url; body %v", resp.StatusCode, body)
	}
	if create, _ := fake.counts(); create != 0 {
		t.Fatalf("provider create calls = %d, want 0: the request must fail before any call", create)
	}
}

func TestNewAttemptAfterTerminalPayment(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	resp, body := h.createPayment(t, app.apiKey, "attempt-1", paymentPayload("order_retry", 49900))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d, body %v", resp.StatusCode, body)
	}
	paymentID := stringField(body, "id")

	// While the first attempt is unfinished, a second is refused.
	resp, body = h.createAttempt(t, app.apiKey, "order_retry", "attempt-2", map[string]any{"amount": 49900})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 while an attempt is active; body %v", resp.StatusCode, body)
	}

	// Settle the first attempt, then a new attempt is allowed.
	providerID := fake.providerPaymentIDs()[0]
	fake.setStatus(providerID, "canceled")
	h.request(t, http.MethodGet, "/v1/payments/"+paymentID, app.apiKey, nil)
	if got := h.paymentStatus(t, paymentID); got != "canceled" {
		t.Fatalf("stored status = %q, want canceled", got)
	}

	resp, body = h.createAttempt(t, app.apiKey, "order_retry", "attempt-3", map[string]any{"amount": 49900})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second attempt: status %d, body %v", resp.StatusCode, body)
	}
	if got := numberField(body, "attempt_number"); got != 2 {
		t.Fatalf("attempt_number = %v, want 2", got)
	}
}

func TestAttemptsEndpointRejectsPaidOrder(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	resp, body := h.createPayment(t, app.apiKey, "paid-1", paymentPayload("order_paid", 49900))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d, body %v", resp.StatusCode, body)
	}
	paymentID := stringField(body, "id")

	fake.setStatus(fake.providerPaymentIDs()[0], "succeeded")
	h.request(t, http.MethodGet, "/v1/payments/"+paymentID, app.apiKey, nil)

	resp, body = h.createAttempt(t, app.apiKey, "order_paid", "paid-2", map[string]any{"amount": 49900})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %v", resp.StatusCode, body)
	}
	if code := errorCode(body); code != "PAYMENT_ALREADY_SUCCEEDED" {
		t.Fatalf("code = %q, want PAYMENT_ALREADY_SUCCEEDED", code)
	}
}

func TestConcurrentSameKeyNeverReturnsHollowSuccess(t *testing.T) {
	h, fake := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	// Slow the provider so a loser of the idempotency race replays while the
	// winner is still inside the provider call.
	fake.mu.Lock()
	fake.createDelay = 150 * time.Millisecond
	fake.mu.Unlock()

	const workers = 4
	type outcome struct {
		code   int
		id     string
		status string
	}
	results := make([]outcome, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			body, err := json.Marshal(paymentPayload("order_same_key", 49900))
			if err != nil {
				return
			}
			req, err := http.NewRequest(http.MethodPost, h.server.URL+"/v1/payments", bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+app.apiKey)
			req.Header.Set("Idempotency-Key", "shared-key")

			resp, err := h.server.Client().Do(req)
			if err != nil {
				return
			}
			defer func() { _ = resp.Body.Close() }()
			payload, _ := io.ReadAll(resp.Body)

			var decoded map[string]any
			_ = json.Unmarshal(payload, &decoded)
			results[index] = outcome{
				code:   resp.StatusCode,
				id:     stringField(decoded, "id"),
				status: stringField(decoded, "status"),
			}
		}(i)
	}
	wg.Wait()

	firstID := ""
	for i, result := range results {
		if result.id == "" {
			t.Fatalf("worker %d produced no payment id", i)
		}
		if firstID == "" {
			firstID = result.id
		}
		if result.id != firstID {
			t.Fatalf("worker %d returned payment %s, want %s", i, result.id, firstID)
		}

		switch result.status {
		case "created", "creating", "creation_unknown":
			// No checkout URL exists yet, so the client must be told to retry.
			if result.code != http.StatusAccepted {
				t.Fatalf("worker %d: status %q with HTTP %d, want 202 (hollow 201)", i, result.status, result.code)
			}
		case "pending":
			if result.code != http.StatusCreated {
				t.Fatalf("worker %d: pending with HTTP %d, want 201", i, result.code)
			}
		default:
			t.Fatalf("worker %d: unexpected status %q", i, result.status)
		}
	}

	if create, _ := fake.counts(); create != 1 {
		t.Fatalf("provider create calls = %d, want 1", create)
	}
}

func TestMetricsEndpointExposesCounters(t *testing.T) {
	h, _ := newPaymentHarness(t)
	app := h.provisionApp(t, "SaaS")

	h.createPayment(t, app.apiKey, "metrics-1", paymentPayload("order_metrics", 100))

	resp, err := h.server.Client().Get(h.server.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw := make([]byte, 64<<10)
	n, _ := resp.Body.Read(raw)
	text := string(raw[:n])

	for _, want := range []string{"kazna_payment_creations_total", "kazna_provider_calls_total", "kazna_http_request_duration_seconds", "kazna_unfinished_payments"} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics output is missing %s:\n%s", want, text)
		}
	}
}
