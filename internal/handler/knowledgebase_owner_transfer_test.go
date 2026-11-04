package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTransferKBService records the transfer call and replays a canned error.
type stubTransferKBService struct {
	interfaces.KnowledgeBaseService
	kb       *types.KnowledgeBase
	err      error
	called   bool
	kbIDArg  string
	target   string
	cands    []*types.KBTransferCandidate
	candErr  error
	queryArg string
	limitArg int
}

func (s *stubTransferKBService) ListKnowledgeBaseTransferCandidates(
	_ context.Context, kbID, query string, limit int,
) ([]*types.KBTransferCandidate, error) {
	s.called = true
	s.kbIDArg = kbID
	s.queryArg = query
	s.limitArg = limit
	return s.cands, s.candErr
}

// transferCandidateUserService only implements the batched lookup the
// candidate handler uses.
type transferCandidateUserService struct {
	interfaces.UserService
	users map[string]*types.User
}

func (s *transferCandidateUserService) GetUsersByIDs(
	_ context.Context, ids []string,
) (map[string]*types.User, error) {
	out := make(map[string]*types.User, len(ids))
	for _, id := range ids {
		if u, ok := s.users[id]; ok {
			out[id] = u
		}
	}
	return out, nil
}

func (s *stubTransferKBService) TransferKnowledgeBaseOwnership(
	_ context.Context, kbID, newOwnerUserID string,
) (*types.KnowledgeBase, error) {
	s.called = true
	s.kbIDArg = kbID
	s.target = newOwnerUserID
	return s.kb, s.err
}

func transferRouter(svc interfaces.KnowledgeBaseService, grant *middleware.KBAccess, callerTenant uint64) *gin.Engine {
	return transferRouterAs(svc, grant, callerTenant, "caller", types.TenantRoleContributor)
}

func transferRouterAs(
	svc interfaces.KnowledgeBaseService,
	grant *middleware.KBAccess,
	callerTenant uint64,
	callerID string,
	callerRole types.TenantRole,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: callerTenant, UserID: callerID, Role: callerRole,
		})
		ctx = context.WithValue(ctx, types.TenantIDContextKey, callerTenant)
		ctx = context.WithValue(ctx, types.UserIDContextKey, callerID)
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, callerRole)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), callerTenant)
		if grant != nil {
			c.Set(middleware.KBAccessContextKey, grant)
		}
		c.Next()
	})
	h := &KnowledgeBaseHandler{service: svc}
	r.POST("/:id/transfer-owner", h.TransferKnowledgeBaseOwnership)
	return r
}

func doTransferRequest(r *gin.Engine, kbID, body string) (*httptest.ResponseRecorder, map[string]any) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/"+kbID+"/transfer-owner", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var parsed map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	}
	return w, parsed
}

func ownTenantGrant(kb *types.KnowledgeBase) *middleware.KBAccess {
	return &middleware.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.Caller{TenantID: 1, UserID: "caller", Role: types.TenantRoleContributor},
		EffectiveTenantID: kb.TenantID,
		Permission:        types.OrgRoleAdmin,
	}
}

func transferCandidatesRouter(
	svc interfaces.KnowledgeBaseService,
	users interfaces.UserService,
	grant *middleware.KBAccess,
	callerTenant uint64,
	callerID string,
	callerRole types.TenantRole,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: callerTenant, UserID: callerID, Role: callerRole,
		})
		ctx = context.WithValue(ctx, types.TenantIDContextKey, callerTenant)
		ctx = context.WithValue(ctx, types.UserIDContextKey, callerID)
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, callerRole)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), callerTenant)
		if grant != nil {
			c.Set(middleware.KBAccessContextKey, grant)
		}
		c.Next()
	})
	h := &KnowledgeBaseHandler{service: svc, userService: users}
	r.GET("/:id/transfer-candidates", h.ListKnowledgeBaseTransferCandidates)
	return r
}

func TestListKnowledgeBaseTransferCandidates_HandlerProjectsUsers(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "caller"}
	grant := &middleware.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.Caller{TenantID: 1, UserID: "caller", Role: types.TenantRoleContributor},
		EffectiveTenantID: 1, Permission: types.OrgRoleAdmin,
	}
	svc := &stubTransferKBService{cands: []*types.KBTransferCandidate{
		{UserID: "local", TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive},
		{UserID: "remote", TenantID: 2, TenantName: "售前团队", Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
	}}
	users := &transferCandidateUserService{users: map[string]*types.User{
		"local":  {ID: "local", Username: "李四", Email: "lisi@example.com"},
		"remote": {ID: "remote", Username: "吴友发-0132", Email: "wuyf@example.com"},
	}}
	r := transferCandidatesRouter(svc, users, grant, 1, "caller", types.TenantRoleContributor)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/kb-1/transfer-candidates?q=wu&limit=7", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "wu", svc.queryArg)
	assert.Equal(t, 7, svc.limitArg)

	var parsed struct {
		Data []KBTransferCandidateView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	require.Len(t, parsed.Data, 2)
	assert.Equal(t, "李四", parsed.Data[0].Username)
	assert.Equal(t, "lisi@example.com", parsed.Data[0].Email)
	assert.Equal(t, "吴友发-0132", parsed.Data[1].Username)
	assert.Empty(t, parsed.Data[1].Email, "other workspaces' emails must not be exposed")
	assert.Equal(t, "售前团队", parsed.Data[1].TenantName)
}

func TestListKnowledgeBaseTransferCandidates_HandlerCrossWorkspaceCreatorAllowed(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "remote-owner"}
	grant := &middleware.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.Caller{TenantID: 2, UserID: "remote-owner", Role: types.TenantRoleOwner},
		EffectiveTenantID: 1, Permission: types.OrgRoleAdmin,
	}
	svc := &stubTransferKBService{}
	r := transferCandidatesRouter(svc, &transferCandidateUserService{}, grant, 2, "remote-owner", types.TenantRoleOwner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/kb-1/transfer-candidates", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, svc.called)
}

