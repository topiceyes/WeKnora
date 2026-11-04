package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Cross-workspace ownership transfer: creator_id moves to a user whose own
// workspace differs from the KB's. The handlers must accept that creator for
// delete / clear, and reject everyone else from outside the KB workspace.

const (
	crossKBTenant   = uint64(7) // KB data workspace
	crossCallerTent = uint64(8) // the transferred owner's workspace
	crossKBCreator  = "owner"
)

type crossTenantKBService struct {
	interfaces.KnowledgeBaseService
	kb      *types.KnowledgeBase
	deleted []string
}

func (s *crossTenantKBService) GetKnowledgeBaseByID(
	context.Context, string,
) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *crossTenantKBService) DeleteKnowledgeBase(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

type crossTenantKnowledgeService struct {
	interfaces.KnowledgeService
	listCalls int
}

func (s *crossTenantKnowledgeService) ListKnowledgeByKnowledgeBaseID(
	context.Context, string,
) ([]*types.Knowledge, error) {
	s.listCalls++
	return nil, nil
}

func crossTenantRouter(userID string, role types.TenantRole, register func(*gin.Engine)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: crossCallerTent, UserID: userID, Role: role,
		})
		c.Set(types.TenantIDContextKey.String(), crossCallerTent)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	register(r)
	return r
}

func crossTenantKB() *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: "kb-product", Name: "产品技术", TenantID: crossKBTenant, CreatorID: crossKBCreator,
	}
}

func TestDeleteKnowledgeBaseAllowsCrossWorkspaceCreator(t *testing.T) {
	svc := &crossTenantKBService{kb: crossTenantKB()}
	h := &KnowledgeBaseHandler{service: svc}
	r := crossTenantRouter(crossKBCreator, types.TenantRoleContributor, func(r *gin.Engine) {
		r.DELETE("/knowledge-bases/:id", h.DeleteKnowledgeBase)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/knowledge-bases/kb-product", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"kb-product"}, svc.deleted)
}

func TestDeleteKnowledgeBaseBlocksCrossWorkspaceNonCreator(t *testing.T) {
	svc := &crossTenantKBService{kb: crossTenantKB()}
	h := &KnowledgeBaseHandler{service: svc}
	r := crossTenantRouter("someone-else", types.TenantRoleOwner, func(r *gin.Engine) {
		r.DELETE("/knowledge-bases/:id", h.DeleteKnowledgeBase)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/knowledge-bases/kb-product", nil))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Empty(t, svc.deleted)
}

func TestDeleteKnowledgeBaseBlocksDemotedCrossWorkspaceCreator(t *testing.T) {
	svc := &crossTenantKBService{kb: crossTenantKB()}
	h := &KnowledgeBaseHandler{service: svc}
	// Below the CreatorRoleFloor: ResolveKB denies before the handler runs.
	r := crossTenantRouter(crossKBCreator, types.TenantRoleViewer, func(r *gin.Engine) {
		r.DELETE("/knowledge-bases/:id", h.DeleteKnowledgeBase)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/knowledge-bases/kb-product", nil))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Empty(t, svc.deleted)
}

func TestClearKnowledgeBaseContentsAllowsCrossWorkspaceCreator(t *testing.T) {
	kg := &crossTenantKnowledgeService{}
	h := &KnowledgeHandler{
		kbService: &crossTenantKBService{kb: crossTenantKB()},
		kgService: kg,
	}
	r := crossTenantRouter(crossKBCreator, types.TenantRoleContributor, func(r *gin.Engine) {
		r.DELETE("/knowledge-bases/:id/knowledge", h.ClearKnowledgeBaseContents)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(
		http.MethodDelete, "/knowledge-bases/kb-product/knowledge", nil,
	))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, kg.listCalls)
}

func TestClearKnowledgeBaseContentsBlocksCrossWorkspaceNonCreator(t *testing.T) {
	kg := &crossTenantKnowledgeService{}
	h := &KnowledgeHandler{
		kbService: &crossTenantKBService{kb: crossTenantKB()},
		kgService: kg,
	}
	r := crossTenantRouter("someone-else", types.TenantRoleOwner, func(r *gin.Engine) {
		r.DELETE("/knowledge-bases/:id/knowledge", h.ClearKnowledgeBaseContents)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(
		http.MethodDelete, "/knowledge-bases/kb-product/knowledge", nil,
	))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Zero(t, kg.listCalls)
}
