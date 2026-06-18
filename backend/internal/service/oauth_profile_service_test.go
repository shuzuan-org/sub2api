package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type oauthProfileAPIKeyRepoStub struct {
	keys        []APIKey
	lastUserID  int64
	lastParams  pagination.PaginationParams
	lastFilters APIKeyListFilters
}

func (s *oauthProfileAPIKeyRepoStub) Create(context.Context, *APIKey) error {
	panic("unexpected Create call")
}

func (s *oauthProfileAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	panic("unexpected GetByID call")
}

func (s *oauthProfileAPIKeyRepoStub) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID call")
}

func (s *oauthProfileAPIKeyRepoStub) GetByKey(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKey call")
}

func (s *oauthProfileAPIKeyRepoStub) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKeyForAuth call")
}

func (s *oauthProfileAPIKeyRepoStub) Update(context.Context, *APIKey) error {
	panic("unexpected Update call")
}

func (s *oauthProfileAPIKeyRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}

func (s *oauthProfileAPIKeyRepoStub) ListByUserID(_ context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	s.lastUserID = userID
	s.lastParams = params
	s.lastFilters = filters
	return append([]APIKey(nil), s.keys...), &pagination.PaginationResult{}, nil
}

func (s *oauthProfileAPIKeyRepoStub) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}

func (s *oauthProfileAPIKeyRepoStub) CountByUserID(context.Context, int64) (int64, error) {
	panic("unexpected CountByUserID call")
}

func (s *oauthProfileAPIKeyRepoStub) ExistsByKey(context.Context, string) (bool, error) {
	panic("unexpected ExistsByKey call")
}

func (s *oauthProfileAPIKeyRepoStub) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}

func (s *oauthProfileAPIKeyRepoStub) SearchAPIKeys(context.Context, int64, string, int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}

func (s *oauthProfileAPIKeyRepoStub) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}

func (s *oauthProfileAPIKeyRepoStub) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}

func (s *oauthProfileAPIKeyRepoStub) CountByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}

func (s *oauthProfileAPIKeyRepoStub) ListKeysByUserID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}

func (s *oauthProfileAPIKeyRepoStub) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}

func (s *oauthProfileAPIKeyRepoStub) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}

func (s *oauthProfileAPIKeyRepoStub) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastUsed call")
}

func (s *oauthProfileAPIKeyRepoStub) IncrementRateLimitUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}

func (s *oauthProfileAPIKeyRepoStub) ResetRateLimitWindows(context.Context, int64) error {
	panic("unexpected ResetRateLimitWindows call")
}

func (s *oauthProfileAPIKeyRepoStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

func TestOAuthProfileServiceListProfilesFiltersActiveAnthropicKeys(t *testing.T) {
	anthropicGroupID := int64(100)
	openAIGroupID := int64(200)
	inactiveAnthropicGroupID := int64(300)
	anotherAnthropicGroupID := int64(400)
	claudeCodeOnlyGroupID := int64(500)
	past := time.Now().Add(-time.Hour)

	repo := &oauthProfileAPIKeyRepoStub{
		keys: []APIKey{
			{
				ID:      10,
				UserID:  42,
				Name:    "Claude key",
				Status:  StatusAPIKeyActive,
				GroupID: &anthropicGroupID,
				Group:   &Group{ID: anthropicGroupID, Name: "Claude", Platform: PlatformAnthropic, Status: StatusActive},
			},
			{
				ID:      11,
				UserID:  42,
				Name:    "disabled anthropic",
				Status:  StatusAPIKeyDisabled,
				GroupID: &anthropicGroupID,
				Group:   &Group{ID: anthropicGroupID, Name: "Claude", Platform: PlatformAnthropic, Status: StatusActive},
			},
			{
				ID:      12,
				UserID:  42,
				Name:    "openai key",
				Status:  StatusAPIKeyActive,
				GroupID: &openAIGroupID,
				Group:   &Group{ID: openAIGroupID, Name: "OpenAI", Platform: PlatformOpenAI, Status: StatusActive},
			},
			{
				ID:      13,
				UserID:  42,
				Name:    "inactive group",
				Status:  StatusAPIKeyActive,
				GroupID: &inactiveAnthropicGroupID,
				Group:   &Group{ID: inactiveAnthropicGroupID, Name: "Inactive Claude", Platform: PlatformAnthropic, Status: StatusDisabled},
			},
			{
				ID:        14,
				UserID:    42,
				Name:      "expired key",
				Status:    StatusAPIKeyActive,
				GroupID:   &anthropicGroupID,
				Group:     &Group{ID: anthropicGroupID, Name: "Claude", Platform: PlatformAnthropic, Status: StatusActive},
				ExpiresAt: &past,
			},
			{
				ID:        15,
				UserID:    42,
				Name:      "quota exhausted",
				Status:    StatusAPIKeyActive,
				GroupID:   &anthropicGroupID,
				Group:     &Group{ID: anthropicGroupID, Name: "Claude", Platform: PlatformAnthropic, Status: StatusActive},
				Quota:     1,
				QuotaUsed: 1,
			},
			{
				ID:      16,
				UserID:  42,
				Name:    "backup claude",
				Status:  StatusAPIKeyActive,
				GroupID: &anotherAnthropicGroupID,
				Group:   &Group{ID: anotherAnthropicGroupID, Name: "Backup Claude", Platform: PlatformAnthropic, Status: StatusActive},
			},
			{
				ID:      17,
				UserID:  42,
				Name:    "claude code only",
				Status:  StatusAPIKeyActive,
				GroupID: &claudeCodeOnlyGroupID,
				Group: &Group{
					ID:             claudeCodeOnlyGroupID,
					Name:           "Claude Code",
					Platform:       PlatformAnthropic,
					Status:         StatusActive,
					ClaudeCodeOnly: true,
				},
			},
		},
	}
	svc := NewOAuthProfileService(repo, &oauthUserRepoStub{user: &User{ID: 42, Email: "u@example.com", Status: StatusActive}})

	profiles, err := svc.ListProfiles(context.Background(), 42)
	require.NoError(t, err)

	require.Equal(t, int64(42), repo.lastUserID)
	require.Equal(t, pagination.PaginationParams{Page: 1, PageSize: 1000}, repo.lastParams)
	require.Equal(t, StatusAPIKeyActive, repo.lastFilters.Status)

	require.Len(t, profiles, 2)
	require.Equal(t, OAuthProfile{
		ID:          "10",
		Name:        "Claude key",
		DisplayName: "Claude key (Claude)",
		Status:      StatusAPIKeyActive,
		Platform:    PlatformAnthropic,
		GroupID:     anthropicGroupID,
		GroupName:   "Claude",
		IsDefault:   true,
	}, profiles[0])
	require.Equal(t, OAuthProfile{
		ID:          "16",
		Name:        "backup claude",
		DisplayName: "backup claude (Backup Claude)",
		Status:      StatusAPIKeyActive,
		Platform:    PlatformAnthropic,
		GroupID:     anotherAnthropicGroupID,
		GroupName:   "Backup Claude",
		IsDefault:   false,
	}, profiles[1])
}
