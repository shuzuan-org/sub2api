package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/userallowedgroup"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newChannelInviteE2ETestClient(t *testing.T) (*dbent.Client, service.UserRepository, service.ChannelInviteRepository) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:channel_invite_e2e?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return client, NewUserRepository(client, db), NewChannelInviteRepository(client)
}

func createChannelInviteTestUser(t *testing.T, ctx context.Context, client *dbent.Client, email string, balance float64, phone *string) *dbent.User {
	t.Helper()
	b := client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		SetBalance(balance)
	if phone != nil {
		now := time.Now()
		b.SetPhoneNumber(*phone).SetPhoneBoundAt(now).SetPhoneBonusGrantedAt(now)
	}
	u, err := b.Save(ctx)
	require.NoError(t, err)
	return u
}

func TestChannelInviteEndToEndDeferredBonusAfterPhoneBinding(t *testing.T) {
	client, userRepo, inviteRepo := newChannelInviteE2ETestClient(t)
	ctx := context.Background()

	adminUser := createChannelInviteTestUser(t, ctx, client, "channel-admin@test.local", 0, nil)
	invitee := createChannelInviteTestUser(t, ctx, client, "channel-invitee@test.local", 0, nil)
	group, err := client.Group.Create().SetName("channel-minimax-extra").SetPlatform("minimax").Save(ctx)
	require.NoError(t, err)

	inviteSvc := service.NewChannelInviteService(inviteRepo, userRepo, client, nil, nil)

	start := time.Now().Add(-time.Minute)
	end := time.Now().Add(time.Hour)
	batch, err := inviteSvc.CreateBatch(ctx, &service.CreateChannelInviteBatchInput{
		Name:           "E2E 渠道邀请码批次",
		BonusAmount:    25,
		MaxUsesPerCode: 1,
		StartTime:      &start,
		EndTime:        &end,
		CreatedBy:      adminUser.ID,
		GroupIDs:       []int64{group.ID},
	})
	require.NoError(t, err)
	require.NotZero(t, batch.ID)

	codes, err := inviteSvc.GenerateCodes(ctx, batch.ID, 1)
	require.NoError(t, err)
	require.Len(t, codes, 1)
	require.NotEmpty(t, codes[0].Code)

	require.NoError(t, inviteSvc.ClaimCode(ctx, invitee.ID, codes[0].Code))

	allowed, err := client.UserAllowedGroup.Query().
		Where(userallowedgroup.UserIDEQ(invitee.ID), userallowedgroup.GroupIDEQ(group.ID)).
		Exist(ctx)
	require.NoError(t, err)
	require.True(t, allowed, "claiming a channel invite should add the user to the configured group")

	usages, _, err := inviteSvc.ListUsages(ctx, batch.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, usages, 1)
	require.False(t, usages[0].BonusGranted, "bonus must stay pending before phone binding")

	userAfterClaim, err := userRepo.GetByID(ctx, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, 0.0, userAfterClaim.Balance)

	_, err = userRepo.BindPhoneAndGrantBonus(ctx, invitee.ID, "+8613910000001", 100)
	require.NoError(t, err)
	granted, err := inviteSvc.GrantPendingBonuses(ctx, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, 25.0, granted)

	userAfterPhone, err := userRepo.GetByID(ctx, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, 125.0, userAfterPhone.Balance, "phone bonus + channel invite bonus should both be granted")

	usages, _, err = inviteSvc.ListUsages(ctx, batch.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, usages, 1)
	require.True(t, usages[0].BonusGranted)
	require.NotNil(t, usages[0].BonusGrantedAt)

	otherUser := createChannelInviteTestUser(t, ctx, client, "channel-other@test.local", 0, nil)
	err = inviteSvc.ClaimCode(ctx, otherUser.ID, codes[0].Code)
	require.ErrorIs(t, err, service.ErrChannelInviteCodeMaxUsed)
}

func TestChannelInviteEndToEndImmediateBonusForPhoneBoundUser(t *testing.T) {
	client, userRepo, inviteRepo := newChannelInviteE2ETestClient(t)
	ctx := context.Background()

	adminUser := createChannelInviteTestUser(t, ctx, client, "channel-admin-2@test.local", 0, nil)
	phone := "+8613910000002"
	invitee := createChannelInviteTestUser(t, ctx, client, "channel-invitee-bound@test.local", 0, &phone)
	group, err := client.Group.Create().SetName("channel-minimax-bound-extra").SetPlatform("minimax").Save(ctx)
	require.NoError(t, err)

	inviteSvc := service.NewChannelInviteService(inviteRepo, userRepo, client, nil, nil)

	batch, err := inviteSvc.CreateBatch(ctx, &service.CreateChannelInviteBatchInput{
		Name:           "E2E 已绑手机立即到账批次",
		BonusAmount:    30,
		MaxUsesPerCode: 1,
		CreatedBy:      adminUser.ID,
		GroupIDs:       []int64{group.ID},
	})
	require.NoError(t, err)

	codes, err := inviteSvc.GenerateCodes(ctx, batch.ID, 1)
	require.NoError(t, err)
	require.Len(t, codes, 1)

	require.NoError(t, inviteSvc.ClaimCode(ctx, invitee.ID, codes[0].Code))

	userAfterClaim, err := userRepo.GetByID(ctx, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, 30.0, userAfterClaim.Balance, "phone-bound user should receive channel bonus immediately")

	usages, _, err := inviteSvc.ListUsages(ctx, batch.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, usages, 1)
	require.True(t, usages[0].BonusGranted)
	require.NotNil(t, usages[0].BonusGrantedAt)
}
