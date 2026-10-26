package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/dingtalk"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ---------------------------------------------------------------------------
// DingTalkOrgImportService 编排测试
// fake 用接口嵌入：只实现编排依赖的少数方法，其余调用即 panic（测试路径不触达）。
// ---------------------------------------------------------------------------

type fakeImportUserService struct {
	interfaces.UserService
	// users 按 ding_user_id 键控：EnsureDingTalkUser 的返回；不在 map 里的
	// ding_user_id 视为「新建号成功」。
	usersByDingID map[string]*types.User
	ensureCalls   int
}

func (f *fakeImportUserService) EnsureDingTalkUser(
	_ context.Context, profile *dingtalk.DingTalkUserProfile, _ types.TenantProvisioningMode,
) (*types.User, bool, error) {
	f.ensureCalls++
	if u, ok := f.usersByDingID[profile.DingUserID]; ok {
		return u, false, nil
	}
	return nil, false, errors.New("unknown ding user in fake")
}

type fakeImportOrgService struct {
	interfaces.OrganizationService
	adminTenants map[uint64]bool
	members      map[uint64]*types.OrganizationTenantMember // tenantID → member
	addCalls     []types.OrganizationTenantMember
	addErr       error
	limitErr     bool
}

func (f *fakeImportOrgService) IsTenantOrgAdmin(_ context.Context, _ string, tenantID uint64) (bool, error) {
	return f.adminTenants[tenantID], nil
}

