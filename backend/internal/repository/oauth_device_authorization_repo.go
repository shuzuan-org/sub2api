package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type oauthDeviceAuthorizationRepository struct {
	db *sql.DB
}

func NewOAuthDeviceAuthorizationRepository(_ *dbent.Client, sqlDB *sql.DB) service.OAuthDeviceAuthorizationRepository {
	return &oauthDeviceAuthorizationRepository{db: sqlDB}
}

func (r *oauthDeviceAuthorizationRepository) Create(ctx context.Context, session *service.OAuthDeviceAuthorization) error {
	if session == nil {
		return nil
	}
	scopes, err := json.Marshal(session.Scopes)
	if err != nil {
		return err
	}
	intervalSeconds := int(session.Interval.Seconds())
	if intervalSeconds <= 0 {
		intervalSeconds = int(service.OAuthDeviceAuthorizationPoll.Seconds())
	}
	err = scanSingleRow(ctx, r.db, `
	INSERT INTO oauth_device_authorizations (device_code_hash, user_code_hash, client_id, scopes, status, expires_at, interval_seconds, device_name, cli_version, platform)
	VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10)
	RETURNING id, created_at, updated_at`,
		[]any{session.DeviceCodeHash, session.UserCodeHash, session.ClientID, string(scopes), session.Status, session.ExpiresAt, intervalSeconds, session.DeviceName, session.CLIVersion, session.Platform},
		&session.ID, &session.CreatedAt, &session.UpdatedAt,
	)
	return err
}

func (r *oauthDeviceAuthorizationRepository) GetByUserCodeHash(ctx context.Context, userCodeHash string) (*service.OAuthDeviceAuthorization, error) {
	return selectDeviceAuthorization(ctx, r.db, `
	SELECT id, device_code_hash, user_code_hash, client_id, user_id, scopes, status, expires_at, interval_seconds, last_poll_at, approved_at, denied_at, cancelled_at, used_at, device_name, cli_version, platform, created_at, updated_at
	FROM oauth_device_authorizations
	WHERE user_code_hash = $1`, userCodeHash)
}

func (r *oauthDeviceAuthorizationRepository) Approve(ctx context.Context, userCodeHash string, userID int64, now time.Time) (*service.OAuthDeviceAuthorization, error) {
	return selectDeviceAuthorization(ctx, r.db, `
	UPDATE oauth_device_authorizations
	SET status = $3, user_id = $2, approved_at = COALESCE(approved_at, $4), updated_at = NOW()
	WHERE user_code_hash = $1 AND status = $5 AND expires_at > $4
	RETURNING id, device_code_hash, user_code_hash, client_id, user_id, scopes, status, expires_at, interval_seconds, last_poll_at, approved_at, denied_at, cancelled_at, used_at, device_name, cli_version, platform, created_at, updated_at`,
		userCodeHash, userID, service.OAuthDeviceStatusApproved, now, service.OAuthDeviceStatusPending)
}

func (r *oauthDeviceAuthorizationRepository) Deny(ctx context.Context, userCodeHash string, userID int64, now time.Time) (*service.OAuthDeviceAuthorization, error) {
	return selectDeviceAuthorization(ctx, r.db, `
	UPDATE oauth_device_authorizations
	SET status = $3, user_id = $2, denied_at = COALESCE(denied_at, $4), updated_at = NOW()
	WHERE user_code_hash = $1 AND status = $5 AND expires_at > $4
	RETURNING id, device_code_hash, user_code_hash, client_id, user_id, scopes, status, expires_at, interval_seconds, last_poll_at, approved_at, denied_at, cancelled_at, used_at, device_name, cli_version, platform, created_at, updated_at`,
		userCodeHash, userID, service.OAuthDeviceStatusDenied, now, service.OAuthDeviceStatusPending)
}

