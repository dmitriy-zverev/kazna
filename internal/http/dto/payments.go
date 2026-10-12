package dto

import (
	"time"

	"github.com/dmitriy-zverev/kazna/internal/payments"
)

// CreatePaymentRequest is the payload for POST /v1/payments.
type CreatePaymentRequest struct {
	OrderID     string            `json:"order_id"`
	Amount      int64             `json:"amount"`
	Currency    string            `json:"currency"`
	Description string            `json:"description"`
	ReturnURL   *string           `json:"return_url"`
	Metadata    map[string]string `json:"metadata"`
}

// CreatePaymentAttemptRequest is the payload for
// POST /v1/orders/{order_id}/payment-attempts: the same fields without
// order_id, which comes from the path.
type CreatePaymentAttemptRequest struct {
	Amount      int64             `json:"amount"`
	Currency    string            `json:"currency"`
	Description string            `json:"description"`
	ReturnURL   *string           `json:"return_url"`
	Metadata    map[string]string `json:"metadata"`
}

// Payment is the public representation of a payment.
type Payment struct {
	ID            string     `json:"id"`
	OrderID       string     `json:"order_id"`
	AttemptNumber int32      `json:"attempt_number"`
	Status        string     `json:"status"`
	Amount        int64      `json:"amount"`
	Currency      string     `json:"currency"`
	Provider      string     `json:"provider"`
	CheckoutURL   *string    `json:"checkout_url"`
	CreatedAt     time.Time  `json:"created_at"`
	PaidAt        *time.Time `json:"paid_at"`
}

// PaymentPending is the 202 response used while a provider result is unknown.
type PaymentPending struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	CheckoutURL *string `json:"checkout_url"`
	RetryAfter  int     `json:"retry_after"`
}

// NewPayment maps a domain payment to its wire representation.
func NewPayment(p payments.Payment) Payment {
	return Payment{
		ID:            p.ID.String(),
		OrderID:       p.OrderID,
		AttemptNumber: p.AttemptNumber,
		Status:        string(p.Status),
		Amount:        p.AmountMinor,
		Currency:      p.Currency,
		Provider:      p.Provider,
		CheckoutURL:   p.CheckoutURL,
		CreatedAt:     p.CreatedAt,
		PaidAt:        p.CompletedAt,
	}
}

// NewPayments maps a slice of domain payments.
func NewPayments(items []payments.Payment) []Payment {
	out := make([]Payment, 0, len(items))
	for _, item := range items {
		out = append(out, NewPayment(item))
	}
	return out
}

// NewPaymentPending maps a pending create result to the 202 body.
func NewPaymentPending(result payments.CreateResult) PaymentPending {
	return PaymentPending{
		ID:          result.Payment.ID.String(),
		Status:      string(result.Payment.Status),
		CheckoutURL: result.Payment.CheckoutURL,
		RetryAfter:  result.RetryAfterSeconds,
	}
}
