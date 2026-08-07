package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

// --- stubs（嵌接口 + 覆写所需方法，同包既有模式）---

type opsUserRepoStub struct {
	UserRepository
	byID    map[int64]*User
	byEmail map[string]*User
}

func (s *opsUserRepoStub) GetByID(_ context.Context, id int64) (*User, error) {
	if u, ok := s.byID[id]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}

func (s *opsUserRepoStub) GetByEmail(_ context.Context, email string) (*User, error) {
	if u, ok := s.byEmail[email]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}

type opsRedeemRepoStub struct {
	RedeemCodeRepository
	total float64
}

func (s *opsRedeemRepoStub) SumPositiveBalanceByUser(context.Context, int64) (float64, error) {
	return s.total, nil
}

type opsUsageRepoStub struct {
	UsageLogRepository
	stats    *usagestats.UsageStats
	gotStart time.Time
	gotEnd   time.Time
}

func (s *opsUsageRepoStub) GetUserStatsAggregated(_ context.Context, _ int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	s.gotStart = startTime
	s.gotEnd = endTime
	return s.stats, nil
}

// --- helpers ---

func newUserOperationsSQLite(t *testing.T) *dbent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:user_operations_%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func mustCreateOpsUser(t *testing.T, ctx context.Context, client *dbent.Client, email, username string) *dbent.User {
	t.Helper()
	u, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-hash").
		SetRole(RoleUser).
		SetStatus(StatusActive).
		SetUsername(username).
		Save(ctx)
	require.NoError(t, err)
	return u
}

// opsChannelBaseTime 渠道测试数据的基准时间，配合显式 created_at/claimed_at 让排序断言确定。
var opsChannelBaseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func mustCreateOpsChannelBatch(
	t *testing.T, ctx context.Context, client *dbent.Client,
	name string, ownerID int64, bonus float64, createdAt time.Time,
) *dbent.ChannelInviteBatch {
	t.Helper()
	b, err := client.ChannelInviteBatch.Create().
		SetName(name).
		SetBonusAmount(bonus).
		SetStatus(ChannelInviteBatchStatusActive).
		SetCreatedBy(ownerID).
		SetCreatedAt(createdAt).
		Save(ctx)
	require.NoError(t, err)
	return b
}

func mustCreateOpsChannelCode(
	t *testing.T, ctx context.Context, client *dbent.Client,
	batchID int64, code string, usedCount int,
) *dbent.ChannelInviteCode {
	t.Helper()
	c, err := client.ChannelInviteCode.Create().
		SetBatchID(batchID).
		SetCode(code).
		SetMaxUses(usedCount + 1).
		SetUsedCount(usedCount).
		Save(ctx)
	require.NoError(t, err)
	return c
}

func mustCreateOpsChannelUsage(
	t *testing.T, ctx context.Context, client *dbent.Client,
	codeID, batchID, userID int64, granted bool, claimedAt time.Time,
) *dbent.ChannelInviteCodeUsage {
	t.Helper()
	u, err := client.ChannelInviteCodeUsage.Create().
		SetCodeID(codeID).
		SetBatchID(batchID).
		SetUserID(userID).
		SetBonusGranted(granted).
		SetClaimedAt(claimedAt).
		Save(ctx)
	require.NoError(t, err)
	return u
}

func opsServiceUserFromEnt(u *dbent.User) *User {
	return &User{
		ID:           u.ID,
		Email:        u.Email,
		Username:     u.Username,
		ReferralCode: u.ReferralCode,
		ReferredBy:   u.ReferredBy,
		CreatedAt:    u.CreatedAt,
	}
}

func newOpsService(client *dbent.Client, userRepo *opsUserRepoStub, redeem *opsRedeemRepoStub, usage *opsUsageRepoStub) *UserOperationsService {
	if redeem == nil {
		redeem = &opsRedeemRepoStub{}
	}
	if usage == nil {
		usage = &opsUsageRepoStub{stats: &usagestats.UsageStats{}}
	}
	return NewUserOperationsService(client, userRepo, redeem, usage)
}

// --- tests ---

func TestUserOperations_RequiresAccountOrUserID(t *testing.T) {
	client := newUserOperationsSQLite(t)
	svc := newOpsService(client, &opsUserRepoStub{}, nil, nil)

	_, err := svc.GetReport(context.Background(), "", 0)
	require.ErrorIs(t, err, ErrOperationsAccountRequired)
}

