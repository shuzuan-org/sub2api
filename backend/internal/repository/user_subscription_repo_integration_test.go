//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

type UserSubscriptionRepoSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	repo   *userSubscriptionRepository
}

func (s *UserSubscriptionRepoSuite) SetupTest() {
	s.ctx = context.Background()
	tx := testEntTx(s.T())
	s.client = tx.Client()
	s.repo = NewUserSubscriptionRepository(s.client).(*userSubscriptionRepository)
}

func TestUserSubscriptionRepoSuite(t *testing.T) {
	suite.Run(t, new(UserSubscriptionRepoSuite))
}

func (s *UserSubscriptionRepoSuite) mustCreateUser(email string, role string) *service.User {
	s.T().Helper()

	if role == "" {
		role = service.RoleUser
	}

	u, err := s.client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetStatus(service.StatusActive).
		SetRole(role).
		Save(s.ctx)
	s.Require().NoError(err, "create user")
	return userEntityToService(u)
}

func (s *UserSubscriptionRepoSuite) mustCreateGroup(name string) *service.Group {
	s.T().Helper()

	g, err := s.client.Group.Create().
		SetName(name).
		SetStatus(service.StatusActive).
		Save(s.ctx)
	s.Require().NoError(err, "create group")
	return groupEntityToService(g)
}

func (s *UserSubscriptionRepoSuite) mustCreatePlan(name string) *service.SubscriptionPlan {
	s.T().Helper()

	p, err := s.client.SubscriptionPlan.Create().
		SetName(name).
		SetStatus(service.StatusActive).
		SetVisibility(service.VisibilityPublic).
		Save(s.ctx)
	s.Require().NoError(err, "create subscription plan")
	return subscriptionPlanEntityToService(p)
}