func (r *oauthDeviceAuthorizationRepository) GetByDeviceCodeHash(ctx context.Context, deviceCodeHash, clientID string) (*service.OAuthDeviceAuthorization, error) {
	return selectDeviceAuthorization(ctx, r.db, `
	SELECT id, device_code_hash, user_code_hash, client_id, user_id, scopes, status, expires_at, interval_seconds, last_poll_at, approved_at, denied_at, cancelled_at, used_at, device_name, cli_version, platform, created_at, updated_at
	FROM oauth_device_authorizations
	WHERE device_code_hash = $1 AND client_id = $2`, deviceCodeHash, clientID)
}

func (r *oauthDeviceAuthorizationRepository) MarkPolled(ctx context.Context, id int64, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE oauth_device_authorizations
		SET last_poll_at = $2, poll_count = poll_count + 1, updated_at = NOW()
		WHERE id = $1`, id, now)
	return err
}

func (r *oauthDeviceAuthorizationRepository) Cancel(ctx context.Context, deviceCodeHash, clientID string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE oauth_device_authorizations
		SET status = $3, cancelled_at = COALESCE(cancelled_at, $4), updated_at = NOW()
		WHERE device_code_hash = $1 AND client_id = $2 AND status = $5 AND expires_at > $4`,
		deviceCodeHash, clientID, service.OAuthDeviceStatusCancelled, now, service.OAuthDeviceStatusPending)
	return err
}

func (r *oauthDeviceAuthorizationRepository) ConsumeApproved(ctx context.Context, deviceCodeHash, clientID string, now time.Time) (*service.OAuthDeviceAuthorization, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	session, err := selectDeviceAuthorization(ctx, tx, `
	SELECT id, device_code_hash, user_code_hash, client_id, user_id, scopes, status, expires_at, interval_seconds, last_poll_at, approved_at, denied_at, cancelled_at, used_at, device_name, cli_version, platform, created_at, updated_at
	FROM oauth_device_authorizations
	WHERE device_code_hash = $1 AND client_id = $2
	FOR UPDATE`, deviceCodeHash, clientID)
	if err != nil {
		return nil, err
	}
	if session.Status != service.OAuthDeviceStatusApproved || !session.ExpiresAt.After(now) || session.UserID == nil || *session.UserID <= 0 {
		return nil, service.ErrOAuthInvalidToken
	}
	consumed, err := selectDeviceAuthorization(ctx, tx, `
	UPDATE oauth_device_authorizations
	SET status = $3, used_at = COALESCE(used_at, $4), updated_at = NOW()
	WHERE id = $1 AND status = $2
	RETURNING id, device_code_hash, user_code_hash, client_id, user_id, scopes, status, expires_at, interval_seconds, last_poll_at, approved_at, denied_at, cancelled_at, used_at, device_name, cli_version, platform, created_at, updated_at`,
		session.ID, service.OAuthDeviceStatusApproved, service.OAuthDeviceStatusUsed, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return consumed, nil
}

func selectDeviceAuthorization(ctx context.Context, q sqlQueryer, query string, args ...any) (*service.OAuthDeviceAuthorization, error) {
	var out service.OAuthDeviceAuthorization
	var scopesRaw []byte
	var userID sql.NullInt64
	var intervalSeconds int
	err := scanSingleRow(ctx, q, query, args,
		&out.ID, &out.DeviceCodeHash, &out.UserCodeHash, &out.ClientID, &userID, &scopesRaw, &out.Status, &out.ExpiresAt, &intervalSeconds, &out.LastPollAt, &out.ApprovedAt, &out.DeniedAt, &out.CancelledAt, &out.UsedAt, &out.DeviceName, &out.CLIVersion, &out.Platform, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrOAuthInvalidToken
		}
		return nil, err
	}
	if userID.Valid {
		out.UserID = &userID.Int64
	}
	if err := json.Unmarshal(scopesRaw, &out.Scopes); err != nil {
		return nil, err
	}
	out.Interval = time.Duration(intervalSeconds) * time.Second
	return &out, nil
}
