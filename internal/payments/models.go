package payments

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Domain errors. HTTP handlers map these to the normalized error envelope.
var (
	ErrValidation            = errors.New("payments: validation failed")
	ErrNotFound              = errors.New("payments: not found")
	ErrIdempotencyConflict   = errors.New("payments: idempotency conflict")
	ErrActiveAttemptExists   = errors.New("payments: an unfinished attempt already exists for this order")
	ErrOrderAlreadySucceeded = errors.New("payments: order already paid")
	ErrNoProviderAccount     = errors.New("payments: no active provider account")
	ErrIllegalTransition     = errors.New("payments: illegal status transition")
)

// Status is the internal payment state (docs/PAYMENT_LIFECYCLE.md §1).
type Status string

// Internal payment states.
const (
	StatusCreated         Status = "created"
	StatusCreating        Status = "creating"
	StatusCreationUnknown Status = "creation_unknown"
	StatusPending         Status = "pending"
	StatusSucceeded       Status = "succeeded"
	StatusCanceled        Status = "canceled"
)

// Terminal reports whether no further transition is possible.
func (s Status) Terminal() bool { return s == StatusSucceeded || s == StatusCanceled }

// OperationType classifies an interaction with the provider.
type OperationType string

// Operation types.
const (
	OperationCreatePayment OperationType = "create_payment"
	OperationGetPayment    OperationType = "get_payment"
	OperationCreateRefund  OperationType = "create_refund"
)

// OperationStatus tracks the outcome of a provider interaction.
type OperationStatus string

// Operation statuses. `unknown` means the provider may or may not have applied
// the operation; it must never be retried with a fresh idempotency key.
const (
	OperationPending     OperationStatus = "pending"
	OperationInProgress  OperationStatus = "in_progress"
	OperationSucceeded   OperationStatus = "succeeded"
	OperationFailed      OperationStatus = "failed"
	OperationUnknown     OperationStatus = "unknown"
	OperationNeedsReview OperationStatus = "needs_review"
)

// Validation limits (docs/TECHNICAL_PLAN.md §12.4).
const (
	MaxOrderIDLen     = 128
	MaxDescriptionLen = 128
	MaxMetadataKeys   = 20
	MaxMetadataKeyLen = 40
	MaxMetadataValLen = 64
	// MaxListLimit caps the page size of the payments list endpoint.
	MaxListLimit     = 100
	DefaultListLimit = 20
)

// Payment is a payment attempt for an order.
type Payment struct {
	ID                uuid.UUID
	ApplicationID     uuid.UUID
	ProviderAccountID uuid.UUID
	OrderID           string
	AttemptNumber     int32
	Provider          string
	ProviderPaymentID *string
	AmountMinor       int64
	Currency          string
	Description       string
	Status            Status
	CheckoutURL       *string
	ReturnURL         *string
	Metadata          map[string]string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       *time.Time
	NextCheckAt       *time.Time
}

// CreatePaymentInput is the payload for creating a payment or a new attempt.
type CreatePaymentInput struct {
	OrderID     string
	AmountMinor int64
	Currency    string
	Description string
	ReturnURL   *string
	Metadata    map[string]string
}

// CreateResult is the outcome of a create request.
type CreateResult struct {
	Payment Payment
	// Replayed is true when an existing payment was returned for a repeated
	// idempotency key instead of a new one being created.
	Replayed bool
	// Pending is true when the provider result is not yet known (HTTP 202).
	Pending bool
	// RetryAfterSeconds is the suggested delay before polling again.
	RetryAfterSeconds int
}

// HTTPStatus maps a create result to its response status.
func (r CreateResult) HTTPStatus() int {
	if r.Pending {
		return 202
	}
	return 201
}

// ListQuery filters the payments list.
type ListQuery struct {
	OrderID string
	Limit   int32
}

// ListResult is a page of payments.
type ListResult struct {
	Payments []Payment
}
