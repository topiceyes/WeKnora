package dingtalk

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// ---------------------------------------------------------------------------
// mocks
// ---------------------------------------------------------------------------

// mockContactClient 实现 contactClient，按部门返回固定成员页（单页，无分页）。
type mockContactClient struct {
	tokenErr error
	deptIDs  []int64
	deptErr  error
	users    map[int64][]DeptUserDetail
	listErr  error
}

func (m *mockContactClient) GetCorpAccessToken(context.Context) (string, error) {
	if m.tokenErr != nil {
		return "", m.tokenErr
	}
	return "corp-token", nil
}

func (m *mockContactClient) ListDepartmentIDs(context.Context) ([]int64, error) {
	if m.deptErr != nil {
		return nil, m.deptErr
	}
	return m.deptIDs, nil
}

func (m *mockContactClient) ListDeptUsers(_ context.Context, deptID, cursor, size int64) (*DeptUserPage, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return &DeptUserPage{List: m.users[deptID], HasMore: false}, nil
}

// mockUserRepo 内存 UserRepo。ListActiveBindings 读的是 sqlite users 表，
// 所以测试需要同时 seed sqlite 与这里的 map（保持 is_active 初始一致）。
type mockUserRepo struct {
	users map[string]*types.User
}

func newMockUserRepo() *mockUserRepo { return &mockUserRepo{users: map[string]*types.User{}} }

func (m *mockUserRepo) add(u *types.User) { m.users[u.ID] = u }

func (m *mockUserRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}

func (m *mockUserRepo) GetUserByEmail(_ context.Context, email string) (*types.User, error) {
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *mockUserRepo) UpdateUser(_ context.Context, user *types.User) error {
	if _, ok := m.users[user.ID]; !ok {
		return errors.New("user not found")
	}
	m.users[user.ID] = user
	return nil
}

// mockTokenRevoker 记录撤销过令牌的用户 id。
type mockTokenRevoker struct {
	revoked []string
}

func (m *mockTokenRevoker) RevokeTokensByUserID(_ context.Context, userID string) error {
	m.revoked = append(m.revoked, userID)
	return nil
}

