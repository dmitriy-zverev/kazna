//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/config"
	"github.com/dmitriy-zverev/kazna/internal/database"
	httpserver "github.com/dmitriy-zverev/kazna/internal/http"
	"github.com/dmitriy-zverev/kazna/internal/metrics"
	"github.com/dmitriy-zverev/kazna/internal/payments"
	"github.com/dmitriy-zverev/kazna/internal/providers"
	"github.com/dmitriy-zverev/kazna/internal/providers/yookassa"
	"github.com/dmitriy-zverev/kazna/internal/security"
)

const (
	adminToken = "test-admin-token"
	masterKey  = "abababababababababababababababababababababababababababababababab"
)

type harnessOptions struct {
	production      bool
	providerURL     string
	providerTimeout time.Duration
	pollInterval    time.Duration
}

type harness struct {
	server *httptest.Server
	pool   *pgxpool.Pool
	apps   *applications.Service
}

func newHarness(t *testing.T) *harness { return newHarnessWith(t, harnessOptions{}) }

func newProductionHarness(t *testing.T) *harness {
	return newHarnessWith(t, harnessOptions{production: true})
}

func newHarnessWith(t *testing.T, opts harnessOptions) *harness {
	t.Helper()
	ctx := context.Background()

	if opts.providerURL == "" {
		// Unreachable on purpose: a connection error is an unknown outcome.
		opts.providerURL = "http://127.0.0.1:1"
	}
	if opts.providerTimeout == 0 {
		opts.providerTimeout = 500 * time.Millisecond
	}
	if opts.pollInterval == 0 {
		// Zero means "always due", so a GET refreshes from the provider.
		opts.pollInterval = time.Nanosecond
	}

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("kazna"),
		tcpostgres.WithUsername("kazna"),
		tcpostgres.WithPassword("kazna"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	if err := database.RunMigrations(dsn); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	pool, err := database.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	cipher, err := security.NewCipher(masterKey, "")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	appEnv := "test"
	if opts.production {
		appEnv = "production"
	}

	cfg := &config.Config{
		AppEnv:          appEnv,
		AdminToken:      adminToken,
		YooKassaAPIURL:  opts.providerURL,
		LogLevel:        "error",
		ShutdownTimeout: 5 * time.Second,
	}

	apps := applications.NewService(pool, cipher, applications.Options{Production: opts.production})
	paymentService := payments.NewService(pool, apps,
		func(shopID, secretKey string) providers.Client {
			return yookassa.NewClient(opts.providerURL, shopID, secretKey)
		},
		payments.Options{
			Production:                opts.production,
			ProviderIdempotencyWindow: 24 * time.Hour,
			ProviderTimeout:           opts.providerTimeout,
			PollBaseInterval:          opts.pollInterval,
		},
	)

	registry := metrics.New()
	paymentService = payments.NewService(pool, apps,
		func(shopID, secretKey string) providers.Client {
			return yookassa.NewClient(opts.providerURL, shopID, secretKey)
		},
		payments.Options{
			Production:                opts.production,
			ProviderIdempotencyWindow: 24 * time.Hour,
			ProviderTimeout:           opts.providerTimeout,
			PollBaseInterval:          opts.pollInterval,
			Recorder:                  registry,
		},
	)

	router := httpserver.NewRouter(httpserver.Deps{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:       cfg,
		Health:       database.NewChecker(pool),
		Applications: apps,
		Payments:     paymentService,
		Metrics:      registry,
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return &harness{server: server, pool: pool, apps: apps}
}

func (h *harness) request(t *testing.T, method, path, token string, body any) (*http.Response, map[string]any) {
	t.Helper()
	return h.requestWithHeaders(t, method, path, token, body, nil)
}

func (h *harness) requestWithHeaders(t *testing.T, method, path, token string, body any, headers map[string]string) (*http.Response, map[string]any) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	return resp, decoded
}

type provisioned struct {
	id            string
	apiKey        string
	webhookSecret string
}

func (h *harness) createApplication(t *testing.T, name string) provisioned {
	t.Helper()
	return h.createApplicationWith(t, map[string]any{"name": name})
}

func (h *harness) createApplicationWith(t *testing.T, payload map[string]any) provisioned {
	t.Helper()

	resp, body := h.request(t, http.MethodPost, "/admin/applications", adminToken, payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create application: status %d, body %v", resp.StatusCode, body)
	}

	app, _ := body["application"].(map[string]any)
	key, _ := body["api_key"].(map[string]any)

	result := provisioned{
		id:            stringField(app, "id"),
		apiKey:        stringField(key, "api_key"),
		webhookSecret: stringField(body, "webhook_secret"),
	}
	if result.id == "" || result.apiKey == "" || result.webhookSecret == "" {
		t.Fatalf("create application: incomplete response %v", body)
	}
	return result
}

// attachProvider connects a store and returns the provider account id.
func (h *harness) attachProvider(t *testing.T, appID, shopID, secret string, isDefault bool) string {
	t.Helper()
	payload := map[string]any{
		"provider":    "yookassa",
		"shop_id":     shopID,
		"secret_key":  secret,
		"environment": "test",
	}
	if isDefault {
		payload["is_default"] = true
	}

	resp, body := h.request(t, http.MethodPost, "/admin/applications/"+appID+"/providers", adminToken, payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("attach provider: status %d, body %v", resp.StatusCode, body)
	}
	return stringField(body, "id")
}

// provisionApp creates an application with a connected store, ready to pay.
func (h *harness) provisionApp(t *testing.T, name string) provisioned {
	t.Helper()
	app := h.createApplication(t, name)
	h.attachProvider(t, app.id, "shop-"+app.id[:8], "secret-"+app.id[:8], true)
	return app
}

func (h *harness) countDefaults(t *testing.T, appID string) int {
	t.Helper()
	var count int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM application_providers WHERE application_id = $1 AND is_default`,
		appID).Scan(&count); err != nil {
		t.Fatalf("count defaults: %v", err)
	}
	return count
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func numberField(m map[string]any, key string) float64 {
	value, _ := m[key].(float64)
	return value
}

func errorCode(body map[string]any) string {
	errObj, _ := body["error"].(map[string]any)
	return stringField(errObj, "code")
}

// fakeYooKassa is a controllable stand-in for the provider API. It is
// idempotent on Idempotence-Key, like the real API, so a second provider
// payment can only appear if the service used a different key.
type fakeYooKassa struct {
	mu sync.Mutex

	createCalls        int
	getCalls           int
	idempotencyKeys    []string
	byIdempotencyKey   map[string]string // key → provider payment id
	statuses           map[string]string // provider payment id → status
	nextID             int
	createStatus       int           // non-zero: reply with this HTTP status
	createDelay        time.Duration // delay before replying
	hangUntilCanceled  bool          // never reply; wait for the client to give up
	lastCreateBody     string
	lastGetIDempotency string
}

func newFakeYooKassa() *fakeYooKassa {
	return &fakeYooKassa{
		byIdempotencyKey: map[string]string{},
		statuses:         map[string]string{},
	}
}

func (f *fakeYooKassa) setStatus(providerPaymentID, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[providerPaymentID] = status
}

func (f *fakeYooKassa) counts() (create, get int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalls, f.getCalls
}

func (f *fakeYooKassa) providerPaymentIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.byIdempotencyKey))
	for _, id := range f.byIdempotencyKey {
		ids = append(ids, id)
	}
	return ids
}

func (f *fakeYooKassa) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/payments", f.handleCreate)
	mux.HandleFunc("/v3/payments/", f.handleGet)
	return mux
}

func (f *fakeYooKassa) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	raw, _ := io.ReadAll(r.Body)
	key := r.Header.Get("Idempotence-Key")

	f.mu.Lock()
	f.createCalls++
	f.idempotencyKeys = append(f.idempotencyKeys, key)
	f.lastCreateBody = string(raw)
	createStatus := f.createStatus
	delay := f.createDelay
	hang := f.hangUntilCanceled
	f.mu.Unlock()

	if hang {
		<-r.Context().Done()
		return
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	if createStatus != 0 {
		w.WriteHeader(createStatus)
		_, _ = w.Write([]byte(`{"type":"error","code":"invalid_request","description":"rejected"}`))
		return
	}

	f.mu.Lock()
	id, seen := f.byIdempotencyKey[key]
	if !seen {
		f.nextID++
		id = "provider-payment-" + strconv.Itoa(f.nextID)
		f.byIdempotencyKey[key] = id
		f.statuses[id] = "pending"
	}
	f.mu.Unlock()

	writeFakePayment(w, id, f.statusFor(id))
}

func (f *fakeYooKassa) handleGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v3/payments/")

	f.mu.Lock()
	f.getCalls++
	f.lastGetIDempotency = r.Header.Get("Idempotence-Key")
	known := f.statuses[id] != ""
	f.mu.Unlock()

	if !known {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"error","code":"not_found"}`))
		return
	}
	writeFakePayment(w, id, f.statusFor(id))
}

func (f *fakeYooKassa) statusFor(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses[id]
}

func writeFakePayment(w http.ResponseWriter, id, status string) {
	paid := status == "succeeded"
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{
		"id": "` + id + `",
		"status": "` + status + `",
		"paid": ` + boolString(paid) + `,
		"amount": {"value": "499.00", "currency": "RUB"},
		"confirmation": {"type": "redirect", "confirmation_url": "https://yoomoney.ru/checkout/` + id + `"}
	}`))
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
