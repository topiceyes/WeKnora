package dingtalk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveBindingsAreCorpScopedAndExcludeInactiveAndDeletedUsers(t *testing.T) {
	r := setupRepo(t)
	ctx := context.Background()
	for _, id := range []string{"live", "other-corp", "inactive", "deleted", "unbound"} {
		seedUser(t, r, id, id, id+"@example.com", id != "inactive")
	}
	for _, id := range []string{"live", "inactive", "deleted"} {
		bind(t, r, "ding-"+id, id, id)
	}
	require.NoError(t, r.BindToUser(ctx, "other-corp", "ding-other", "union-other", "other-corp", "Other", "", ""))
	require.NoError(t, r.db.Exec("UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?", "deleted").Error)
	rows, err := r.ListActiveBindings(ctx, "corp-1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "live", rows[0].UserID)
	_, err = r.ListActiveBindings(ctx, "")
	require.Error(t, err)
}

func TestSyncDoesNotReconcileOtherCorpsOrInactiveAccounts(t *testing.T) {
	client := &mockContactClient{users: map[int64][]DeptUserDetail{1: {{UserID: "ding-live", Name: "Live"}}}}
	svc, repo, users, revoker := setupSync(t, client)
	seedSyncUser(t, repo, users, "live", "live", "live@example.com")
	bind(t, repo, "ding-live", "live", "Live")
	seedSyncUser(t, repo, users, "foreign", "foreign", "foreign@example.com")
	require.NoError(t, repo.BindToUser(context.Background(), "corp-old", "ding-foreign", "union-foreign", "foreign", "Foreign", "", ""))
	for _, id := range []string{"inactive-1", "inactive-2", "inactive-3"} {
		seedSyncUser(t, repo, users, id, id, id+"@example.com")
		bind(t, repo, "ding-"+id, id, id)
		require.NoError(t, repo.db.Exec("UPDATE users SET is_active = ? WHERE id = ?", false, id).Error)
		users.users[id].IsActive = false
	}
	run, err := svc.RunFullSync(context.Background(), SyncTriggerManual)
	require.NoError(t, err)
	assert.Equal(t, SyncStatusSuccess, run.Status)
	assert.Zero(t, run.DeactivatedCount)
	assert.True(t, users.users["foreign"].IsActive)
	assert.Empty(t, revoker.revoked)
}

func TestSyncDeactivationFailureIsPersistedAndRetryable(t *testing.T) {
	client := &mockContactClient{users: map[int64][]DeptUserDetail{1: {{UserID: "ding-live", Name: "Live"}}}}
	svc, repo, users, revoker := setupSync(t, client)
	for _, id := range []string{"live", "departed"} {
		seedSyncUser(t, repo, users, id, id, id+"@example.com")
		bind(t, repo, "ding-"+id, id, id)
	}
	users.updateErr = errors.New("write unavailable")
	run, err := svc.RunFullSync(context.Background(), SyncTriggerCron)
	require.Error(t, err)
	assert.Equal(t, SyncStatusFailed, run.Status)
	assert.Equal(t, 1, run.UpdatedCount, "completed profile work remains visible")
	assert.Zero(t, run.DeactivatedCount)
	assert.True(t, users.users["departed"].IsActive, "a failed save must not mutate the cached object")
	assert.Empty(t, revoker.revoked)
	persisted := lastRun(t, repo)
	assert.Equal(t, SyncStatusFailed, persisted.Status)
	assert.Contains(t, persisted.ErrorMessage, "write unavailable")
	require.NotNil(t, persisted.FinishedAt)
	users.updateErr = nil
	run, err = svc.RunFullSync(context.Background(), SyncTriggerCron)
	require.NoError(t, err)
	assert.Equal(t, SyncStatusSuccess, run.Status)
	assert.Equal(t, 1, run.DeactivatedCount)
	assert.False(t, users.users["departed"].IsActive)
	assert.True(t, revoker.has("departed"))
}

func TestSyncSnapshotFailureIsNotReportedAsSuccess(t *testing.T) {
	svc, repo, _, _ := setupSync(t, &mockContactClient{users: map[int64][]DeptUserDetail{1: {{UserID: "unbound", Name: "New"}}}})
	require.NoError(t, repo.db.Exec("DROP TABLE dingtalk_contacts").Error)
	run, err := svc.RunFullSync(context.Background(), SyncTriggerManual)
	require.Error(t, err)
	assert.Equal(t, SyncStatusFailed, run.Status)
	assert.Equal(t, 1, run.TotalRemote)
	assert.Contains(t, lastRun(t, repo).ErrorMessage, "persist contacts")
}

func TestSyncIdentityWriteFailurePreventsOffboarding(t *testing.T) {
	svc, repo, users, revoker := setupSync(t, &mockContactClient{users: map[int64][]DeptUserDetail{1: {{UserID: "ding-live", Name: "Live"}}}})
	for _, id := range []string{"live", "departed"} {
		seedSyncUser(t, repo, users, id, id, id+"@example.com")
		bind(t, repo, "ding-"+id, id, id)
	}
	require.NoError(t, repo.db.Exec(`CREATE TRIGGER fail_identity_update BEFORE UPDATE ON user_dingtalk_identities BEGIN SELECT RAISE(ABORT, 'identity write unavailable'); END`).Error)
	run, err := svc.RunFullSync(context.Background(), SyncTriggerCron)
	require.Error(t, err)
	assert.Equal(t, SyncStatusFailed, run.Status)
	assert.Zero(t, run.DeactivatedCount)
	assert.True(t, users.users["departed"].IsActive)
	assert.Empty(t, revoker.revoked)
	assert.Contains(t, lastRun(t, repo).ErrorMessage, "update identity")
}

type failFirstUserUpdate struct {
	*mockUserRepo
	calls int
}

func (r *failFirstUserUpdate) UpdateUser(ctx context.Context, user *types.User) error {
	r.calls++
	if r.calls == 1 {
		return errors.New("temporary user write failure")
	}
	return r.mockUserRepo.UpdateUser(ctx, user)
}

func TestSchedulerRetriesFailedDeactivation(t *testing.T) {
	svc, repo, users, revoker := setupSync(t, &mockContactClient{users: map[int64][]DeptUserDetail{1: {{UserID: "ding-live", Name: "Live"}}}})
	for _, id := range []string{"live", "departed"} {
		seedSyncUser(t, repo, users, id, id, id+"@example.com")
		bind(t, repo, "ding-"+id, id, id)
	}
	failing := &failFirstUserUpdate{mockUserRepo: users}
	svc.userRepo = failing
	scheduler := NewScheduler(svc, "@daily")
	defer scheduler.Stop()
	scheduler.runWithRetryInterval(context.Background(), time.Millisecond)
	runs, total, err := repo.ListSyncRuns(context.Background(), 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, runs, 2)
	assert.Equal(t, SyncStatusSuccess, runs[0].Status)
	assert.Equal(t, 1, runs[0].DeactivatedCount)
	assert.Equal(t, SyncStatusFailed, runs[1].Status)
	assert.Contains(t, runs[1].ErrorMessage, "temporary user write failure")
	assert.Equal(t, 2, failing.calls)
	assert.False(t, users.users["departed"].IsActive)
	assert.True(t, revoker.has("departed"))
}

func TestSyncEmailMatchingPreservesOtherCorpBinding(t *testing.T) {
	svc, repo, users, _ := setupSync(t, &mockContactClient{users: map[int64][]DeptUserDetail{1: {{UserID: "ding-new", Email: "foreign@example.com"}}}})
	seedSyncUser(t, repo, users, "foreign", "foreign", "foreign@example.com")
	require.NoError(t, repo.BindToUser(context.Background(), "corp-other", "ding-old", "union-old", "foreign", "Foreign", "", ""))
	run, err := svc.RunFullSync(context.Background(), SyncTriggerManual)
	require.NoError(t, err)
	assert.Zero(t, run.CreatedCount)
	identity, err := repo.GetByUserID(context.Background(), "foreign")
	require.NoError(t, err)
	assert.Equal(t, "corp-other", identity.CorpID)
	assert.Equal(t, "ding-old", identity.DingUserID)
	assert.True(t, users.users["foreign"].IsActive)
}

func TestSchedulerRetryWaitRespectsCancellation(t *testing.T) {
	scheduler := NewScheduler(nil, "@daily")
	defer scheduler.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan bool, 1)
	go func() { done <- scheduler.waitRetry(ctx, time.Hour) }()
	select {
	case retry := <-done:
		assert.False(t, retry)
	case <-time.After(time.Second):
		t.Fatal("retry wait ignored context cancellation")
	}
}

func TestSchedulerStopCancelsRunningRetryWait(t *testing.T) {
	scheduler := NewScheduler(nil, "@daily")
	entered := make(chan struct{}, 1)
	_, err := scheduler.cron.AddFunc("@every 1s", func() {
		entered <- struct{}{}
		scheduler.waitRetry(context.Background(), time.Hour)
	})
	require.NoError(t, err)
	scheduler.cron.Start()
	defer scheduler.Stop()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("cron job did not start")
	}
	stopped := make(chan struct{})
	go func() {
		scheduler.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for retry interval before canceling it")
	}
}