func TestListKnowledgeBaseTransferCandidates_HandlerErrors(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "caller"}
	grant := &middleware.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.Caller{TenantID: 1, UserID: "caller", Role: types.TenantRoleContributor},
		EffectiveTenantID: 1, Permission: types.OrgRoleAdmin,
	}

	t.Run("forbidden", func(t *testing.T) {
		svc := &stubTransferKBService{candErr: service.ErrKBOwnerTransferForbidden}
		r := transferCandidatesRouter(svc, &transferCandidateUserService{}, grant, 1, "caller", types.TenantRoleContributor)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/kb-1/transfer-candidates", nil))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing grant", func(t *testing.T) {
		svc := &stubTransferKBService{}
		r := transferCandidatesRouter(svc, &transferCandidateUserService{}, nil, 1, "caller", types.TenantRoleContributor)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/kb-1/transfer-candidates", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.False(t, svc.called)
	})
}

func TestTransferKnowledgeBaseOwnership_HandlerSuccess(t *testing.T) {
	svc := &stubTransferKBService{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "target"}}
	r := transferRouter(svc, ownTenantGrant(&types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "caller"}), 1)

	w, parsed := doTransferRequest(r, "kb-1", `{"new_owner_user_id":"target"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, svc.called)
	assert.Equal(t, "kb-1", svc.kbIDArg)
	assert.Equal(t, "target", svc.target)
	data, ok := parsed["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "target", data["creator_id"])
}

func TestTransferKnowledgeBaseOwnership_CrossWorkspaceGrantForbidden(t *testing.T) {
	// KB shared from tenant 1 while the caller's own tenant is 2.
	sharedGrant := &middleware.KBAccess{
		KnowledgeBase:     &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "owner-t1"},
		Caller:            types.Caller{TenantID: 2, UserID: "caller", Role: types.TenantRoleContributor},
		EffectiveTenantID: 1,
		Permission:        types.OrgRoleEditor,
	}
	svc := &stubTransferKBService{}
	r := transferRouter(svc, sharedGrant, 2)

	w, _ := doTransferRequest(r, "kb-1", `{"new_owner_user_id":"target"}`)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, svc.called, "cross-workspace grants must never reach the service")
}

func TestTransferKnowledgeBaseOwnership_CrossWorkspaceCreatorAllowed(t *testing.T) {
	// After an earlier cross-workspace transfer the creator sits in tenant 2
	// while the KB remains in tenant 1; they must still be able to hand it on.
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "remote-owner"}
	grant := &middleware.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.Caller{TenantID: 2, UserID: "remote-owner", Role: types.TenantRoleOwner},
		EffectiveTenantID: 1,
		Permission:        types.OrgRoleAdmin,
	}
	svc := &stubTransferKBService{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "target"}}
	r := transferRouterAs(svc, grant, 2, "remote-owner", types.TenantRoleOwner)

	w, _ := doTransferRequest(r, "kb-1", `{"new_owner_user_id":"target"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, svc.called)
}

func TestTransferKnowledgeBaseOwnership_MissingGrantNotFound(t *testing.T) {
	svc := &stubTransferKBService{}
	r := transferRouter(svc, nil, 1)

	w, _ := doTransferRequest(r, "kb-1", `{"new_owner_user_id":"target"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.False(t, svc.called)
}

func TestTransferKnowledgeBaseOwnership_MissingBodyField(t *testing.T) {
	svc := &stubTransferKBService{}
	r := transferRouter(svc, ownTenantGrant(&types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "caller"}), 1)

	w, _ := doTransferRequest(r, "kb-1", `{}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, svc.called)
}

func TestTransferKnowledgeBaseOwnership_ErrorMapping(t *testing.T) {
	grant := ownTenantGrant(&types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "caller"})
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not_found", apprepo.ErrKnowledgeBaseNotFound, http.StatusNotFound},
		{"forbidden", service.ErrKBOwnerTransferForbidden, http.StatusForbidden},
		{"self", service.ErrKBOwnerTransferTargetSelf, http.StatusBadRequest},
		{"synthetic", service.ErrKBOwnerTransferSynthetic, http.StatusBadRequest},
		{"not_member", service.ErrKBOwnerTransferNotMember, http.StatusBadRequest},
		{"inactive", service.ErrKBOwnerTransferInactive, http.StatusBadRequest},
		{"target_role", service.ErrKBOwnerTransferTargetRole, http.StatusBadRequest},
		{"conflict", service.ErrKBOwnerTransferConflict, http.StatusConflict},
		{"internal", context.DeadlineExceeded, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubTransferKBService{err: tc.err}
			r := transferRouter(svc, grant, 1)
			w, _ := doTransferRequest(r, "kb-1", `{"new_owner_user_id":"target"}`)
			assert.Equal(t, tc.want, w.Code, w.Body.String())
			assert.True(t, svc.called)
		})
	}
}
