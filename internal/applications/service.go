package applications

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitriy-zverev/kazna/internal/credentials"
	"github.com/dmitriy-zverev/kazna/internal/database"
	"github.com/dmitriy-zverev/kazna/internal/database/sqlcgen"
	"github.com/dmitriy-zverev/kazna/internal/security"
)

const maxSlugLen = 64

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Options tune environment-dependent behaviour.
type Options struct {
	// Production enables strict outbound URL validation (https only, no
	// internal hosts) per docs/SECURITY.md §4.
	Production bool
}

// Service owns application, API key and provider-connection business logic.
// HTTP handlers must call this instead of touching the database directly.
type Service struct {
	pool   *pgxpool.Pool
	q      *sqlcgen.Queries
	cipher *security.Cipher
	urls   security.OutboundURLOptions
}

// NewService wires the service to a pool, the secret cipher and its options.
func NewService(pool *pgxpool.Pool, cipher *security.Cipher, opts Options) *Service {
	return &Service{
		pool:   pool,
		q:      sqlcgen.New(pool),
		cipher: cipher,
		urls: security.OutboundURLOptions{
			AllowHTTP:    !opts.Production,
			AllowPrivate: !opts.Production,
		},
	}
}

// CreateApplication registers an application and issues its first API key and
// webhook signing secret in one transaction, so an application never exists
// without usable credentials.
func (s *Service) CreateApplication(ctx context.Context, in CreateApplicationInput) (Application, Provisioned, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 255 {
		return Application{}, Provisioned{}, fmt.Errorf("%w: name is required and must be at most 255 characters", ErrValidation)
	}
	in.DefaultReturnURL = normalizeOptional(in.DefaultReturnURL)
	in.WebhookURL = normalizeOptional(in.WebhookURL)
	if err := s.validateURL("default_return_url", in.DefaultReturnURL); err != nil {
		return Application{}, Provisioned{}, err
	}
	if err := s.validateURL("webhook_url", in.WebhookURL); err != nil {
		return Application{}, Provisioned{}, err
	}

	webhookSecret, err := generateWebhookSecret()
	if err != nil {
		return Application{}, Provisioned{}, err
	}
	encryptedSecret, err := s.cipher.Encrypt([]byte(webhookSecret))
	if err != nil {
		return Application{}, Provisioned{}, err
	}

	base := slugify(name)
	var lastErr error

	for attempt := 0; attempt < 5; attempt++ {
		slug := base
		if attempt > 0 {
			suffix, err := randomHex(3)
			if err != nil {
				return Application{}, Provisioned{}, err
			}
			slug = truncate(base, maxSlugLen-len(suffix)-1) + "-" + suffix
		}

		app, key, err := s.createApplicationTx(ctx, slug, name, in, encryptedSecret)
		switch {
		case err == nil:
			return app, Provisioned{APIKey: key, WebhookSecret: webhookSecret}, nil
		case isUniqueViolation(err):
			lastErr = err
			continue
		default:
			return Application{}, Provisioned{}, err
		}
	}

	// The cause is logged, never returned: it contains database identifiers.
	slog.ErrorContext(ctx, "applications: could not allocate a unique slug",
		slog.String("name", name),
		slog.Any("error", lastErr),
	)
	return Application{}, Provisioned{}, fmt.Errorf("%w: could not allocate a unique slug", ErrConflict)
}

