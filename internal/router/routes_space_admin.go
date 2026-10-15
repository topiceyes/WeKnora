package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

// RegisterSpaceAdminRoutes mounts the platform-administrator space overview
// endpoints.
//
// Why a separate Register* function instead of extending
// RegisterSystemAdminRoutes: that function's signature (and its call site in
// router.go) belongs to upstream, and this surface is fork-local. Keeping it
// in its own file means upstream syncs touch one line in router.go instead of
// a signature both sides are editing.
//
// Guarding: g.SystemAdmin() = middleware.RequireSystemAdmin, which is always
// enforced — it is not modulated by the tenant RBAC rollout switch, and it is
// not gated by cfg.Tenant.EnableCrossTenantAccess either. That is deliberate:
// the EnableCrossTenantAccess switch contains the can_access_all_tenants
// attribute on ordinary users, not the platform-admin tier. See
// middleware/access.go RequireCrossTenantAccess for the same reasoning.
//
// The routes registered here are undeclared for the API-key gate (they are
// not wrapped in g.apiKeyGroup), so API keys are denied by default-deny even
// when they carry a platform scope.
//
// The sibling "list every space" read deliberately has NO endpoint here: it
// already exists as GET /tenants/all (routes_auth_tenant.go), and that guard
// now lets system administrators through. Adding a second listing would give
// the frontend two sources of truth for the same table.
func RegisterSpaceAdminRoutes(
	r *gin.RouterGroup,
	kbHandler *handler.KnowledgeBaseHandler,
	g *rbacGuards,
) {
	spaces := r.Group("/system/admin/spaces", g.SystemAdmin())
	{
		spaces.GET("/:id/knowledge-bases", kbHandler.ListKnowledgeBasesByTenantForAdmin)
	}
}
