//go:build integration

// Package integration exercises the service against a real PostgreSQL instance.
//
// Run with:
//
//	make test-integration
//
// These tests are the Stage 2 acceptance criterion: two applications with
// different API keys authenticate independently and cannot reach each other's
// data, and provider secrets are unreadable in the database.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/dmitriy-zverev/kazna/internal/security"
)

const (
	adminToken   = "test-admin-token"
	providerBase = "http://127.0.0.1:1" // unreachable on purpose
	masterKey    = "abababababababababababababababababababababababababababababababab"
)

type harness struct {
	server *httptest.Server
	pool   *pgxpool.Pool
}

type provisioned struct {
	id            string
	apiKey        string
	webhookSecret string
}

func newHarness(t *testing.T) *harness { return newHarnessWith(t, false) }

func newProductionHarness(t *testing.T) *harness { return newHarnessWith(t, true) }

func newHarnessWith(t *testing.T, production bool) *harness {
	t.Helper()
	ctx := context.Background()

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
	if production {
		appEnv = "production"
	}

	cfg := &config.Config{
		AppEnv:          appEnv,
		AdminToken:      adminToken,
		YooKassaAPIURL:  providerBase,
		LogLevel:        "error",
		ShutdownTimeout: 5 * time.Second,
	}

	router := httpserver.NewRouter(httpserver.Deps{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:       cfg,
		Health:       database.NewChecker(pool),
		Applications: applications.NewService(pool, cipher, applications.Options{Production: production}),
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return &harness{server: server, pool: pool}
}

func (h *harness) request(t *testing.T, method, path, token string, body any) (*http.Response, map[string]any) {
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

// createApplication provisions an application through the admin API.
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

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func TestTwoApplicationsAreIsolated(t *testing.T) {
	h := newHarness(t)

	appA := h.createApplication(t, "SaaS A")
	appB := h.createApplication(t, "SaaS B")

	if appA.id == appB.id {
		t.Fatal("two applications received the same id")
	}
	if appA.apiKey == appB.apiKey {
		t.Fatal("two applications received the same API key")
	}
	if appA.webhookSecret == appB.webhookSecret {
		t.Fatal("two applications received the same webhook secret")
	}

	// Each key resolves to its own application and to nothing else.
	for _, tc := range []struct{ name, key, wantID string }{
		{"A", appA.apiKey, appA.id},
		{"B", appB.apiKey, appB.id},
	} {
		resp, body := h.request(t, http.MethodGet, "/v1/me", tc.key, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("me with key %s: status %d, body %v", tc.name, resp.StatusCode, body)
		}
		if got := stringField(body, "application_id"); got != tc.wantID {
			t.Fatalf("key %s resolved to %q, want %q", tc.name, got, tc.wantID)
		}
	}
}

func TestAPIKeyCannotReachAdminAPI(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	resp, _ := h.request(t, http.MethodGet, "/admin/applications", app.apiKey, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("application key reached the admin API: status %d, want 401", resp.StatusCode)
	}
}

func TestUnknownAndTamperedKeysAreRejected(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	tampered := app.apiKey[:len(app.apiKey)-1] + "X"
	if tampered == app.apiKey {
		t.Fatal("test setup produced an unchanged key")
	}

	for _, bad := range []string{"app_live_doesnotexist", tampered, "nonsense"} {
		resp, _ := h.request(t, http.MethodGet, "/v1/me", bad, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("key %q: status %d, want 401", bad, resp.StatusCode)
		}
	}
}

func TestRevokedKeyStopsWorking(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	resp, body := h.request(t, http.MethodGet, "/admin/applications/"+app.id+"/keys", adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list keys: status %d, body %v", resp.StatusCode, body)
	}
	data, _ := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected exactly one key, got %d", len(data))
	}
	first, _ := data[0].(map[string]any)
	keyID := stringField(first, "id")

	resp, _ = h.request(t, http.MethodDelete, "/admin/applications/"+app.id+"/keys/"+keyID, adminToken, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: status %d, want 204", resp.StatusCode)
	}

	resp, _ = h.request(t, http.MethodGet, "/v1/me", app.apiKey, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked key still works: status %d, want 401", resp.StatusCode)
	}
}

func TestProviderSecretIsStoredEncrypted(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	const plaintext = "live_secret_key_do_not_leak"

	resp, body := h.request(t, http.MethodPost, "/admin/applications/"+app.id+"/providers", adminToken, map[string]any{
		"provider":    "yookassa",
		"shop_id":     "123456",
		"secret_key":  plaintext,
		"environment": "test",
		"is_default":  true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("attach provider: status %d, body %v", resp.StatusCode, body)
	}
	if _, leaked := body["secret_key"]; leaked {
		t.Fatalf("secret_key must never be returned by the API: %v", body)
	}

	var stored []byte
	err := h.pool.QueryRow(context.Background(),
		`SELECT encrypted_secret_key FROM provider_accounts WHERE shop_id = $1`, "123456").Scan(&stored)
	if err != nil {
		t.Fatalf("read stored secret: %v", err)
	}
	if bytes.Contains(stored, []byte(plaintext)) {
		t.Fatal("plaintext secret is present in the database column")
	}

	cipher, _ := security.NewCipher(masterKey, "")
	decrypted, err := cipher.Decrypt(stored)
	if err != nil {
		t.Fatalf("stored secret does not decrypt: %v", err)
	}
	if string(decrypted) != plaintext {
		t.Fatalf("decrypted secret = %q, want %q", decrypted, plaintext)
	}
}

func TestProviderConnectionStatusIsRecorded(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	resp, body := h.request(t, http.MethodPost, "/admin/applications/"+app.id+"/providers", adminToken, map[string]any{
		"provider":    "yookassa",
		"shop_id":     "777",
		"secret_key":  "secret",
		"environment": "test",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("attach provider: status %d, body %v", resp.StatusCode, body)
	}
	providerID := stringField(body, "id")

	// The configured provider URL is unreachable, so the check must fail
	// gracefully and mark the connection as needing attention.
	resp, body = h.request(t, http.MethodPost, "/admin/providers/"+providerID+"/test", adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("test connection: status %d, body %v", resp.StatusCode, body)
	}
	if status := stringField(body, "status"); status != "failed" {
		t.Fatalf("status = %q, want failed for an unreachable provider", status)
	}
	if reason := stringField(body, "reason"); reason == "" {
		t.Fatal("failure must include a machine-readable reason")
	}

	var status string
	if err := h.pool.QueryRow(context.Background(),
		`SELECT status FROM provider_accounts WHERE shop_id = $1`, "777").Scan(&status); err != nil {
		t.Fatalf("read provider status: %v", err)
	}
	if status != applications.ProviderStatusNeedsAttention {
		t.Fatalf("stored status = %q, want %q", status, applications.ProviderStatusNeedsAttention)
	}
}

func (h *harness) attachProvider(t *testing.T, appID, shopID, secret string, isDefault bool) (*http.Response, map[string]any) {
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
	return h.request(t, http.MethodPost, "/admin/applications/"+appID+"/providers", adminToken, payload)
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

func TestOnlyOneDefaultProviderPerApplication(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	for _, shop := range []string{"111", "222"} {
		if resp, body := h.attachProvider(t, app.id, shop, "secret", true); resp.StatusCode != http.StatusCreated {
			t.Fatalf("attach provider %s: status %d, body %v", shop, resp.StatusCode, body)
		}
	}

	if got := h.countDefaults(t, app.id); got != 1 {
		t.Fatalf("default provider count = %d, want 1", got)
	}
}

// Re-attaching an existing provider without is_default must not strip the
// application of its default connection.
func TestReattachingProviderKeepsDefault(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	if resp, body := h.attachProvider(t, app.id, "123456", "secret", true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("first attach: status %d, body %v", resp.StatusCode, body)
	}
	if got := h.countDefaults(t, app.id); got != 1 {
		t.Fatalf("default count after first attach = %d, want 1", got)
	}

	// Same shop, same secret, is_default omitted.
	if resp, body := h.attachProvider(t, app.id, "123456", "secret", false); resp.StatusCode != http.StatusCreated {
		t.Fatalf("second attach: status %d, body %v", resp.StatusCode, body)
	}
	if got := h.countDefaults(t, app.id); got != 1 {
		t.Fatalf("default count after re-attach = %d, want 1 (regression: default was cleared)", got)
	}
}

// A different secret for an already connected shop must be refused rather than
// silently ignored.
func TestReattachingProviderWithDifferentSecretIsRejected(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	if resp, body := h.attachProvider(t, app.id, "123456", "original-secret", true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("first attach: status %d, body %v", resp.StatusCode, body)
	}

	resp, body := h.attachProvider(t, app.id, "123456", "different-secret", false)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 when the secret differs; body %v", resp.StatusCode, body)
	}

	// The stored secret must be unchanged.
	cipher, _ := security.NewCipher(masterKey, "")
	var stored []byte
	if err := h.pool.QueryRow(context.Background(),
		`SELECT encrypted_secret_key FROM provider_accounts WHERE shop_id = '123456'`).Scan(&stored); err != nil {
		t.Fatalf("read stored secret: %v", err)
	}
	decrypted, err := cipher.Decrypt(stored)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != "original-secret" {
		t.Fatalf("stored secret = %q, want original-secret", decrypted)
	}
}

func TestWebhookSecretIsIssuedOnceAndStoredEncrypted(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	if app.webhookSecret == "" {
		t.Fatal("webhook secret must be issued at creation")
	}

	var stored []byte
	if err := h.pool.QueryRow(context.Background(),
		`SELECT webhook_secret_encrypted FROM applications WHERE id = $1`, app.id).Scan(&stored); err != nil {
		t.Fatalf("read stored webhook secret: %v", err)
	}
	if len(stored) == 0 {
		t.Fatal("webhook secret was not persisted")
	}
	if bytes.Contains(stored, []byte(app.webhookSecret)) {
		t.Fatal("webhook secret is stored in plaintext")
	}

	cipher, _ := security.NewCipher(masterKey, "")
	decrypted, err := cipher.Decrypt(stored)
	if err != nil {
		t.Fatalf("stored webhook secret does not decrypt: %v", err)
	}
	if string(decrypted) != app.webhookSecret {
		t.Fatal("stored webhook secret does not match the issued value")
	}

	// It must not be readable through the application API.
	resp, body := h.request(t, http.MethodGet, "/admin/applications/"+app.id, adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get application: status %d", resp.StatusCode)
	}
	if _, leaked := body["webhook_secret"]; leaked {
		t.Fatalf("webhook secret leaked through the application representation: %v", body)
	}
}

func TestWebhookSecretRotation(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	resp, body := h.request(t, http.MethodPost, "/admin/applications/"+app.id+"/webhook-secret", adminToken, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("rotate: status %d, body %v", resp.StatusCode, body)
	}
	rotated := stringField(body, "webhook_secret")
	if rotated == "" || rotated == app.webhookSecret {
		t.Fatalf("rotation did not produce a new secret (got %q)", rotated)
	}

	cipher, _ := security.NewCipher(masterKey, "")
	var stored []byte
	if err := h.pool.QueryRow(context.Background(),
		`SELECT webhook_secret_encrypted FROM applications WHERE id = $1`, app.id).Scan(&stored); err != nil {
		t.Fatalf("read stored secret: %v", err)
	}
	decrypted, err := cipher.Decrypt(stored)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != rotated {
		t.Fatal("stored secret does not match the rotated value")
	}
}

func TestProductionRejectsInternalURLs(t *testing.T) {
	h := newProductionHarness(t)

	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"internal webhook host", map[string]any{
			"name": "SaaS", "webhook_url": "https://webhook-sink:8080/events",
		}},
		{"private ip webhook", map[string]any{
			"name": "SaaS", "webhook_url": "https://10.0.0.5/events",
		}},
		{"loopback webhook", map[string]any{
			"name": "SaaS", "webhook_url": "https://127.0.0.1/events",
		}},
		{"metadata endpoint", map[string]any{
			"name": "SaaS", "webhook_url": "http://169.254.169.254/latest/meta-data",
		}},
		{"plain http return url", map[string]any{
			"name": "SaaS", "default_return_url": "http://example.com/billing",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := h.request(t, http.MethodPost, "/admin/applications", adminToken, tc.payload)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %v; body %v", resp.StatusCode, tc.payload, body)
			}
		})
	}

	// A public https destination is still accepted.
	resp, body := h.request(t, http.MethodPost, "/admin/applications", adminToken, map[string]any{
		"name": "Public SaaS", "webhook_url": "https://93.184.216.34/events",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("public https url rejected: status %d, body %v", resp.StatusCode, body)
	}
}

func TestDevelopmentAllowsInternalWebhookURL(t *testing.T) {
	h := newHarness(t)

	// The verification stand registers a webhook sink on the compose network.
	resp, body := h.request(t, http.MethodPost, "/admin/applications", adminToken, map[string]any{
		"name": "SaaS", "webhook_url": "http://webhook-sink:8080/payment-events",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("development must accept an internal webhook host: status %d, body %v", resp.StatusCode, body)
	}
}

func TestAdminApplicationValidation(t *testing.T) {
	h := newHarness(t)

	resp, _ := h.request(t, http.MethodPost, "/admin/applications", adminToken, map[string]any{"name": "   "})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank name: status %d, want 400", resp.StatusCode)
	}

	app := h.createApplication(t, "SaaS")

	resp, _ = h.request(t, http.MethodPost, "/admin/applications/"+app.id+"/providers", adminToken, map[string]any{
		"provider":    "yookassa",
		"shop_id":     "1",
		"secret_key":  "secret",
		"environment": "sandbox",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad environment: status %d, want 400", resp.StatusCode)
	}

	resp, _ = h.request(t, http.MethodGet, "/admin/applications/not-a-uuid", adminToken, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad uuid: status %d, want 400", resp.StatusCode)
	}

	// A conflict message must not leak database identifiers.
	resp, body := h.request(t, http.MethodPost, "/admin/applications/"+app.id+"/providers", adminToken, map[string]any{
		"provider":    "yookassa",
		"shop_id":     "1",
		"secret_key":  "secret",
		"environment": "test",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("attach provider: status %d, body %v", resp.StatusCode, body)
	}
	resp, body = h.request(t, http.MethodPost, "/admin/applications/"+app.id+"/providers", adminToken, map[string]any{
		"provider":    "yookassa",
		"shop_id":     "1",
		"secret_key":  "other",
		"environment": "test",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("conflicting secret: status %d, want 409", resp.StatusCode)
	}
	errObj, _ := body["error"].(map[string]any)
	message := stringField(errObj, "message")
	for _, leak := range []string{"pgx", "SQLSTATE", "constraint", "23505"} {
		if strings.Contains(message, leak) {
			t.Fatalf("conflict message leaks internals: %q", message)
		}
	}
}
