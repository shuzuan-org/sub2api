package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var errOAuthClientExists = infraerrors.Conflict("OAUTH_CLIENT_EXISTS", "oauth client already exists")

type oauthClientRepository struct {
	sql sqlExecutor
}

func NewOAuthClientRepository(_ *dbent.Client, sqlDB *sql.DB) service.OAuthClientRepository {
	return &oauthClientRepository{sql: sqlDB}
}

func (r *oauthClientRepository) Create(ctx context.Context, client *service.OAuthClient) error {
	if client == nil {
		return nil
	}
	redirectURIs, err := json.Marshal(client.RedirectURIs)
	if err != nil {
		return err
	}
	scopes, err := json.Marshal(client.Scopes)
	if err != nil {
		return err
	}
	err = scanSingleRow(ctx, r.sql, `
INSERT INTO oauth_clients (client_id, client_secret_hash, name, redirect_uris, scopes, status)
VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6)
RETURNING id, created_at, updated_at`,
		[]any{client.ClientID, client.ClientSecretHash, client.Name, string(redirectURIs), string(scopes), client.Status},
		&client.ID, &client.CreatedAt, &client.UpdatedAt,
	)
	if err != nil {
		return translatePersistenceError(err, nil, errOAuthClientExists)
	}
	return nil
}

func (r *oauthClientRepository) GetByClientID(ctx context.Context, clientID string) (*service.OAuthClient, error) {
	var out service.OAuthClient
	var redirectURIsRaw, scopesRaw []byte
	err := scanSingleRow(ctx, r.sql, `
SELECT id, client_id, client_secret_hash, name, redirect_uris, scopes, status, created_at, updated_at
FROM oauth_clients
WHERE client_id = $1 AND deleted_at IS NULL`, []any{clientID},
		&out.ID, &out.ClientID, &out.ClientSecretHash, &out.Name, &redirectURIsRaw, &scopesRaw, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrOAuthInvalidClient
		}
		return nil, err
	}
	if err := json.Unmarshal(redirectURIsRaw, &out.RedirectURIs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(scopesRaw, &out.Scopes); err != nil {
		return nil, err
	}
	return &out, nil
}

type oauthAuthorizationCodeRepository struct {
	sql sqlExecutor
}

func NewOAuthAuthorizationCodeRepository(_ *dbent.Client, sqlDB *sql.DB) service.OAuthAuthorizationCodeRepository {
	return &oauthAuthorizationCodeRepository{sql: sqlDB}
}

func (r *oauthAuthorizationCodeRepository) Create(ctx context.Context, code *service.OAuthAuthorizationCode) error {
	if code == nil {
		return nil
	}
	scopes, err := json.Marshal(code.Scopes)
	if err != nil {
		return err
	}
	err = scanSingleRow(ctx, r.sql, `
INSERT INTO oauth_authorization_codes (code_hash, client_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
RETURNING id, created_at, updated_at`,
		[]any{code.CodeHash, code.ClientID, code.UserID, code.RedirectURI, string(scopes), code.CodeChallenge, code.CodeChallengeMethod, code.ExpiresAt},
		&code.ID, &code.CreatedAt, &code.UpdatedAt,
	)
	return err
}

func (r *oauthAuthorizationCodeRepository) GetByCodeHash(ctx context.Context, codeHash string) (*service.OAuthAuthorizationCode, error) {
	var out service.OAuthAuthorizationCode
	var scopesRaw []byte
	err := scanSingleRow(ctx, r.sql, `
SELECT id, code_hash, client_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at, used_at, created_at, updated_at
FROM oauth_authorization_codes
WHERE code_hash = $1`, []any{codeHash},
		&out.ID, &out.CodeHash, &out.ClientID, &out.UserID, &out.RedirectURI, &scopesRaw, &out.CodeChallenge, &out.CodeChallengeMethod, &out.ExpiresAt, &out.UsedAt, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrOAuthInvalidCode
		}
		return nil, err
	}
	if err := json.Unmarshal(scopesRaw, &out.Scopes); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *oauthAuthorizationCodeRepository) MarkUsed(ctx context.Context, id int64, usedAt time.Time) error {
	res, err := r.sql.ExecContext(ctx, `
UPDATE oauth_authorization_codes
SET used_at = $2, updated_at = NOW()
WHERE id = $1 AND used_at IS NULL`, id, usedAt)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return service.ErrOAuthCodeUsed
	}
	return nil
}