func (s *UserSubscriptionRepoSuite) mustCreateSubscription(userID, planID int64, mutate func(*dbent.UserSubscriptionCreate)) *dbent.UserSubscription {
	s.T().Helper()

	now := time.Now()
	create := s.client.UserSubscription.Create().
		SetUserID(userID).
		SetPlanID(planID).
		SetStartsAt(now.Add(-1 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("")

	if mutate != nil {
		mutate(create)
	}

	sub, err := create.Save(s.ctx)
	s.Require().NoError(err, "create user subscription")
	return sub
}

// --- Create / GetByID / Update / Delete ---

func (s *UserSubscriptionRepoSuite) TestCreate() {
	user := s.mustCreateUser("sub-create@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-create")

	sub := &service.UserSubscription{
		UserID:    user.ID,
		PlanID:    plan.ID,
		Status:    service.SubscriptionStatusActive,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	err := s.repo.Create(s.ctx, sub)
	s.Require().NoError(err, "Create")
	s.Require().NotZero(sub.ID, "expected ID to be set")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().Equal(sub.UserID, got.UserID)
	s.Require().Equal(sub.PlanID, got.PlanID)
}

func (s *UserSubscriptionRepoSuite) TestGetByID_WithPreloads() {
	user := s.mustCreateUser("preload@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-preload")
	admin := s.mustCreateUser("admin@test.com", service.RoleAdmin)

	sub := s.mustCreateSubscription(user.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetAssignedBy(admin.ID)
	})

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().NotNil(got.User, "expected User preload")
	s.Require().NotNil(got.Plan, "expected Plan preload")
	s.Require().NotNil(got.AssignedByUser, "expected AssignedByUser preload")
	s.Require().Equal(user.ID, got.User.ID)
	s.Require().Equal(plan.ID, got.Plan.ID)
	s.Require().Equal(admin.ID, got.AssignedByUser.ID)
}

func (s *UserSubscriptionRepoSuite) TestGetByID_NotFound() {
	_, err := s.repo.GetByID(s.ctx, 999999)
	s.Require().Error(err, "expected error for non-existent ID")
}

func (s *UserSubscriptionRepoSuite) TestUpdate() {
	user := s.mustCreateUser("update@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-update")
	created := s.mustCreateSubscription(user.ID, plan.ID, nil)

	sub, err := s.repo.GetByID(s.ctx, created.ID)
	s.Require().NoError(err, "GetByID")

	sub.Notes = "updated notes"
	s.Require().NoError(s.repo.Update(s.ctx, sub), "Update")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err, "GetByID after update")
	s.Require().Equal("updated notes", got.Notes)
}

func (s *UserSubscriptionRepoSuite) TestDelete() {
	user := s.mustCreateUser("delete@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-delete")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	err := s.repo.Delete(s.ctx, sub.ID)
	s.Require().NoError(err, "Delete")

	_, err = s.repo.GetByID(s.ctx, sub.ID)
	s.Require().Error(err, "expected error after delete")
}

func (s *UserSubscriptionRepoSuite) TestDelete_Idempotent() {
	s.Require().NoError(s.repo.Delete(s.ctx, 42424242), "Delete should be idempotent")
}

// --- GetByUserIDAndPlanID / GetActiveByUserIDAndPlanID ---

func (s *UserSubscriptionRepoSuite) TestGetByUserIDAndPlanID() {
	user := s.mustCreateUser("byuser@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-byuser")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	got, err := s.repo.GetByUserIDAndPlanID(s.ctx, user.ID, plan.ID)
	s.Require().NoError(err, "GetByUserIDAndPlanID")
	s.Require().Equal(sub.ID, got.ID)
	s.Require().NotNil(got.Plan, "expected Plan preload")
}

func (s *UserSubscriptionRepoSuite) TestGetByUserIDAndPlanID_NotFound() {
	_, err := s.repo.GetByUserIDAndPlanID(s.ctx, 999999, 999999)
	s.Require().Error(err, "expected error for non-existent pair")
}

func (s *UserSubscriptionRepoSuite) TestGetActiveByUserIDAndPlanID() {
	user := s.mustCreateUser("active@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-active")

	active := s.mustCreateSubscription(user.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(2 * time.Hour))
	})

	got, err := s.repo.GetActiveByUserIDAndPlanID(s.ctx, user.ID, plan.ID)
	s.Require().NoError(err, "GetActiveByUserIDAndPlanID")
	s.Require().Equal(active.ID, got.ID)
}

func (s *UserSubscriptionRepoSuite) TestGetActiveByUserIDAndPlanID_ExpiredIgnored() {
	user := s.mustCreateUser("expired@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-expired")

	s.mustCreateSubscription(user.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-2 * time.Hour))
	})

	_, err := s.repo.GetActiveByUserIDAndPlanID(s.ctx, user.ID, plan.ID)
	s.Require().Error(err, "expected error for expired subscription")
}

// 叠加购买：同一 (user, plan) 重复购买会新建记录而非续期，有效期重叠期间
// 并存多条 active 记录。查询必须返回过期最晚的一条，而不是 NotSingularError
// ——后者会被计费层包成 503 并推开全局熔断器。
func (s *UserSubscriptionRepoSuite) TestGetActiveByUserIDAndPlanID_OverlappingStacked() {
	user := s.mustCreateUser("stacked@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-stacked")

	s.mustCreateSubscription(user.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	later := s.mustCreateSubscription(user.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(72 * time.Hour))
	})

	got, err := s.repo.GetActiveByUserIDAndPlanID(s.ctx, user.ID, plan.ID)
	s.Require().NoError(err, "overlapping active subscriptions must not error")
	s.Require().Equal(later.ID, got.ID, "expected the latest-expiring subscription")
	s.Require().NotNil(got.Plan, "expected Plan preload")
}

// --- ListByUserID / ListActiveByUserID ---

