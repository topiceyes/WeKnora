package service

import (
	"context"
	"regexp"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/dingtalk"
	"github.com/Tencent/WeKnora/internal/types"
)

// collidingUserRepo 让 GetUserByUsername 永远命中已有用户，模拟用户名撞名。
type collidingUserRepo struct {
	stubUserRepoForAuth
}

func (c *collidingUserRepo) GetUserByUsername(context.Context, string) (*types.User, error) {
	return &types.User{ID: "taken", Username: "taken"}, nil
}

func TestGenerateDingTalkUsernameCanonical(t *testing.T) {
	// 规范格式：姓名-工号（无随机段）
	svc := &userService{userRepo: &stubUserRepoForAuth{}}
	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{
		Nick: "汪利辉", JobNumber: "0003",
	})
	if name != "汪利辉-0003" {
		t.Fatalf("generateDingTalkUsername(汪利辉, 0003) = %q, want 汪利辉-0003", name)
	}
}

func TestGenerateDingTalkUsernameNameOnlyFallback(t *testing.T) {
	// 无工号：姓名-4位随机
	svc := &userService{userRepo: &stubUserRepoForAuth{}}
	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{
		Nick: "汪利辉",
	})
	if !regexp.MustCompile(`^汪利辉-[a-z0-9]{4}$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(no job) = %q, want 姓名 + 4 random chars", name)
	}
}

func TestGenerateDingTalkUsernameNothingFallback(t *testing.T) {
	// 姓名工号全无：dt-8位随机兜底
	svc := &userService{userRepo: &stubUserRepoForAuth{}}
	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{})
	if !regexp.MustCompile(`^dt-[a-z0-9]{8}$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(nothing) = %q, want dt- + 8 random chars", name)
	}
}

func TestGenerateDingTalkUsernameCollisionFallsBackToTimestamp(t *testing.T) {
	// 查重永远命中（规范名是确定性的，撞 20 次）→ 时间戳兜底。
	svc := &userService{userRepo: &collidingUserRepo{}}
	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{
		Nick: "张三", JobNumber: "8823",
	})
	if !regexp.MustCompile(`^dt-[0-9]+$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(collision) = %q, want dt- + timestamp fallback", name)
	}
}

// ---------------------------------------------------------------------------
// EnsureDingTalkUser（组织批量导入的建号入口）
// ---------------------------------------------------------------------------

// setupDingTalkIdentityRepo 起 sqlite 内存库建 identity 表，返回真实仓储与
// db 句柄（db 仅用于测试断言行数）。
func setupDingTalkIdentityRepo(t *testing.T) (*dingtalk.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
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
	return dingtalk.NewRepository(db), db
}

func TestEnsureDingTalkUserBoundByDingUserID(t *testing.T) {
	repo, _ := setupDingTalkIdentityRepo(t)
	ctx := context.Background()
	if err := repo.BindToUser(ctx, "corp-1", "ding-1", "union-1", "u-1", "张三", "", "zs@c.com"); err != nil {
		t.Fatalf("BindToUser: %v", err)
	}
	svc := &userService{
		userRepo:     &stubUserRepoForAuth{users: map[string]*types.User{"u-1": {ID: "u-1", Email: "zs@c.com", IsActive: true}}},
		dingtalkRepo: repo,
	}

	user, created, err := svc.EnsureDingTalkUser(ctx, &dingtalk.DingTalkUserProfile{
		CorpID: "corp-1", DingUserID: "ding-1", UnionID: "union-1", Nick: "张三",
	}, types.TenantProvisioningCreatePersonal)
	if err != nil || created {
		t.Fatalf("EnsureDingTalkUser: created=%v err=%v", created, err)
	}
	if user.ID != "u-1" {
		t.Fatalf("want bound user u-1, got %s", user.ID)
	}
}

// TestEnsureDingTalkUserUpgradesPlaceholder 登录期留下的 "union:" 占位绑定，
// 导入时持有真实 userid → 原行 Update 升级，不插新行（user_id 唯一索引保护）。
func TestEnsureDingTalkUserUpgradesPlaceholder(t *testing.T) {
	repo, db := setupDingTalkIdentityRepo(t)
	ctx := context.Background()
	if err := repo.BindToUser(ctx, "corp-1", "union:union-9", "union-9", "u-9", "占位", "", "ph@dingtalk.local"); err != nil {
		t.Fatalf("seed placeholder: %v", err)
	}
	svc := &userService{
		userRepo:     &stubUserRepoForAuth{users: map[string]*types.User{"u-9": {ID: "u-9", Email: "ph@dingtalk.local", IsActive: true}}},
		dingtalkRepo: repo,
	}

	user, created, err := svc.EnsureDingTalkUser(ctx, &dingtalk.DingTalkUserProfile{
		CorpID: "corp-1", DingUserID: "ding-real-9", UnionID: "union-9",
		Nick: "汪利辉", JobNumber: "0003",
	}, types.TenantProvisioningCreatePersonal)
	if err != nil || created {
		t.Fatalf("EnsureDingTalkUser: created=%v err=%v", created, err)
	}
	if user.ID != "u-9" {
		t.Fatalf("want placeholder user u-9, got %s", user.ID)
	}

	// 原行升级成真实 userid，全表仍只有一行。
	upgraded, err := repo.GetByDingUserID(ctx, "corp-1", "ding-real-9")
	if err != nil {
		t.Fatalf("GetByDingUserID after upgrade: %v", err)
	}
	if upgraded.UserID != "u-9" || upgraded.Nick != "汪利辉" || upgraded.JobNumber != "0003" {
		t.Fatalf("placeholder upgrade wrong: %+v", upgraded)
	}
	var count int64
	if err := db.Model(&dingtalk.Identity{}).Count(&count).Error; err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if count != 1 {
		t.Fatalf("placeholder upgrade must not insert new row, got %d rows", count)
	}
}

func TestEnsureDingTalkUserMergesByEmail(t *testing.T) {
	repo, _ := setupDingTalkIdentityRepo(t)
	ctx := context.Background()
	svc := &userService{
		userRepo:     &stubUserRepoForAuth{users: map[string]*types.User{"u-2": {ID: "u-2", Email: "exist@c.com", IsActive: true}}},
		dingtalkRepo: repo,
	}

	user, created, err := svc.EnsureDingTalkUser(ctx, &dingtalk.DingTalkUserProfile{
		CorpID: "corp-1", DingUserID: "ding-2", UnionID: "union-2",
		Nick: "李四", Email: "exist@c.com",
	}, types.TenantProvisioningCreatePersonal)
	if err != nil || created {
		t.Fatalf("EnsureDingTalkUser: created=%v err=%v", created, err)
	}
	if user.ID != "u-2" {
		t.Fatalf("want email-matched user u-2, got %s", user.ID)
	}
	// 邮箱合并后建立了绑定。
	identity, err := repo.GetByDingUserID(ctx, "corp-1", "ding-2")
	if err != nil {
		t.Fatalf("email merge must bind identity: %v", err)
	}
	if identity.UserID != "u-2" {
		t.Fatalf("identity bound to %s, want u-2", identity.UserID)
	}
}