func TestUserOperations_UserIDBypassesAccount(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)
	row := mustCreateOpsUser(t, ctx, client, "direct@test.com", "direct")
	target := opsServiceUserFromEnt(row)

	repo := &opsUserRepoStub{byID: map[int64]*User{row.ID: target}, byEmail: map[string]*User{}}
	svc := newOpsService(client, repo, nil, nil)

	// account 随便传，user_id 优先
	res, err := svc.GetReport(ctx, "ignored@test.com", row.ID)
	require.NoError(t, err)
	require.NotNil(t, res.Report)
	require.Equal(t, row.ID, res.Report.User.ID)
	require.Empty(t, res.Candidates)
}

func TestUserOperations_EmailWinsOverUsername(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)
	// 一个用户 email 为 "alice"（极端场景），另一个用户名为 "alice"
	emailRow := mustCreateOpsUser(t, ctx, client, "alice", "someone")
	mustCreateOpsUser(t, ctx, client, "other@test.com", "alice")
	emailUser := opsServiceUserFromEnt(emailRow)

	repo := &opsUserRepoStub{
		byID:    map[int64]*User{emailRow.ID: emailUser},
		byEmail: map[string]*User{"alice": emailUser},
	}
	svc := newOpsService(client, repo, nil, nil)

	res, err := svc.GetReport(ctx, "alice", 0)
	require.NoError(t, err)
	require.NotNil(t, res.Report)
	require.Equal(t, emailRow.ID, res.Report.User.ID)
}

func TestUserOperations_UsernameSingleMatch(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)
	row := mustCreateOpsUser(t, ctx, client, "bob@test.com", "bob")

	repo := &opsUserRepoStub{
		byID:    map[int64]*User{row.ID: opsServiceUserFromEnt(row)},
		byEmail: map[string]*User{},
	}
	svc := newOpsService(client, repo, nil, nil)

	res, err := svc.GetReport(ctx, "bob", 0)
	require.NoError(t, err)
	require.NotNil(t, res.Report)
	require.Equal(t, row.ID, res.Report.User.ID)
}

func TestUserOperations_UsernameNotFound(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)
	svc := newOpsService(client, &opsUserRepoStub{byEmail: map[string]*User{}}, nil, nil)

	_, err := svc.GetReport(ctx, "nobody", 0)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestUserOperations_UsernameMultiMatchReturnsCandidates(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)
	a := mustCreateOpsUser(t, ctx, client, "a@test.com", "dup")
	b := mustCreateOpsUser(t, ctx, client, "b@test.com", "dup")

	svc := newOpsService(client, &opsUserRepoStub{byEmail: map[string]*User{}}, nil, nil)

	res, err := svc.GetReport(ctx, "dup", 0)
	require.NoError(t, err)
	require.Nil(t, res.Report)
	require.Len(t, res.Candidates, 2)
	// ID 升序确定性排序
	require.Equal(t, a.ID, res.Candidates[0].ID)
	require.Equal(t, b.ID, res.Candidates[1].ID)
	require.Equal(t, "a@test.com", res.Candidates[0].Email)
}

