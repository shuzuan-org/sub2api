package service

import (
	"context"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbalipay "github.com/Wei-Shaw/sub2api/ent/alipayorder"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// operationsInviteesCap 运营报告中被邀请人明细的最大条数（invited_count 仍为真实总数）。
const operationsInviteesCap = 100

// operationsUsernameMatchLimit username 非唯一，按用户名查询候选用户的上限。
const operationsUsernameMatchLimit = 50

// ErrOperationsAccountRequired 未提供 account 且未提供 user_id。
var ErrOperationsAccountRequired = infraerrors.BadRequest("OPERATIONS_ACCOUNT_REQUIRED", "account or user_id is required")

// UserOperationsBrief 报告中引用的其他用户（邀请人/被邀请人/候选人）的精简信息。
type UserOperationsBrief struct {
	ID        int64
	Email     string
	Username  string
	CreatedAt time.Time
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
