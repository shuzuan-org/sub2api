//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type fakeMetaskRepo struct {
	rec       *MetaskMigrationRecord
	freezes   int
	failed    []string
	completed bool
}

func (r *fakeMetaskRepo) Preview(context.Context, int64) (*MetaskMigrationPreview, error) {
	return &MetaskMigrationPreview{Role: RoleUser, Status: StatusActive, BalanceMU: 5000, KeyCount: 2}, nil
}
func (r *fakeMetaskRepo) Get(context.Context, int64) (*MetaskMigrationRecord, error) {
	return r.rec, nil
}
func (r *fakeMetaskRepo) Freeze(_ context.Context, userID int64, _ time.Time) (*MetaskMigrationFreezeResult, error) {
	r.freezes++
	if r.rec != nil {
		return &MetaskMigrationFreezeResult{Record: r.rec}, nil
	}
	r.rec = &MetaskMigrationRecord{UserID: userID, Status: MetaskMigrationStatusFrozen, Payload: MetaskMigrationPayload{Sub2APIID: userID, BalanceMU: 5000}}
	return &MetaskMigrationFreezeResult{Record: r.rec, Created: true}, nil
}
func (r *fakeMetaskRepo) MarkCompleted(_ context.Context, _ int64, result json.RawMessage) error {
	r.completed = true
	r.rec.Status = MetaskMigrationStatusCompleted
	r.rec.Result = result
	return nil
}
func (r *fakeMetaskRepo) MarkFailed(_ context.Context, _ int64, msg string) error {
	r.failed = append(r.failed, msg)
	return nil
}
func (r *fakeMetaskRepo) ListPending(context.Context, time.Duration, int) ([]int64, error) {
	if r.rec != nil && r.rec.Status == MetaskMigrationStatusFrozen {
		return []int64{r.rec.UserID}, nil
	}
	return nil, nil
}

type fakeMetaskClient struct {
	err   error
	calls int
	token string
}

func (c *fakeMetaskClient) Apply(_ context.Context, _, token string, _ *MetaskMigrationPayload) (json.RawMessage, error) {
	c.calls++
	c.token = token
	if c.err != nil {
		return nil, c.err
	}
	return json.RawMessage(`{"credited_mu":5000}`), nil
}

func newMetaskTestService(repo *fakeMetaskRepo, client *fakeMetaskClient, enabled bool) *MetaskMigrationService {
	cfg := &config.Config{MetaskMigration: config.MetaskMigrationConfig{
		Enabled: enabled, BaseURL: "https://new.example", Token: "tok", GatewayBaseURL: "https://api.new.example/v1/",
	}}
	return NewMetaskMigrationService(repo, client, cfg, nil, nil, nil, nil)
}

func TestMetaskMigration_DisabledRefuses(t *testing.T) {
	repo, client := &fakeMetaskRepo{}, &fakeMetaskClient{}
	svc := newMetaskTestService(repo, client, false)
	_, err := svc.Migrate(context.Background(), 7)
	require.ErrorIs(t, err, ErrMetaskMigrationDisabled)
	require.Zero(t, repo.freezes)
	st, err := svc.GetStatus(context.Background(), 7)
	require.NoError(t, err)
	require.False(t, st.Enabled)
	require.False(t, st.Eligible)
}

func TestMetaskMigration_SuccessThenReplay(t *testing.T) {
	repo, client := &fakeMetaskRepo{}, &fakeMetaskClient{}
	svc := newMetaskTestService(repo, client, true)
	st, err := svc.GetStatus(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, st.Eligible)
	require.Equal(t, int64(5000), st.BalanceMU)
	require.Equal(t, "https://new.example", st.SiteURL)
	require.Equal(t, "https://api.new.example/v1", st.GatewayBaseURL)

	st, err = svc.Migrate(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, MetaskMigrationStatusCompleted, st.Status)
	require.JSONEq(t, `{"credited_mu":5000}`, string(st.Result))
	require.Equal(t, "tok", client.token)
	require.True(t, repo.completed)

	// 已完成后再点：不再调新站
	_, err = svc.Migrate(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 1, client.calls)
}

func TestMetaskMigration_ApplyFailureStaysFrozenAndRetries(t *testing.T) {
	repo, client := &fakeMetaskRepo{}, &fakeMetaskClient{err: errors.New("metask-ai returned 502")}
	svc := newMetaskTestService(repo, client, true)
	_, err := svc.Migrate(context.Background(), 7)
	require.ErrorIs(t, err, ErrMetaskMigrationPending)
	require.Equal(t, MetaskMigrationStatusFrozen, repo.rec.Status)
	require.Equal(t, []string{"metask-ai returned 502"}, repo.failed)

	// 新站恢复后，后台重试用同一份快照补调
	client.err = nil
	svc.retryPending()
	require.Equal(t, MetaskMigrationStatusCompleted, repo.rec.Status)
	require.Equal(t, 2, client.calls)
}