func TestUserOperations_ReportAssembly(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)

	inviterRow := mustCreateOpsUser(t, ctx, client, "inviter@test.com", "inviter")
	code := "AB3CD9"
	targetRow, err := client.User.Create().
		SetEmail("target@test.com").
		SetPasswordHash("test-hash").
		SetRole(RoleUser).
		SetStatus(StatusActive).
		SetUsername("target").
		SetReferralCode(code).
		SetReferredBy(inviterRow.ID).
		Save(ctx)
	require.NoError(t, err)

	// 3 个被邀请人
	for i := 0; i < 3; i++ {
		_, err := client.User.Create().
			SetEmail(fmt.Sprintf("invitee%d@test.com", i)).
			SetPasswordHash("test-hash").
			SetRole(RoleUser).
			SetStatus(StatusActive).
			SetReferredBy(targetRow.ID).
			Save(ctx)
		require.NoError(t, err)
	}

	// 支付宝订单：两笔已支付 + 一笔 pending（不计入）
	expires := time.Now().Add(30 * time.Minute)
	for i, o := range []struct {
		status string
		cny    int
		usd    float64
	}{
		{"paid", 10000, 100},
		{"paid", 29800, 300},
		{"pending", 5000, 50},
	} {
		_, err := client.AlipayOrder.Create().
			SetOrderNo(fmt.Sprintf("AP-test-%d", i)).
			SetUserID(targetRow.ID).
			SetPackageID(0).
			SetCnyFee(o.cny).
			SetUsdAmount(o.usd).
			SetStatus(o.status).
			SetExpiresAt(expires).
			Save(ctx)
		require.NoError(t, err)
	}

	target := opsServiceUserFromEnt(targetRow)
	repo := &opsUserRepoStub{
		byID: map[int64]*User{
			targetRow.ID:  target,
			inviterRow.ID: opsServiceUserFromEnt(inviterRow),
		},
		byEmail: map[string]*User{"target@test.com": target},
	}
	redeem := &opsRedeemRepoStub{total: 250}
	usage := &opsUsageRepoStub{stats: &usagestats.UsageStats{
		TotalRequests: 42, TotalInputTokens: 1000, TotalOutputTokens: 500,
		TotalCacheTokens: 250, TotalTokens: 1750, TotalCost: 8.5, TotalActualCost: 6.4,
	}}
	svc := newOpsService(client, repo, redeem, usage)

	res, err := svc.GetReport(ctx, "target@test.com", 0)
	require.NoError(t, err)
	require.NotNil(t, res.Report)
	r := res.Report

	// 邀请关系
	require.NotNil(t, r.ReferralCode)
	require.Equal(t, code, *r.ReferralCode)
	require.NotNil(t, r.Inviter)
	require.Equal(t, inviterRow.ID, r.Inviter.ID)
	require.Equal(t, "inviter@test.com", r.Inviter.Email)
	require.EqualValues(t, 3, r.InvitedCount)
	require.Len(t, r.Invitees, 3)
	require.False(t, r.InviteesTruncated)

	// 充值
	require.EqualValues(t, 2, r.AlipayPaidCount)
	require.EqualValues(t, 39800, r.AlipayCnyFeeTotal)
	require.InDelta(t, 400.0, r.AlipayUsdTotal, 1e-9)
	require.InDelta(t, 250.0, r.RedeemBalanceTotal, 1e-9)

	// token 消耗透传 + 全期时间范围（起点 Unix(0)，终点在未来）
	require.EqualValues(t, 42, r.Usage.TotalRequests)
	require.True(t, usage.gotStart.Equal(time.Unix(0, 0).UTC()))
	require.True(t, usage.gotEnd.After(time.Now()))
}

