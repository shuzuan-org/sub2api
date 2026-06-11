package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/channelinvitebatch"
	"github.com/Wei-Shaw/sub2api/ent/channelinvitebatchgroup"
	"github.com/Wei-Shaw/sub2api/ent/channelinvitecode"
	"github.com/Wei-Shaw/sub2api/ent/channelinvitecodeusage"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type channelInviteRepository struct {
	client *dbent.Client
}

func NewChannelInviteRepository(client *dbent.Client) service.ChannelInviteRepository {
	return &channelInviteRepository{client: client}
}

// ======================== 批次 CRUD ========================

func (r *channelInviteRepository) CreateBatch(ctx context.Context, batch *service.ChannelInviteBatch, groupIDs []int64) error {
	client := clientFromContext(ctx, r.client)

	builder := client.ChannelInviteBatch.Create().
		SetName(batch.Name).
		SetBonusAmount(batch.BonusAmount).
		SetMaxUsesPerCode(batch.MaxUsesPerCode).
		SetStatus(batch.Status).
		SetCreatedBy(batch.CreatedBy).
		SetNotes(batch.Notes).
		SetActivityCopyText(batch.ActivityCopyText)

	if batch.StartTime != nil {
		builder.SetStartTime(*batch.StartTime)
	}
	if batch.EndTime != nil {
		builder.SetEndTime(*batch.EndTime)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return err
	}

	batch.ID = created.ID
	batch.CreatedAt = created.CreatedAt
	batch.UpdatedAt = created.UpdatedAt

	// 创建分组关联
	if len(groupIDs) > 0 {
		bulk := make([]*dbent.ChannelInviteBatchGroupCreate, 0, len(groupIDs))
		for _, gid := range groupIDs {
			bulk = append(bulk, client.ChannelInviteBatchGroup.Create().
				SetBatchID(created.ID).
				SetGroupID(gid))
		}
		if err := client.ChannelInviteBatchGroup.CreateBulk(bulk...).Exec(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (r *channelInviteRepository) GetBatch(ctx context.Context, id int64) (*service.ChannelInviteBatch, error) {
	m, err := r.client.ChannelInviteBatch.Query().
		Where(channelinvitebatch.IDEQ(id)).
		WithCreator().
		WithBatchGroups(func(q *dbent.ChannelInviteBatchGroupQuery) {
			q.WithGroup()
		}).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrChannelInviteBatchNotFound
		}
		return nil, err
	}
	return channelInviteBatchEntityToService(m), nil
}

func (r *channelInviteRepository) UpdateBatch(ctx context.Context, id int64, input *service.UpdateChannelInviteBatchInput) error {
	client := clientFromContext(ctx, r.client)
	builder := client.ChannelInviteBatch.UpdateOneID(id)

	if input.Name != nil {
		builder.SetName(*input.Name)
	}
	if input.BonusAmount != nil {
		builder.SetBonusAmount(*input.BonusAmount)
	}
	if input.MaxUsesPerCode != nil {
		builder.SetMaxUsesPerCode(*input.MaxUsesPerCode)
	}
	if input.StartTime != nil {
		builder.SetStartTime(*input.StartTime)
	}
	if input.EndTime != nil {
		builder.SetEndTime(*input.EndTime)
	}
	if input.Status != nil {
		builder.SetStatus(*input.Status)
	}
	if input.Notes != nil {
		builder.SetNotes(*input.Notes)
	}
	if input.ActivityCopyText != nil {
		builder.SetActivityCopyText(*input.ActivityCopyText)
	}

	_, err := builder.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrChannelInviteBatchNotFound
		}
		return err
	}

	// 如果提供了 groupIDs，替换分组关联
	if input.GroupIDs != nil {
		if err := r.ReplaceBatchGroups(ctx, id, input.GroupIDs); err != nil {
			return err
		}
	}

	return nil
}

func (r *channelInviteRepository) DeleteBatch(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.ChannelInviteBatch.Delete().Where(channelinvitebatch.IDEQ(id)).Exec(ctx)
	return err
}

