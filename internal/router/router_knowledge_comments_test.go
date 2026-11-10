package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type commentOwnershipLookup struct{ downloadKnowledgeLookup }

func (s *commentOwnershipLookup) GetOwningKBCreatorID(context.Context, string) (string, error) {
	return "someone", nil
}
func commentRoutesEngine(role types.TenantRole, tenant uint64, share interfaces.KBShareService) *gin.Engine {
	enabled := true
	kb := &types.KnowledgeBase{ID: "kb", TenantID: tenant, CreatorID: "someone"}
	guards := &rbacGuards{
		cfg:                &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
		knowledgeKBCreator: func(*gin.Context) (string, error) { return "someone", nil },
		knowledgeService: &commentOwnershipLookup{
			downloadKnowledgeLookup{knowledge: &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb", TenantID: tenant}},
		},
		kbService:      &stubWikiKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": kb}},
		kbShareService: share,
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		ctx = context.WithValue(ctx, types.UserIDContextKey, "nonowner")
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Next()
	})
	RegisterKnowledgeRoutes(r.Group("/api/v1"), &handler.KnowledgeHandler{}, guards)
	return r
}
func TestKnowledgeCommentWriteRoutesKeepDocumentPermissions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		role   types.TenantRole
		tenant uint64
		share  interfaces.KBShareService
	}{
		{"tenant viewer", types.TenantRoleViewer, 1, nil},
		{"non-owner contributor", types.TenantRoleContributor, 1, nil},
		{"admin with viewer share", types.TenantRoleAdmin, 2,
			&downloadKBShareStub{permission: types.OrgRoleViewer, source: 2}},
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			t.Run(tc.name+method, func(t *testing.T) {
				engine := commentRoutesEngine(tc.role, tc.tenant, tc.share)
				path := "/api/v1/knowledge/doc/comments"
				if method == http.MethodPut {
					path += "/comment"
				}
				req := httptest.NewRequest(
					method,
					path,
					strings.NewReader(`{"body":"attempt","action":"resolve","base_revision":1}`),
				)
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				require.Equal(t, http.StatusForbidden, rec.Code, "%s", rec.Body.String())
			})
		}
	}
}
