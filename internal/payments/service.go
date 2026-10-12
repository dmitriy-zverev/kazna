package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/database"
	"github.com/dmitriy-zverev/kazna/internal/database/sqlcgen"
	"github.com/dmitriy-zverev/kazna/internal/providers"
	"github.com/dmitriy-zverev/kazna/internal/security"
)

// errIdempotencyRace means a concurrent request claimed the same idempotency
// key first. The caller re-reads and replays instead of creating a payment.
var errIdempotencyRace = errors.New("payments: idempotency key claimed concurrently")

// CredentialsProvider decrypts provider account secrets. *applications.Service
// satisfies it.
type CredentialsProvider interface {
	ProviderCredentials(ctx context.Context, accountID uuid.UUID) (shopID, secret string, err error)
}

// Options tune the payment core.
type Options struct {
	// Production enables strict outbound URL validation.
	Production bool
	// ProviderIdempotencyWindow is how long a repeated provider key counts as
	// the same request. Recorded per operation so the NEEDS_REVIEW rule can be
	// applied later without a schema change.
	ProviderIdempotencyWindow time.Duration
	// ProviderTimeout bounds a synchronous provider call.
	ProviderTimeout time.Duration
	// PollBaseInterval is the initial delay before the first status check.
	PollBaseInterval time.Duration
	// Recorder receives metrics. Optional.
	Recorder Recorder
}

// Service owns the payment lifecycle. HTTP handlers must call this instead of
// touching the database or a provider directly.
type Service struct {
	pool        *pgxpool.Pool
	q           *sqlcgen.Queries
	credentials CredentialsProvider
	clients     providers.ClientFactory
	opts        Options
	urls        security.OutboundURLOptions
	recorder    Recorder
	now         func() time.Time
}

// NewService wires the payment core.
func NewService(pool *pgxpool.Pool, credentials CredentialsProvider, clients providers.ClientFactory, opts Options) *Service {
	return &Service{
		pool:        pool,
		q:           sqlcgen.New(pool),
		credentials: credentials,
		clients:     clients,
		opts:        opts,
		urls: security.OutboundURLOptions{
			AllowHTTP:    !opts.Production,
			AllowPrivate: !opts.Production,
		},
		recorder: opts.Recorder,
		now:      time.Now,
	}
}

// CreatePayment creates the first payment attempt for an order.
func (s *Service) CreatePayment(ctx context.Context, app applications.Application, idempotencyKey string, in CreatePaymentInput) (CreateResult, error) {
	return s.create(ctx, app, in, idempotencyKey)
}

// CreateAttempt creates a further attempt for an order. It is only allowed once
// every previous attempt is terminal (docs/PAYMENT_LIFECYCLE.md §3).
func (s *Service) CreateAttempt(ctx context.Context, app applications.Application, orderID, idempotencyKey string, in CreatePaymentInput) (CreateResult, error) {
	in.OrderID = orderID
	return s.create(ctx, app, in, idempotencyKey)
}

func (s *Service) create(ctx context.Context, app applications.Application, in CreatePaymentInput, idempotencyKey string) (CreateResult, error) {
	if err := s.validate(app, idempotencyKey, &in); err != nil {
		return CreateResult{}, err
	}

	hash, err := requestHash(in)
	if err != nil {
		return CreateResult{}, err
	}

	// Step 3 of the create algorithm: idempotency.
	existing, err := s.q.GetIdempotencyRequest(ctx, sqlcgen.GetIdempotencyRequestParams{
		ApplicationID:  database.ToUUID(app.ID),
		IdempotencyKey: idempotencyKey,
	})
	switch {
	case err == nil:
		return s.replay(ctx, app.ID, existing, hash)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return CreateResult{}, err
	}

	if paid, err := s.q.OrderHasSucceededPayment(ctx, sqlcgen.OrderHasSucceededPaymentParams{
		ApplicationID: database.ToUUID(app.ID),
		OrderID:       in.OrderID,
	}); err != nil {
		return CreateResult{}, err
	} else if paid {
		return CreateResult{}, ErrOrderAlreadySucceeded
	}

	account, err := s.q.GetDefaultProviderAccountForApplication(ctx, database.ToUUID(app.ID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CreateResult{}, ErrNoProviderAccount
		}
		return CreateResult{}, err
	}

	// Step 4: local transaction. The payment, the operation record carrying the
	// provider idempotency key, and the request record all commit together —
	// before any external call.
	local, err := s.createLocal(ctx, app, account, idempotencyKey, hash, in)
	if errors.Is(err, errIdempotencyRace) {
		raced, getErr := s.q.GetIdempotencyRequest(ctx, sqlcgen.GetIdempotencyRequestParams{
			ApplicationID:  database.ToUUID(app.ID),
			IdempotencyKey: idempotencyKey,
		})
		if getErr != nil {
			return CreateResult{}, err
		}
		return s.replay(ctx, app.ID, raced, hash)
	}
	if err != nil {
		return CreateResult{}, err
	}

	// Steps 5-7: call the provider outside any transaction, then persist.
	return s.callProvider(ctx, local, in)
}

