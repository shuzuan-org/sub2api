//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// TestSetVisiblePlans_RollbackOnFailure 验证 SetVisiblePlans 的删+插是原子的：
// 当插入因 FK 违约失败时，旧绑定必须完整保留（不能出现"绑定被清空"的幽灵分组）。
//
// 使用 testEntClient（裸 client）而非 suite 的 tx，让 SetVisiblePlans 真正自管事务，
// 否则在外层 tx 包裹下无法观察到回滚语义。测试自行清理写入的数据。
func TestSetVisiblePlans_RollbackOnFailure(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newGroupRepositoryWithSQL(client, integrationDB)

	// 准备：一个 subscriber 分组 + 两个有效 plan。
	g := &service.Group{
		Name:       "rollback-test-group",
		Platform:   service.PlatformAnthropic,
		Visibility: service.VisibilitySubscriber,
		Status:     service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, g))
	planA, err := client.SubscriptionPlan.Create().
		SetName("rollback-plan-A").SetStatus(service.StatusActive).SetVisibility(service.VisibilityPublic).Save(ctx)
	require.NoError(t, err)
	planB, err := client.SubscriptionPlan.Create().
		SetName("rollback-plan-B").SetStatus(service.StatusActive).SetVisibility(service.VisibilityPublic).Save(ctx)
	require.NoError(t, err)

	// 清理（testEntClient 写真库，不自动回滚）。
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM group_visible_plans WHERE group_id = $1", g.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM subscription_plans WHERE id = ANY($1)", pq.Array([]int64{planA.ID, planB.ID}))
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id = $1", g.ID)
	})

	// 建立初始绑定 [A, B]。
	require.NoError(t, repo.SetVisiblePlans(ctx, g.ID, []int64{planA.ID, planB.ID}))
	before, err := repo.LoadVisiblePlansByGroupIDs(ctx, []int64{g.ID})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{planA.ID, planB.ID}, before[g.ID], "initial binding")

	// 重绑为 [A, 不存在的 plan]：插入应触发 FK 违约 → 整个操作回滚。
	const nonExistentPlanID int64 = 999999999
	err = repo.SetVisiblePlans(ctx, g.ID, []int64{planA.ID, nonExistentPlanID})
	require.Error(t, err, "expected FK violation on non-existent plan_id")

	// 断言：旧绑定 [A, B] 必须完整保留（回滚生效），而不是被清空或半更新。
	after, err := repo.LoadVisiblePlansByGroupIDs(ctx, []int64{g.ID})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{planA.ID, planB.ID}, after[g.ID],
		"binding must be unchanged after failed rebind (atomic rollback)")
}
