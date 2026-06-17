package repository

import (
	"context"
	"errors"
	"sort"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/groupvisibleplan"
)

// LoadVisiblePlansByGroupIDs 批量加载分组绑定的订阅计划 ID，返回 map[groupID][]planID。
// 用于 subscriber 可见性判断（用户持有其中任一 plan 的有效订阅即可见）。
func (r *groupRepository) LoadVisiblePlansByGroupIDs(ctx context.Context, groupIDs []int64) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}
	client := clientFromContext(ctx, r.client)
	rows, err := client.GroupVisiblePlan.Query().
		Where(groupvisibleplan.GroupIDIn(groupIDs...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].GroupID] = append(out[rows[i].GroupID], rows[i].PlanID)
	}
	// Deterministic ordering.
	for gid := range out {
		sort.Slice(out[gid], func(i, j int) bool { return out[gid][i] < out[gid][j] })
	}
	return out, nil
}

// SetVisiblePlans 全量替换某分组绑定的订阅计划集合（先删后批量插入，去重）。
//
// 删除与插入必须原子化：否则中途失败会留下"绑定被清空"的分组（subscriber 档下谁都看不到）。
// 复用已有事务的两种途径：
//   - ctx 携带事务（外层通过 dbent.NewTxContext 注入，如 admin_service 的 group 创建/更新）；
//   - r.client 本身是事务 client（如集成测试 suite 注入 tx.Client()）。
//
// 二者任一成立即视为"已在事务中"，直接复用，由外层负责提交/回滚；否则本方法自开事务兜底。
func (r *groupRepository) SetVisiblePlans(ctx context.Context, groupID int64, planIDs []int64) error {
	inOuterTx := dbent.TxFromContext(ctx) != nil
	if !inOuterTx {
		// 探测 r.client 是否已是事务 client：是则复用，否则新开事务。
		tx, err := r.client.Tx(ctx)
		if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
			return err
		}
		if err == nil {
			// 本方法新开了事务，需自行提交/回滚。
			defer func() { _ = tx.Rollback() }()
			ctx = dbent.NewTxContext(ctx, tx)
			if execErr := r.execSetVisiblePlans(ctx, groupID, planIDs); execErr != nil {
				return execErr
			}
			return tx.Commit()
		}
		// err == ErrTxStarted：r.client 已是事务 client，复用之。
	}
	return r.execSetVisiblePlans(ctx, groupID, planIDs)
}

// execSetVisiblePlans 在调用方提供的（事务）上下文中执行删+插，自身不管理事务边界。
func (r *groupRepository) execSetVisiblePlans(ctx context.Context, groupID int64, planIDs []int64) error {
	client := clientFromContext(ctx, r.client)

	// 联接表是 subscriber 可见性读取的唯一来源，先清空旧绑定。
	if _, err := client.GroupVisiblePlan.Delete().
		Where(groupvisibleplan.GroupIDEQ(groupID)).
		Exec(ctx); err != nil {
		return err
	}

	unique := make(map[int64]struct{}, len(planIDs))
	for _, id := range planIDs {
		if id <= 0 {
			continue
		}
		unique[id] = struct{}{}
	}
	if len(unique) == 0 {
		return nil
	}

	creates := make([]*dbent.GroupVisiblePlanCreate, 0, len(unique))
	for planID := range unique {
		creates = append(creates, client.GroupVisiblePlan.Create().SetGroupID(groupID).SetPlanID(planID))
	}
	return client.GroupVisiblePlan.
		CreateBulk(creates...).
		OnConflictColumns(groupvisibleplan.FieldGroupID, groupvisibleplan.FieldPlanID).
		DoNothing().
		Exec(ctx)
}
