package service

import (
	"context"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbalipay "github.com/Wei-Shaw/sub2api/ent/alipayorder"
	dbchannelbatch "github.com/Wei-Shaw/sub2api/ent/channelinvitebatch"
	dbchannelusage "github.com/Wei-Shaw/sub2api/ent/channelinvitecodeusage"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// operationsInviteesCap 运营报告中被邀请人明细的最大条数（invited_count 仍为真实总数）。
const operationsInviteesCap = 100

// operationsUsernameMatchLimit username 非唯一，按用户名查询候选用户的上限。
const operationsUsernameMatchLimit = 50

// operationsChannelInviteesCap 渠道邀请码兑换明细的最大条数（ChannelInvitedCount 仍为真实总数）。
const operationsChannelInviteesCap = 100

// ErrOperationsAccountRequired 未提供 account 且未提供 user_id。
var ErrOperationsAccountRequired = infraerrors.BadRequest("OPERATIONS_ACCOUNT_REQUIRED", "account or user_id is required")

// UserOperationsBrief 报告中引用的其他用户（邀请人/被邀请人/候选人）的精简信息。
type UserOperationsBrief struct {
	ID        int64
	Email     string
	Username  string
	CreatedAt time.Time
}

// UserOperationsChannelBatch 用户作为渠道合作方（码主）名下的活动批次。
type UserOperationsChannelBatch struct {
	ID          int64
	Name        string
	Status      string
	BonusAmount float64
	Codes       []string // 批次下的码明细（默认一活动一码）
	CodeCount   int
	UsedCount   int // 该批次全部码的已使用次数合计
	StartTime   *time.Time
	EndTime     *time.Time
	CreatedAt   time.Time
}

// UserOperationsChannelClaim 该用户自己兑换过的渠道邀请码（被渠道拉新的一侧，
// 对应 referral 体系里的 Inviter）。
type UserOperationsChannelClaim struct {
	BatchID      int64
	BatchName    string
	Code         string
	OwnerID      int64                // 批次 created_by，码主已删除时仍保留
	Owner        *UserOperationsBrief // 码主信息；码主已删除时为 nil
	BonusAmount  float64
	BonusGranted bool
	ClaimedAt    time.Time
}

// UserOperationsChannelInvitee 通过该用户名下渠道码进站的用户（对应 referral 体系里的 Invitee）。
type UserOperationsChannelInvitee struct {
	User         UserOperationsBrief
	BatchID      int64
	BatchName    string
	Code         string
	BonusGranted bool
	ClaimedAt    time.Time
}

// UserOperationsReport 单个用户的运营画像报告。
type UserOperationsReport struct {
	User *User

	// 邀请关系
	ReferralCode      *string               // 专属邀请码，懒创建可能为 nil；只读报告不触发创建
	InviterID         *int64                // 原始 referred_by（邀请人被删除时仍保留）
	Inviter           *UserOperationsBrief  // 邀请人信息；无邀请人或邀请人已删除时为 nil
	InvitedCount      int64                 // 邀请用户总数（真实 DB count）
	Invitees          []UserOperationsBrief // 被邀请人明细（最新在前，最多 operationsInviteesCap 条）
	InviteesTruncated bool

	// 渠道邀请码关系（与 referral 相互独立的另一套拉新体系：referral 认 users.referred_by，
	// 渠道认 channel_invite_batches.created_by + 兑换记录，两边数据不互通）
	ChannelClaims            []UserOperationsChannelClaim   // 该用户兑换过的渠道码（无则空切片）
	ChannelBatches           []UserOperationsChannelBatch   // 名下批次（最新在前）；空 = 不是任何渠道活动的码主
	ChannelCodeCount         int                            // 名下批次的码总数
	ChannelInvitedCount      int64                          // 名下批次被兑换总次数（真实 DB count）
	ChannelInvitees          []UserOperationsChannelInvitee // 兑换明细（最新在前，最多 operationsChannelInviteesCap 条）
	ChannelInviteesTruncated bool

	// 充值（支付宝已支付订单 + 余额型兑换码）
	AlipayPaidCount    int64   // 已支付订单笔数
	AlipayCnyFeeTotal  int64   // 实付人民币合计（分）
	AlipayUsdTotal     float64 // 到账 U 代币合计
	RedeemBalanceTotal float64 // 余额型兑换码（balance/admin_balance, value>0）合计

	// token 消耗（全期汇总）
	Usage *usagestats.UsageStats
}

// UserOperationsLookupResult GetReport 的查询结果：命中单个用户时 Report 非空；
// username 命中多个用户时 Candidates 非空，由调用方用 user_id 复查。
type UserOperationsLookupResult struct {
	Report     *UserOperationsReport
	Candidates []UserOperationsBrief
}

// UserOperationsService 管理端用户运营查询服务：按 email/username/user_id 定位用户，
// 聚合邀请关系、充值金额与 token 消耗。
// username 查询、被邀请人列表与支付宝聚合直接走 entClient（沿用 InviteService 模式），
// 避免扩 UserRepository 接口。
type UserOperationsService struct {
	entClient      *dbent.Client
	userRepo       UserRepository
	redeemCodeRepo RedeemCodeRepository
	usageLogRepo   UsageLogRepository
}

// NewUserOperationsService 创建用户运营查询服务实例。
func NewUserOperationsService(
	entClient *dbent.Client,
	userRepo UserRepository,
	redeemCodeRepo RedeemCodeRepository,
	usageLogRepo UsageLogRepository,
) *UserOperationsService {
	return &UserOperationsService{
		entClient:      entClient,
		userRepo:       userRepo,
		redeemCodeRepo: redeemCodeRepo,
		usageLogRepo:   usageLogRepo,
	}
}

// GetReport 按 userID（优先）或 account（email 精确 → username 精确）定位用户并出报告。
// username 命中多个时返回 Candidates，不出报告。
func (s *UserOperationsService) GetReport(ctx context.Context, account string, userID int64) (*UserOperationsLookupResult, error) {
	u, candidates, err := s.resolveUser(ctx, account, userID)
	if err != nil {
		return nil, err
	}
	if len(candidates) > 0 {
		return &UserOperationsLookupResult{Candidates: candidates}, nil
	}

	report, err := s.buildReport(ctx, u)
	if err != nil {
		return nil, err
	}
	return &UserOperationsLookupResult{Report: report}, nil
}

// resolveUser 定位目标用户：userID → email 精确 → username 精确（可能多命中）。
func (s *UserOperationsService) resolveUser(ctx context.Context, account string, userID int64) (*User, []UserOperationsBrief, error) {
	if userID > 0 {
		u, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			return nil, nil, err
		}
		return u, nil, nil
	}

	if account == "" {
		return nil, nil, ErrOperationsAccountRequired
	}

	u, err := s.userRepo.GetByEmail(ctx, account)
	if err == nil {
		return u, nil, nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return nil, nil, err
	}

	// username 无唯一约束且默认空串：空串绝不参与查询（上面已挡掉），精确匹配可能多条。
	rows, err := s.entClient.User.Query().
		Where(dbuser.UsernameEQ(account)).
		Order(dbent.Asc(dbuser.FieldID)).
		Limit(operationsUsernameMatchLimit).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	switch len(rows) {
	case 0:
		return nil, nil, ErrUserNotFound
	case 1:
		// 走 GetByID 拿到带 AllowedGroups 的完整 service.User，并复用统一的实体转换。
		u, err := s.userRepo.GetByID(ctx, rows[0].ID)
		if err != nil {
			return nil, nil, err
		}
		return u, nil, nil
	default:
		candidates := make([]UserOperationsBrief, 0, len(rows))
		for _, r := range rows {
			candidates = append(candidates, UserOperationsBrief{
				ID:        r.ID,
				Email:     r.Email,
				Username:  r.Username,
				CreatedAt: r.CreatedAt,
			})
		}
		return nil, candidates, nil
	}
}

