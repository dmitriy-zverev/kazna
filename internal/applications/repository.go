package applications

import (
	"github.com/dmitriy-zverev/kazna/internal/database"
	"github.com/dmitriy-zverev/kazna/internal/database/sqlcgen"
)

// Conversions between generated row types and domain types. Keeping them in one
// place means the domain never depends on pgx types.

func toApplication(row sqlcgen.Application) (Application, error) {
	id, err := database.FromUUID(row.ID)
	if err != nil {
		return Application{}, err
	}
	created, err := database.FromTimestamp(row.CreatedAt)
	if err != nil {
		return Application{}, err
	}
	updated, err := database.FromTimestamp(row.UpdatedAt)
	if err != nil {
		return Application{}, err
	}
	return Application{
		ID:               id,
		Name:             row.Name,
		Slug:             row.Slug,
		Status:           row.Status,
		DefaultReturnURL: row.DefaultReturnUrl,
		WebhookURL:       row.WebhookUrl,
		CreatedAt:        created,
		UpdatedAt:        updated,
	}, nil
}

func toApplications(rows []sqlcgen.Application) ([]Application, error) {
	out := make([]Application, 0, len(rows))
	for _, row := range rows {
		app, err := toApplication(row)
		if err != nil {
			return nil, err
		}
		out = append(out, app)
	}
	return out, nil
}

func toAPIKeyInfo(row sqlcgen.ApiKey) (APIKeyInfo, error) {
	id, err := database.FromUUID(row.ID)
	if err != nil {
		return APIKeyInfo{}, err
	}
	created, err := database.FromTimestamp(row.CreatedAt)
	if err != nil {
		return APIKeyInfo{}, err
	}
	return APIKeyInfo{
		ID:         id,
		Name:       row.Name,
		Prefix:     row.KeyPrefix,
		ExpiresAt:  database.TimestampPtr(row.ExpiresAt),
		LastUsedAt: database.TimestampPtr(row.LastUsedAt),
		RevokedAt:  database.TimestampPtr(row.RevokedAt),
		CreatedAt:  created,
	}, nil
}

func toAPIKeyInfos(rows []sqlcgen.ApiKey) ([]APIKeyInfo, error) {
	out := make([]APIKeyInfo, 0, len(rows))
	for _, row := range rows {
		info, err := toAPIKeyInfo(row)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

func toProviderAccount(row sqlcgen.ProviderAccount, isDefault bool) (ProviderAccount, error) {
	id, err := database.FromUUID(row.ID)
	if err != nil {
		return ProviderAccount{}, err
	}
	created, err := database.FromTimestamp(row.CreatedAt)
	if err != nil {
		return ProviderAccount{}, err
	}
	updated, err := database.FromTimestamp(row.UpdatedAt)
	if err != nil {
		return ProviderAccount{}, err
	}
	return ProviderAccount{
		ID:          id,
		Provider:    row.Provider,
		ShopID:      row.ShopID,
		Environment: row.Environment,
		Status:      row.Status,
		IsDefault:   isDefault,
		CreatedAt:   created,
		UpdatedAt:   updated,
	}, nil
}

func toProviderAccounts(rows []sqlcgen.ListProviderAccountsForApplicationRow) ([]ProviderAccount, error) {
	out := make([]ProviderAccount, 0, len(rows))
	for _, row := range rows {
		account, err := toProviderAccount(sqlcgen.ProviderAccount{
			ID:                 row.ID,
			Provider:           row.Provider,
			ShopID:             row.ShopID,
			EncryptedSecretKey: row.EncryptedSecretKey,
			Environment:        row.Environment,
			Status:             row.Status,
			CreatedAt:          row.CreatedAt,
			UpdatedAt:          row.UpdatedAt,
		}, row.IsDefault)
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, nil
}
