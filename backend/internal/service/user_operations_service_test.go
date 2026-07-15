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