func TestUserOperations_ChannelInviteOwnerAndClaim(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)

	ownerRow := mustCreateOpsUser(t, ctx, client, "owner@test.com", "码主")
	otherOwnerRow := mustCreateOpsUser(t, ctx, client, "other-owner@test.com", "别家码主")

	// 目标用户名下两个批次；另一码主的批次不得混入。
	batchA := mustCreateOpsChannelBatch(t, ctx, client, "小红书拉新", ownerRow.ID, 50, opsChannelBaseTime)
	batchB := mustCreateOpsChannelBatch(t, ctx, client, "抖音拉新", ownerRow.ID, 30, opsChannelBaseTime.Add(time.Hour))
	foreignBatch := mustCreateOpsChannelBatch(t, ctx, client, "别家活动", otherOwnerRow.ID, 10, opsChannelBaseTime)

	codeA := mustCreateOpsChannelCode(t, ctx, client, batchA.ID, "AAA111", 2)
	codeB := mustCreateOpsChannelCode(t, ctx, client, batchB.ID, "BBB222", 1)
	foreignCode := mustCreateOpsChannelCode(t, ctx, client, foreignBatch.ID, "ZZZ999", 1)

	// 两人通过码主的码进站，另有一人走别家的码（不计入）。
	inviteeA := mustCreateOpsUser(t, ctx, client, "ia@test.com", "ia")
	inviteeB := mustCreateOpsUser(t, ctx, client, "ib@test.com", "ib")
	stranger := mustCreateOpsUser(t, ctx, client, "stranger@test.com", "stranger")
	mustCreateOpsChannelUsage(t, ctx, client, codeA.ID, batchA.ID, inviteeA.ID, true, opsChannelBaseTime.Add(time.Hour))
	mustCreateOpsChannelUsage(t, ctx, client, codeB.ID, batchB.ID, inviteeB.ID, false, opsChannelBaseTime.Add(2*time.Hour))
	mustCreateOpsChannelUsage(t, ctx, client, foreignCode.ID, foreignBatch.ID, stranger.ID, true, opsChannelBaseTime.Add(3*time.Hour))

	// 码主自己也兑换过别家的码 → claims 一条。
	mustCreateOpsChannelUsage(t, ctx, client, foreignCode.ID, foreignBatch.ID, ownerRow.ID, true, opsChannelBaseTime.Add(4*time.Hour))

	target := opsServiceUserFromEnt(ownerRow)
	repo := &opsUserRepoStub{
		byID:    map[int64]*User{ownerRow.ID: target},
		byEmail: map[string]*User{"owner@test.com": target},
	}
	svc := newOpsService(client, repo, nil, nil)

	res, err := svc.GetReport(ctx, "owner@test.com", 0)
	require.NoError(t, err)
	r := res.Report

	// referral 侧为空，渠道侧有数据：两套体系互不影响
	require.EqualValues(t, 0, r.InvitedCount)

	// 码主侧：只含自己的批次，最新在前
	require.Len(t, r.ChannelBatches, 2)
	require.Equal(t, batchB.ID, r.ChannelBatches[0].ID)
	require.Equal(t, "抖音拉新", r.ChannelBatches[0].Name)
	require.Equal(t, []string{"BBB222"}, r.ChannelBatches[0].Codes)
	require.Equal(t, 1, r.ChannelBatches[0].UsedCount)
	require.Equal(t, batchA.ID, r.ChannelBatches[1].ID)
	require.Equal(t, 2, r.ChannelBatches[1].UsedCount)
	require.InDelta(t, 50.0, r.ChannelBatches[1].BonusAmount, 1e-9)
	require.Equal(t, 2, r.ChannelCodeCount)

	// 兑换明细：只含自己批次下的 2 条
	require.EqualValues(t, 2, r.ChannelInvitedCount)
	require.Len(t, r.ChannelInvitees, 2)
	require.False(t, r.ChannelInviteesTruncated)
	gotInvitees := map[int64]UserOperationsChannelInvitee{}
	for _, iv := range r.ChannelInvitees {
		gotInvitees[iv.User.ID] = iv
	}
	require.Contains(t, gotInvitees, inviteeA.ID)
	require.Equal(t, "AAA111", gotInvitees[inviteeA.ID].Code)
	require.Equal(t, "小红书拉新", gotInvitees[inviteeA.ID].BatchName)
	require.True(t, gotInvitees[inviteeA.ID].BonusGranted)
	require.Contains(t, gotInvitees, inviteeB.ID)
	require.False(t, gotInvitees[inviteeB.ID].BonusGranted)
	require.NotContains(t, gotInvitees, stranger.ID)

	// 被邀请侧：自己兑换过别家的码
	require.Len(t, r.ChannelClaims, 1)
	claim := r.ChannelClaims[0]
	require.Equal(t, foreignBatch.ID, claim.BatchID)
	require.Equal(t, "别家活动", claim.BatchName)
	require.Equal(t, "ZZZ999", claim.Code)
	require.Equal(t, otherOwnerRow.ID, claim.OwnerID)
	require.NotNil(t, claim.Owner)
	require.Equal(t, "other-owner@test.com", claim.Owner.Email)
	require.InDelta(t, 10.0, claim.BonusAmount, 1e-9)
	require.True(t, claim.BonusGranted)
}

func TestUserOperations_ChannelInviteEmptyForPlainUser(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)
	row := mustCreateOpsUser(t, ctx, client, "plain@test.com", "plain")

	target := opsServiceUserFromEnt(row)
	repo := &opsUserRepoStub{
		byID:    map[int64]*User{row.ID: target},
		byEmail: map[string]*User{"plain@test.com": target},
	}
	svc := newOpsService(client, repo, nil, nil)

	res, err := svc.GetReport(ctx, "plain@test.com", 0)
	require.NoError(t, err)
	r := res.Report
	// 非码主也非被邀请：全部为空切片而非 nil（JSON 输出 [] 而不是 null）
	require.NotNil(t, r.ChannelClaims)
	require.Empty(t, r.ChannelClaims)
	require.NotNil(t, r.ChannelBatches)
	require.Empty(t, r.ChannelBatches)
	require.NotNil(t, r.ChannelInvitees)
	require.Empty(t, r.ChannelInvitees)
	require.EqualValues(t, 0, r.ChannelInvitedCount)
	require.Equal(t, 0, r.ChannelCodeCount)
	require.False(t, r.ChannelInviteesTruncated)
}

