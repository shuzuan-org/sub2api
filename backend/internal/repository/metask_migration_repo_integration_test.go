//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMetaskMigrationRepository_FreezeAndComplete(t *testing.T) {
	ctx := context.Background()
	client := integrationEntClient
	repo := NewMetaskMigrationRepository(integrationDB)
	suffix := time.Now().Format("150405.000000")

	user := mustCreateUser(t, client, &service.User{Email: "Migrate-" + suffix + "@Example.com", Balance: 12.3456, Username: "mig"})
	other := mustCreateUser(t, client, &service.User{Email: "bystander-" + suffix + "@example.com", Balance: 7})
	rawKey := "sk-migrate-" + suffix
	mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: rawKey, Name: "main"})
	mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-off-" + suffix, Name: "off", Status: service.StatusDisabled})
	mustCreateApiKey(t, client, &service.APIKey{UserID: other.ID, Key: "sk-other-" + suffix, Name: "other"})
	plan, err := client.SubscriptionPlan.Create().
		SetName("migrate-plan-" + suffix).SetStatus(service.StatusActive).SetVisibility(service.VisibilityPublic).
		SetMonthlyLimitUsd(500).Save(ctx)
	require.NoError(t, err)
	expires := time.Now().Add(40 * 24 * time.Hour)
	mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, PlanID: plan.ID, ExpiresAt: expires})
	mustCreateSubscription(t, client, &service.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartsAt: time.Now().Add(-48 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
	})

	t.Cleanup(func() {
		for _, id := range []int64{user.ID, other.ID} {
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM metask_migrations WHERE user_id = $1", id)
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM user_subscriptions WHERE user_id = $1", id)
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE user_id = $1", id)
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", id)
		}
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM subscription_plans WHERE id = $1", plan.ID)
	})

	// 预览不改任何数据
	pv, err := repo.Preview(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(12346), pv.BalanceMU)
	require.Equal(t, 1, pv.KeyCount)
	require.Len(t, pv.Subscriptions, 1)
	rec, err := repo.Get(ctx, user.ID)
	require.NoError(t, err)
	require.Nil(t, rec)

	fr, err := repo.Freeze(ctx, user.ID, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	require.True(t, fr.Created)
	require.Equal(t, []string{rawKey}, fr.RawKeys)
	require.Equal(t, []int64{plan.ID}, fr.PlanIDs)
	p := fr.Record.Payload
	require.Equal(t, user.ID, p.Sub2APIID)
	require.Equal(t, "migrate-"+suffix+"@example.com", p.Email)
	require.Equal(t, int64(12346), p.BalanceMU)
	require.Len(t, p.Keys, 1)
	sum := sha256.Sum256([]byte(rawKey))
	require.Equal(t, hex.EncodeToString(sum[:]), p.Keys[0].KeyHash)
	require.Equal(t, rawKey[:11], p.Keys[0].KeyPrefix)
	require.Len(t, p.Subscriptions, 1)
	require.NotNil(t, p.Subscriptions[0].MonthlyLimitMU)
	require.Equal(t, int64(500000), *p.Subscriptions[0].MonthlyLimitMU)
	body, err := json.Marshal(p)
	require.NoError(t, err)
	require.NotContains(t, string(body), rawKey, "Key 明文不能出现在发给新站的请求体里")

	var balance float64
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance, status FROM users WHERE id = $1", user.ID).Scan(&balance, &status))
	require.Zero(t, balance)
	require.Equal(t, service.StatusActive, status, "入账成功前账号保持可登录，便于重试")
	count := func(query string, args ...any) int {
		var n int
		require.NoError(t, integrationDB.QueryRowContext(ctx, query, args...).Scan(&n))
		return n
	}
	require.Zero(t, count("SELECT count(*) FROM api_keys WHERE user_id = $1 AND status = 'active'", user.ID))
	require.Zero(t, count("SELECT count(*) FROM user_subscriptions WHERE user_id = $1 AND status = 'active' AND expires_at > now()", user.ID))
	require.Equal(t, 1, count("SELECT count(*) FROM user_subscriptions WHERE user_id = $1 AND status = 'suspended'", user.ID))
	// 其他用户不受影响
	require.Equal(t, 1, count("SELECT count(*) FROM api_keys WHERE user_id = $1 AND status = 'active'", other.ID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", other.ID).Scan(&balance))
	require.InDelta(t, 7.0, balance, 1e-9)

	// 重复点击：返回原快照，不再读已清零的余额
	again, err := repo.Freeze(ctx, user.ID, time.Time{})
	require.NoError(t, err)
	require.False(t, again.Created)
	require.Equal(t, int64(12346), again.Record.Payload.BalanceMU)
	require.Equal(t, service.MetaskMigrationStatusFrozen, again.Record.Status)

	require.NoError(t, repo.MarkFailed(ctx, user.ID, "boom"))
	pending, err := repo.ListPending(ctx, 0, 100)
	require.NoError(t, err)
	require.Contains(t, pending, user.ID)
	rec, err = repo.Get(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, 1, rec.Attempts)
	require.Equal(t, "boom", rec.LastError)

	require.NoError(t, repo.MarkCompleted(ctx, user.ID, json.RawMessage(`{"credited_mu":12346}`)))
	rec, err = repo.Get(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, service.MetaskMigrationStatusCompleted, rec.Status)
	require.JSONEq(t, `{"credited_mu":12346}`, string(rec.Result))
	require.NotNil(t, rec.CompletedAt)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM users WHERE id = $1", user.ID).Scan(&status))
	require.Equal(t, service.StatusDisabled, status)
	pending, err = repo.ListPending(ctx, 0, 100)
	require.NoError(t, err)
	require.NotContains(t, pending, user.ID)

	// 管理员不能迁移，且不留下任何改动
	admin := mustCreateUser(t, client, &service.User{Email: "admin-" + suffix + "@example.com", Role: service.RoleAdmin, Balance: 3})
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", admin.ID) })
	_, err = repo.Freeze(ctx, admin.ID, time.Time{})
	require.ErrorIs(t, err, service.ErrMetaskMigrationAdmin)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", admin.ID).Scan(&balance))
	require.InDelta(t, 3.0, balance, 1e-9, fmt.Sprintf("admin %d balance must be untouched", admin.ID))
}
