// Package providers defines the provider-agnostic contract that the payment
// core depends on. Concrete adapters live in subpackages (providers/yookassa)
// and must not leak provider-specific types into the core.
package providers

import "context"

// Name identifies a payment provider.
type Name string

// ProviderYooKassa is the only provider implemented for MVP.
const ProviderYooKassa Name = "yookassa"

// PaymentStatus is the normalized provider payment state.
type PaymentStatus string

// Normalized provider statuses. Anything else is treated as unknown.
const (
	StatusPending   PaymentStatus = "pending"
	StatusSucceeded PaymentStatus = "succeeded"
	StatusCanceled  PaymentStatus = "canceled"
)

// CreatePaymentRequest is everything needed to create a payment at a provider.
// IdempotencyKey must be sent to the provider so a retry is not a new payment.
type CreatePaymentRequest struct {
	AmountMinor    int64
	Currency       string
	Description    string
	ReturnURL      string
	Metadata       map[string]string
	IdempotencyKey string
}

// Payment is the normalized result of a provider payment read or create.
type Payment struct {
	ProviderPaymentID string
	Status            PaymentStatus
	CheckoutURL       string
	Paid              bool
	AmountMinor       int64
	Currency          string
}

// Client is the per-provider-account API surface used by the payment core.
type Client interface {
	CreatePayment(ctx context.Context, req CreatePaymentRequest) (Payment, error)
	GetPayment(ctx context.Context, providerPaymentID string) (Payment, error)
}

// ClientFactory builds a client for one provider account.
type ClientFactory func(shopID, secretKey string) Client