// buildReport 聚合单个用户的运营画像（admin 低 QPS，顺序查询即可）。
func (s *UserOperationsService) buildReport(ctx context.Context, u *User) (*UserOperationsReport, error) {
	report := &UserOperationsReport{
		User:         u,
		ReferralCode: u.ReferralCode,
		InviterID:    u.ReferredBy,
	}

	// 邀请人：referred_by 非空则查询；邀请人已（软）删除时容错，保留原始 ID。
	if u.ReferredBy != nil {
		inviter, err := s.userRepo.GetByID(ctx, *u.ReferredBy)
		switch {
		case err == nil:
			report.Inviter = &UserOperationsBrief{
				ID:        inviter.ID,
				Email:     inviter.Email,
				Username:  inviter.Username,
				CreatedAt: inviter.CreatedAt,
			}
		case errors.Is(err, ErrUserNotFound):
			// 邀请人已删除：Inviter 置空，InviterID 保留
		default:
			return nil, err
		}
	}

	// 被邀请人：真实总数 + 截断明细（镜像 InviteService.ListInvitees 的查询写法）。
	inviteeQuery := s.entClient.User.Query().Where(dbuser.ReferredByEQ(u.ID))
	total, err := inviteeQuery.Clone().Count(ctx)
	if err != nil {
		return nil, err
	}
	report.InvitedCount = int64(total)

	inviteeRows, err := inviteeQuery.
		Order(dbent.Desc(dbuser.FieldCreatedAt)).
		Limit(operationsInviteesCap).
		All(ctx)
	if err != nil {
		return nil, err
	}
	report.Invitees = make([]UserOperationsBrief, 0, len(inviteeRows))
	for _, r := range inviteeRows {
		report.Invitees = append(report.Invitees, UserOperationsBrief{
			ID:        r.ID,
			Email:     r.Email,
			Username:  r.Username,
			CreatedAt: r.CreatedAt,
		})
	}
	report.InviteesTruncated = total > len(report.Invitees)

	// 渠道邀请码：码主视角（名下批次 + 兑换明细）与被邀请视角（自己兑换过的码）。
	if err := s.fillChannelInvite(ctx, report, u.ID); err != nil {
		return nil, err
	}

	// 支付宝充值：status='paid' 订单的笔数、实付分、到账 U 聚合。
	paidCount, cnyFeeTotal, usdTotal, err := s.sumPaidAlipayOrders(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	report.AlipayPaidCount = paidCount
	report.AlipayCnyFeeTotal = cnyFeeTotal
	report.AlipayUsdTotal = usdTotal

	// 兑换码充值：balance/admin_balance 且 value>0 的合计。
	redeemTotal, err := s.redeemCodeRepo.SumPositiveBalanceByUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	report.RedeemBalanceTotal = redeemTotal

	// token 消耗全期汇总。GetUserStatsAggregated 的 WHERE 是硬时间边界，
	// 全期需显式传 [Unix(0), 明天)，传零值会查不到任何数据。
	usage, err := s.usageLogRepo.GetUserStatsAggregated(
		ctx, u.ID,
		time.Unix(0, 0).UTC(),
		time.Now().UTC().AddDate(0, 0, 1),
	)
	if err != nil {
		return nil, err
	}
	report.Usage = usage

	return report, nil
}

// fillChannelInvite 填充渠道邀请码相关数据（直接走 entClient，与本服务其余聚合一致）。
// 渠道体系与 referral 完全独立：用户可能只在其中一边有数据，两块都为空是正常结果。
func (s *UserOperationsService) fillChannelInvite(ctx context.Context, report *UserOperationsReport, userID int64) error {
	if err := s.fillChannelClaims(ctx, report, userID); err != nil {
		return err
	}
	return s.fillChannelOwnership(ctx, report, userID)
}

// fillChannelClaims 被邀请侧：该用户兑换过的渠道码。
// 业务上限制"每人只能参加一次渠道活动"，但历史数据可能多条，故按切片返回。
func (s *UserOperationsService) fillChannelClaims(ctx context.Context, report *UserOperationsReport, userID int64) error {
	rows, err := s.entClient.ChannelInviteCodeUsage.Query().
		Where(dbchannelusage.UserIDEQ(userID)).
		WithCode().
		WithBatch(func(q *dbent.ChannelInviteBatchQuery) { q.WithCreator() }).
		Order(dbent.Desc(dbchannelusage.FieldClaimedAt)).
		All(ctx)
	if err != nil {
		return err
	}

	report.ChannelClaims = make([]UserOperationsChannelClaim, 0, len(rows))
	for _, r := range rows {
		claim := UserOperationsChannelClaim{
			BatchID:      r.BatchID,
			BonusGranted: r.BonusGranted,
			ClaimedAt:    r.ClaimedAt,
		}
		if code := r.Edges.Code; code != nil {
			claim.Code = code.Code
		}
		// 批次/码主可能已被删除：能取到什么填什么，OwnerID 始终保留原始值。
		if batch := r.Edges.Batch; batch != nil {
			claim.BatchName = batch.Name
			claim.BonusAmount = batch.BonusAmount
			claim.OwnerID = batch.CreatedBy
			if owner := batch.Edges.Creator; owner != nil {
				claim.Owner = &UserOperationsBrief{
					ID:        owner.ID,
					Email:     owner.Email,
					Username:  owner.Username,
					CreatedAt: owner.CreatedAt,
				}
			}
		}
		report.ChannelClaims = append(report.ChannelClaims, claim)
	}
	return nil
}

// fillChannelOwnership 码主侧：名下批次（含码明细与使用计数）+ 跨批次的兑换明细。
func (s *UserOperationsService) fillChannelOwnership(ctx context.Context, report *UserOperationsReport, userID int64) error {
	batchRows, err := s.entClient.ChannelInviteBatch.Query().
		Where(dbchannelbatch.CreatedByEQ(userID)).
		WithCodes().
		Order(dbent.Desc(dbchannelbatch.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return err
	}

	report.ChannelBatches = make([]UserOperationsChannelBatch, 0, len(batchRows))
	report.ChannelInvitees = make([]UserOperationsChannelInvitee, 0)
	batchIDs := make([]int64, 0, len(batchRows))
	batchNames := make(map[int64]string, len(batchRows))

	for _, b := range batchRows {
		codes := make([]string, 0, len(b.Edges.Codes))
		used := 0
		for _, c := range b.Edges.Codes {
			codes = append(codes, c.Code)
			used += c.UsedCount
		}
		report.ChannelBatches = append(report.ChannelBatches, UserOperationsChannelBatch{
			ID:          b.ID,
			Name:        b.Name,
			Status:      b.Status,
			BonusAmount: b.BonusAmount,
			Codes:       codes,
			CodeCount:   len(codes),
			UsedCount:   used,
			StartTime:   b.StartTime,
			EndTime:     b.EndTime,
			CreatedAt:   b.CreatedAt,
		})
		report.ChannelCodeCount += len(codes)
		batchIDs = append(batchIDs, b.ID)
		batchNames[b.ID] = b.Name
	}

	// 不是码主：跳过兑换查询（BatchIDIn 空集会退化成全表扫）。
	if len(batchIDs) == 0 {
		return nil
	}

	usageQuery := s.entClient.ChannelInviteCodeUsage.Query().Where(dbchannelusage.BatchIDIn(batchIDs...))
	total, err := usageQuery.Clone().Count(ctx)
	if err != nil {
		return err
	}
	report.ChannelInvitedCount = int64(total)

	usageRows, err := usageQuery.
		WithCode().
		WithUser().
		Order(dbent.Desc(dbchannelusage.FieldClaimedAt)).
		Limit(operationsChannelInviteesCap).
		All(ctx)
	if err != nil {
		return err
	}

	for _, r := range usageRows {
		invitee := UserOperationsChannelInvitee{
			User:         UserOperationsBrief{ID: r.UserID},
			BatchID:      r.BatchID,
			BatchName:    batchNames[r.BatchID],
			BonusGranted: r.BonusGranted,
			ClaimedAt:    r.ClaimedAt,
		}
		if code := r.Edges.Code; code != nil {
			invitee.Code = code.Code
		}
		// 兑换人可能已（软）删除：保留 user_id，其余字段留空。
		if u := r.Edges.User; u != nil {
			invitee.User = UserOperationsBrief{
				ID:        u.ID,
				Email:     u.Email,
				Username:  u.Username,
				CreatedAt: u.CreatedAt,
			}
		}
		report.ChannelInvitees = append(report.ChannelInvitees, invitee)
	}
	report.ChannelInviteesTruncated = total > len(report.ChannelInvitees)

	return nil
}

// sumPaidAlipayOrders 聚合用户所有已支付订单：笔数、cny_fee 合计（分）、usd_amount 合计（到账 U）。
// 零行时 SUM 为 NULL，用指针字段防御性扫描（与 SumPositiveBalanceByUser 的防御思路一致）。
func (s *UserOperationsService) sumPaidAlipayOrders(ctx context.Context, userID int64) (count int64, cnyFeeTotal int64, usdTotal float64, err error) {
	var rows []struct {
		Count  *int64   `json:"count"`
		CnySum *int64   `json:"cny_sum"`
		UsdSum *float64 `json:"usd_sum"`
	}
	err = s.entClient.AlipayOrder.Query().
		Where(
			dbalipay.UserIDEQ(userID),
			dbalipay.StatusEQ("paid"), // 与 alipay_order_repo.MarkPaid 的字面量一致，无既有常量
		).
		Aggregate(
			dbent.As(dbent.Count(), "count"),
			dbent.As(dbent.Sum(dbalipay.FieldCnyFee), "cny_sum"),
			dbent.As(dbent.Sum(dbalipay.FieldUsdAmount), "usd_sum"),
		).
		Scan(ctx, &rows)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, 0, nil
	}
	r := rows[0]
	if r.Count != nil {
		count = *r.Count
	}
	if r.CnySum != nil {
		cnyFeeTotal = *r.CnySum
	}
	if r.UsdSum != nil {
		usdTotal = *r.UsdSum
	}
	return count, cnyFeeTotal, usdTotal, nil
}
