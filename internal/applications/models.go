package applications

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Domain errors. HTTP handlers map these to the normalized error envelope.
var (
	ErrNotFound     = errors.New("applications: not found")
	ErrUnauthorized = errors.New("applications: unauthorized")
	ErrConflict     = errors.New("applications: conflict")
	ErrValidation   = errors.New("applications: validation failed")
)

// Application is a client application registered with the payment service.
type Application struct {
	ID               uuid.UUID
	Name             string
	Slug             string
	Status           string
	DefaultReturnURL *string
	WebhookURL       *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Active reports whether the application may use the API.
func (a Application) Active() bool { return a.Status == StatusActive }

// Application statuses.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// IssuedKey is a newly minted API key. FullKey is returned to the caller once
// and never stored; only Prefix and Hash are persisted.
type IssuedKey struct {
	ID        uuid.UUID
	Name      string
	Prefix    string
	FullKey   string
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// APIKeyInfo describes an existing key without exposing key material.
type APIKeyInfo struct {
	ID         uuid.UUID
	Name       string
	Prefix     string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// Revoked reports whether the key has been revoked.
func (k APIKeyInfo) Revoked() bool { return k.RevokedAt != nil }

// ProviderAccount is a connection to an external payment provider.
type ProviderAccount struct {
	ID          uuid.UUID
	Provider    string
	ShopID      string
	Environment string
	Status      string
	IsDefault   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Provider environments and statuses.
const (
	EnvironmentTest       = "test"
	EnvironmentProduction = "production"

	ProviderStatusActive         = "active"
	ProviderStatusNeedsAttention = "needs_attention"
	ProviderStatusDisabled       = "disabled"
)

// Provider names.
const ProviderYooKassa = "yookassa"

// Authenticated is the result of a successful API key check.
type Authenticated struct {
	Application Application
	KeyID       uuid.UUID
}

// Provisioned is what an application needs to start integrating. Both values
// are shown exactly once, at creation.
type Provisioned struct {
	APIKey        IssuedKey
	WebhookSecret string
}

// CreateApplicationInput is the payload for creating an application.
type CreateApplicationInput struct {
	Name             string
	DefaultReturnURL *string
	WebhookURL       *string
}

// AttachProviderInput is the payload for connecting a provider account.
type AttachProviderInput struct {
	Provider    string
	ShopID      string
	SecretKey   string
	Environment string
	IsDefault   bool
}