func (s *UserSubscriptionRepoSuite) TestListByUserID() {
	user := s.mustCreateUser("listby@test.com", service.RoleUser)
	p1 := s.mustCreatePlan("p-list1")
	p2 := s.mustCreatePlan("p-list2")

	s.mustCreateSubscription(user.ID, p1.ID, nil)
	s.mustCreateSubscription(user.ID, p2.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	subs, err := s.repo.ListByUserID(s.ctx, user.ID)
	s.Require().NoError(err, "ListByUserID")
	s.Require().Len(subs, 2)
	for _, sub := range subs {
		s.Require().NotNil(sub.Plan, "expected Plan preload")
	}
}

func (s *UserSubscriptionRepoSuite) TestListActiveByUserID() {
	user := s.mustCreateUser("listactive@test.com", service.RoleUser)
	p1 := s.mustCreatePlan("p-act1")
	p2 := s.mustCreatePlan("p-act2")

	s.mustCreateSubscription(user.ID, p1.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	s.mustCreateSubscription(user.ID, p2.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	subs, err := s.repo.ListActiveByUserID(s.ctx, user.ID)
	s.Require().NoError(err, "ListActiveByUserID")
	s.Require().Len(subs, 1)
	s.Require().Equal(service.SubscriptionStatusActive, subs[0].Status)
}

// --- ListByPlanID ---

func (s *UserSubscriptionRepoSuite) TestListByPlanID() {
	user1 := s.mustCreateUser("u1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("u2@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-listplan")

	s.mustCreateSubscription(user1.ID, plan.ID, nil)
	s.mustCreateSubscription(user2.ID, plan.ID, nil)

	subs, page, err := s.repo.ListByPlanID(s.ctx, plan.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	s.Require().NoError(err, "ListByPlanID")
	s.Require().Len(subs, 2)
	s.Require().Equal(int64(2), page.Total)
	for _, sub := range subs {
		s.Require().NotNil(sub.User, "expected User preload")
		s.Require().NotNil(sub.Plan, "expected Plan preload")
	}
}

// --- List with filters ---

func (s *UserSubscriptionRepoSuite) TestList_NoFilters() {
	user := s.mustCreateUser("list@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-list")
	s.mustCreateSubscription(user.ID, plan.ID, nil)

	subs, page, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, nil, nil, "", "", "")
	s.Require().NoError(err, "List")
	s.Require().Len(subs, 1)
	s.Require().Equal(int64(1), page.Total)
}

func (s *UserSubscriptionRepoSuite) TestList_FilterByUserID() {
	user1 := s.mustCreateUser("filter1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("filter2@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-filter")

	s.mustCreateSubscription(user1.ID, plan.ID, nil)
	s.mustCreateSubscription(user2.ID, plan.ID, nil)

	subs, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, &user1.ID, nil, "", "", "")
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	s.Require().Equal(user1.ID, subs[0].UserID)
}

func (s *UserSubscriptionRepoSuite) TestList_FilterByPlanID() {
	user := s.mustCreateUser("planfilter@test.com", service.RoleUser)
	p1 := s.mustCreatePlan("p-f1")
	p2 := s.mustCreatePlan("p-f2")

	s.mustCreateSubscription(user.ID, p1.ID, nil)
	s.mustCreateSubscription(user.ID, p2.ID, nil)

	subs, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, nil, &p1.ID, "", "", "")
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	s.Require().Equal(p1.ID, subs[0].PlanID)
}

func (s *UserSubscriptionRepoSuite) TestList_FilterByStatus() {
	user1 := s.mustCreateUser("statfilter1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("statfilter2@test.com", service.RoleUser)
	plan1 := s.mustCreatePlan("p-stat-1")
	plan2 := s.mustCreatePlan("p-stat-2")

	s.mustCreateSubscription(user1.ID, plan1.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusActive)
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	s.mustCreateSubscription(user2.ID, plan2.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	subs, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, nil, nil, service.SubscriptionStatusExpired, "", "")
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	s.Require().Equal(service.SubscriptionStatusExpired, subs[0].Status)
}

// --- Usage tracking ---

func (s *UserSubscriptionRepoSuite) TestIncrementUsage() {
	user := s.mustCreateUser("usage@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-usage")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	err := s.repo.IncrementUsage(s.ctx, sub.ID, 1.25)
	s.Require().NoError(err, "IncrementUsage")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(1.25, got.DailyUsageUSD, 1e-6)
	s.Require().InDelta(1.25, got.WeeklyUsageUSD, 1e-6)
	s.Require().InDelta(1.25, got.MonthlyUsageUSD, 1e-6)
}

func (s *UserSubscriptionRepoSuite) TestIncrementUsage_Accumulates() {
	user := s.mustCreateUser("accum@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-accum")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	s.Require().NoError(s.repo.IncrementUsage(s.ctx, sub.ID, 1.0))
	s.Require().NoError(s.repo.IncrementUsage(s.ctx, sub.ID, 2.5))

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(3.5, got.DailyUsageUSD, 1e-6)
}

func (s *UserSubscriptionRepoSuite) TestActivateWindows() {
	user := s.mustCreateUser("activate@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-activate")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	activateAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	err := s.repo.ActivateWindows(s.ctx, sub.ID, activateAt)
	s.Require().NoError(err, "ActivateWindows")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().NotNil(got.DailyWindowStart)
	s.Require().NotNil(got.WeeklyWindowStart)
	s.Require().NotNil(got.MonthlyWindowStart)
	s.Require().WithinDuration(activateAt, *got.DailyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestResetDailyUsage() {
	user := s.mustCreateUser("resetd@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-resetd")
	sub := s.mustCreateSubscription(user.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetDailyUsageUsd(10.0)
		c.SetWeeklyUsageUsd(20.0)
	})

	resetAt := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	err := s.repo.ResetDailyUsage(s.ctx, sub.ID, resetAt)
	s.Require().NoError(err, "ResetDailyUsage")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0.0, got.DailyUsageUSD, 1e-6)
	s.Require().InDelta(20.0, got.WeeklyUsageUSD, 1e-6)
	s.Require().NotNil(got.DailyWindowStart)
	s.Require().WithinDuration(resetAt, *got.DailyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestResetWeeklyUsage() {
	user := s.mustCreateUser("resetw@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-resetw")
	sub := s.mustCreateSubscription(user.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetWeeklyUsageUsd(15.0)
		c.SetMonthlyUsageUsd(30.0)
	})

	resetAt := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)
	err := s.repo.ResetWeeklyUsage(s.ctx, sub.ID, resetAt)
	s.Require().NoError(err, "ResetWeeklyUsage")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0.0, got.WeeklyUsageUSD, 1e-6)
	s.Require().InDelta(30.0, got.MonthlyUsageUSD, 1e-6)
	s.Require().NotNil(got.WeeklyWindowStart)
	s.Require().WithinDuration(resetAt, *got.WeeklyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestResetMonthlyUsage() {
	user := s.mustCreateUser("resetm@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-resetm")
	sub := s.mustCreateSubscription(user.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetMonthlyUsageUsd(25.0)
	})

	resetAt := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	err := s.repo.ResetMonthlyUsage(s.ctx, sub.ID, resetAt)
	s.Require().NoError(err, "ResetMonthlyUsage")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0.0, got.MonthlyUsageUSD, 1e-6)
	s.Require().NotNil(got.MonthlyWindowStart)
	s.Require().WithinDuration(resetAt, *got.MonthlyWindowStart, time.Microsecond)
}

// --- UpdateStatus / ExtendExpiry / UpdateNotes ---

func (s *UserSubscriptionRepoSuite) TestUpdateStatus() {
	user := s.mustCreateUser("status@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-status")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	err := s.repo.UpdateStatus(s.ctx, sub.ID, service.SubscriptionStatusExpired)
	s.Require().NoError(err, "UpdateStatus")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().Equal(service.SubscriptionStatusExpired, got.Status)
}

func (s *UserSubscriptionRepoSuite) TestExtendExpiry() {
	user := s.mustCreateUser("extend@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-extend")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	newExpiry := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err := s.repo.ExtendExpiry(s.ctx, sub.ID, newExpiry)
	s.Require().NoError(err, "ExtendExpiry")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().WithinDuration(newExpiry, got.ExpiresAt, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestUpdateNotes() {
	user := s.mustCreateUser("notes@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-notes")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	err := s.repo.UpdateNotes(s.ctx, sub.ID, "VIP user")
	s.Require().NoError(err, "UpdateNotes")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().Equal("VIP user", got.Notes)
}

// --- ListExpired / BatchUpdateExpiredStatus ---

func (s *UserSubscriptionRepoSuite) TestListExpired() {
	user := s.mustCreateUser("listexp@test.com", service.RoleUser)
	planActive := s.mustCreatePlan("p-listexp-active")
	planExpired := s.mustCreatePlan("p-listexp-expired")

	s.mustCreateSubscription(user.ID, planActive.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	s.mustCreateSubscription(user.ID, planExpired.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	expired, err := s.repo.ListExpired(s.ctx)
	s.Require().NoError(err, "ListExpired")
	s.Require().Len(expired, 1)
}

func (s *UserSubscriptionRepoSuite) TestBatchUpdateExpiredStatus() {
	user := s.mustCreateUser("batch@test.com", service.RoleUser)
	planFuture := s.mustCreatePlan("p-batch-future")
	planPast := s.mustCreatePlan("p-batch-past")

	active := s.mustCreateSubscription(user.ID, planFuture.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	expiredActive := s.mustCreateSubscription(user.ID, planPast.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	affected, affectedUsers, err := s.repo.BatchUpdateExpiredStatus(s.ctx)
	s.Require().NoError(err, "BatchUpdateExpiredStatus")
	s.Require().Equal(int64(1), affected)
	s.Require().Equal([]int64{user.ID}, affectedUsers)

	gotActive, _ := s.repo.GetByID(s.ctx, active.ID)
	s.Require().Equal(service.SubscriptionStatusActive, gotActive.Status)

	gotExpired, _ := s.repo.GetByID(s.ctx, expiredActive.ID)
	s.Require().Equal(service.SubscriptionStatusExpired, gotExpired.Status)
}

// --- ExistsByUserIDAndPlanID ---

func (s *UserSubscriptionRepoSuite) TestExistsByUserIDAndPlanID() {
	user := s.mustCreateUser("exists@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-exists")

	s.mustCreateSubscription(user.ID, plan.ID, nil)

	exists, err := s.repo.ExistsByUserIDAndPlanID(s.ctx, user.ID, plan.ID)
	s.Require().NoError(err, "ExistsByUserIDAndPlanID")
	s.Require().True(exists)

	notExists, err := s.repo.ExistsByUserIDAndPlanID(s.ctx, user.ID, 999999)
	s.Require().NoError(err)
	s.Require().False(notExists)
}

// --- CountByPlanID / CountActiveByPlanID ---

func (s *UserSubscriptionRepoSuite) TestCountByPlanID() {
	user1 := s.mustCreateUser("cnt1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("cnt2@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-count")

	s.mustCreateSubscription(user1.ID, plan.ID, nil)
	s.mustCreateSubscription(user2.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	count, err := s.repo.CountByPlanID(s.ctx, plan.ID)
	s.Require().NoError(err, "CountByPlanID")
	s.Require().Equal(int64(2), count)
}

func (s *UserSubscriptionRepoSuite) TestCountActiveByPlanID() {
	user1 := s.mustCreateUser("cntact1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("cntact2@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-cntact")

	s.mustCreateSubscription(user1.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	s.mustCreateSubscription(user2.ID, plan.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour)) // expired by time
	})

	count, err := s.repo.CountActiveByPlanID(s.ctx, plan.ID)
	s.Require().NoError(err, "CountActiveByPlanID")
	s.Require().Equal(int64(1), count, "only future expiry counts as active")
}

// --- DeleteByPlanID ---

func (s *UserSubscriptionRepoSuite) TestDeleteByPlanID() {
	user1 := s.mustCreateUser("delplan1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("delplan2@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-delplan")

	s.mustCreateSubscription(user1.ID, plan.ID, nil)
	s.mustCreateSubscription(user2.ID, plan.ID, nil)

	affected, err := s.repo.DeleteByPlanID(s.ctx, plan.ID)
	s.Require().NoError(err, "DeleteByPlanID")
	s.Require().Equal(int64(2), affected)

	count, _ := s.repo.CountByPlanID(s.ctx, plan.ID)
	s.Require().Zero(count)
}

// --- Combined scenario ---

func (s *UserSubscriptionRepoSuite) TestActiveExpiredBoundaries_UsageAndReset_BatchUpdateExpiredStatus() {
	user := s.mustCreateUser("subr@example.com", service.RoleUser)
	planActive := s.mustCreatePlan("p-subr-active")
	planExpired := s.mustCreatePlan("p-subr-expired")

	active := s.mustCreateSubscription(user.ID, planActive.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(2 * time.Hour))
	})
	expiredActive := s.mustCreateSubscription(user.ID, planExpired.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-2 * time.Hour))
	})

	got, err := s.repo.GetActiveByUserIDAndPlanID(s.ctx, user.ID, planActive.ID)
	s.Require().NoError(err, "GetActiveByUserIDAndPlanID")
	s.Require().Equal(active.ID, got.ID, "expected active subscription")

	activateAt := time.Now().Add(-25 * time.Hour)
	s.Require().NoError(s.repo.ActivateWindows(s.ctx, active.ID, activateAt), "ActivateWindows")
	s.Require().NoError(s.repo.IncrementUsage(s.ctx, active.ID, 1.25), "IncrementUsage")

	after, err := s.repo.GetByID(s.ctx, active.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().InDelta(1.25, after.DailyUsageUSD, 1e-6)
	s.Require().InDelta(1.25, after.WeeklyUsageUSD, 1e-6)
	s.Require().InDelta(1.25, after.MonthlyUsageUSD, 1e-6)
	s.Require().NotNil(after.DailyWindowStart, "expected DailyWindowStart activated")
	s.Require().NotNil(after.WeeklyWindowStart, "expected WeeklyWindowStart activated")
	s.Require().NotNil(after.MonthlyWindowStart, "expected MonthlyWindowStart activated")

	resetAt := time.Now().Truncate(time.Microsecond) // truncate to microsecond for DB precision
	s.Require().NoError(s.repo.ResetDailyUsage(s.ctx, active.ID, resetAt), "ResetDailyUsage")
	afterReset, err := s.repo.GetByID(s.ctx, active.ID)
	s.Require().NoError(err, "GetByID after reset")
	s.Require().InDelta(0.0, afterReset.DailyUsageUSD, 1e-6)
	s.Require().NotNil(afterReset.DailyWindowStart)
	s.Require().WithinDuration(resetAt, *afterReset.DailyWindowStart, time.Microsecond)

	affected, _, err := s.repo.BatchUpdateExpiredStatus(s.ctx)
	s.Require().NoError(err, "BatchUpdateExpiredStatus")
	s.Require().Equal(int64(1), affected, "expected 1 affected row")

	updated, err := s.repo.GetByID(s.ctx, expiredActive.ID)
	s.Require().NoError(err, "GetByID expired")
	s.Require().Equal(service.SubscriptionStatusExpired, updated.Status, "expected status expired")
}

// --- 软删除过滤测试 ---

func (s *UserSubscriptionRepoSuite) TestIncrementUsage_SoftDeletedPlan() {
	user := s.mustCreateUser("softdeleted@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-softdeleted")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	// 软删除订阅计划
	_, err := s.client.SubscriptionPlan.UpdateOneID(plan.ID).SetDeletedAt(time.Now()).Save(s.ctx)
	s.Require().NoError(err, "soft delete plan")

	// IncrementUsage 应该失败，因为计划已软删除
	err = s.repo.IncrementUsage(s.ctx, sub.ID, 1.0)
	s.Require().Error(err, "should fail for soft-deleted plan")
	s.Require().ErrorIs(err, service.ErrSubscriptionNotFound)
}

func (s *UserSubscriptionRepoSuite) TestIncrementUsage_NotFound() {
	err := s.repo.IncrementUsage(s.ctx, 999999, 1.0)
	s.Require().Error(err, "should fail for non-existent subscription")
	s.Require().ErrorIs(err, service.ErrSubscriptionNotFound)
}

// --- nil 入参测试 ---

func (s *UserSubscriptionRepoSuite) TestCreate_NilInput() {
	err := s.repo.Create(s.ctx, nil)
	s.Require().Error(err, "Create should fail with nil input")
	s.Require().ErrorIs(err, service.ErrSubscriptionNilInput)
}

func (s *UserSubscriptionRepoSuite) TestUpdate_NilInput() {
	err := s.repo.Update(s.ctx, nil)
	s.Require().Error(err, "Update should fail with nil input")
	s.Require().ErrorIs(err, service.ErrSubscriptionNilInput)
}

// --- 并发用量更新测试 ---

func (s *UserSubscriptionRepoSuite) TestIncrementUsage_Concurrent() {
	user := s.mustCreateUser("concurrent@test.com", service.RoleUser)
	plan := s.mustCreatePlan("p-concurrent")
	sub := s.mustCreateSubscription(user.ID, plan.ID, nil)

	const numGoroutines = 10
	const incrementPerGoroutine = 1.5

	// 启动多个 goroutine 并发调用 IncrementUsage
	errCh := make(chan error, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			errCh <- s.repo.IncrementUsage(s.ctx, sub.ID, incrementPerGoroutine)
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < numGoroutines; i++ {
		err := <-errCh
		s.Require().NoError(err, "IncrementUsage should succeed")
	}

	// 验证累加结果正确
	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	expectedUsage := float64(numGoroutines) * incrementPerGoroutine
	s.Require().InDelta(expectedUsage, got.DailyUsageUSD, 1e-6, "daily usage should be correctly accumulated")
	s.Require().InDelta(expectedUsage, got.WeeklyUsageUSD, 1e-6, "weekly usage should be correctly accumulated")
	s.Require().InDelta(expectedUsage, got.MonthlyUsageUSD, 1e-6, "monthly usage should be correctly accumulated")
}

func (s *UserSubscriptionRepoSuite) TestTxContext_RollbackIsolation() {
	baseClient := testEntClient(s.T())
	tx, err := baseClient.Tx(context.Background())
	s.Require().NoError(err, "begin tx")
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	txCtx := dbent.NewTxContext(context.Background(), tx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	userEnt, err := tx.Client().User.Create().
		SetEmail("tx-user-" + suffix + "@example.com").
		SetPasswordHash("test").
		Save(txCtx)
	s.Require().NoError(err, "create user in tx")

	planEnt, err := tx.Client().SubscriptionPlan.Create().
		SetName("tx-plan-" + suffix).
		SetStatus(service.StatusActive).
		SetVisibility(service.VisibilityPublic).
		Save(txCtx)
	s.Require().NoError(err, "create plan in tx")

	repo := NewUserSubscriptionRepository(baseClient)
	sub := &service.UserSubscription{
		UserID:     userEnt.ID,
		PlanID:     planEnt.ID,
		ExpiresAt:  time.Now().AddDate(0, 0, 30),
		Status:     service.SubscriptionStatusActive,
		AssignedAt: time.Now(),
		Notes:      "tx",
	}
	s.Require().NoError(repo.Create(txCtx, sub), "create subscription in tx")
	s.Require().NoError(repo.UpdateNotes(txCtx, sub.ID, "tx-note"), "update subscription in tx")

	s.Require().NoError(tx.Rollback(), "rollback tx")
	tx = nil

	_, err = repo.GetByID(context.Background(), sub.ID)
	s.Require().ErrorIs(err, service.ErrSubscriptionNotFound)
}
