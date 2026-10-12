// Package dto holds the JSON shapes of the HTTP API. Keeping them separate from
// domain models means a schema change never silently changes the wire format.
package dto

import (
	"time"

	"github.com/dmitriy-zverev/kazna/internal/applications"
)

// Application is the public representation of an application.
type Application struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	Status           string    `json:"status"`
	DefaultReturnURL *string   `json:"default_return_url"`
	WebhookURL       *string   `json:"webhook_url"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// CreateApplicationRequest is the payload for POST /admin/applications.
type CreateApplicationRequest struct {
	Name             string  `json:"name"`
	DefaultReturnURL *string `json:"default_return_url"`
	WebhookURL       *string `json:"webhook_url"`
}

// CreateApplicationResponse returns the application and its first API key. The
// key is present only in this response.
type CreateApplicationResponse struct {
	Application Application `json:"application"`
	APIKey      IssuedKey   `json:"api_key"`
}

// IssuedKey carries a key that will never be shown again.
type IssuedKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	APIKey    string     `json:"api_key"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// APIKey describes an existing key without key material.
type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreateAPIKeyRequest is the payload for POST /admin/applications/{id}/keys.
type CreateAPIKeyRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// AttachProviderRequest is the payload for connecting a provider store.
type AttachProviderRequest struct {
	Provider    string `json:"provider"`
	ShopID      string `json:"shop_id"`
	SecretKey   string `json:"secret_key"`
	Environment string `json:"environment"`
	IsDefault   bool   `json:"is_default"`
}

// ProviderAccount is the public representation of a provider connection. The
// secret is never included.
type ProviderAccount struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	ShopID      string    `json:"shop_id"`
	Environment string    `json:"environment"`
	Status      string    `json:"status"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProviderTestResponse reports the outcome of a connectivity check.
type ProviderTestResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// Me is the response of GET /v1/me and proves which application a key belongs to.
type Me struct {
	ApplicationID string `json:"application_id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
}

// List is the envelope for collection responses.
type List[T any] struct {
	Data []T `json:"data"`
}

// NewApplication maps a domain application to its wire representation.
func NewApplication(a applications.Application) Application {
	return Application{
		ID:               a.ID.String(),
		Name:             a.Name,
		Slug:             a.Slug,
		Status:           a.Status,
		DefaultReturnURL: a.DefaultReturnURL,
		WebhookURL:       a.WebhookURL,
		CreatedAt:        a.CreatedAt,
		UpdatedAt:        a.UpdatedAt,
	}
}

// NewApplications maps a slice of domain applications.
func NewApplications(items []applications.Application) []Application {
	out := make([]Application, 0, len(items))
	for _, item := range items {
		out = append(out, NewApplication(item))
	}
	return out
}

// NewIssuedKey maps a freshly issued key, including the one-time secret.
func NewIssuedKey(k applications.IssuedKey) IssuedKey {
	return IssuedKey{
		ID:        k.ID.String(),
		Name:      k.Name,
		Prefix:    k.Prefix,
		APIKey:    k.FullKey,
		ExpiresAt: k.ExpiresAt,
		CreatedAt: k.CreatedAt,
	}
}

// NewAPIKey maps key metadata.
func NewAPIKey(k applications.APIKeyInfo) APIKey {
	return APIKey{
		ID:         k.ID.String(),
		Name:       k.Name,
		Prefix:     k.Prefix,
		ExpiresAt:  k.ExpiresAt,
		LastUsedAt: k.LastUsedAt,
		RevokedAt:  k.RevokedAt,
		CreatedAt:  k.CreatedAt,
	}
}

// NewAPIKeys maps a slice of key metadata.
func NewAPIKeys(items []applications.APIKeyInfo) []APIKey {
	out := make([]APIKey, 0, len(items))
	for _, item := range items {
		out = append(out, NewAPIKey(item))
	}
	return out
}

// NewProviderAccount maps a provider connection without its secret.
func NewProviderAccount(p applications.ProviderAccount) ProviderAccount {
	return ProviderAccount{
		ID:          p.ID.String(),
		Provider:    p.Provider,
		ShopID:      p.ShopID,
		Environment: p.Environment,
		Status:      p.Status,
		IsDefault:   p.IsDefault,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// NewProviderAccounts maps a slice of provider connections.
func NewProviderAccounts(items []applications.ProviderAccount) []ProviderAccount {
	out := make([]ProviderAccount, 0, len(items))
	for _, item := range items {
		out = append(out, NewProviderAccount(item))
	}
	return out
}
