package yookassa

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dmitriy-zverev/kazna/internal/providers"
)

// API payloads. Only the fields the service actually uses are modeled.

type apiAmount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type apiConfirmation struct {
	Type            string `json:"type,omitempty"`
	ReturnURL       string `json:"return_url,omitempty"`
	ConfirmationURL string `json:"confirmation_url,omitempty"`
}

type apiPayment struct {
	ID           string            `json:"id"`
	Status       string            `json:"status"`
	Paid         bool              `json:"paid"`
	Amount       apiAmount         `json:"amount"`
	Description  string            `json:"description"`
	Confirmation *apiConfirmation  `json:"confirmation"`
	Metadata     map[string]string `json:"metadata"`
}

type createPaymentBody struct {
	Amount       apiAmount         `json:"amount"`
	Capture      bool              `json:"capture"`
	Confirmation apiConfirmation   `json:"confirmation"`
	Description  string            `json:"description,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// CreatePayment creates a one-stage payment with redirect confirmation.
//
// IdempotencyKey is sent as the provider Idempotence-Key header: repeating the
// call with the same key and an unchanged body is the same payment, never a
// second one (docs/PAYMENT_LIFECYCLE.md §4).
//
// No receipt is sent (decision recorded in docs/ARCHITECTURE.md §9): shops that
// require fiscalization will reject the call until receipt support lands.
func (c *Client) CreatePayment(ctx context.Context, req providers.CreatePaymentRequest) (providers.Payment, error) {
	body := createPaymentBody{
		Amount:  apiAmount{Value: formatAmountMinor(req.AmountMinor), Currency: req.Currency},
		Capture: true,
		Confirmation: apiConfirmation{
			Type:      "redirect",
			ReturnURL: req.ReturnURL,
		},
		Description: req.Description,
		Metadata:    req.Metadata,
	}

	var response apiPayment
	if _, err := c.do(ctx, http.MethodPost, "/v3/payments", req.IdempotencyKey, body, &response); err != nil {
		return providers.Payment{}, err
	}
	return c.toProviderPayment(response)
}

// GetPayment reads the current provider state of a payment.
func (c *Client) GetPayment(ctx context.Context, providerPaymentID string) (providers.Payment, error) {
	if providerPaymentID == "" {
		return providers.Payment{}, &providers.Error{
			Code:    providers.CodeInvalidRequest,
			Message: "provider payment id is required",
		}
	}

	var response apiPayment
	path := "/v3/payments/" + providerPaymentID
	if _, err := c.do(ctx, http.MethodGet, path, "", nil, &response); err != nil {
		return providers.Payment{}, err
	}
	return c.toProviderPayment(response)
}

func (c *Client) toProviderPayment(response apiPayment) (providers.Payment, error) {
	status, err := mapStatus(response.Status)
	if err != nil {
		return providers.Payment{}, err
	}

	amountMinor, err := parseAmountMinor(response.Amount.Value)
	if err != nil {
		return providers.Payment{}, &providers.Error{
			Code:       providers.CodeStateUnknown,
			Message:    err.Error(),
			HTTPStatus: http.StatusOK,
		}
	}

	checkoutURL := ""
	if response.Confirmation != nil {
		checkoutURL = response.Confirmation.ConfirmationURL
	}

	return providers.Payment{
		ProviderPaymentID: response.ID,
		Status:            status,
		CheckoutURL:       checkoutURL,
		Paid:              response.Paid,
		AmountMinor:       amountMinor,
		Currency:          response.Amount.Currency,
	}, nil
}

// mapStatus translates a provider status into our normalized vocabulary. An
// unrecognized status is an error rather than a silent guess: the state machine
// must not receive an invented value.
func mapStatus(status string) (providers.PaymentStatus, error) {
	switch status {
	case "pending", "waiting_for_capture":
		// waiting_for_capture cannot occur with capture:true, but mapping it to
		// pending is the safe interpretation if the shop is configured otherwise.
		return providers.StatusPending, nil
	case "succeeded":
		return providers.StatusSucceeded, nil
	case "canceled":
		return providers.StatusCanceled, nil
	default:
		return "", &providers.Error{
			Code:       providers.CodeStateUnknown,
			HTTPStatus: http.StatusOK,
			Message:    fmt.Sprintf("unrecognized provider status %q", status),
		}
	}
}
