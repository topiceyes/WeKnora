package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// OrgDingTalkHandler 共享空间「从钉钉组织架构添加成员」的组织级端点
// （二开 fork 新增）。数据全部读本地同步快照（dingtalk_departments /
// dingtalk_contacts），不实时调钉钉 API。
// 路由层统一挂 Admin() 门控（见 router.RegisterOrganizationDingTalkRoutes），
// service 层另有 IsTenantOrgAdmin 双重校验（与 InviteMember 同构）。
// dingtalk_auth.enable 关闭时 container 不构造本 handler，路由不注册。
type OrgDingTalkHandler struct {
	importSvc        *service.DingTalkOrgImportService
	configInfo       *config.Config
	systemSettingSvc interfaces.SystemSettingService
}

// NewOrgDingTalkHandler 构造 handler。configInfo/systemSettingSvc 用于解析
// 新账号的默认空间模式（与注册/登录同一优先级链）。
func NewOrgDingTalkHandler(
	importSvc *service.DingTalkOrgImportService,
	configInfo *config.Config,
	systemSettingSvc interfaces.SystemSettingService,
) *OrgDingTalkHandler {
	return &OrgDingTalkHandler{
		importSvc:        importSvc,
		configInfo:       configInfo,
		systemSettingSvc: systemSettingSvc,
	}
}

// orgDingTalkCaller 从上下文取调用方 tenant（Admin() 闸门已保证登录态）。
func orgDingTalkCaller(c *gin.Context) uint64 {
	return c.GetUint64(types.TenantIDContextKey.String())
}

// mapOrgDingTalkError 把 service 层哨兵错误映射到 HTTP 错误。
func mapOrgDingTalkError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrOrgPermissionDenied):
		c.Error(apperrors.NewForbiddenError("Only organization admins can browse DingTalk contacts"))
	case errors.Is(err, service.ErrInvalidRole):
		c.Error(apperrors.NewValidationError("Invalid role; must be viewer, editor, or admin"))
	case errors.Is(err, service.ErrInvalidDingTalkImportBatch):
		c.Error(apperrors.NewValidationError("ding_user_ids must contain 1-500 entries"))
	default:
		logger.Errorf(c.Request.Context(), "org dingtalk endpoint failed: %v", err)
		c.Error(apperrors.NewInternalServerError("DingTalk contacts operation failed"))
	}
}