// replay returns the current state of the payment a repeated key refers to.
// §6.2: the *current* result, not a stored snapshot.
func (s *Service) replay(ctx context.Context, appID uuid.UUID, row sqlcgen.IdempotencyRequest, hash []byte) (CreateResult, error) {
	if !bytes.Equal(row.RequestHash, hash) {
		return CreateResult{}, ErrIdempotencyConflict
	}
	if !row.PaymentID.Valid {
		return CreateResult{}, ErrNotFound
	}

	payment, err := s.getPaymentRow(ctx, appID, row.PaymentID)
	if err != nil {
		return CreateResult{}, err
	}
	s.recordCreation(OutcomeReplayed)
	return CreateResult{
		Payment:  payment,
		Replayed: true,
		// A payment with no checkout URL yet is not actionable for the client:
		// it is still being created, or the provider call has not happened.
		// Reporting 201 there would hand back a hollow success.
		Pending:           !payment.Status.Terminal() && payment.CheckoutURL == nil,
		RetryAfterSeconds: 2,
	}, nil
}

type localCreation struct {
	payment     Payment
	operationID uuid.UUID
	providerKey string
}

func (s *Service) createLocal(
	ctx context.Context,
	app applications.Application,
	account sqlcgen.ProviderAccount,
	idempotencyKey string,
	hash []byte,
	in CreatePaymentInput,
) (localCreation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return localCreation{}, fmt.Errorf("payments: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)

	// Serialize creation per (application, order). Without this, the
	// "already paid" check below and the insert could interleave with a
	// concurrent success: the partial unique index stops two *active* attempts,
	// but a payment that becomes terminal in that window would no longer be
	// covered by it, and a second attempt for a paid order could be created.
	if err := q.LockOrderForPayment(ctx, sqlcgen.LockOrderForPaymentParams{
		ApplicationID: app.ID.String(),
		OrderID:       in.OrderID,
	}); err != nil {
		return localCreation{}, err
	}

	// Authoritative check, inside the lock.
	if paid, err := q.OrderHasSucceededPayment(ctx, sqlcgen.OrderHasSucceededPaymentParams{
		ApplicationID: database.ToUUID(app.ID),
		OrderID:       in.OrderID,
	}); err != nil {
		return localCreation{}, err
	} else if paid {
		return localCreation{}, ErrOrderAlreadySucceeded
	}

	// Re-check idempotency now that the order is locked. A concurrent request
	// with the same key that won the race has already committed its record, and
	// the caller must replay it rather than be told about a conflict.
	if _, err := q.GetIdempotencyRequest(ctx, sqlcgen.GetIdempotencyRequestParams{
		ApplicationID:  database.ToUUID(app.ID),
		IdempotencyKey: idempotencyKey,
	}); err == nil {
		return localCreation{}, errIdempotencyRace
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return localCreation{}, err
	}

	attempt, err := q.NextAttemptNumber(ctx, sqlcgen.NextAttemptNumberParams{
		ApplicationID: database.ToUUID(app.ID),
		OrderID:       in.OrderID,
	})
	if err != nil {
		return localCreation{}, err
	}

	metadata, err := marshalMetadata(in.Metadata)
	if err != nil {
		return localCreation{}, err
	}

	paymentRow, err := q.CreatePayment(ctx, sqlcgen.CreatePaymentParams{
		ApplicationID:     database.ToUUID(app.ID),
		ProviderAccountID: account.ID,
		OrderID:           in.OrderID,
		AttemptNumber:     attempt,
		Provider:          string(providers.ProviderYooKassa),
		AmountMinor:       in.AmountMinor,
		Currency:          in.Currency,
		Description:       in.Description,
		Status:            string(StatusCreated),
		ReturnUrl:         in.ReturnURL,
		Metadata:          metadata,
	})
	if err != nil {
		// Both unique indexes here mean "another attempt is already active".
		if isUniqueViolation(err) {
			return localCreation{}, ErrActiveAttemptExists
		}
		return localCreation{}, err
	}

	payment, err := toPayment(paymentRow)
	if err != nil {
		return localCreation{}, err
	}

	// The provider key is generated and stored BEFORE the call, so a lost
	// response can be retried with the same key and body (docs/RELIABILITY.md §2).
	providerKey := uuid.NewString()
	requestBody, err := json.Marshal(providerRequestBody(in))
	if err != nil {
		return localCreation{}, fmt.Errorf("payments: encode operation body: %w", err)
	}

	operationRow, err := q.CreatePaymentOperation(ctx, sqlcgen.CreatePaymentOperationParams{
		PaymentID:              paymentRow.ID,
		OperationType:          string(OperationCreatePayment),
		ProviderIdempotencyKey: providerKey,
		RequestHash:            hash,
		RequestBody:            requestBody,
		IdempotencyExpiresAt:   database.ToTimestamp(s.now().Add(s.opts.ProviderIdempotencyWindow)),
	})
	if err != nil {
		return localCreation{}, err
	}
	operationID, err := database.FromUUID(operationRow.ID)
	if err != nil {
		return localCreation{}, err
	}

	if _, err := q.CreateIdempotencyRequest(ctx, sqlcgen.CreateIdempotencyRequestParams{
		ApplicationID:  database.ToUUID(app.ID),
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		PaymentID:      paymentRow.ID,
	}); err != nil {
		if isUniqueViolation(err) {
			return localCreation{}, errIdempotencyRace
		}
		return localCreation{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return localCreation{}, fmt.Errorf("payments: commit: %w", err)
	}

	return localCreation{payment: payment, operationID: operationID, providerKey: providerKey}, nil
}

// callProvider performs step 5-6 of the algorithm. The call happens outside any
// database transaction, and the result is persisted in a short one afterwards.
func (s *Service) callProvider(ctx context.Context, local localCreation, in CreatePaymentInput) (CreateResult, error) {
	shopID, secret, err := s.credentials.ProviderCredentials(ctx, local.payment.ProviderAccountID)
	if err != nil {
		return CreateResult{}, err
	}

	if err := ValidateTransition(local.payment.Status, StatusCreating); err != nil {
		return CreateResult{}, err
	}
	if err := s.q.MarkPaymentCreating(ctx, database.ToUUID(local.payment.ID)); err != nil {
		return CreateResult{}, err
	}
	if err := s.q.StartPaymentOperation(ctx, database.ToUUID(local.operationID)); err != nil {
		return CreateResult{}, err
	}

	request := providers.CreatePaymentRequest{
		AmountMinor:    in.AmountMinor,
		Currency:       in.Currency,
		Description:    in.Description,
		ReturnURL:      derefString(in.ReturnURL),
		Metadata:       providerMetadata(local.payment),
		IdempotencyKey: local.providerKey,
	}

	callCtx, cancel := context.WithTimeout(ctx, s.opts.ProviderTimeout)
	defer cancel()

	started := time.Now()
	result, callErr := s.clients(shopID, secret).CreatePayment(callCtx, request)
	s.recordProviderCall(string(OperationCreatePayment), callErr, started)
	if callErr != nil {
		return s.handleCreateFailure(ctx, local, callErr)
	}
	s.recordCreation(OutcomeCreated)

	nextCheck := s.now().Add(s.opts.PollBaseInterval)
	if err := s.q.SetPaymentCreated(ctx, sqlcgen.SetPaymentCreatedParams{
		ID:                database.ToUUID(local.payment.ID),
		ProviderPaymentID: &result.ProviderPaymentID,
		CheckoutUrl:       &result.CheckoutURL,
		NextCheckAt:       database.ToTimestamp(nextCheck),
	}); err != nil {
		return CreateResult{}, err
	}
	s.completeOperation(ctx, local.operationID, OperationSucceeded, "")

	payment, err := s.getPaymentRow(ctx, local.payment.ApplicationID, database.ToUUID(local.payment.ID))
	if err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Payment: payment}, nil
}

// handleCreateFailure distinguishes a refusal from an unknown outcome. A
// refusal settles the payment; an unknown outcome must not (docs/RELIABILITY.md §4).
func (s *Service) handleCreateFailure(ctx context.Context, local localCreation, callErr error) (CreateResult, error) {
	code := providers.CodeOf(callErr)

	if !providers.Definitive(code) {
		nextCheck := s.now().Add(s.opts.PollBaseInterval)
		if err := s.q.SetPaymentCreationUnknown(ctx, sqlcgen.SetPaymentCreationUnknownParams{
			ID:          database.ToUUID(local.payment.ID),
			NextCheckAt: database.ToTimestamp(nextCheck),
		}); err != nil {
			return CreateResult{}, err
		}
		s.completeOperation(ctx, local.operationID, OperationUnknown, callErr.Error())
		s.recordCreation(OutcomeCreationUnknown)

		payment, err := s.getPaymentRow(ctx, local.payment.ApplicationID, database.ToUUID(local.payment.ID))
		if err != nil {
			return CreateResult{}, err
		}
		// The caller gets 202: the outcome is not known yet, and a new payment
		// must not be created with a different key.
		return CreateResult{Payment: payment, Pending: true, RetryAfterSeconds: 2}, nil
	}

	if err := s.q.SetPaymentCanceled(ctx, database.ToUUID(local.payment.ID)); err != nil {
		return CreateResult{}, err
	}
	s.completeOperation(ctx, local.operationID, OperationFailed, callErr.Error())
	s.recordCreation(OutcomeRejected)
	return CreateResult{}, callErr
}

func (s *Service) completeOperation(ctx context.Context, operationID uuid.UUID, status OperationStatus, lastError string) {
	if err := s.q.CompletePaymentOperation(ctx, sqlcgen.CompletePaymentOperationParams{
		ID:        database.ToUUID(operationID),
		Status:    string(status),
		LastError: &lastError,
	}); err != nil {
		slog.ErrorContext(ctx, "payments: cannot record operation result",
			slog.String("operation_id", operationID.String()),
			slog.Any("error", err),
		)
	}
}

// GetPayment returns a payment, refreshing it from the provider when it is
// unfinished and due for a check. The refresh is best effort: a provider
// problem must not break a status read.
func (s *Service) GetPayment(ctx context.Context, appID, paymentID uuid.UUID) (Payment, error) {
	payment, err := s.getPaymentRow(ctx, appID, database.ToUUID(paymentID))
	if err != nil {
		return Payment{}, err
	}
	return s.maybeRefresh(ctx, payment), nil
}

func (s *Service) maybeRefresh(ctx context.Context, payment Payment) Payment {
	if payment.Status.Terminal() || payment.ProviderPaymentID == nil {
		return payment
	}
	if payment.NextCheckAt != nil && payment.NextCheckAt.After(s.now()) {
		return payment
	}

	refreshed, err := s.refresh(ctx, payment)
	if err != nil {
		slog.WarnContext(ctx, "payments: read-through refresh failed",
			slog.String("payment_id", payment.ID.String()),
			slog.Any("error", err),
		)
		return payment
	}
	return refreshed
}

func (s *Service) refresh(ctx context.Context, payment Payment) (Payment, error) {
	shopID, secret, err := s.credentials.ProviderCredentials(ctx, payment.ProviderAccountID)
	if err != nil {
		return payment, err
	}

	callCtx, cancel := context.WithTimeout(ctx, s.opts.ProviderTimeout)
	defer cancel()

	started := time.Now()
	result, err := s.clients(shopID, secret).GetPayment(callCtx, *payment.ProviderPaymentID)
	s.recordProviderCall(string(OperationGetPayment), err, started)
	if err != nil {
		return payment, err
	}
	return s.applyProviderState(ctx, payment, result)
}

// applyProviderState moves a payment to the state the provider reports, but only
// along a legal transition. A late or repeated read therefore cannot roll a
// succeeded payment back (docs/PAYMENT_LIFECYCLE.md §2).
func (s *Service) applyProviderState(ctx context.Context, payment Payment, result providers.Payment) (Payment, error) {
	next := internalStatus(result.Status)
	if next == "" || !CanTransition(payment.Status, next) {
		return payment, nil
	}

	nextCheck := s.now().Add(s.opts.PollBaseInterval)
	if err := s.q.ApplyProviderStatus(ctx, sqlcgen.ApplyProviderStatusParams{
		ID:          database.ToUUID(payment.ID),
		Status:      string(next),
		NextCheckAt: database.ToTimestamp(nextCheck),
	}); err != nil {
		return payment, err
	}
	return s.getPaymentRow(ctx, payment.ApplicationID, database.ToUUID(payment.ID))
}

// ListPayments returns payments for one application, newest first.
func (s *Service) ListPayments(ctx context.Context, appID uuid.UUID, query ListQuery) (ListResult, error) {
	limit := query.Limit
	switch {
	case limit <= 0:
		limit = DefaultListLimit
	case limit > MaxListLimit:
		limit = MaxListLimit
	}

	var orderID *string
	if trimmed := strings.TrimSpace(query.OrderID); trimmed != "" {
		orderID = &trimmed
	}

	rows, err := s.q.ListPaymentsForApplication(ctx, sqlcgen.ListPaymentsForApplicationParams{
		ApplicationID: database.ToUUID(appID),
		OrderID:       orderID,
		Limit:         limit,
	})
	if err != nil {
		return ListResult{}, err
	}

	payments, err := toPayments(rows)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{Payments: payments}, nil
}

// UnfinishedCount reports how many payments are not in a terminal state.
func (s *Service) UnfinishedCount(ctx context.Context) (int64, error) {
	return s.q.CountUnfinishedPayments(ctx)
}

func (s *Service) getPaymentRow(ctx context.Context, appID uuid.UUID, paymentID pgtype.UUID) (Payment, error) {
	row, err := s.q.GetPaymentForApplication(ctx, sqlcgen.GetPaymentForApplicationParams{
		ID:            paymentID,
		ApplicationID: database.ToUUID(appID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Payment{}, ErrNotFound
		}
		return Payment{}, err
	}
	return toPayment(row)
}
