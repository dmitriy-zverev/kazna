package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dmitriy-zverev/kazna/internal/auth"
	"github.com/dmitriy-zverev/kazna/internal/http/dto"
	"github.com/dmitriy-zverev/kazna/internal/http/respond"
	"github.com/dmitriy-zverev/kazna/internal/payments"
	"github.com/dmitriy-zverev/kazna/internal/providers"
)

// Payments serves the application-facing payment API.
type Payments struct {
	svc *payments.Service
}

// NewPayments builds the payment handler set.
func NewPayments(svc *payments.Service) *Payments {
	return &Payments{svc: svc}
}

// Register mounts the payment routes. The caller applies API key auth.
func (h *Payments) Register(r chi.Router) {
	r.Post("/payments", h.create)
	r.Get("/payments", h.list)
	r.Get("/payments/{payment_id}", h.get)
	r.Post("/orders/{order_id}/payment-attempts", h.createAttempt)
}

func (h *Payments) create(w http.ResponseWriter, r *http.Request) {
	authenticated, ok := auth.From(r.Context())
	if !ok {
		respond.Error(w, r, http.StatusUnauthorized, respond.CodeUnauthorized, "not authenticated", false)
		return
	}

	var req dto.CreatePaymentRequest
	if !respond.DecodeJSON(w, r, &req) {
		return
	}

	result, err := h.svc.CreatePayment(r.Context(), authenticated.Application, r.Header.Get("Idempotency-Key"),
		payments.CreatePaymentInput{
			OrderID:     req.OrderID,
			AmountMinor: req.Amount,
			Currency:    req.Currency,
			Description: req.Description,
			ReturnURL:   req.ReturnURL,
			Metadata:    req.Metadata,
		})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeCreateResult(w, r, result)
}

func (h *Payments) createAttempt(w http.ResponseWriter, r *http.Request) {
	authenticated, ok := auth.From(r.Context())
	if !ok {
		respond.Error(w, r, http.StatusUnauthorized, respond.CodeUnauthorized, "not authenticated", false)
		return
	}

	orderID := chi.URLParam(r, "order_id")

	var req dto.CreatePaymentAttemptRequest
	if !respond.DecodeJSON(w, r, &req) {
		return
	}

	result, err := h.svc.CreateAttempt(r.Context(), authenticated.Application, orderID,
		r.Header.Get("Idempotency-Key"), payments.CreatePaymentInput{
			AmountMinor: req.Amount,
			Currency:    req.Currency,
			Description: req.Description,
			ReturnURL:   req.ReturnURL,
			Metadata:    req.Metadata,
		})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeCreateResult(w, r, result)
}

func (h *Payments) get(w http.ResponseWriter, r *http.Request) {
	authenticated, ok := auth.From(r.Context())
	if !ok {
		respond.Error(w, r, http.StatusUnauthorized, respond.CodeUnauthorized, "not authenticated", false)
		return
	}

	paymentID, ok := paymentIDParam(w, r)
	if !ok {
		return
	}

	payment, err := h.svc.GetPayment(r.Context(), authenticated.Application.ID, paymentID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, dto.NewPayment(payment))
}

func (h *Payments) list(w http.ResponseWriter, r *http.Request) {
	authenticated, ok := auth.From(r.Context())
	if !ok {
		respond.Error(w, r, http.StatusUnauthorized, respond.CodeUnauthorized, "not authenticated", false)
		return
	}

	var limit int32
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := parsePositiveInt(raw)
		if err != nil {
			respond.Error(w, r, http.StatusBadRequest, respond.CodeInvalidRequest, "limit must be a positive integer", false)
			return
		}
		limit = parsed
	}

	result, err := h.svc.ListPayments(r.Context(), authenticated.Application.ID, payments.ListQuery{
		OrderID: r.URL.Query().Get("order_id"),
		Limit:   limit,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, dto.List[dto.Payment]{Data: dto.NewPayments(result.Payments)})
}

func writeCreateResult(w http.ResponseWriter, r *http.Request, result payments.CreateResult) {
	if result.Pending {
		respond.JSON(w, http.StatusAccepted, dto.NewPaymentPending(result))
		return
	}
	respond.JSON(w, http.StatusCreated, dto.NewPayment(result.Payment))
}

// fail maps payment domain and provider errors to the normalized envelope.
func (h *Payments) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, payments.ErrValidation):
		respond.Error(w, r, http.StatusBadRequest, respond.CodeInvalidRequest, publicMessage(err), false)
	case errors.Is(err, payments.ErrNotFound):
		respond.Error(w, r, http.StatusNotFound, respond.CodePaymentNotFound, "payment not found", false)
	case errors.Is(err, payments.ErrIdempotencyConflict):
		respond.Error(w, r, http.StatusConflict, respond.CodeIdempotencyConflict,
			"this Idempotency-Key was used with a different request", false)
	case errors.Is(err, payments.ErrActiveAttemptExists):
		respond.Error(w, r, http.StatusConflict, respond.CodeConflict,
			"an unfinished payment attempt already exists for this order", false)
	case errors.Is(err, payments.ErrOrderAlreadySucceeded):
		respond.Error(w, r, http.StatusConflict, respond.CodePaymentAlreadySucceeded,
			"this order has already been paid", false)
	case errors.Is(err, payments.ErrNoProviderAccount):
		respond.Error(w, r, http.StatusUnprocessableEntity, respond.CodeProviderNotConfigured,
			"no active provider account is configured for this application", false)
	case errors.Is(err, payments.ErrIllegalTransition):
		respond.Error(w, r, http.StatusConflict, respond.CodeConflict, publicMessage(err), false)
	default:
		if provErr, ok := providers.AsError(err); ok {
			status, code := providerHTTPStatus(provErr)
			respond.Error(w, r, status, code, provErr.Message, provErr.Retryable)
			return
		}
		slog.ErrorContext(r.Context(), "payments: unhandled error",
			slog.Any("error", err),
			slog.String("request_id", respond.RequestID(r)),
		)
		respond.Error(w, r, http.StatusInternalServerError, respond.CodeInternalError, "internal error", true)
	}
}

// providerHTTPStatus maps a normalized provider failure to HTTP.
func providerHTTPStatus(provErr *providers.Error) (int, string) {
	switch provErr.Code {
	case providers.CodeInvalidRequest:
		return http.StatusBadRequest, respond.CodeInvalidRequest
	case providers.CodeAuthFailed:
		return http.StatusBadGateway, respond.CodeProviderAuthFailed
	case providers.CodeUnavailable:
		return http.StatusServiceUnavailable, respond.CodeProviderUnavailable
	case providers.CodeTimeout:
		return http.StatusGatewayTimeout, respond.CodeProviderTimeout
	default:
		return http.StatusBadGateway, respond.CodePaymentStateUnknown
	}
}

func paymentIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "payment_id"))
	if err != nil {
		respond.Error(w, r, http.StatusBadRequest, respond.CodeInvalidRequest, "invalid payment_id", false)
		return uuid.Nil, false
	}
	return id, true
}

func parsePositiveInt(raw string) (int32, error) {
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, errors.New("must be positive")
	}
	return int32(value), nil
}