func (f *fakeImportOrgService) ListTenantMembers(_ context.Context, _ string) ([]*types.OrganizationTenantMember, error) {
	out := make([]*types.OrganizationTenantMember, 0, len(f.members))
	for _, m := range f.members {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeImportOrgService) GetTenantMember(_ context.Context, _ string, tenantID uint64) (*types.OrganizationTenantMember, error) {
	if m, ok := f.members[tenantID]; ok {
		return m, nil
	}
	return nil, errors.New("not a member")
}

func (f *fakeImportOrgService) AddTenantMember(_ context.Context, orgID string, tenantID uint64, representativeUserID string, role types.OrgMemberRole) error {
	if f.limitErr {
		return ErrOrgMemberLimitReached
	}
	if f.addErr != nil {
		return f.addErr
	}
	m := &types.OrganizationTenantMember{
		OrganizationID: orgID, TenantID: tenantID,
		RepresentativeUserID: representativeUserID, Role: role,
	}
	f.members[tenantID] = m
	f.addCalls = append(f.addCalls, *m)
	return nil
}

// setupImportSnapshot 建 sqlite 内存库（contacts + identities + users 三表），
// 返回 dingtalk 仓储。
func setupImportSnapshot(t *testing.T) (*dingtalk.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id VARCHAR(36) PRIMARY KEY, username VARCHAR(100), email VARCHAR(255), tenant_id INTEGER,
		is_active BOOLEAN, deleted_at DATETIME)`).Error; err != nil {
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
	if err := db.AutoMigrate(&dingtalk.Contact{}); err != nil {
		t.Fatalf("migrate contacts: %v", err)
	}
	return dingtalk.NewRepository(db), db
}

func seedContact(t *testing.T, repo *dingtalk.Repository, dingUserID, name string, deptIDs ...int64) {
	t.Helper()
	if err := repo.ReplaceContacts(context.Background(), "corp-1", map[string]*dingtalk.DeptUserDetail{
		dingUserID: {UserID: dingUserID, UnionID: "union-" + dingUserID, Name: name, DeptIDList: deptIDs},
	}); err != nil {
		t.Fatalf("seed contact %s: %v", dingUserID, err)
	}
}

func newImportSvc(repo *dingtalk.Repository, us interfaces.UserService, org interfaces.OrganizationService) *DingTalkOrgImportService {
	return NewDingTalkOrgImportService(repo, us, org, "corp-1")
}

func TestImportRequiresOrgAdmin(t *testing.T) {
	repo, _ := setupImportSnapshot(t)
	seedContact(t, repo, "ding-1", "张三", 100)
	org := &fakeImportOrgService{adminTenants: map[uint64]bool{42: true}, members: map[uint64]*types.OrganizationTenantMember{}}
	svc := newImportSvc(repo, &fakeImportUserService{}, org)

	_, err := svc.Import(context.Background(), "org-1", 999, []string{"ding-1"}, types.OrgRoleViewer, types.TenantProvisioningCreatePersonal)
	if !errors.Is(err, ErrOrgPermissionDenied) {
		t.Fatalf("non-admin caller must get ErrOrgPermissionDenied, got %v", err)
	}
	if _, _, err := svc.ListDepartments(context.Background(), "org-1", 999); !errors.Is(err, ErrOrgPermissionDenied) {
		t.Fatalf("ListDepartments non-admin: %v", err)
	}
	if _, _, err := svc.ListContacts(context.Background(), "org-1", 999, 0, ""); !errors.Is(err, ErrOrgPermissionDenied) {
		t.Fatalf("ListContacts non-admin: %v", err)
	}
}

func TestImportValidatesRoleAndBatch(t *testing.T) {
	repo, _ := setupImportSnapshot(t)
	org := &fakeImportOrgService{adminTenants: map[uint64]bool{42: true}, members: map[uint64]*types.OrganizationTenantMember{}}
	svc := newImportSvc(repo, &fakeImportUserService{}, org)
	ctx := context.Background()

	if _, err := svc.Import(ctx, "org-1", 42, []string{"ding-1"}, "superuser", types.TenantProvisioningCreatePersonal); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("invalid role: %v", err)
	}
	if _, err := svc.Import(ctx, "org-1", 42, nil, types.OrgRoleViewer, types.TenantProvisioningCreatePersonal); !errors.Is(err, ErrInvalidDingTalkImportBatch) {
		t.Fatalf("empty batch: %v", err)
	}
	oversize := make([]string, MaxDingTalkImportBatch+1)
	for i := range oversize {
		oversize[i] = "x"
	}
	if _, err := svc.Import(ctx, "org-1", 42, oversize, types.OrgRoleViewer, types.TenantProvisioningCreatePersonal); !errors.Is(err, ErrInvalidDingTalkImportBatch) {
		t.Fatalf("oversize batch: %v", err)
	}
}

func TestImportOnePerPath(t *testing.T) {
	repo, _ := setupImportSnapshot(t)
	ctx := context.Background()
	// 快照三人：已注册未入组 / 未注册 / 已入组。
	if err := repo.ReplaceContacts(ctx, "corp-1", map[string]*dingtalk.DeptUserDetail{
		"ding-reg":    {UserID: "ding-reg", UnionID: "union-reg", Name: "已注册", DeptIDList: []int64{100}},
		"ding-new":    {UserID: "ding-new", UnionID: "union-new", Name: "新人", DeptIDList: []int64{100}},
		"ding-member": {UserID: "ding-member", UnionID: "union-member", Name: "老成员", DeptIDList: []int64{200}},
	}); err != nil {
		t.Fatalf("seed contacts: %v", err)
	}

	us := &fakeImportUserService{usersByDingID: map[string]*types.User{
		"ding-reg":    {ID: "u-reg", TenantID: 5001, IsActive: true},
		"ding-new":    {ID: "u-new", TenantID: 5002, IsActive: true},
		"ding-member": {ID: "u-member", TenantID: 5003, IsActive: true},
	}}
	org := &fakeImportOrgService{
		adminTenants: map[uint64]bool{42: true},
		members:      map[uint64]*types.OrganizationTenantMember{5003: {TenantID: 5003, Role: types.OrgRoleViewer}},
	}
	svc := newImportSvc(repo, us, org)

	results, err := svc.Import(ctx, "org-1", 42,
		[]string{"ding-reg", "ding-new", "ding-member", "ding-ghost", "ding-reg"},
		types.OrgRoleEditor, types.TenantProvisioningCreatePersonal)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	// 重复的 ding-reg 被去重，4 条结果。
	if len(results) != 4 {
		t.Fatalf("want 4 results (dup deduped), got %d", len(results))
	}
	byID := map[string]*DingTalkImportResultItem{}
	for _, r := range results {
		byID[r.DingUserID] = r
	}
	if byID["ding-reg"].Status != DingTalkImportAdded || byID["ding-reg"].TenantID != 5001 {
		t.Fatalf("ding-reg: %+v", byID["ding-reg"])
	}
	if byID["ding-new"].Status != DingTalkImportAdded {
		t.Fatalf("ding-new: %+v", byID["ding-new"])
	}
	if byID["ding-member"].Status != DingTalkImportAlreadyMember {
		t.Fatalf("ding-member: %+v", byID["ding-member"])
	}
	if byID["ding-ghost"].Status != DingTalkImportError || byID["ding-ghost"].Error != "not_in_snapshot" {
		t.Fatalf("ding-ghost: %+v", byID["ding-ghost"])
	}
	// representative 传本人 user.ID，角色按请求值。
	if len(org.addCalls) != 2 {
		t.Fatalf("want 2 AddTenantMember calls, got %d", len(org.addCalls))
	}
	for _, call := range org.addCalls {
		if call.Role != types.OrgRoleEditor {
			t.Fatalf("role not propagated: %+v", call)
		}
		if call.RepresentativeUserID == "" {
			t.Fatalf("representative must be the imported user: %+v", call)
		}
	}
}

func TestImportSkipsDisabledAccount(t *testing.T) {
	repo, _ := setupImportSnapshot(t)
	seedContact(t, repo, "ding-off", "已禁用", 100)
	us := &fakeImportUserService{usersByDingID: map[string]*types.User{
		"ding-off": {ID: "u-off", TenantID: 5009, IsActive: false},
	}}
	org := &fakeImportOrgService{adminTenants: map[uint64]bool{42: true}, members: map[uint64]*types.OrganizationTenantMember{}}
	svc := newImportSvc(repo, us, org)

	results, err := svc.Import(context.Background(), "org-1", 42, []string{"ding-off"}, types.OrgRoleViewer, types.TenantProvisioningCreatePersonal)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if results[0].Status != DingTalkImportError || results[0].Error != "account_disabled" {
		t.Fatalf("disabled account: %+v", results[0])
	}
	if len(org.addCalls) != 0 {
		t.Fatal("disabled account must not be enrolled")
	}
}

func TestImportMemberLimitIsPerItem(t *testing.T) {
	repo, _ := setupImportSnapshot(t)
	ctx := context.Background()
	if err := repo.ReplaceContacts(ctx, "corp-1", map[string]*dingtalk.DeptUserDetail{
		"ding-a": {UserID: "ding-a", UnionID: "u-a", Name: "甲"},
		"ding-b": {UserID: "ding-b", UnionID: "u-b", Name: "乙"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	us := &fakeImportUserService{usersByDingID: map[string]*types.User{
		"ding-a": {ID: "u-a", TenantID: 6001, IsActive: true},
		"ding-b": {ID: "u-b", TenantID: 6002, IsActive: true},
	}}
	org := &fakeImportOrgService{adminTenants: map[uint64]bool{42: true}, members: map[uint64]*types.OrganizationTenantMember{}, limitErr: true}
	svc := newImportSvc(repo, us, org)

	results, err := svc.Import(ctx, "org-1", 42, []string{"ding-a", "ding-b"}, types.OrgRoleViewer, types.TenantProvisioningCreatePersonal)
	if err != nil {
		t.Fatalf("member limit must degrade per-item, not fail batch: %v", err)
	}
	for _, r := range results {
		if r.Status != DingTalkImportError || r.Error != "member_limit_reached" {
			t.Fatalf("limit item: %+v", r)
		}
	}
}

func TestListContactsStatesAndFilters(t *testing.T) {
	repo, db := setupImportSnapshot(t)
	ctx := context.Background()
	if err := repo.ReplaceContacts(ctx, "corp-1", map[string]*dingtalk.DeptUserDetail{
		"ding-unbound": {UserID: "ding-unbound", UnionID: "u1", Name: "未注册", JobNumber: "0001", DeptIDList: []int64{100}},
		"ding-reg":     {UserID: "ding-reg", UnionID: "u2", Name: "已注册", Email: "reg@c.com", DeptIDList: []int64{100}},
		"ding-member":  {UserID: "ding-member", UnionID: "u3", Name: "已加入", DeptIDList: []int64{200}},
	}); err != nil {
		t.Fatalf("seed contacts: %v", err)
	}
	// 两个绑定：一个租户不在组织（registered），一个已在组织（already_member）。
	if err := db.Exec("INSERT INTO users (id, username, email, tenant_id, is_active) VALUES (?, ?, ?, ?, ?)",
		"u-reg", "reguser", "reg@c.com", 7001, true).Error; err != nil {
		t.Fatalf("seed user u-reg: %v", err)
	}
	if err := db.Exec("INSERT INTO users (id, username, email, tenant_id, is_active) VALUES (?, ?, ?, ?, ?)",
		"u-member", "memberuser", "m@c.com", 7002, true).Error; err != nil {
		t.Fatalf("seed user u-member: %v", err)
	}
	if err := repo.BindToUser(ctx, "corp-1", "ding-reg", "u2", "u-reg", "已注册", "", "reg@c.com"); err != nil {
		t.Fatalf("bind ding-reg: %v", err)
	}
	if err := repo.BindToUser(ctx, "corp-1", "ding-member", "u3", "u-member", "已加入", "", "m@c.com"); err != nil {
		t.Fatalf("bind ding-member: %v", err)
	}

	org := &fakeImportOrgService{
		adminTenants: map[uint64]bool{42: true},
		members:      map[uint64]*types.OrganizationTenantMember{7002: {TenantID: 7002}},
	}
	svc := newImportSvc(repo, &fakeImportUserService{}, org)

	all, syncedAt, err := svc.ListContacts(ctx, "org-1", 42, 0, "")
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	if syncedAt == nil {
		t.Fatal("synced_at must be present for non-empty snapshot")
	}
	if len(all) != 3 {
		t.Fatalf("root view must include all, got %d", len(all))
	}
	stateByID := map[string]string{}
	for _, c := range all {
		stateByID[c.DingUserID] = c.State
	}
	if stateByID["ding-unbound"] != DingTalkContactUnregistered ||
		stateByID["ding-reg"] != DingTalkContactRegistered ||
		stateByID["ding-member"] != DingTalkContactAlreadyMember {
		t.Fatalf("states wrong: %v", stateByID)
	}

	// 部门过滤：dept 100 两人、dept 200 一人、根(1)全员。
	dept100, _, _ := svc.ListContacts(ctx, "org-1", 42, 100, "")
	if len(dept100) != 2 {
		t.Fatalf("dept 100 should have 2, got %d", len(dept100))
	}
	root, _, _ := svc.ListContacts(ctx, "org-1", 42, 1, "")
	if len(root) != 3 {
		t.Fatalf("root dept must not filter by dept_id_list (root never in list), got %d", len(root))
	}
	// 关键词：工号/邮箱子串。
	byKeyword, _, _ := svc.ListContacts(ctx, "org-1", 42, 0, "0001")
	if len(byKeyword) != 1 || byKeyword[0].DingUserID != "ding-unbound" {
		t.Fatalf("keyword by job number: %+v", byKeyword)
	}
}
