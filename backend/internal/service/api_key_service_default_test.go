//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type defaultAPIKeyRepoStub struct {
	APIKeyRepository
	count     int64
	countErr  error
	createErr error
	created   []*APIKey
}

func (s *defaultAPIKeyRepoStub) CountByUserID(context.Context, int64) (int64, error) {
	return s.count, s.countErr
}

func (s *defaultAPIKeyRepoStub) Create(_ context.Context, key *APIKey) error {
	if s.createErr != nil {
		return s.createErr
	}
	cp := *key
	s.created = append(s.created, &cp)
	return nil
}

type defaultAPIKeyUserRepoStub struct {
	UserRepository
	user *User
}

func (s *defaultAPIKeyUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	if s.user == nil {
		return &User{ID: 1, Status: StatusActive}, nil
	}
	return s.user, nil
}

type defaultAPIKeyGroupRepoStub struct {
	GroupRepository
	groups []Group
}

func (s *defaultAPIKeyGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	return s.groups, nil
}

func (s *defaultAPIKeyGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	for i := range s.groups {
		if s.groups[i].ID == id {
			return &s.groups[i], nil
		}
	}
	return nil, ErrGroupNotFound
}

func TestAPIKeyService_CreateDefaultAPIKeyForNewUser_BindsDefaultMiniMaxHighspeedGroup(t *testing.T) {
	repo := &defaultAPIKeyRepoStub{}
	defaultGroupID := int64(42)
	svc := NewAPIKeyService(
		repo,
		&defaultAPIKeyUserRepoStub{user: &User{ID: 9, Status: StatusActive}},
		&defaultAPIKeyGroupRepoStub{groups: []Group{
			{ID: 1, Name: "MiniMax-M2.7-Highspeed", IsExclusive: true, Status: StatusActive},
			{ID: defaultGroupID, Name: "minimax-m2.7-highspeed", IsExclusive: false, Status: StatusActive},
		}},
		nil,
		nil,
		nil,
		&config.Config{Default: config.DefaultConfig{APIKeyPrefix: "test-"}},
	)

	err := svc.CreateDefaultAPIKeyForNewUser(context.Background(), 9)
	require.NoError(t, err)
	require.Len(t, repo.created, 1)
	require.Equal(t, int64(9), repo.created[0].UserID)
	require.Equal(t, defaultRegistrationAPIKeyName, repo.created[0].Name)
	require.NotNil(t, repo.created[0].GroupID)
	require.Equal(t, defaultGroupID, *repo.created[0].GroupID)
	require.True(t, strings.HasPrefix(repo.created[0].Key, "test-"))
}

func TestAPIKeyService_CreateDefaultAPIKeyForNewUser_SkipsWhenUserAlreadyHasKeys(t *testing.T) {
	repo := &defaultAPIKeyRepoStub{count: 1}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)

	err := svc.CreateDefaultAPIKeyForNewUser(context.Background(), 9)
	require.NoError(t, err)
	require.Empty(t, repo.created)
}

func TestAPIKeyService_CreateDefaultAPIKeyForNewUser_CreatesUnboundWhenDefaultGroupMissing(t *testing.T) {
	repo := &defaultAPIKeyRepoStub{}
	svc := NewAPIKeyService(
		repo,
		&defaultAPIKeyUserRepoStub{user: &User{ID: 9, Status: StatusActive}},
		&defaultAPIKeyGroupRepoStub{groups: []Group{{ID: 1, Name: "default", Status: StatusActive}}},
		nil,
		nil,
		nil,
		nil,
	)

	err := svc.CreateDefaultAPIKeyForNewUser(context.Background(), 9)
	require.NoError(t, err)
	require.Len(t, repo.created, 1)
	require.Equal(t, defaultRegistrationAPIKeyName, repo.created[0].Name)
	require.Nil(t, repo.created[0].GroupID)
	require.True(t, strings.HasPrefix(repo.created[0].Key, "sk-"))
}