func TestUserOperations_ChannelInviteesTruncated(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)
	ownerRow := mustCreateOpsUser(t, ctx, client, "bigowner@test.com", "bigowner")
	batch := mustCreateOpsChannelBatch(t, ctx, client, "大批次", ownerRow.ID, 5, opsChannelBaseTime)
	code := mustCreateOpsChannelCode(t, ctx, client, batch.ID, "CAP001", operationsChannelInviteesCap+1)

	for i := 0; i < operationsChannelInviteesCap+1; i++ {
		u := mustCreateOpsUser(t, ctx, client, fmt.Sprintf("cap%d@test.com", i), fmt.Sprintf("cap%d", i))
		mustCreateOpsChannelUsage(t, ctx, client, code.ID, batch.ID, u.ID, true, opsChannelBaseTime.Add(time.Duration(i)*time.Minute))
	}

	target := opsServiceUserFromEnt(ownerRow)
	repo := &opsUserRepoStub{
		byID:    map[int64]*User{ownerRow.ID: target},
		byEmail: map[string]*User{"bigowner@test.com": target},
	}
	svc := newOpsService(client, repo, nil, nil)

	res, err := svc.GetReport(ctx, "bigowner@test.com", 0)
	require.NoError(t, err)
	require.EqualValues(t, operationsChannelInviteesCap+1, res.Report.ChannelInvitedCount)
	require.Len(t, res.Report.ChannelInvitees, operationsChannelInviteesCap)
	require.True(t, res.Report.ChannelInviteesTruncated)
}

func TestUserOperations_DeletedInviterTolerated(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)

	missingInviterID := int64(99999)
	row, err := client.User.Create().
		SetEmail("orphan@test.com").
		SetPasswordHash("test-hash").
		SetRole(RoleUser).
		SetStatus(StatusActive).
		SetReferredBy(missingInviterID).
		Save(ctx)
	require.NoError(t, err)

	target := opsServiceUserFromEnt(row)
	repo := &opsUserRepoStub{
		byID:    map[int64]*User{row.ID: target}, // 邀请人 ID 不在 map 中 → ErrUserNotFound
		byEmail: map[string]*User{"orphan@test.com": target},
	}
	svc := newOpsService(client, repo, nil, nil)

	res, err := svc.GetReport(ctx, "orphan@test.com", 0)
	require.NoError(t, err)
	r := res.Report
	require.Nil(t, r.Inviter)
	require.NotNil(t, r.InviterID)
	require.Equal(t, missingInviterID, *r.InviterID)
	require.Nil(t, r.ReferralCode) // 从未生成的邀请码原样透传 nil
}

func TestUserOperations_InviteesTruncated(t *testing.T) {
	ctx := context.Background()
	client := newUserOperationsSQLite(t)
	row := mustCreateOpsUser(t, ctx, client, "big@test.com", "big")

	for i := 0; i < operationsInviteesCap+1; i++ {
		_, err := client.User.Create().
			SetEmail(fmt.Sprintf("bulk%d@test.com", i)).
			SetPasswordHash("test-hash").
			SetRole(RoleUser).
			SetStatus(StatusActive).
			SetReferredBy(row.ID).
			Save(ctx)
		require.NoError(t, err)
	}

	target := opsServiceUserFromEnt(row)
	repo := &opsUserRepoStub{
		byID:    map[int64]*User{row.ID: target},
		byEmail: map[string]*User{"big@test.com": target},
	}
	svc := newOpsService(client, repo, nil, nil)

	res, err := svc.GetReport(ctx, "big@test.com", 0)
	require.NoError(t, err)
	require.EqualValues(t, operationsInviteesCap+1, res.Report.InvitedCount)
	require.Len(t, res.Report.Invitees, operationsInviteesCap)
	require.True(t, res.Report.InviteesTruncated)
}
