//go:build integration

// Package integration exercises the service against a real PostgreSQL instance.
//
// Run with:
//
//	make test-integration
//
// Stage 2 acceptance: two applications with different API keys authenticate
// independently and cannot reach each other's data, and provider secrets are
// unreadable in the database.
package integration

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/security"
)

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

func TestOnlyOneDefaultProviderPerApplication(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	for _, shop := range []string{"111", "222"} {
		h.attachProvider(t, app.id, shop, "secret", true)
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

	h.attachProvider(t, app.id, "123456", "secret", true)
	if got := h.countDefaults(t, app.id); got != 1 {
		t.Fatalf("default count after first attach = %d, want 1", got)
	}

	// Same shop, same secret, is_default omitted.
	h.attachProvider(t, app.id, "123456", "secret", false)
	if got := h.countDefaults(t, app.id); got != 1 {
		t.Fatalf("default count after re-attach = %d, want 1 (regression: default was cleared)", got)
	}
}

// A different secret for an already connected shop must be refused rather than
// silently ignored.
func TestReattachingProviderWithDifferentSecretIsRejected(t *testing.T) {
	h := newHarness(t)
	app := h.createApplication(t, "SaaS")

	h.attachProvider(t, app.id, "123456", "original-secret", true)

	resp, body := h.request(t, http.MethodPost, "/admin/applications/"+app.id+"/providers", adminToken, map[string]any{
		"provider":    "yookassa",
		"shop_id":     "123456",
		"secret_key":  "different-secret",
		"environment": "test",
	})
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
