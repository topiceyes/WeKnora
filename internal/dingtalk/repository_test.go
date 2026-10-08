package dingtalk

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRepo(t *testing.T) *Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// users 表手动建（只含本测试需要的字段）
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id VARCHAR(36) PRIMARY KEY,
		username VARCHAR(100), email VARCHAR(255),
		is_active BOOLEAN, is_system_admin BOOLEAN,
		can_access_all_tenants BOOLEAN, deleted_at DATETIME)`).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS user_dingtalk_identities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id VARCHAR(36), union_id VARCHAR(128), ding_user_id VARCHAR(128),
		corp_id VARCHAR(128), nick VARCHAR(255), mobile VARCHAR(64), email VARCHAR(255),
		dept_id_list TEXT DEFAULT '[]', job_number VARCHAR(64), title VARCHAR(255),
		user_access_token TEXT, token_expires_at DATETIME, last_login_at DATETIME,
		last_synced_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
		t.Fatalf("create identities: %v", err)
	}
	if err := db.AutoMigrate(&SyncRun{}); err != nil {
		t.Fatalf("migrate sync_runs: %v", err)
	}
	return NewRepository(db)
}

func seedUser(t *testing.T, r *Repository, id, username, email string, active bool) {
	t.Helper()
	if err := r.db.Exec(
		"INSERT INTO users (id, username, email, is_active, is_system_admin, can_access_all_tenants) VALUES (?, ?, ?, ?, ?, ?)",
		id, username, email, active, false, false).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func TestUpsertAndGetByDingUserID(t *testing.T) {
	r := setupRepo(t)
	seedUser(t, r, "u-1", "zhangsan", "zs@c.com", true)
	ctx := context.Background()

	if err := r.BindToUser(ctx, "corp-1", "ding-1", "union-1", "u-1", "张三", "138", "zs@c.com"); err != nil {
		t.Fatalf("BindToUser: %v", err)
	}
	got, err := r.GetByDingUserID(ctx, "corp-1", "ding-1")
	if err != nil {
		t.Fatalf("GetByDingUserID: %v", err)
	}
	if got.UserID != "u-1" || got.UnionID != "union-1" {
		t.Fatalf("got %+v", got)
	}
	// upsert 幂等：重复绑定更新而非报错
	if err := r.BindToUser(ctx, "corp-1", "ding-1", "union-1", "u-1", "张三三", "138", "zs@c.com"); err != nil {
		t.Fatalf("re-bind: %v", err)
	}
	got2, _ := r.GetByDingUserID(ctx, "corp-1", "ding-1")
	if got2.Nick != "张三三" {
		t.Fatalf("upsert did not update, nick=%q", got2.Nick)
	}
}

func TestGetNotFound(t *testing.T) {
	r := setupRepo(t)
	_, err := r.GetByDingUserID(context.Background(), "corp-1", "nobody")
	if err != ErrIdentityNotFound {
		t.Fatalf("expected ErrIdentityNotFound, got %v", err)
	}
}

func TestListActiveBindingsJoinsUserFlags(t *testing.T) {
	r := setupRepo(t)
	ctx := context.Background()
	seedUser(t, r, "u-1", "zhangsan", "zs@c.com", true)
	seedUser(t, r, "u-2", "admin", "admin@c.com", true)
	if err := r.db.Exec("UPDATE users SET is_system_admin = ? WHERE id = ?", true, "u-2").Error; err != nil {
		t.Fatalf("mark admin: %v", err)
	}

	r.BindToUser(ctx, "corp-1", "ding-1", "union-1", "u-1", "张三", "", "")
	r.BindToUser(ctx, "corp-1", "ding-2", "union-2", "u-2", "管理员", "", "")

	bindings, err := r.ListActiveBindings(ctx)
	if err != nil {
		t.Fatalf("ListActiveBindings: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(bindings))
	}
	var admin *ActiveBinding
	for _, b := range bindings {
		if b.DingUserID == "ding-2" {
			admin = b
		}
	}
	if admin == nil || !admin.UserIsSystemAdmin {
		t.Fatalf("admin binding flags wrong: %+v", admin)
	}
}

func TestSyncRunLifecycle(t *testing.T) {
	r := setupRepo(t)
	ctx := context.Background()

	run, err := r.CreateSyncRun(ctx, SyncTriggerCron)
	if err != nil {
		t.Fatalf("CreateSyncRun: %v", err)
	}
	if run.Status != SyncStatusRunning {
		t.Fatalf("status=%q", run.Status)
	}
	running, _ := r.HasRunningSync(ctx)
	if !running {
		t.Fatal("expected HasRunningSync true while running")
	}

	if err := r.FinishSyncRun(ctx, run.ID, SyncStatusSuccess, 10, 2, 5, 1, ""); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}
	running, _ = r.HasRunningSync(ctx)
	if running {
		t.Fatal("expected HasRunningSync false after finish")
	}

	runs, total, err := r.ListSyncRuns(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListSyncRuns: %v", err)
	}
	if total != 1 || len(runs) != 1 || runs[0].DeactivatedCount != 1 {
		t.Fatalf("runs=%+v total=%d", runs, total)
	}
}