func (r *channelInviteRepository) ListBatches(ctx context.Context, params pagination.PaginationParams, status, search string) ([]service.ChannelInviteBatch, *pagination.PaginationResult, error) {
	q := r.client.ChannelInviteBatch.Query()

	if status != "" {
		q = q.Where(channelinvitebatch.StatusEQ(status))
	}
	if search != "" {
		q = q.Where(channelinvitebatch.NameContainsFold(search))
	}

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	batches, err := q.
		WithCreator().
		WithCodes().
		WithBatchGroups(func(q *dbent.ChannelInviteBatchGroupQuery) {
			q.WithGroup()
		}).
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(dbent.Desc(channelinvitebatch.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	out := channelInviteBatchEntitiesToService(batches)

	// 填充每个批次的 code/usage 计数
	for i := range out {
		codeCount, usedCount, err := r.GetBatchCodeStats(ctx, out[i].ID)
		if err == nil {
			out[i].CodeCount = codeCount
			out[i].UsedCount = usedCount
		}
	}

	return out, paginationResultFromTotal(int64(total), params), nil
}

// ======================== 码操作 ========================

func (r *channelInviteRepository) CreateCodes(ctx context.Context, codes []service.ChannelInviteCode) error {
	client := clientFromContext(ctx, r.client)
	bulk := make([]*dbent.ChannelInviteCodeCreate, 0, len(codes))
	for i := range codes {
		bulk = append(bulk, client.ChannelInviteCode.Create().
			SetBatchID(codes[i].BatchID).
			SetCode(codes[i].Code).
			SetStatus(codes[i].Status).
			SetMaxUses(codes[i].MaxUses))
	}
	created, err := client.ChannelInviteCode.CreateBulk(bulk...).Save(ctx)
	if err != nil {
		return err
	}
	for i, c := range created {
		codes[i].ID = c.ID
		codes[i].CreatedAt = c.CreatedAt
		codes[i].UpdatedAt = c.UpdatedAt
	}
	return nil
}

func (r *channelInviteRepository) GetCodeByID(ctx context.Context, id int64) (*service.ChannelInviteCode, error) {
	m, err := r.client.ChannelInviteCode.Query().
		Where(channelinvitecode.IDEQ(id)).
		WithBatch().
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrChannelInviteCodeNotFound
		}
		return nil, err
	}
	return channelInviteCodeEntityToService(m), nil
}

func (r *channelInviteRepository) GetCodeByCode(ctx context.Context, code string) (*service.ChannelInviteCode, error) {
	m, err := r.client.ChannelInviteCode.Query().
		Where(channelinvitecode.CodeEQ(code)).
		WithBatch().
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrChannelInviteCodeNotFound
		}
		return nil, err
	}
	return channelInviteCodeEntityToService(m), nil
}

func (r *channelInviteRepository) GetCodeByCodeForUpdate(ctx context.Context, code string) (*service.ChannelInviteCode, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.ChannelInviteCode.Query().
		Where(channelinvitecode.CodeEQ(code)).
		WithBatch().
		ForUpdate().
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrChannelInviteCodeNotFound
		}
		return nil, err
	}
	return channelInviteCodeEntityToService(m), nil
}

