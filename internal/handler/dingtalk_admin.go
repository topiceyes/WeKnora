package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	dingtalkauth "github.com/Tencent/WeKnora/internal/dingtalk"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
)

// DingTalkAdminHandler 钉钉通讯录同步的系统管理端点（二开 fork 新增）。
// 路由层统一挂 SystemAdmin 门控（见 router.RegisterDingTalkAdminRoutes），
// handler 内不再重复鉴权。
type DingTalkAdminHandler struct {
	sync *dingtalkauth.SyncService
	repo *dingtalkauth.Repository
}

// NewDingTalkAdminHandler 构造 handler。sync/repo 由 container 在
// dingtalk_auth.enable 时注入。
func NewDingTalkAdminHandler(sync *dingtalkauth.SyncService, repo *dingtalkauth.Repository) *DingTalkAdminHandler {
	return &DingTalkAdminHandler{sync: sync, repo: repo}
}

// TriggerDingTalkSync godoc
// @Summary      手动触发钉钉通讯录同步
// @Description  立即执行一次全量通讯录对账（补绑定/更新资料/离职禁用）；已有同步进行中时返回 409
// @Tags         系统管理-钉钉
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /system/dingtalk/sync [post]
func (h *DingTalkAdminHandler) TriggerDingTalkSync(c *gin.Context) {
	ctx := c.Request.Context()
	run, err := h.sync.RunFullSync(ctx, dingtalkauth.SyncTriggerManual)
	if err != nil {
		if errors.Is(err, dingtalkauth.ErrSyncAlreadyRunning) {
			c.Error(apperrors.NewConflictError("已有钉钉通讯录同步进行中"))
			return
		}
		logger.Errorf(ctx, "TriggerDingTalkSync failed: %v", err)
		c.Error(apperrors.NewInternalServerError("dingtalk sync failed").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    run,
	})
}

// ListDingTalkSyncRuns godoc
// @Summary      钉钉通讯录同步历史
// @Description  分页返回同步运行记录（按开始时间倒序）
// @Tags         系统管理-钉钉
// @Produce      json
// @Param        page       query  int  false  "页码（从 1 起）"  default(1)
// @Param        page_size  query  int  false  "每页数量（最大 100）"  default(20)
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /system/dingtalk/sync/runs [get]
func (h *DingTalkAdminHandler) ListDingTalkSyncRuns(c *gin.Context) {
	ctx := c.Request.Context()
	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}

	runs, total, err := h.repo.ListSyncRuns(ctx, (page-1)*pageSize, pageSize)
	if err != nil {
		logger.Errorf(ctx, "ListDingTalkSyncRuns failed: %v", err)
		c.Error(apperrors.NewInternalServerError("failed to list dingtalk sync runs").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"runs":      runs,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// ListDingTalkIdentities godoc
// @Summary      钉钉身份绑定列表
// @Description  分页返回钉钉身份与本地账号的绑定关系；keyword 匹配昵称/手机号/邮箱/用户名
// @Tags         系统管理-钉钉
// @Produce      json
// @Param        page       query  int     false  "页码（从 1 起）"  default(1)
// @Param        page_size  query  int     false  "每页数量（最大 100）"  default(20)
// @Param        keyword    query  string  false  "按昵称/手机号/邮箱/用户名模糊筛选"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /system/dingtalk/identities [get]
func (h *DingTalkAdminHandler) ListDingTalkIdentities(c *gin.Context) {
	ctx := c.Request.Context()
	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}
	keyword := strings.TrimSpace(c.Query("keyword"))

	identities, total, err := h.repo.ListIdentities(ctx, (page-1)*pageSize, pageSize, keyword)
	if err != nil {
		logger.Errorf(ctx, "ListDingTalkIdentities failed: %v", err)
		c.Error(apperrors.NewInternalServerError("failed to list dingtalk identities").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"identities": identities,
			"total":      total,
			"page":       page,
			"page_size":  pageSize,
		},
	})
}
