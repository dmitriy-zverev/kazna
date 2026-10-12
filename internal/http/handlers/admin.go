package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/http/dto"
	"github.com/dmitriy-zverev/kazna/internal/http/respond"
	"github.com/dmitriy-zverev/kazna/internal/providers/yookassa"
)

// Admin serves the administrative API (docs/TECHNICAL_PLAN.md §11). There is no
// UI yet, so these plain JSON methods are the only way to provision clients.
type Admin struct {
	apps            *applications.Service
	providerBaseURL string
}

// NewAdmin builds the admin handler set.
func NewAdmin(apps *applications.Service, providerBaseURL string) *Admin {
	return &Admin{apps: apps, providerBaseURL: providerBaseURL}
}

// Register mounts admin routes on r. The caller is responsible for applying
// admin authentication.
func (h *Admin) Register(r chi.Router) {
	r.Post("/applications", h.createApplication)
	r.Get("/applications", h.listApplications)
	r.Get("/applications/{application_id}", h.getApplication)

	r.Post("/applications/{application_id}/keys", h.createAPIKey)
	r.Get("/applications/{application_id}/keys", h.listAPIKeys)
	r.Delete("/applications/{application_id}/keys/{key_id}", h.revokeAPIKey)

	r.Post("/applications/{application_id}/webhook-secret", h.rotateWebhookSecret)

	r.Post("/applications/{application_id}/providers", h.attachProvider)
	r.Get("/applications/{application_id}/providers", h.listProviders)
	r.Post("/providers/{provider_id}/test", h.testProvider)
}

func (h *Admin) createApplication(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateApplicationRequest
	if !respond.DecodeJSON(w, r, &req) {
		return
	}

	app, provisioned, err := h.apps.CreateApplication(r.Context(), applications.CreateApplicationInput{
		Name:             req.Name,
		DefaultReturnURL: req.DefaultReturnURL,
		WebhookURL:       req.WebhookURL,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	respond.JSON(w, http.StatusCreated, dto.CreateApplicationResponse{
		Application:   dto.NewApplication(app),
		APIKey:        dto.NewIssuedKey(provisioned.APIKey),
		WebhookSecret: provisioned.WebhookSecret,
	})
}

func (h *Admin) listApplications(w http.ResponseWriter, r *http.Request) {
	apps, err := h.apps.ListApplications(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, dto.List[dto.Application]{Data: dto.NewApplications(apps)})
}

func (h *Admin) getApplication(w http.ResponseWriter, r *http.Request) {
	appID, ok := h.uuidParam(w, r, "application_id")
	if !ok {
		return
	}
	app, err := h.apps.GetApplication(r.Context(), appID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, dto.NewApplication(app))
}

func (h *Admin) createAPIKey(w http.ResponseWriter, r *http.Request) {
	appID, ok := h.uuidParam(w, r, "application_id")
	if !ok {
		return
	}

	var req dto.CreateAPIKeyRequest
	if !respond.DecodeJSON(w, r, &req) {
		return
	}

	key, err := h.apps.IssueAPIKey(r.Context(), appID, req.Name, req.ExpiresAt)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, dto.NewIssuedKey(key))
}

func (h *Admin) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	appID, ok := h.uuidParam(w, r, "application_id")
	if !ok {
		return
	}
	keys, err := h.apps.ListAPIKeys(r.Context(), appID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, dto.List[dto.APIKey]{Data: dto.NewAPIKeys(keys)})
}

func (h *Admin) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	appID, ok := h.uuidParam(w, r, "application_id")
	if !ok {
		return
	}
	keyID, ok := h.uuidParam(w, r, "key_id")
	if !ok {
		return
	}

	if err := h.apps.RevokeAPIKey(r.Context(), appID, keyID); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rotateWebhookSecret issues a new HMAC signing secret. The previous secret
// stops being valid for new deliveries as soon as this returns.
func (h *Admin) rotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	appID, ok := h.uuidParam(w, r, "application_id")
	if !ok {
		return
	}

	secret, err := h.apps.RotateWebhookSecret(r.Context(), appID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, dto.WebhookSecretResponse{WebhookSecret: secret})
}

func (h *Admin) attachProvider(w http.ResponseWriter, r *http.Request) {
	appID, ok := h.uuidParam(w, r, "application_id")
	if !ok {
		return
	}

	var req dto.AttachProviderRequest
	if !respond.DecodeJSON(w, r, &req) {
		return
	}

	account, err := h.apps.AttachProviderAccount(r.Context(), appID, applications.AttachProviderInput{
		Provider:    req.Provider,
		ShopID:      req.ShopID,
		SecretKey:   req.SecretKey,
		Environment: req.Environment,
		IsDefault:   req.IsDefault,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, dto.NewProviderAccount(account))
}

func (h *Admin) listProviders(w http.ResponseWriter, r *http.Request) {
	appID, ok := h.uuidParam(w, r, "application_id")
	if !ok {
		return
	}
	accounts, err := h.apps.ListProviderAccounts(r.Context(), appID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, dto.List[dto.ProviderAccount]{Data: dto.NewProviderAccounts(accounts)})
}

// testProvider runs a read-only provider request that never creates a payment
// (docs/TECHNICAL_PLAN.md §11.2) and records the resulting connection status.
func (h *Admin) testProvider(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.uuidParam(w, r, "provider_id")
	if !ok {
		return
	}

	shopID, secret, err := h.apps.ProviderCredentials(r.Context(), accountID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	client := yookassa.NewClient(h.providerBaseURL, shopID, secret)
	checkErr := client.CheckConnection(r.Context())

	status := applications.ProviderStatusActive
	result := dto.ProviderTestResponse{Status: "connected"}
	if checkErr != nil {
		status = applications.ProviderStatusNeedsAttention
		result = dto.ProviderTestResponse{Status: "failed", Reason: providerReason(checkErr)}
	}

	if err := h.apps.SetProviderStatus(r.Context(), accountID, status); err != nil {
		h.fail(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func providerReason(err error) string {
	if provErr, ok := yookassa.AsError(err); ok {
		return provErr.Code
	}
	return respond.CodeProviderUnavailable
}

func (h *Admin) uuidParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		respond.Error(w, r, http.StatusBadRequest, respond.CodeInvalidRequest, "invalid "+name, false)
		return uuid.Nil, false
	}
	return id, true
}

// fail maps domain errors to the normalized error envelope and logs the rest.
func (h *Admin) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, applications.ErrValidation):
		respond.Error(w, r, http.StatusBadRequest, respond.CodeInvalidRequest, publicMessage(err), false)
	case errors.Is(err, applications.ErrNotFound):
		respond.Error(w, r, http.StatusNotFound, respond.CodeNotFound, "resource not found", false)
	case errors.Is(err, applications.ErrConflict):
		respond.Error(w, r, http.StatusConflict, respond.CodeConflict, publicMessage(err), false)
	default:
		slog.ErrorContext(r.Context(), "admin: unhandled error",
			slog.Any("error", err),
			slog.String("request_id", respond.RequestID(r)),
		)
		respond.Error(w, r, http.StatusInternalServerError, respond.CodeInternalError, "internal error", true)
	}
}

// publicMessage strips the package-level sentinel so clients see only the detail.
func publicMessage(err error) string {
	msg := err.Error()
	for _, sentinel := range []error{applications.ErrValidation, applications.ErrConflict} {
		msg = strings.TrimPrefix(msg, sentinel.Error()+": ")
	}
	return msg
}