func (r *channelInviteRepository) ListCodes(ctx context.Context, batchID int64, params pagination.PaginationParams, status, search string) ([]service.ChannelInviteCode, *pagination.PaginationResult, error) {
	q := r.client.ChannelInviteCode.Query().
		Where(channelinvitecode.BatchIDEQ(batchID))

	if status != "" {
		q = q.Where(channelinvitecode.StatusEQ(status))
	}
	if search != "" {
		q = q.Where(channelinvitecode.CodeContainsFold(search))
	}

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	codes, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(dbent.Desc(channelinvitecode.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	out := channelInviteCodeEntitiesToService(codes)
	return out, paginationResultFromTotal(int64(total), params), nil
}

func (r *channelInviteRepository) IncrementUsedCount(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	return client.ChannelInviteCode.UpdateOneID(id).
		AddUsedCount(1).
		Exec(ctx)
}

// ======================== 使用记录 ========================

func (r *channelInviteRepository) CreateUsage(ctx context.Context, usage *service.ChannelInviteCodeUsage) error {
	client := clientFromContext(ctx, r.client)
	created, err := client.ChannelInviteCodeUsage.Create().
		SetCodeID(usage.CodeID).
		SetBatchID(usage.BatchID).
		SetUserID(usage.UserID).
		SetBonusGranted(usage.BonusGranted).
		SetClaimedAt(usage.ClaimedAt).
		Save(ctx)
	if err != nil {
		return err
	}
	usage.ID = created.ID
	return nil
}

func (r *channelInviteRepository) GetUsageByCodeAndUser(ctx context.Context, codeID, userID int64) (*service.ChannelInviteCodeUsage, error) {
	m, err := r.client.ChannelInviteCodeUsage.Query().
		Where(
			channelinvitecodeusage.CodeIDEQ(codeID),
			channelinvitecodeusage.UserIDEQ(userID),
		).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return channelInviteCodeUsageEntityToService(m), nil
}

func (r *channelInviteRepository) ListUsagesByBatch(ctx context.Context, batchID int64, params pagination.PaginationParams) ([]service.ChannelInviteCodeUsage, *pagination.PaginationResult, error) {
	q := r.client.ChannelInviteCodeUsage.Query().
		Where(channelinvitecodeusage.BatchIDEQ(batchID))

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	usages, err := q.
		WithUser().
		WithCode().
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(dbent.Desc(channelinvitecodeusage.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	out := channelInviteCodeUsageEntitiesToService(usages)
	return out, paginationResultFromTotal(int64(total), params), nil
}

func (r *channelInviteRepository) GrantBonus(ctx context.Context, usageID int64, bonusAmount float64) error {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	return client.ChannelInviteCodeUsage.UpdateOneID(usageID).
		SetBonusGranted(true).
		SetBonusGrantedAt(now).
		Exec(ctx)
}

func (r *channelInviteRepository) ListPendingBonusByUser(ctx context.Context, userID int64) ([]service.ChannelInviteCodeUsage, error) {
	usages, err := r.client.ChannelInviteCodeUsage.Query().
		Where(
			channelinvitecodeusage.UserIDEQ(userID),
			channelinvitecodeusage.BonusGrantedEQ(false),
		).
		WithCode().
		WithBatch().
		All(ctx)
	if err != nil {
		return nil, err
	}
	return channelInviteCodeUsageEntitiesToService(usages), nil
}

// ======================== 批次分组关联 ========================

func (r *channelInviteRepository) GetBatchGroupIDs(ctx context.Context, batchID int64) ([]int64, error) {
	groups, err := r.client.ChannelInviteBatchGroup.Query().
		Where(channelinvitebatchgroup.BatchIDEQ(batchID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.GroupID)
	}
	return ids, nil
}

func (r *channelInviteRepository) ReplaceBatchGroups(ctx context.Context, batchID int64, groupIDs []int64) error {
	client := clientFromContext(ctx, r.client)

	// 删除旧关联
	_, err := client.ChannelInviteBatchGroup.Delete().
		Where(channelinvitebatchgroup.BatchIDEQ(batchID)).
		Exec(ctx)
	if err != nil {
		return err
	}

	// 创建新关联
	if len(groupIDs) > 0 {
		bulk := make([]*dbent.ChannelInviteBatchGroupCreate, 0, len(groupIDs))
		for _, gid := range groupIDs {
			bulk = append(bulk, client.ChannelInviteBatchGroup.Create().
				SetBatchID(batchID).
				SetGroupID(gid))
		}
		return client.ChannelInviteBatchGroup.CreateBulk(bulk...).Exec(ctx)
	}

	return nil
}

// ======================== 用户判定 ========================

func (r *channelInviteRepository) HasPriorBonusGrantedByUser(ctx context.Context, userID int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	return client.ChannelInviteCodeUsage.Query().
		Where(
			channelinvitecodeusage.UserIDEQ(userID),
			channelinvitecodeusage.BonusGrantedEQ(true),
		).
		Exist(ctx)
}

func (r *channelInviteRepository) HasPendingBonusByUser(ctx context.Context, userID int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	return client.ChannelInviteCodeUsage.Query().
		Where(
			channelinvitecodeusage.UserIDEQ(userID),
			channelinvitecodeusage.BonusGrantedEQ(false),
		).
		Exist(ctx)
}

// ======================== 批量计数 ========================

func (r *channelInviteRepository) GetBatchCodeStats(ctx context.Context, batchID int64) (codeCount, usedCount int, err error) {
	codes, err := r.client.ChannelInviteCode.Query().
		Where(channelinvitecode.BatchIDEQ(batchID)).
		All(ctx)
	if err != nil {
		return 0, 0, err
	}
	codeCount = len(codes)
	for _, c := range codes {
		if c.UsedCount > 0 {
			usedCount++
		}
	}
	return codeCount, usedCount, nil
}

// ======================== Entity -> Service 转换 ========================

func channelInviteBatchEntityToService(m *dbent.ChannelInviteBatch) *service.ChannelInviteBatch {
	if m == nil {
		return nil
	}
	b := &service.ChannelInviteBatch{
		ID:               m.ID,
		Name:             m.Name,
		BonusAmount:      m.BonusAmount,
		MaxUsesPerCode:   m.MaxUsesPerCode,
		StartTime:        m.StartTime,
		EndTime:          m.EndTime,
		Status:           m.Status,
		Notes:            derefString(m.Notes),
		ActivityCopyText: derefString(m.ActivityCopyText),
		CreatedBy:        m.CreatedBy,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
	if m.Edges.Creator != nil {
		b.Creator = userEntityToService(m.Edges.Creator)
	}
	if m.Edges.BatchGroups != nil {
		groups := make([]service.Group, 0, len(m.Edges.BatchGroups))
		for _, bg := range m.Edges.BatchGroups {
			if bg.Edges.Group != nil {
				groups = append(groups, *groupEntityToService(bg.Edges.Group))
			}
		}
		b.Groups = groups
	}
	if m.Edges.Codes != nil {
		codes := make([]service.ChannelInviteCode, 0, len(m.Edges.Codes))
		for _, c := range m.Edges.Codes {
			if s := channelInviteCodeEntityToService(c); s != nil {
				codes = append(codes, *s)
			}
		}
		b.Codes = codes
	}
	return b
}

func channelInviteBatchEntitiesToService(models []*dbent.ChannelInviteBatch) []service.ChannelInviteBatch {
	out := make([]service.ChannelInviteBatch, 0, len(models))
	for i := range models {
		if s := channelInviteBatchEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func channelInviteCodeEntityToService(m *dbent.ChannelInviteCode) *service.ChannelInviteCode {
	if m == nil {
		return nil
	}
	c := &service.ChannelInviteCode{
		ID:        m.ID,
		BatchID:   m.BatchID,
		Code:      m.Code,
		Status:    m.Status,
		MaxUses:   m.MaxUses,
		UsedCount: m.UsedCount,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
	if m.Edges.Batch != nil {
		c.Batch = channelInviteBatchEntityToService(m.Edges.Batch)
	}
	return c
}

func channelInviteCodeEntitiesToService(models []*dbent.ChannelInviteCode) []service.ChannelInviteCode {
	out := make([]service.ChannelInviteCode, 0, len(models))
	for i := range models {
		if s := channelInviteCodeEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func channelInviteCodeUsageEntityToService(m *dbent.ChannelInviteCodeUsage) *service.ChannelInviteCodeUsage {
	if m == nil {
		return nil
	}
	u := &service.ChannelInviteCodeUsage{
		ID:           m.ID,
		CodeID:       m.CodeID,
		BatchID:      m.BatchID,
		UserID:       m.UserID,
		BonusGranted: m.BonusGranted,
		BonusGrantedAt: m.BonusGrantedAt,
		ClaimedAt:    m.ClaimedAt,
	}
	if m.Edges.Code != nil {
		u.Code = channelInviteCodeEntityToService(m.Edges.Code)
	}
	if m.Edges.Batch != nil {
		u.Batch = channelInviteBatchEntityToService(m.Edges.Batch)
	}
	if m.Edges.User != nil {
		u.User = userEntityToService(m.Edges.User)
	}
	return u
}

func channelInviteCodeUsageEntitiesToService(models []*dbent.ChannelInviteCodeUsage) []service.ChannelInviteCodeUsage {
	out := make([]service.ChannelInviteCodeUsage, 0, len(models))
	for i := range models {
		if s := channelInviteCodeUsageEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}
