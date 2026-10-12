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
)

type harness struct {
	server *httptest.Server
	pool   *pgxpool.Pool
}

func newHarness(t *testing.T) *harness {
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

	cipher, err := security.NewCipher(strings.Repeat("ab", 32), "")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	cfg := &config.Config{
		AppEnv:          "test",
		AdminToken:      adminToken,
		YooKassaAPIURL:  providerBase,
		LogLevel:        "error",
		ShutdownTimeout: 5 * time.Second,
	}

	router := httpserver.NewRouter(httpserver.Deps{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:       cfg,
		Health:       database.NewChecker(pool),
		Applications: applications.NewService(pool, cipher),
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

// createApplication provisions an application through the admin API and returns
// its id and the one-time API key.
func (h *harness) createApplication(t *testing.T, name string) (id, apiKey string) {
	t.Helper()

	resp, body := h.request(t, http.MethodPost, "/admin/applications", adminToken,
		map[string]any{"name": name})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create application %q: status %d, body %v", name, resp.StatusCode, body)
	}

	app, _ := body["application"].(map[string]any)
	key, _ := body["api_key"].(map[string]any)

	id, _ = app["id"].(string)
	apiKey, _ = key["api_key"].(string)
	if id == "" || apiKey == "" {
		t.Fatalf("create application %q: missing id or api_key in %v", name, body)
	}
	return id, apiKey
}

func TestTwoApplicationsAreIsolated(t *testing.T) {
	h := newHarness(t)

	idA, keyA := h.createApplication(t, "SaaS A")
	idB, keyB := h.createApplication(t, "SaaS B")

	if idA == idB {
		t.Fatal("two applications received the same id")
	}
	if keyA == keyB {
		t.Fatal("two applications received the same API key")
	}

	// Each key resolves to its own application and to nothing else.
	for _, tc := range []struct{ name, key, wantID string }{
		{"A", keyA, idA},
		{"B", keyB, idB},
	} {
		resp, body := h.request(t, http.MethodGet, "/v1/me", tc.key, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("me with key %s: status %d, body %v", tc.name, resp.StatusCode, body)
		}
		if got, _ := body["application_id"].(string); got != tc.wantID {
			t.Fatalf("key %s resolved to %q, want %q", tc.name, got, tc.wantID)
		}
	}
}

func TestAPIKeyCannotReachAdminAPI(t *testing.T) {
	h := newHarness(t)
	_, key := h.createApplication(t, "SaaS")

	resp, _ := h.request(t, http.MethodGet, "/admin/applications", key, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("application key reached the admin API: status %d, want 401", resp.StatusCode)
	}
}

func TestUnknownAndTamperedKeysAreRejected(t *testing.T) {
	h := newHarness(t)
	_, key := h.createApplication(t, "SaaS")

	tampered := key[:len(key)-1] + "X"
	if tampered == key {
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
	appID, key := h.createApplication(t, "SaaS")

	resp, body := h.request(t, http.MethodGet, "/admin/applications/"+appID+"/keys", adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list keys: status %d, body %v", resp.StatusCode, body)
	}
	data, _ := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected exactly one key, got %d", len(data))
	}
	first, _ := data[0].(map[string]any)
	keyID, _ := first["id"].(string)

	resp, _ = h.request(t, http.MethodDelete, "/admin/applications/"+appID+"/keys/"+keyID, adminToken, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: status %d, want 204", resp.StatusCode)
	}

	resp, _ = h.request(t, http.MethodGet, "/v1/me", key, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked key still works: status %d, want 401", resp.StatusCode)
	}
}

func TestProviderSecretIsStoredEncrypted(t *testing.T) {
	h := newHarness(t)
	appID, _ := h.createApplication(t, "SaaS")

	const plaintext = "live_secret_key_do_not_leak"

	resp, body := h.request(t, http.MethodPost, "/admin/applications/"+appID+"/providers", adminToken, map[string]any{
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

	cipher, _ := security.NewCipher(strings.Repeat("ab", 32), "")
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
	appID, _ := h.createApplication(t, "SaaS")

	resp, body := h.request(t, http.MethodPost, "/admin/applications/"+appID+"/providers", adminToken, map[string]any{
		"provider":    "yookassa",
		"shop_id":     "777",
		"secret_key":  "secret",
		"environment": "test",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("attach provider: status %d, body %v", resp.StatusCode, body)
	}
	providerID, _ := body["id"].(string)

	// The configured provider URL is unreachable, so the check must fail
	// gracefully and mark the connection as needing attention.
	resp, body = h.request(t, http.MethodPost, "/admin/providers/"+providerID+"/test", adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("test connection: status %d, body %v", resp.StatusCode, body)
	}
	if status, _ := body["status"].(string); status != "failed" {
		t.Fatalf("status = %q, want failed for an unreachable provider", status)
	}
	if reason, _ := body["reason"].(string); reason == "" {
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

func TestOnlyOneDefaultProviderPerApplication(t *testing.T) {
	h := newHarness(t)
	appID, _ := h.createApplication(t, "SaaS")

	for i, shop := range []string{"111", "222"} {
		resp, body := h.request(t, http.MethodPost, "/admin/applications/"+appID+"/providers", adminToken, map[string]any{
			"provider":    "yookassa",
			"shop_id":     shop,
			"secret_key":  "secret",
			"environment": "test",
			"is_default":  true,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("attach provider %d: status %d, body %v", i, resp.StatusCode, body)
		}
	}

	var defaults int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM application_providers WHERE application_id = $1 AND is_default`,
		appID).Scan(&defaults); err != nil {
		t.Fatalf("count defaults: %v", err)
	}
	if defaults != 1 {
		t.Fatalf("default provider count = %d, want 1", defaults)
	}
}

func TestAdminApplicationValidation(t *testing.T) {
	h := newHarness(t)

	resp, _ := h.request(t, http.MethodPost, "/admin/applications", adminToken, map[string]any{"name": "   "})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank name: status %d, want 400", resp.StatusCode)
	}

	appID, _ := h.createApplication(t, "SaaS")
	resp, _ = h.request(t, http.MethodPost, "/admin/applications/"+appID+"/providers", adminToken, map[string]any{
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
}