func (m *mockTokenRevoker) has(userID string) bool {
	for _, id := range m.revoked {
		if id == userID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// setupSync 建 sqlite repo + mock client/userRepo/revoker 的 SyncService。
// seed 用户同时落 sqlite users 表（供 ListActiveBindings join）与 mockUserRepo。
func setupSync(t *testing.T, client *mockContactClient) (*SyncService, *Repository, *mockUserRepo, *mockTokenRevoker) {
	t.Helper()
	repo := setupRepo(t)
	userRepo := newMockUserRepo()
	revoker := &mockTokenRevoker{}
	svc := &SyncService{
		client:       client,
		repo:         repo,
		userRepo:     userRepo,
		tokenRevoker: revoker,
		corpID:       "corp-1",
	}
	return svc, repo, userRepo, revoker
}

// seedSyncUser 同时在 sqlite 与 mockUserRepo 里造一个活跃用户。
func seedSyncUser(t *testing.T, r *Repository, ur *mockUserRepo, id, username, email string) {
	t.Helper()
	seedUser(t, r, id, username, email, true)
	ur.add(&types.User{ID: id, Username: username, Email: email, IsActive: true})
}

func bind(t *testing.T, r *Repository, dingUserID, userID, nick string) {
	t.Helper()
	if err := r.BindToUser(context.Background(), "corp-1", dingUserID, "union-"+dingUserID, userID, nick, "", ""); err != nil {
		t.Fatalf("BindToUser %s: %v", dingUserID, err)
	}
}

func lastRun(t *testing.T, r *Repository) *SyncRun {
	t.Helper()
	runs, total, err := r.ListSyncRuns(context.Background(), 0, 1)
	if err != nil || total == 0 {
		t.Fatalf("ListSyncRuns: total=%d err=%v", total, err)
	}
	return runs[0]
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

// TestSyncDeactivatesDepartedUser 本地有绑定 / 远端无 → 禁用 + 撤销令牌。
func TestSyncDeactivatesDepartedUser(t *testing.T) {
	ctx := context.Background()
	client := &mockContactClient{
		deptIDs: []int64{1},
		users: map[int64][]DeptUserDetail{
			1: {{UserID: "ding-stay", Name: "留任"}},
		},
	}
	svc, repo, userRepo, revoker := setupSync(t, client)
	seedSyncUser(t, repo, userRepo, "u-stay", "stay", "stay@c.com")
	seedSyncUser(t, repo, userRepo, "u-left", "left", "left@c.com")
	bind(t, repo, "ding-stay", "u-stay", "留任")
	bind(t, repo, "ding-left", "u-left", "离职")

	run, err := svc.RunFullSync(ctx, SyncTriggerCron)
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if run.DeactivatedCount != 1 {
		t.Fatalf("expected 1 deactivation, got %d", run.DeactivatedCount)
	}
	if userRepo.users["u-left"].IsActive {
		t.Fatal("departed user should be deactivated")
	}
	if !revoker.has("u-left") {
		t.Fatal("departed user tokens should be revoked")
	}
	if !userRepo.users["u-stay"].IsActive || revoker.has("u-stay") {
		t.Fatal("remaining user must stay untouched")
	}
	// 留任者资料快照应被刷新
	identity, err := repo.GetByDingUserID(ctx, "corp-1", "ding-stay")
	if err != nil {
		t.Fatalf("GetByDingUserID: %v", err)
	}
	if identity.LastSyncedAt == nil {
		t.Fatal("last_synced_at should be updated")
	}

	persisted := lastRun(t, repo)
	if persisted.Status != SyncStatusSuccess || persisted.TotalRemote != 1 || persisted.DeactivatedCount != 1 {
		t.Fatalf("persisted run wrong: %+v", persisted)
	}
}

// TestSyncHalfVolumeProtection 远端人数低于活跃绑定一半 → 整批跳过禁用。
func TestSyncHalfVolumeProtection(t *testing.T) {
	client := &mockContactClient{
		deptIDs: []int64{1},
		users: map[int64][]DeptUserDetail{
			1: {{UserID: "ding-1", Name: "甲"}},
		},
	}
	svc, repo, userRepo, revoker := setupSync(t, client)
	for _, id := range []string{"u-1", "u-2", "u-3"} {
		seedSyncUser(t, repo, userRepo, id, id, id+"@c.com")
	}
	bind(t, repo, "ding-1", "u-1", "甲")
	bind(t, repo, "ding-2", "u-2", "乙")
	bind(t, repo, "ding-3", "u-3", "丙")

	run, err := svc.RunFullSync(context.Background(), SyncTriggerCron)
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if run.DeactivatedCount != 0 {
		t.Fatalf("half-volume protection should skip all deactivations, got %d", run.DeactivatedCount)
	}
	if run.ErrorMessage == "" {
		t.Fatal("anomaly should be recorded in error_message")
	}
	for _, id := range []string{"u-2", "u-3"} {
		if !userRepo.users[id].IsActive {
			t.Fatalf("user %s must not be deactivated under protection", id)
		}
	}
	if len(revoker.revoked) != 0 {
		t.Fatalf("no tokens should be revoked, got %v", revoker.revoked)
	}
}

// TestSyncSkipsPrivilegedUsers SystemAdmin / CanAccessAllTenants 不被同步禁用。
func TestSyncSkipsPrivilegedUsers(t *testing.T) {
	ctx := context.Background()
	client := &mockContactClient{
		deptIDs: []int64{1},
		users: map[int64][]DeptUserDetail{
			1: {{UserID: "ding-normal-1", Name: "甲"}, {UserID: "ding-normal-2", Name: "乙"}},
		},
	}
	svc, repo, userRepo, revoker := setupSync(t, client)
	seedSyncUser(t, repo, userRepo, "u-admin", "admin", "admin@c.com")
	seedSyncUser(t, repo, userRepo, "u-super", "super", "super@c.com")
	seedSyncUser(t, repo, userRepo, "u-n1", "n1", "n1@c.com")
	seedSyncUser(t, repo, userRepo, "u-n2", "n2", "n2@c.com")
	// 特权标记落在 sqlite users 表（ListActiveBindings 从这里 join）
	if err := repo.db.Exec("UPDATE users SET is_system_admin = ? WHERE id = ?", true, "u-admin").Error; err != nil {
		t.Fatalf("mark admin: %v", err)
	}
	if err := repo.db.Exec("UPDATE users SET can_access_all_tenants = ? WHERE id = ?", true, "u-super").Error; err != nil {
		t.Fatalf("mark access-all: %v", err)
	}
	bind(t, repo, "ding-admin", "u-admin", "管理员")
	bind(t, repo, "ding-super", "u-super", "超管")
	bind(t, repo, "ding-normal-1", "u-n1", "甲")
	bind(t, repo, "ding-normal-2", "u-n2", "乙")

	run, err := svc.RunFullSync(ctx, SyncTriggerCron)
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if run.DeactivatedCount != 0 {
		t.Fatalf("privileged users must not be deactivated, got %d", run.DeactivatedCount)
	}
	if !userRepo.users["u-admin"].IsActive || !userRepo.users["u-super"].IsActive {
		t.Fatal("privileged users must stay active")
	}
	if len(revoker.revoked) != 0 {
		t.Fatalf("no tokens should be revoked, got %v", revoker.revoked)
	}
}

// TestSyncSkipsUnionPlaceholder unionId 占位绑定不参与对账禁用。
func TestSyncSkipsUnionPlaceholder(t *testing.T) {
	client := &mockContactClient{
		deptIDs: []int64{1},
		users: map[int64][]DeptUserDetail{
			1: {{UserID: "ding-real", Name: "真实"}},
		},
	}
	svc, repo, userRepo, revoker := setupSync(t, client)
	seedSyncUser(t, repo, userRepo, "u-real", "real", "real@c.com")
	seedSyncUser(t, repo, userRepo, "u-placeholder", "ph", "ph@c.com")
	bind(t, repo, "ding-real", "u-real", "真实")
	// 占位记录：登录时未拿到企业 userid，ding_user_id 以 "union:" 开头
	bind(t, repo, "union:abc123", "u-placeholder", "占位")

	run, err := svc.RunFullSync(context.Background(), SyncTriggerCron)
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if run.DeactivatedCount != 0 {
		t.Fatalf("placeholder identity must not be reconciled, got %d deactivations", run.DeactivatedCount)
	}
	if !userRepo.users["u-placeholder"].IsActive {
		t.Fatal("placeholder user must stay active")
	}
	if revoker.has("u-placeholder") {
		t.Fatal("placeholder user tokens must not be revoked")
	}
}

// TestSyncBindsNewHireByEmail 无绑定但 email 匹配本地账号 → 补绑定。
func TestSyncBindsNewHireByEmail(t *testing.T) {
	ctx := context.Background()
	client := &mockContactClient{
		deptIDs: []int64{1},
		users: map[int64][]DeptUserDetail{
			1: {{UserID: "ding-new", Name: "新人", Email: "new@c.com", Title: "工程师"}},
		},
	}
	svc, repo, userRepo, _ := setupSync(t, client)
	seedSyncUser(t, repo, userRepo, "u-new", "newbie", "new@c.com")

	run, err := svc.RunFullSync(ctx, SyncTriggerManual)
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if run.CreatedCount != 1 {
		t.Fatalf("expected 1 created binding, got %d", run.CreatedCount)
	}
	identity, err := repo.GetByDingUserID(ctx, "corp-1", "ding-new")
	if err != nil {
		t.Fatalf("GetByDingUserID: %v", err)
	}
	if identity.UserID != "u-new" || identity.Nick != "新人" {
		t.Fatalf("bound identity wrong: %+v", identity)
	}
	// 新绑定用户在远端 → 不会被禁用
	if run.DeactivatedCount != 0 || !userRepo.users["u-new"].IsActive {
		t.Fatal("newly bound user must stay active")
	}
}

// TestSyncFailureDisablesNobody 远端拉取失败 → 整轮 failed，不做任何禁用。
func TestSyncFailureDisablesNobody(t *testing.T) {
	client := &mockContactClient{
		deptErr: errors.New("network boom"),
	}
	svc, repo, userRepo, revoker := setupSync(t, client)
	seedSyncUser(t, repo, userRepo, "u-1", "u1", "u1@c.com")
	bind(t, repo, "ding-1", "u-1", "甲")

	run, err := svc.RunFullSync(context.Background(), SyncTriggerCron)
	if err == nil {
		t.Fatal("expected error when remote fetch fails")
	}
	if run.Status != SyncStatusFailed {
		t.Fatalf("run should be failed, got %s", run.Status)
	}
	if !userRepo.users["u-1"].IsActive {
		t.Fatal("failed sync must not deactivate anyone")
	}
	if len(revoker.revoked) != 0 {
		t.Fatalf("failed sync must not revoke tokens, got %v", revoker.revoked)
	}

	persisted := lastRun(t, repo)
	if persisted.Status != SyncStatusFailed || persisted.ErrorMessage == "" {
		t.Fatalf("persisted run should record failure: %+v", persisted)
	}
}

// TestSyncRejectsConcurrentRun 已有 running 同步时拒绝并发触发。
func TestSyncRejectsConcurrentRun(t *testing.T) {
	client := &mockContactClient{deptIDs: []int64{1}}
	svc, repo, _, _ := setupSync(t, client)
	if _, err := repo.CreateSyncRun(context.Background(), SyncTriggerCron); err != nil {
		t.Fatalf("CreateSyncRun: %v", err)
	}

	_, err := svc.RunFullSync(context.Background(), SyncTriggerManual)
	if !errors.Is(err, ErrSyncAlreadyRunning) {
		t.Fatalf("expected ErrSyncAlreadyRunning, got %v", err)
	}
}

// TestSchedulerValidatesCronExpression 非法表达式 Start 报错；5 段与 6 段
// （配置默认 "0 0 3 * * *"）均可启动/停止。
func TestSchedulerValidatesCronExpression(t *testing.T) {
	bad := NewScheduler(&SyncService{}, "not-a-cron")
	if err := bad.Start(); err == nil {
		t.Fatal("expected error for invalid cron expression")
	}

	for _, expr := range []string{"0 3 * * *", "0 0 3 * * *"} {
		s := NewScheduler(&SyncService{}, expr)
		if err := s.Start(); err != nil {
			t.Fatalf("Start(%q): %v", expr, err)
		}
		s.Stop()
	}
}
