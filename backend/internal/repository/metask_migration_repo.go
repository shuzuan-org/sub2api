package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// metaskMigrationRepository 用原生 SQL：冻结要在同一个事务里锁用户行并改 users / api_keys / user_subscriptions 三张表。
type metaskMigrationRepository struct {
	db *sql.DB
}

func NewMetaskMigrationRepository(sqlDB *sql.DB) service.MetaskMigrationRepository {
	return &metaskMigrationRepository{db: sqlDB}
}

type metaskQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const metaskActiveSubsQuery = `
	SELECT s.plan_id, p.name, round(p.monthly_limit_usd * 1000)::bigint, s.expires_at
	  FROM user_subscriptions s
	  JOIN subscription_plans p ON p.id = s.plan_id
	 WHERE s.user_id = $1 AND s.deleted_at IS NULL AND s.status = 'active'
	   AND s.expires_at > now()
	 ORDER BY s.expires_at`

func metaskLoadSubscriptions(ctx context.Context, q metaskQueryer, userID int64) ([]service.MetaskMigrationSubscription, []int64, error) {
	rows, err := q.QueryContext(ctx, metaskActiveSubsQuery, userID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	subs := []service.MetaskMigrationSubscription{}
	planIDs := []int64{}
	for rows.Next() {
		var planID int64
		var name string
		var limit sql.NullInt64
		var expires time.Time
		if err := rows.Scan(&planID, &name, &limit, &expires); err != nil {
			return nil, nil, err
		}
		sub := service.MetaskMigrationSubscription{PlanName: name, ExpiresAt: expires}
		if limit.Valid {
			v := limit.Int64
			sub.MonthlyLimitMU = &v
		}
		subs = append(subs, sub)
		planIDs = append(planIDs, planID)
	}
	return subs, planIDs, rows.Err()
}

func (r *metaskMigrationRepository) Preview(ctx context.Context, userID int64) (*service.MetaskMigrationPreview, error) {
	pv := &service.MetaskMigrationPreview{}
	err := r.db.QueryRowContext(ctx, `
		SELECT role, status, round(balance * 1000)::bigint,
		       (SELECT count(*) FROM api_keys k WHERE k.user_id = u.id AND k.deleted_at IS NULL AND k.status = 'active')
		  FROM users u WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
		Scan(&pv.Role, &pv.Status, &pv.BalanceMU, &pv.KeyCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	pv.Subscriptions, _, err = metaskLoadSubscriptions(ctx, r.db, userID)
	if err != nil {
		return nil, err
	}
	return pv, nil
}

func metaskGetRecord(ctx context.Context, q metaskQueryer, userID int64) (*service.MetaskMigrationRecord, error) {
	rec := &service.MetaskMigrationRecord{UserID: userID}
	var payload []byte
	var result []byte
	var lastErr sql.NullString
	var completedAt sql.NullTime
	err := q.QueryRowContext(ctx, `
		SELECT status, payload, result, attempts, last_error, created_at, completed_at
		  FROM metask_migrations WHERE user_id = $1`, userID).
		Scan(&rec.Status, &payload, &result, &rec.Attempts, &lastErr, &rec.CreatedAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &rec.Payload); err != nil {
		return nil, err
	}
	if len(result) > 0 {
		rec.Result = json.RawMessage(result)
	}
	rec.LastError = lastErr.String
	if completedAt.Valid {
		t := completedAt.Time
		rec.CompletedAt = &t
	}
	return rec, nil
}

func (r *metaskMigrationRepository) Get(ctx context.Context, userID int64) (*service.MetaskMigrationRecord, error) {
	return metaskGetRecord(ctx, r.db, userID)
}

func (r *metaskMigrationRepository) Freeze(ctx context.Context, userID int64, usageSince time.Time) (*service.MetaskMigrationFreezeResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 锁用户行：并发的重复点击、扣费、充值入账都排在后面
	var (
		email, passwordHash, role, status, username, phone string
		createdAt                                          time.Time
		balanceMU                                          int64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT email, password_hash, role, status, coalesce(username, ''),
		       coalesce(nullif(phone_number, ''), nullif(phone, ''), ''), created_at, round(balance * 1000)::bigint
		  FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).
		Scan(&email, &passwordHash, &role, &status, &username, &phone, &createdAt, &balanceMU)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	if rec, err := metaskGetRecord(ctx, tx, userID); err != nil {
		return nil, err
	} else if rec != nil {
		return &service.MetaskMigrationFreezeResult{Record: rec}, nil
	}
	if role == service.RoleAdmin {
		return nil, service.ErrMetaskMigrationAdmin
	}
	if status != service.StatusActive {
		return nil, service.ErrUserNotActive
	}

	payload := service.MetaskMigrationPayload{
		Sub2APIID:    userID,
		Email:        strings.ToLower(strings.TrimSpace(email)),
		PasswordHash: passwordHash,
		Username:     strings.TrimSpace(username),
		Phone:        strings.TrimSpace(phone),
		CreatedAt:    createdAt,
		BalanceMU:    balanceMU,
		Keys:         []service.MetaskMigrationKey{},
	}
	var planIDs []int64
	payload.Subscriptions, planIDs, err = metaskLoadSubscriptions(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if len(payload.Subscriptions) > 0 && !usageSince.IsZero() {
		if err := tx.QueryRowContext(ctx, `
			SELECT coalesce(round(sum(actual_cost) * 1000), 0)::bigint
			  FROM usage_logs WHERE user_id = $1 AND billing_type = 1 AND created_at >= $2`, userID, usageSince).
			Scan(&payload.SubscriptionUsedMU); err != nil {
			return nil, err
		}
	}

	rawKeys := []string{}
	rows, err := tx.QueryContext(ctx, `
		SELECT key, name, created_at, last_used_at FROM api_keys
		 WHERE user_id = $1 AND deleted_at IS NULL AND status = 'active' ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key, name string
		var keyCreated time.Time
		var lastUsed sql.NullTime
		if err := rows.Scan(&key, &name, &keyCreated, &lastUsed); err != nil {
			_ = rows.Close()
			return nil, err
		}
		sum := sha256.Sum256([]byte(key))
		prefix := key
		if len(prefix) > 11 {
			prefix = prefix[:11]
		}
		k := service.MetaskMigrationKey{KeyHash: hex.EncodeToString(sum[:]), KeyPrefix: prefix, Name: name, CreatedAt: keyCreated}
		if lastUsed.Valid {
			t := lastUsed.Time
			k.LastUsedAt = &t
		}
		payload.Keys = append(payload.Keys, k)
		rawKeys = append(rawKeys, key)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	if _, err := tx.ExecContext(ctx, `UPDATE users SET balance = 0, updated_at = now() WHERE id = $1`, userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET status = 'disabled', updated_at = now()
		 WHERE user_id = $1 AND deleted_at IS NULL AND status = 'active'`, userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE user_subscriptions SET status = 'suspended', updated_at = now()
		 WHERE user_id = $1 AND deleted_at IS NULL AND status = 'active' AND expires_at > now()`, userID); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	rec := &service.MetaskMigrationRecord{UserID: userID, Status: service.MetaskMigrationStatusFrozen, Payload: payload}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO metask_migrations (user_id, status, payload) VALUES ($1, $2, $3) RETURNING created_at`,
		userID, service.MetaskMigrationStatusFrozen, string(body)).Scan(&rec.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.MetaskMigrationFreezeResult{Record: rec, Created: true, RawKeys: rawKeys, PlanIDs: planIDs}, nil
}

func (r *metaskMigrationRepository) MarkCompleted(ctx context.Context, userID int64, result json.RawMessage) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		UPDATE metask_migrations
		   SET status = $2, result = $3, attempts = attempts + 1, last_error = NULL, updated_at = now(), completed_at = now()
		 WHERE user_id = $1 AND status <> $2`, userID, service.MetaskMigrationStatusCompleted, string(result)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET status = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		userID, service.StatusDisabled); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *metaskMigrationRepository) MarkFailed(ctx context.Context, userID int64, errMsg string) error {
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE metask_migrations SET attempts = attempts + 1, last_error = $2, updated_at = now()
		 WHERE user_id = $1 AND status = $3`, userID, errMsg, service.MetaskMigrationStatusFrozen)
	return err
}

func (r *metaskMigrationRepository) ListPending(ctx context.Context, olderThan time.Duration, limit int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT user_id FROM metask_migrations
		 WHERE status = $1 AND updated_at < now() - make_interval(secs => $2)
		 ORDER BY updated_at LIMIT $3`, service.MetaskMigrationStatusFrozen, olderThan.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
