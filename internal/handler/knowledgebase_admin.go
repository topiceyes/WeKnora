package handler

import (
	"net/http"
	"strconv"

	errors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// knowledgebase_admin.go — platform-administrator knowledge-base listings.
//
// This is the ONE knowledge-base listing that deliberately crosses the
// tenant boundary: the target workspace id comes from the URL path instead
// of the request context, because a system administrator inspecting a
// workspace they are not a member of has nothing else to key off.
//
// Everything else stays deliberately aligned with the ordinary
// tenant-scoped list (handler/knowledgebase.go ListKnowledgeBases) so the
// two response shapes cannot drift:
//
//   - the same buildKBListResponse DTO builder, and
//   - the same creator-name enrichment.
//
// Route:  GET /api/v1/system/admin/spaces/:id/knowledge-bases
// Guard:  middleware.RequireSystemAdmin — always enforced, independent of
//         both the tenant RBAC rollout switch and EnableCrossTenantAccess.
//
// Read-only by construction: there is no matching write endpoint. A system
// administrator who needs to *change* something enters the workspace first
// (X-Tenant-ID switch, which resolveTenantRole grants a temporary Admin
// role for), then uses the ordinary tenant-scoped routes. Keeping writes
// off this surface means the authz story stays "caller's role inside the
// workspace that owns the resource".

// ListKnowledgeBasesByTenantForAdmin godoc
// @Summary      获取指定空间的知识库列表（系统管理员）
// @Description  平台系统管理员只读查看任意空间的知识库。空间 ID 取自 URL 路径而非请求上下文，
// @Description  这是唯一一个刻意跨空间的知识库列表接口。写入操作不在此暴露。
// @Tags         空间管理
// @Accept       json
// @Produce      json
// @Param        id   path      int     true  "空间 ID"
// @Success      200  {object}  map[string]interface{}  "知识库列表"
// @Failure      400  {object}  errors.AppError         "空间 ID 非法"
// @Failure      403  {object}  errors.AppError         "需要系统管理员"
// @Failure      500  {object}  errors.AppError         "服务器错误"
// @Security     Bearer
// @Router       /system/admin/spaces/{id}/knowledge-bases [get]
func (h *KnowledgeBaseHandler) ListKnowledgeBasesByTenantForAdmin(c *gin.Context) {
	ctx := c.Request.Context()

	// Same validation shape as RequirePathTenantMatch (middleware/access.go):
	// non-empty, parseable, > 0. A malformed id can't have meaningfully
	// matched the route, so it's a 400 rather than a 404.
	tenantID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || tenantID == 0 {
		_ = c.Error(errors.NewValidationError("workspace id must be a positive integer"))
		return
	}

	kbs, err := h.service.ListKnowledgeBasesByTenantID(ctx, tenantID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"target_tenant_id": tenantID})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	enrichKBCreatorNames(ctx, h.userService, kbs)

	// Pass the CALLER's tenant id, not the target's, so every row renders
	// through the cross-tenant "shared" store display. A system admin
	// browsing from outside this workspace must not see its vector-store
	// configuration — the same rule the shared-agent branch of
	// ListKnowledgeBases follows, and the same reason
	// dto.NewTenantResponsesCrossTenant redacts /tenants/all to Viewer.
	// Full detail is available after entering the workspace, where the
	// caller legitimately holds Admin.
	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    h.buildKBListResponse(ctx, kbs, callerTenantID),
	})
}
