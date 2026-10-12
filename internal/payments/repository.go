package payments

import (
	"encoding/json"
	"fmt"

	"github.com/dmitriy-zverev/kazna/internal/database"
	"github.com/dmitriy-zverev/kazna/internal/database/sqlcgen"
)

// Conversions between generated rows and domain types.

func toPayment(row sqlcgen.Payment) (Payment, error) {
	id, err := database.FromUUID(row.ID)
	if err != nil {
		return Payment{}, err
	}
	appID, err := database.FromUUID(row.ApplicationID)
	if err != nil {
		return Payment{}, err
	}
	accountID, err := database.FromUUID(row.ProviderAccountID)
	if err != nil {
		return Payment{}, err
	}
	created, err := database.FromTimestamp(row.CreatedAt)
	if err != nil {
		return Payment{}, err
	}
	updated, err := database.FromTimestamp(row.UpdatedAt)
	if err != nil {
		return Payment{}, err
	}

	metadata := map[string]string{}
	if len(row.Metadata) > 0 {
		if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
			return Payment{}, fmt.Errorf("payments: decode metadata: %w", err)
		}
	}

	return Payment{
		ID:                id,
		ApplicationID:     appID,
		ProviderAccountID: accountID,
		OrderID:           row.OrderID,
		AttemptNumber:     row.AttemptNumber,
		Provider:          row.Provider,
		ProviderPaymentID: row.ProviderPaymentID,
		AmountMinor:       row.AmountMinor,
		Currency:          row.Currency,
		Description:       row.Description,
		Status:            Status(row.Status),
		CheckoutURL:       row.CheckoutUrl,
		ReturnURL:         row.ReturnUrl,
		Metadata:          metadata,
		CreatedAt:         created,
		UpdatedAt:         updated,
		CompletedAt:       database.TimestampPtr(row.CompletedAt),
		NextCheckAt:       database.TimestampPtr(row.NextCheckAt),
	}, nil
}

func toPayments(rows []sqlcgen.Payment) ([]Payment, error) {
	out := make([]Payment, 0, len(rows))
	for _, row := range rows {
		payment, err := toPayment(row)
		if err != nil {
			return nil, err
		}
		out = append(out, payment)
	}
	return out, nil
}
