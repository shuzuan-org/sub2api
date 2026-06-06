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
	INSERT INTO oauth_clients (client_id, client_secret_hash, client_type, name, redirect_uris, allow_loopback_redirect, scopes, status)
	VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7::jsonb, $8)
	RETURNING id, created_at, updated_at`,
		[]any{client.ClientID, nullEmptyString(client.ClientSecretHash), client.ClientType, client.Name, string(redirectURIs), client.AllowLoopbackRedirect, string(scopes), client.Status},
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
	SELECT id, client_id, COALESCE(client_secret_hash, ''), COALESCE(client_type, 'confidential'), name, redirect_uris, scopes, status, created_at, updated_at, COALESCE(allow_loopback_redirect, false)
	FROM oauth_clients
	WHERE client_id = $1 AND deleted_at IS NULL`, []any{clientID},
		&out.ID, &out.ClientID, &out.ClientSecretHash, &out.ClientType, &out.Name, &redirectURIsRaw, &scopesRaw, &out.Status, &out.CreatedAt, &out.UpdatedAt, &out.AllowLoopbackRedirect)
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

type oauthRefreshTokenRepository struct {
	db *sql.DB
}

func NewOAuthRefreshTokenRepository(_ *dbent.Client, sqlDB *sql.DB) service.OAuthRefreshTokenRepository {
	return &oauthRefreshTokenRepository{db: sqlDB}
}

func (r *oauthRefreshTokenRepository) Create(ctx context.Context, token *service.OAuthRefreshToken) error {
	if token == nil {
		return nil
	}
	scopes, err := json.Marshal(token.Scopes)
	if err != nil {
		return err
	}
	err = scanSingleRow(ctx, r.db, `
	INSERT INTO oauth_refresh_tokens (token_hash, hmac_key_id, family_id, parent_token_hash, user_id, client_id, scopes, status, expires_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
	RETURNING id, created_at, updated_at`,
		[]any{token.TokenHash, token.HMACKeyID, token.FamilyID, token.ParentTokenHash, token.UserID, token.ClientID, string(scopes), token.Status, token.ExpiresAt},
		&token.ID, &token.CreatedAt, &token.UpdatedAt,
	)
	return err
}

func (r *oauthRefreshTokenRepository) Rotate(ctx context.Context, tokenHash, clientID string, next *service.OAuthRefreshToken, now time.Time) (*service.OAuthRefreshToken, error) {
	if next == nil {
		return nil, service.ErrOAuthInvalidToken
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	current, err := selectRefreshTokenForUpdate(ctx, tx, tokenHash, clientID)
	if err != nil {
		return nil, err
	}
	if current.Status == service.OAuthRefreshTokenStatusUsed {
		if _, err := tx.ExecContext(ctx, `
			UPDATE oauth_refresh_tokens
			SET status = $2, revoked_at = COALESCE(revoked_at, $3), updated_at = NOW()
			WHERE family_id = $1 AND status <> $2`,
			current.FamilyID, service.OAuthRefreshTokenStatusRevoked, now,
		); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, service.ErrOAuthInvalidToken
	}
	if current.Status != service.OAuthRefreshTokenStatusActive || current.RevokedAt != nil || !current.ExpiresAt.After(now) {
		return nil, service.ErrOAuthInvalidToken
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE oauth_refresh_tokens
		SET status = $2, used_at = $3, updated_at = NOW()
		WHERE id = $1`,
		current.ID, service.OAuthRefreshTokenStatusUsed, now,
	); err != nil {
		return nil, err
	}
	next.FamilyID = current.FamilyID
	next.UserID = current.UserID
	next.ClientID = current.ClientID
	next.Scopes = append([]string(nil), current.Scopes...)
	next.ParentTokenHash = &current.TokenHash
	scopes, err := json.Marshal(next.Scopes)
	if err != nil {
		return nil, err
	}
	if err := scanSingleRow(ctx, tx, `
		INSERT INTO oauth_refresh_tokens (token_hash, hmac_key_id, family_id, parent_token_hash, user_id, client_id, scopes, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
		RETURNING id, created_at, updated_at`,
		[]any{next.TokenHash, next.HMACKeyID, next.FamilyID, next.ParentTokenHash, next.UserID, next.ClientID, string(scopes), next.Status, next.ExpiresAt},
		&next.ID, &next.CreatedAt, &next.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return current, nil
}

func (r *oauthRefreshTokenRepository) RevokeByHash(ctx context.Context, tokenHash, clientID string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE oauth_refresh_tokens
		SET status = $3, revoked_at = COALESCE(revoked_at, $4), updated_at = NOW()
		WHERE token_hash = $1 AND client_id = $2`,
		tokenHash, clientID, service.OAuthRefreshTokenStatusRevoked, now,
	)
	return err
}

func selectRefreshTokenForUpdate(ctx context.Context, tx *sql.Tx, tokenHash, clientID string) (*service.OAuthRefreshToken, error) {
	var out service.OAuthRefreshToken
	var scopesRaw []byte
	err := scanSingleRow(ctx, tx, `
	SELECT id, token_hash, hmac_key_id, family_id, parent_token_hash, user_id, client_id, scopes, status, expires_at, used_at, revoked_at, created_at, updated_at
	FROM oauth_refresh_tokens
	WHERE token_hash = $1 AND client_id = $2
	FOR UPDATE`,
		[]any{tokenHash, clientID},
		&out.ID, &out.TokenHash, &out.HMACKeyID, &out.FamilyID, &out.ParentTokenHash, &out.UserID, &out.ClientID, &scopesRaw, &out.Status, &out.ExpiresAt, &out.UsedAt, &out.RevokedAt, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrOAuthInvalidToken
		}
		return nil, err
	}
	if err := json.Unmarshal(scopesRaw, &out.Scopes); err != nil {
		return nil, err
	}
	return &out, nil
}

func nullEmptyString(value string) any {
	if value == "" {
		return nil
	}
	return value
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
	INSERT INTO oauth_authorization_codes (code_hash, hmac_key_id, client_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at)
	VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
	RETURNING id, created_at, updated_at`,
		[]any{code.CodeHash, code.HMACKeyID, code.ClientID, code.UserID, code.RedirectURI, string(scopes), code.CodeChallenge, code.CodeChallengeMethod, code.ExpiresAt},
		&code.ID, &code.CreatedAt, &code.UpdatedAt,
	)
	return err
}

func (r *oauthAuthorizationCodeRepository) Consume(ctx context.Context, codeHash, clientID, redirectURI string, now time.Time) (*service.OAuthAuthorizationCode, error) {
	var out service.OAuthAuthorizationCode
	var scopesRaw []byte
	err := scanSingleRow(ctx, r.sql, `
	UPDATE oauth_authorization_codes
	SET used_at = $4, updated_at = NOW()
	WHERE code_hash = $1
	  AND client_id = $2
	  AND redirect_uri = $3
	  AND expires_at > $4
	  AND used_at IS NULL
	RETURNING id, code_hash, hmac_key_id, client_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at, used_at, created_at, updated_at`,
		[]any{codeHash, clientID, redirectURI, now},
		&out.ID, &out.CodeHash, &out.HMACKeyID, &out.ClientID, &out.UserID, &out.RedirectURI, &scopesRaw, &out.CodeChallenge, &out.CodeChallengeMethod, &out.ExpiresAt, &out.UsedAt, &out.CreatedAt, &out.UpdatedAt)
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
