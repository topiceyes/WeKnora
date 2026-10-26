package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/dingtalk"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// MaxDingTalkImportBatch 单次批量导入上限：全公司一把导入（几百人）要装得下。
const MaxDingTalkImportBatch = 500

// 钉钉导入的联系人行状态（浏览时逐行标注）。
const (
	DingTalkContactUnregistered  = "unregistered"   // 无本地账号，导入时自动建号
	DingTalkContactRegistered    = "registered"     // 已有本地账号，未加入本组织
	DingTalkContactAlreadyMember = "already_member" // 其空间已是本组织成员
)

// 导入结果的逐项状态。
const (
	DingTalkImportAdded         = "added"
	DingTalkImportAlreadyMember = "already_member"
	DingTalkImportError         = "error"
)

// DingTalkContactView 组织浏览器用的联系人行：快照 + 本地状态标注。
type DingTalkContactView struct {
	dingtalk.Contact
	State         string `json:"state"`
	LocalUserID   string `json:"user_id,omitempty"`
	LocalUsername string `json:"username,omitempty"`
}

// DingTalkImportResultItem 批量导入的逐项结果（部分成功是设计，不进全局事务）。
type DingTalkImportResultItem struct {
	DingUserID string `json:"ding_user_id"`
	Name       string `json:"name"`
	Status     string `json:"status"` // added | already_member | error
	UserID     string `json:"user_id,omitempty"`
	TenantID   uint64 `json:"tenant_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

// DingTalkOrgImportService 共享空间「从钉钉组织架构添加」：浏览本地通讯录快照，
// 批量把钉钉成员的空间拉进组织。全程只读本地表（快照由每日同步写入），
// 不调钉钉 API；未注册者在导入时经 EnsureDingTalkUser 自动建号。
type DingTalkOrgImportService struct {
	repo        *dingtalk.Repository
	userService interfaces.UserService
	orgService  interfaces.OrganizationService
	corpID      string
}

// NewDingTalkOrgImportService 构造导入 service。corpID 来自 dingtalk_auth 配置。
func NewDingTalkOrgImportService(
	repo *dingtalk.Repository,
	userService interfaces.UserService,
	orgService interfaces.OrganizationService,
	corpID string,
) *DingTalkOrgImportService {
	return &DingTalkOrgImportService{
		repo:        repo,
		userService: userService,
		orgService:  orgService,
		corpID:      corpID,
	}
}

// requireOrgAdmin 三个方法的共同前奏：调用方空间必须是组织管理员
// （路由层 g.Admin() 之外的第二道校验，与 InviteMember 同构）。
func (s *DingTalkOrgImportService) requireOrgAdmin(ctx context.Context, orgID string, callerTenantID uint64) error {
	isAdmin, err := s.orgService.IsTenantOrgAdmin(ctx, orgID, callerTenantID)
	if err != nil {
		return err
	}
	if !isAdmin {
		return ErrOrgPermissionDenied
	}
	return nil
}

// ListDepartments 返回本地部门树平铺列表 + 快照最近同步时间（空态提示用）。
func (s *DingTalkOrgImportService) ListDepartments(
	ctx context.Context, orgID string, callerTenantID uint64,
) ([]dingtalk.Department, *time.Time, error) {
	if err := s.requireOrgAdmin(ctx, orgID, callerTenantID); err != nil {
		return nil, nil, err
	}
	depts, err := s.repo.ListDepartments(ctx)
	if err != nil {
		return nil, nil, err
	}
	syncedAt, err := s.repo.ContactsSyncedAt(ctx)
	if err != nil {
		// 同步时间戳只是展示信息，查失败不阻断浏览。
		logger.Warnf(ctx, "DingTalk org import: read contacts synced_at failed: %v", err)
		syncedAt = nil
	}
	return depts, syncedAt, nil
}

// ListContacts 返回快照联系人（可按部门/关键词过滤），逐行标注本地状态。
// deptID 为 0 或 1（根部门/全公司）时不按部门过滤——钉钉的 dept_id_list
// 不会可靠地包含根部门 1。
func (s *DingTalkOrgImportService) ListContacts(
	ctx context.Context, orgID string, callerTenantID uint64, deptID int64, keyword string,
) ([]*DingTalkContactView, *time.Time, error) {
	if err := s.requireOrgAdmin(ctx, orgID, callerTenantID); err != nil {
		return nil, nil, err
	}

	contacts, err := s.repo.ListContacts(ctx)
	if err != nil {
		return nil, nil, err
	}
	identities, err := s.repo.ListIdentitiesWithTenant(ctx, s.corpID)
	if err != nil {
		return nil, nil, err
	}
	members, err := s.orgService.ListTenantMembers(ctx, orgID)
	if err != nil {
		return nil, nil, err
	}
	syncedAt, serr := s.repo.ContactsSyncedAt(ctx)
	if serr != nil {
		logger.Warnf(ctx, "DingTalk org import: read contacts synced_at failed: %v", serr)
		syncedAt = nil
	}

	bindingByDingUserID := make(map[string]*dingtalk.IdentityWithTenant, len(identities))
	for _, id := range identities {
		bindingByDingUserID[id.DingUserID] = id
	}
	memberTenants := make(map[uint64]struct{}, len(members))
	for _, m := range members {
		memberTenants[m.TenantID] = struct{}{}
	}

	keyword = strings.ToLower(strings.TrimSpace(keyword))
	filterByDept := deptID > 1
	out := make([]*DingTalkContactView, 0, len(contacts))
	for i := range contacts {
		c := &contacts[i]
		if filterByDept && !containsInt64(c.DeptIDList, deptID) {
			continue
		}
		if keyword != "" && !contactMatchesKeyword(c, keyword) {
			continue
		}
		view := &DingTalkContactView{Contact: *c, State: DingTalkContactUnregistered}
		if binding, ok := bindingByDingUserID[c.DingUserID]; ok {
			view.LocalUserID = binding.UserID
			view.LocalUsername = binding.Username
			if _, isMember := memberTenants[binding.UserTenantID]; isMember {
				view.State = DingTalkContactAlreadyMember
			} else {
				view.State = DingTalkContactRegistered
			}
		}
		out = append(out, view)
	}
	return out, syncedAt, nil
}

// Import 批量导入：逐项把钉钉成员的空间拉进组织。无全局事务——逐项成败
// 独立返回（部分成功是设计）。幂等：重复导入同批全部 already_member，
// 不产生重复账号（EnsureDingTalkUser 绑定命中）。
func (s *DingTalkOrgImportService) Import(
	ctx context.Context,
	orgID string,
	callerTenantID uint64,
	dingUserIDs []string,
	role types.OrgMemberRole,
	provisioning types.TenantProvisioningMode,
) ([]*DingTalkImportResultItem, error) {
	if err := s.requireOrgAdmin(ctx, orgID, callerTenantID); err != nil {
		return nil, err
	}
	if !role.IsValid() {
		return nil, ErrInvalidRole
	}
	if len(dingUserIDs) == 0 || len(dingUserIDs) > MaxDingTalkImportBatch {
		return nil, ErrInvalidDingTalkImportBatch
	}

	contacts, err := s.repo.ListContacts(ctx)
	if err != nil {
		return nil, err
	}
	contactByDingUserID := make(map[string]*dingtalk.Contact, len(contacts))
	for i := range contacts {
		contactByDingUserID[contacts[i].DingUserID] = &contacts[i]
	}

	results := make([]*DingTalkImportResultItem, 0, len(dingUserIDs))
	seen := make(map[string]struct{}, len(dingUserIDs))
	for _, dingUserID := range dingUserIDs {
		dingUserID = strings.TrimSpace(dingUserID)
		if dingUserID == "" {
			continue
		}
		// 同一批里重复提交同一人：第二个起按 already_member 之外的幂等路径
		// 再跑一遍会重复建号竞态，直接跳过重复项。
		if _, dup := seen[dingUserID]; dup {
			continue
		}
		seen[dingUserID] = struct{}{}
		results = append(results, s.importOne(ctx, orgID, contactByDingUserID, dingUserID, role, provisioning))
	}
	return results, nil
}

// importOne 处理单个钉钉成员的导入。
func (s *DingTalkOrgImportService) importOne(
	ctx context.Context,
	orgID string,
	contactByDingUserID map[string]*dingtalk.Contact,
	dingUserID string,
	role types.OrgMemberRole,
	provisioning types.TenantProvisioningMode,
) *DingTalkImportResultItem {
	item := &DingTalkImportResultItem{DingUserID: dingUserID}

	contact, ok := contactByDingUserID[dingUserID]
	if !ok {
		item.Status = DingTalkImportError
		item.Error = "not_in_snapshot"
		return item
	}
	item.Name = contact.Name

	user, _, err := s.userService.EnsureDingTalkUser(ctx, contact.ToProfile(), provisioning)
	if err != nil {
		logger.Errorf(ctx, "DingTalk org import: ensure user for %s failed: %v", dingUserID, err)
		item.Status = DingTalkImportError
		item.Error = "provision_failed"
		return item
	}
	item.UserID = user.ID
	item.TenantID = user.TenantID

	if !user.IsActive {
		item.Status = DingTalkImportError
		item.Error = "account_disabled"
		return item
	}

	if _, err := s.orgService.GetTenantMember(ctx, orgID, user.TenantID); err == nil {
		item.Status = DingTalkImportAlreadyMember
		return item
	}

	// representative 传本人 user.ID——与 InviteMember 传空不同：这里拉进组织
	// 的是具体的人，名册上的代表用户标他是真实的。
	if err := s.orgService.AddTenantMember(ctx, orgID, user.TenantID, user.ID, role); err != nil {
		if errors.Is(err, ErrOrgMemberLimitReached) {
			item.Status = DingTalkImportError
			item.Error = "member_limit_reached"
			return item
		}
		logger.Errorf(ctx, "DingTalk org import: add tenant %d to org %s failed: %v",
			user.TenantID, orgID, err)
		item.Status = DingTalkImportError
		item.Error = "add_member_failed"
		return item
	}
	item.Status = DingTalkImportAdded
	return item
}

// containsInt64 判断部门 id 列表是否包含目标部门。
func containsInt64(list []int64, target int64) bool {
	for _, id := range list {
		if id == target {
			return true
		}
	}
	return false
}

// contactMatchesKeyword 关键词小写子串匹配姓名/手机号/邮箱/工号。
func contactMatchesKeyword(c *dingtalk.Contact, keyword string) bool {
	return strings.Contains(strings.ToLower(c.Name), keyword) ||
		strings.Contains(strings.ToLower(c.Mobile), keyword) ||
		strings.Contains(strings.ToLower(c.Email), keyword) ||
		strings.Contains(strings.ToLower(c.JobNumber), keyword)
}
