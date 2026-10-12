package payments

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/providers"
	"github.com/dmitriy-zverev/kazna/internal/security"
)

// validate enforces the request limits from docs/TECHNICAL_PLAN.md §12.4 and
// fills in defaults. It mutates in so the caller can use the normalized values.
func (s *Service) validate(app applications.Application, idempotencyKey string, in *CreatePaymentInput) error {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return fmt.Errorf("%w: Idempotency-Key header is required", ErrValidation)
	}
	if len(key) > 64 {
		return fmt.Errorf("%w: Idempotency-Key must be at most 64 characters", ErrValidation)
	}

	in.OrderID = strings.TrimSpace(in.OrderID)
	if in.OrderID == "" || len(in.OrderID) > MaxOrderIDLen {
		return fmt.Errorf("%w: order_id is required and must be at most %d characters", ErrValidation, MaxOrderIDLen)
	}

	if in.AmountMinor <= 0 {
		return fmt.Errorf("%w: amount must be a positive integer in minor units", ErrValidation)
	}

	if in.Currency == "" {
		in.Currency = "RUB"
	}
	if in.Currency != "RUB" {
		return fmt.Errorf("%w: only RUB is supported", ErrValidation)
	}

	in.Description = strings.TrimSpace(in.Description)
	if len(in.Description) > MaxDescriptionLen {
		return fmt.Errorf("%w: description must be at most %d characters", ErrValidation, MaxDescriptionLen)
	}

	if len(in.Metadata) > MaxMetadataKeys {
		return fmt.Errorf("%w: metadata must have at most %d keys", ErrValidation, MaxMetadataKeys)
	}
	for name, value := range in.Metadata {
		if name == "" || len(name) > MaxMetadataKeyLen {
			return fmt.Errorf("%w: metadata keys must be 1..%d characters", ErrValidation, MaxMetadataKeyLen)
		}
		if len(value) > MaxMetadataValLen {
			return fmt.Errorf("%w: metadata values must be at most %d characters", ErrValidation, MaxMetadataValLen)
		}
	}

	// An explicit return URL is validated; the application default was already
	// validated when the application was created.
	if in.ReturnURL != nil {
		trimmed := strings.TrimSpace(*in.ReturnURL)
		if trimmed == "" {
			in.ReturnURL = app.DefaultReturnURL
		} else {
			if err := security.ValidateOutboundURL(trimmed, s.urls); err != nil {
				reason := strings.TrimPrefix(err.Error(), security.ErrInvalidURL.Error()+": ")
				return fmt.Errorf("%w: return_url: %s", ErrValidation, reason)
			}
			in.ReturnURL = &trimmed
		}
	} else {
		in.ReturnURL = app.DefaultReturnURL
	}

	return nil
}

// requestHash is the fingerprint used to detect a reused idempotency key with a
// different body. json.Marshal sorts map keys, so it is deterministic.
func requestHash(in CreatePaymentInput) ([]byte, error) {
	payload := struct {
		OrderID     string            `json:"order_id"`
		AmountMinor int64             `json:"amount_minor"`
		Currency    string            `json:"currency"`
		Description string            `json:"description"`
		ReturnURL   string            `json:"return_url"`
		Metadata    map[string]string `json:"metadata"`
	}{
		OrderID:     in.OrderID,
		AmountMinor: in.AmountMinor,
		Currency:    in.Currency,
		Description: in.Description,
		ReturnURL:   derefString(in.ReturnURL),
		Metadata:    in.Metadata,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("payments: encode request hash: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return sum[:], nil
}

// providerRequestBody is the immutable provider-facing payload stored with the
// operation, so Stage 4 can replay the call byte-for-byte.
func providerRequestBody(in CreatePaymentInput) map[string]any {
	return map[string]any{
		"amount_minor": in.AmountMinor,
		"currency":     in.Currency,
		"description":  in.Description,
		"return_url":   derefString(in.ReturnURL),
		"metadata":     in.Metadata,
	}
}

// providerMetadata is what the provider sees. It carries our own identifiers so
// a payment can be traced back from the provider side.
func providerMetadata(payment Payment) map[string]string {
	metadata := make(map[string]string, len(payment.Metadata)+2)
	for key, value := range payment.Metadata {
		metadata[key] = value
	}
	metadata["order_id"] = payment.OrderID
	metadata["payment_id"] = payment.ID.String()
	return metadata
}

func marshalMetadata(metadata map[string]string) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]string{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("payments: encode metadata: %w", err)
	}
	return encoded, nil
}

// internalStatus maps a normalized provider status onto the internal state.
func internalStatus(status providers.PaymentStatus) Status {
	switch status {
	case providers.StatusPending:
		return StatusPending
	case providers.StatusSucceeded:
		return StatusSucceeded
	case providers.StatusCanceled:
		return StatusCanceled
	default:
		return ""
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