// ListOrgDingTalkDepartments 返回本地同步的钉钉部门树（平铺）+ 快照同步时间。
// @Summary      钉钉部门树（组织级）
// @Description  返回本地每日同步的钉钉部门平铺列表（根部门不落库，前端虚拟「全公司」根节点），供组织成员管理的钉钉导入对话框渲染部门树
// @Tags         组织管理
// @Produce      json
// @Param        id  path  string  true  "组织ID"
// @Success      200  {object}  map[string]interface{}  "{success:true, data:{departments:[{dept_id,name,parent_id}], synced_at}}"
// @Failure      403  {object}  apperrors.AppError
// @Security     Bearer
// @Router       /organizations/{id}/dingtalk/departments [get]
func (h *OrgDingTalkHandler) ListDepartments(c *gin.Context) {
	ctx := c.Request.Context()
	orgID := c.Param("id")

	depts, syncedAt, err := h.importSvc.ListDepartments(ctx, orgID, orgDingTalkCaller(c))
	if err != nil {
		mapOrgDingTalkError(c, err)
		return
	}
	departments := make([]gin.H, 0, len(depts))
	for _, d := range depts {
		departments = append(departments, gin.H{
			"dept_id":   d.DeptID,
			"name":      d.Name,
			"parent_id": d.ParentID,
		})
	}
	data := gin.H{"departments": departments}
	if syncedAt != nil {
		data["synced_at"] = syncedAt
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// ListOrgDingTalkContacts 返回本地通讯录快照（可按部门/关键词过滤），
// 逐行标注本地状态（未注册/已注册/已加入本组织）。
// @Summary      钉钉通讯录快照（组织级）
// @Description  返回本地每日同步的钉钉通讯录快照，逐行标注 state（unregistered/registered/already_member）；dept_id 为 0 或 1 时不过滤部门（全公司）
// @Tags         组织管理
// @Produce      json
// @Param        id       path   string  true   "组织ID"
// @Param        dept_id  query  int     false  "部门ID（0/1=全公司）"
// @Param        keyword  query  string  false  "关键词（姓名/手机号/邮箱/工号）"
// @Success      200  {object}  map[string]interface{}  "{success:true, data:{contacts:[DingTalkContactView], synced_at}}"
// @Failure      403  {object}  apperrors.AppError
// @Security     Bearer
// @Router       /organizations/{id}/dingtalk/contacts [get]
func (h *OrgDingTalkHandler) ListContacts(c *gin.Context) {
	ctx := c.Request.Context()
	orgID := c.Param("id")

	var deptID int64
	if raw := strings.TrimSpace(c.Query("dept_id")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			c.Error(apperrors.NewValidationError("dept_id must be an integer"))
			return
		}
		deptID = v
	}

	contacts, syncedAt, err := h.importSvc.ListContacts(ctx, orgID, orgDingTalkCaller(c), deptID, c.Query("keyword"))
	if err != nil {
		mapOrgDingTalkError(c, err)
		return
	}
	data := gin.H{"contacts": contacts}
	if syncedAt != nil {
		data["synced_at"] = syncedAt
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// importOrgDingTalkRequest 批量导入请求体。
type importOrgDingTalkRequest struct {
	DingUserIDs []string            `json:"ding_user_ids" binding:"required"`
	Role        types.OrgMemberRole `json:"role" binding:"required"`
}

// ImportOrgDingTalkMembers 批量把钉钉成员的空间拉进组织。未注册的钉钉成员
// 在导入时自动建本地账号（随机密码、仅钉钉扫码登录、建个人空间）。
// 逐项成败独立（部分成功是设计），恒 200，逐项结果放 data.results。
// @Summary      从钉钉组织架构批量导入成员
// @Description  按 ding_user_ids 批量导入：已绑定账号直接拉入；未注册的自动建号（仅钉钉登录）后拉入；已是成员的跳过。逐项结果含 added/already_member/error
// @Tags         组织管理
// @Accept       json
// @Produce      json
// @Param        id       path      string                     true  "组织ID"
// @Param        request  body      importOrgDingTalkRequest   true  "导入信息（ding_user_ids 1-500 条，role viewer/editor/admin）"
// @Success      200  {object}  map[string]interface{}  "{success:true, data:{results:[{ding_user_id,name,status,user_id,tenant_id,error}], summary:{added,already_member,failed}}}"
// @Failure      400  {object}  apperrors.AppError
// @Failure      403  {object}  apperrors.AppError
// @Security     Bearer
// @Router       /organizations/{id}/dingtalk/import [post]
func (h *OrgDingTalkHandler) ImportMembers(c *gin.Context) {
	ctx := c.Request.Context()
	orgID := c.Param("id")

	var req importOrgDingTalkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("Invalid request parameters").WithDetails(err.Error()))
		return
	}

	provisioning := resolveDefaultTenantMode(ctx, h.configInfo, h.systemSettingSvc)
	results, err := h.importSvc.Import(
		ctx, orgID, orgDingTalkCaller(c), req.DingUserIDs, req.Role, provisioning)
	if err != nil {
		mapOrgDingTalkError(c, err)
		return
	}

	summary := gin.H{"added": 0, "already_member": 0, "failed": 0}
	for _, r := range results {
		switch r.Status {
		case service.DingTalkImportAdded:
			summary["added"] = summary["added"].(int) + 1
		case service.DingTalkImportAlreadyMember:
			summary["already_member"] = summary["already_member"].(int) + 1
		default:
			summary["failed"] = summary["failed"].(int) + 1
		}
	}
	logger.Infof(ctx, "Org %s dingtalk import: added=%d already=%d failed=%d",
		orgID, summary["added"], summary["already_member"], summary["failed"])
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"results": results, "summary": summary},
	})
}