func (s *Service) createApplicationTx(ctx context.Context, slug, name string, in CreateApplicationInput, encryptedSecret []byte) (Application, IssuedKey, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Application{}, IssuedKey{}, fmt.Errorf("applications: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit

	q := s.q.WithTx(tx)

	row, err := q.CreateApplication(ctx, sqlcgen.CreateApplicationParams{
		Name:                   name,
		Slug:                   slug,
		DefaultReturnUrl:       in.DefaultReturnURL,
		WebhookUrl:             in.WebhookURL,
		WebhookSecretEncrypted: encryptedSecret,
	})
	if err != nil {
		return Application{}, IssuedKey{}, err
	}

	app, err := toApplication(row)
	if err != nil {
		return Application{}, IssuedKey{}, err
	}

	key, err := s.issueKey(ctx, q, app.ID, "Default", nil)
	if err != nil {
		return Application{}, IssuedKey{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Application{}, IssuedKey{}, fmt.Errorf("applications: commit: %w", err)
	}
	return app, key, nil
}

// GetApplication returns one application by id.
func (s *Service) GetApplication(ctx context.Context, id uuid.UUID) (Application, error) {
	row, err := s.q.GetApplication(ctx, database.ToUUID(id))
	if err != nil {
		return Application{}, mapNotFound(err)
	}
	return toApplication(row)
}

// ListApplications returns every application, newest first.
func (s *Service) ListApplications(ctx context.Context) ([]Application, error) {
	rows, err := s.q.ListApplications(ctx)
	if err != nil {
		return nil, err
	}
	return toApplications(rows)
}

// IssueAPIKey mints an additional key for an existing application.
func (s *Service) IssueAPIKey(ctx context.Context, appID uuid.UUID, name string, expiresAt *time.Time) (IssuedKey, error) {
	if _, err := s.q.GetApplication(ctx, database.ToUUID(appID)); err != nil {
		return IssuedKey{}, mapNotFound(err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "API key"
	}
	return s.issueKey(ctx, s.q, appID, name, expiresAt)
}

func (s *Service) issueKey(ctx context.Context, q *sqlcgen.Queries, appID uuid.UUID, name string, expiresAt *time.Time) (IssuedKey, error) {
	generated, err := credentials.Generate()
	if err != nil {
		return IssuedKey{}, err
	}

	row, err := q.CreateAPIKey(ctx, sqlcgen.CreateAPIKeyParams{
		ApplicationID: database.ToUUID(appID),
		KeyPrefix:     generated.Prefix,
		KeyHash:       generated.Hash,
		Name:          name,
		Permissions:   []string{},
		ExpiresAt:     database.ToTimestampPtr(expiresAt),
	})
	if err != nil {
		return IssuedKey{}, err
	}

	info, err := toAPIKeyInfo(row)
	if err != nil {
		return IssuedKey{}, err
	}
	return IssuedKey{
		ID:        info.ID,
		Name:      info.Name,
		Prefix:    info.Prefix,
		FullKey:   generated.FullKey,
		ExpiresAt: info.ExpiresAt,
		CreatedAt: info.CreatedAt,
	}, nil
}

// ListAPIKeys returns key metadata for an application. Key material is never
// available after issuance.
func (s *Service) ListAPIKeys(ctx context.Context, appID uuid.UUID) ([]APIKeyInfo, error) {
	if _, err := s.q.GetApplication(ctx, database.ToUUID(appID)); err != nil {
		return nil, mapNotFound(err)
	}
	rows, err := s.q.ListAPIKeys(ctx, database.ToUUID(appID))
	if err != nil {
		return nil, err
	}
	return toAPIKeyInfos(rows)
}

// RevokeAPIKey revokes a key. Revoking an already revoked or unknown key is a
// not-found error, which keeps the operation idempotent from the caller's view.
func (s *Service) RevokeAPIKey(ctx context.Context, appID, keyID uuid.UUID) error {
	affected, err := s.q.RevokeAPIKey(ctx, sqlcgen.RevokeAPIKeyParams{
		ID:            database.ToUUID(keyID),
		ApplicationID: database.ToUUID(appID),
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Authenticate resolves a presented API key to its application. Every failure
// mode returns ErrUnauthorized so callers cannot probe which part was wrong.
func (s *Service) Authenticate(ctx context.Context, presented string) (Authenticated, error) {
	if !credentials.LooksValid(presented) {
		return Authenticated{}, ErrUnauthorized
	}

	row, err := s.q.GetAPIKeyByPrefix(ctx, credentials.PrefixOf(presented))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Authenticated{}, ErrUnauthorized
		}
		return Authenticated{}, err
	}

	if !credentials.Verify(row.KeyHash, presented) {
		return Authenticated{}, ErrUnauthorized
	}
	if row.RevokedAt.Valid {
		return Authenticated{}, ErrUnauthorized
	}
	if row.ExpiresAt.Valid && !row.ExpiresAt.Time.After(time.Now()) {
		return Authenticated{}, ErrUnauthorized
	}

	appRow, err := s.q.GetApplication(ctx, row.ApplicationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Authenticated{}, ErrUnauthorized
		}
		return Authenticated{}, err
	}
	app, err := toApplication(appRow)
	if err != nil {
		return Authenticated{}, err
	}
	if !app.Active() {
		return Authenticated{}, ErrUnauthorized
	}

	keyID, err := database.FromUUID(row.ID)
	if err != nil {
		return Authenticated{}, err
	}

	// Best effort, and rate-limited to one write per minute per key by the query.
	_ = s.q.TouchAPIKeyLastUsed(ctx, row.ID)

	return Authenticated{Application: app, KeyID: keyID}, nil
}

// AttachProviderAccount connects a provider store to an application. The secret
// is encrypted before it reaches the database. If the same (provider, shop_id,
// environment) already exists it is reused rather than duplicated, and its
// stored secret is left untouched.
func (s *Service) AttachProviderAccount(ctx context.Context, appID uuid.UUID, in AttachProviderInput) (ProviderAccount, error) {
	in.Provider = strings.TrimSpace(in.Provider)
	in.ShopID = strings.TrimSpace(in.ShopID)
	in.Environment = strings.TrimSpace(in.Environment)
	if err := validateProviderInput(in); err != nil {
		return ProviderAccount{}, err
	}
	if _, err := s.q.GetApplication(ctx, database.ToUUID(appID)); err != nil {
		return ProviderAccount{}, mapNotFound(err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProviderAccount{}, fmt.Errorf("applications: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)

	existing, err := q.ListProviderAccountsForApplication(ctx, database.ToUUID(appID))
	if err != nil {
		return ProviderAccount{}, err
	}

	row, err := q.GetProviderAccountByShop(ctx, sqlcgen.GetProviderAccountByShopParams{
		Provider:    in.Provider,
		ShopID:      in.ShopID,
		Environment: in.Environment,
	})
	switch {
	case err == nil:
		// The shop is already connected. Storing a different secret here would
		// be silently ignored, so refuse instead of leaving the caller misled.
		if err := s.assertSameSecret(row, in.SecretKey); err != nil {
			return ProviderAccount{}, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		encrypted, encErr := s.cipher.Encrypt([]byte(in.SecretKey))
		if encErr != nil {
			return ProviderAccount{}, encErr
		}
		row, err = q.CreateProviderAccount(ctx, sqlcgen.CreateProviderAccountParams{
			Provider:           in.Provider,
			ShopID:             in.ShopID,
			EncryptedSecretKey: encrypted,
			Environment:        in.Environment,
		})
		if err != nil {
			return ProviderAccount{}, err
		}
	default:
		return ProviderAccount{}, err
	}

	// Choose the default flag so the application is never left without one:
	//   explicitly requested -> true
	//   already attached     -> keep its current flag
	//   first provider       -> true
	//   otherwise            -> false
	currentlyDefault := false
	for _, item := range existing {
		if item.ID == row.ID {
			currentlyDefault = item.IsDefault
			break
		}
	}

	makeDefault := in.IsDefault || currentlyDefault || len(existing) == 0
	if makeDefault {
		if err := q.ClearDefaultProviderForApplication(ctx, database.ToUUID(appID)); err != nil {
			return ProviderAccount{}, err
		}
	}

	if err := q.AttachProviderAccount(ctx, sqlcgen.AttachProviderAccountParams{
		ApplicationID:     database.ToUUID(appID),
		ProviderAccountID: row.ID,
		IsDefault:         makeDefault,
	}); err != nil {
		return ProviderAccount{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ProviderAccount{}, fmt.Errorf("applications: commit: %w", err)
	}
	return toProviderAccount(row, makeDefault)
}

// ListProviderAccounts returns the provider connections of an application.
func (s *Service) ListProviderAccounts(ctx context.Context, appID uuid.UUID) ([]ProviderAccount, error) {
	if _, err := s.q.GetApplication(ctx, database.ToUUID(appID)); err != nil {
		return nil, mapNotFound(err)
	}
	rows, err := s.q.ListProviderAccountsForApplication(ctx, database.ToUUID(appID))
	if err != nil {
		return nil, err
	}
	return toProviderAccounts(rows)
}

// ProviderCredentials returns the shop id and decrypted secret for a provider
// account. The plaintext is used only for outbound provider calls.
func (s *Service) ProviderCredentials(ctx context.Context, accountID uuid.UUID) (shopID, secret string, err error) {
	row, err := s.q.GetProviderAccount(ctx, database.ToUUID(accountID))
	if err != nil {
		return "", "", mapNotFound(err)
	}
	plaintext, err := s.cipher.Decrypt(row.EncryptedSecretKey)
	if err != nil {
		return "", "", err
	}
	return row.ShopID, string(plaintext), nil
}

// SetProviderStatus records the outcome of a connectivity check.
func (s *Service) SetProviderStatus(ctx context.Context, accountID uuid.UUID, status string) error {
	return s.q.UpdateProviderAccountStatus(ctx, sqlcgen.UpdateProviderAccountStatusParams{
		ID:     database.ToUUID(accountID),
		Status: status,
	})
}

// RotateWebhookSecret issues a new HMAC signing secret and returns it once.
func (s *Service) RotateWebhookSecret(ctx context.Context, appID uuid.UUID) (string, error) {
	if _, err := s.q.GetApplication(ctx, database.ToUUID(appID)); err != nil {
		return "", mapNotFound(err)
	}

	secret, err := generateWebhookSecret()
	if err != nil {
		return "", err
	}
	encrypted, err := s.cipher.Encrypt([]byte(secret))
	if err != nil {
		return "", err
	}
	if err := s.q.UpdateApplicationWebhookSecret(ctx, sqlcgen.UpdateApplicationWebhookSecretParams{
		ID:                     database.ToUUID(appID),
		WebhookSecretEncrypted: encrypted,
	}); err != nil {
		return "", err
	}
	return secret, nil
}

// assertSameSecret refuses to reuse a provider account with a different secret,
// because the stored secret would otherwise be silently ignored.
func (s *Service) assertSameSecret(row sqlcgen.ProviderAccount, presented string) error {
	stored, err := s.cipher.Decrypt(row.EncryptedSecretKey)
	if err != nil {
		return fmt.Errorf("applications: cannot read the stored provider secret: %w", err)
	}
	if subtle.ConstantTimeCompare(stored, []byte(presented)) != 1 {
		return fmt.Errorf("%w: shop %s is already connected with a different secret", ErrConflict, row.ShopID)
	}
	return nil
}

// validateURL applies the environment's outbound URL policy to an optional field.
func (s *Service) validateURL(field string, raw *string) error {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	if err := security.ValidateOutboundURL(*raw, s.urls); err != nil {
		reason := strings.TrimPrefix(err.Error(), security.ErrInvalidURL.Error()+": ")
		return fmt.Errorf("%w: %s: %s", ErrValidation, field, reason)
	}
	return nil
}

func generateWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("applications: webhook secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// normalizeOptional turns an empty or whitespace-only optional string into nil,
// so "not provided" and "provided empty" are stored identically.
func normalizeOptional(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func validateProviderInput(in AttachProviderInput) error {
	if in.Provider != ProviderYooKassa {
		return fmt.Errorf("%w: unsupported provider %q", ErrValidation, in.Provider)
	}
	if in.ShopID == "" || len(in.ShopID) > 64 {
		return fmt.Errorf("%w: shop_id is required and must be at most 64 characters", ErrValidation)
	}
	if in.SecretKey == "" || len(in.SecretKey) > 512 {
		return fmt.Errorf("%w: secret_key is required and must be at most 512 characters", ErrValidation)
	}
	if in.Environment != EnvironmentTest && in.Environment != EnvironmentProduction {
		return fmt.Errorf("%w: environment must be %q or %q", ErrValidation, EnvironmentTest, EnvironmentProduction)
	}
	return nil
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func slugify(name string) string {
	slug := nonSlugChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "app"
	}
	return truncate(slug, maxSlugLen)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return strings.Trim(s, "-")
	}
	return strings.Trim(s[:n], "-")
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("applications: random suffix: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
