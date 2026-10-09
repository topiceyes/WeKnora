package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type stubStatusUserService struct {
	interfaces.UserService
	target      *types.User
	getErr      error
	admins      []*types.User
	updateCalls int
	updated     *types.User
}

func (s *stubStatusUserService) GetUserByID(_ context.Context, _ string) (*types.User, error) {
	return s.target, s.getErr
}

func (s *stubStatusUserService) UpdateUser(_ context.Context, user *types.User) error {
	s.updateCalls++
	s.updated = user
	return nil
}

func (s *stubStatusUserService) ListSystemAdmins(
	_ context.Context, _, _ int,
) ([]*types.User, int64, error) {
	return s.admins, int64(len(s.admins)), nil
}

type stubStatusTokenRepo struct {
	interfaces.AuthTokenRepository
	revokeCalls int
	revokedUser string
	revokeErr   error
}

func (s *stubStatusTokenRepo) RevokeTokensByUserID(_ context.Context, userID string) error {
	s.revokeCalls++
	s.revokedUser = userID
	return s.revokeErr
}

// userAdminRouter mounts the endpoint the same way production does: caller
// identity/system-admin flag injected into the context (the auth middleware's
// job), then the RequireSystemAdmin gate, then the handler.
func userAdminRouter(h *UserAdminHandler, actorID string, actorIsAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, actorID)
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, actorIsAdmin)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	enabled := true
	admin := r.Group("/system/users", middleware.RequireSystemAdmin(
		&config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}))
	admin.PUT("/:id/status", h.UpdateUserStatus)
	return r
}

func doUpdateUserStatus(t *testing.T, r *gin.Engine, targetID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/system/users/"+targetID+"/status", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeStatusData(t *testing.T, w *httptest.ResponseRecorder) (id string, isActive bool) {
	t.Helper()
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			ID       string `json:"id"`
			IsActive bool   `json:"is_active"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if !resp.Success {
		t.Fatalf("success=false body=%s", w.Body.String())
	}
	return resp.Data.ID, resp.Data.IsActive
}

func TestUpdateUserStatus_DisablesUserAndRevokesTokens(t *testing.T) {
	users := &stubStatusUserService{target: &types.User{
		ID: "target-user", Username: "alice", IsActive: true,
	}}
	tokens := &stubStatusTokenRepo{}
	h := &UserAdminHandler{userSvc: users, tokenRepo: tokens}

	w := doUpdateUserStatus(t, userAdminRouter(h, "admin-user", true), "target-user",
		map[string]any{"is_active": false})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.updateCalls != 1 || users.updated == nil || users.updated.IsActive {
		t.Fatalf("update calls=%d updated=%+v", users.updateCalls, users.updated)
	}
	if tokens.revokeCalls != 1 || tokens.revokedUser != "target-user" {
		t.Fatalf("revoke calls=%d user=%q", tokens.revokeCalls, tokens.revokedUser)
	}
	id, isActive := decodeStatusData(t, w)
	if id != "target-user" || isActive {
		t.Fatalf("response id=%q is_active=%v", id, isActive)
	}
}

func TestUpdateUserStatus_EnablesUserWithoutTouchingTokens(t *testing.T) {
	users := &stubStatusUserService{target: &types.User{
		ID: "target-user", Username: "alice", IsActive: false,
	}}
	tokens := &stubStatusTokenRepo{}
	h := &UserAdminHandler{userSvc: users, tokenRepo: tokens}

	w := doUpdateUserStatus(t, userAdminRouter(h, "admin-user", true), "target-user",
		map[string]any{"is_active": true})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.updateCalls != 1 || users.updated == nil || !users.updated.IsActive {
		t.Fatalf("update calls=%d updated=%+v", users.updateCalls, users.updated)
	}
	if tokens.revokeCalls != 0 {
		t.Fatalf("enable must not revoke tokens, got %d calls", tokens.revokeCalls)
	}
	_, isActive := decodeStatusData(t, w)
	if !isActive {
		t.Fatalf("response is_active=%v", isActive)
	}
}

func TestUpdateUserStatus_RejectsSelfDisable(t *testing.T) {
	users := &stubStatusUserService{target: &types.User{
		ID: "admin-user", IsActive: true, IsSystemAdmin: true,
	}}
	tokens := &stubStatusTokenRepo{}
	h := &UserAdminHandler{userSvc: users, tokenRepo: tokens}

	w := doUpdateUserStatus(t, userAdminRouter(h, "admin-user", true), "admin-user",
		map[string]any{"is_active": false})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.updateCalls != 0 || tokens.revokeCalls != 0 {
		t.Fatalf("self disable caused side effects: updates=%d revokes=%d",
			users.updateCalls, tokens.revokeCalls)
	}
}

func TestUpdateUserStatus_RejectsDisablingLastActiveSystemAdmin(t *testing.T) {
	target := &types.User{ID: "target-admin", IsActive: true, IsSystemAdmin: true}
	users := &stubStatusUserService{
		target: target,
		admins: []*types.User{target}, // target is the only active system admin
	}
	tokens := &stubStatusTokenRepo{}
	h := &UserAdminHandler{userSvc: users, tokenRepo: tokens}

	w := doUpdateUserStatus(t, userAdminRouter(h, "admin-user", true), "target-admin",
		map[string]any{"is_active": false})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.updateCalls != 0 || tokens.revokeCalls != 0 {
		t.Fatalf("last-admin disable caused side effects: updates=%d revokes=%d",
			users.updateCalls, tokens.revokeCalls)
	}
}

func TestUpdateUserStatus_AllowsDisablingSystemAdminWhenAnotherActiveAdminExists(t *testing.T) {
	target := &types.User{ID: "target-admin", IsActive: true, IsSystemAdmin: true}
	other := &types.User{ID: "other-admin", IsActive: true, IsSystemAdmin: true}
	users := &stubStatusUserService{target: target, admins: []*types.User{target, other}}
	tokens := &stubStatusTokenRepo{}
	h := &UserAdminHandler{userSvc: users, tokenRepo: tokens}

	w := doUpdateUserStatus(t, userAdminRouter(h, "admin-user", true), "target-admin",
		map[string]any{"is_active": false})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.updateCalls != 1 || tokens.revokeCalls != 1 {
		t.Fatalf("updates=%d revokes=%d", users.updateCalls, tokens.revokeCalls)
	}
}

func TestUpdateUserStatus_ReturnsNotFoundForMissingUser(t *testing.T) {
	users := &stubStatusUserService{target: nil, getErr: errors.New("user not found")}
	tokens := &stubStatusTokenRepo{}
	h := &UserAdminHandler{userSvc: users, tokenRepo: tokens}

	w := doUpdateUserStatus(t, userAdminRouter(h, "admin-user", true), "ghost-user",
		map[string]any{"is_active": false})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.updateCalls != 0 || tokens.revokeCalls != 0 {
		t.Fatalf("missing user caused side effects: updates=%d revokes=%d",
			users.updateCalls, tokens.revokeCalls)
	}
}

func TestUpdateUserStatus_RejectsNonSystemAdmin(t *testing.T) {
	users := &stubStatusUserService{target: &types.User{ID: "target-user", IsActive: true}}
	tokens := &stubStatusTokenRepo{}
	h := &UserAdminHandler{userSvc: users, tokenRepo: tokens}

	w := doUpdateUserStatus(t, userAdminRouter(h, "regular-user", false), "target-user",
		map[string]any{"is_active": false})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.updateCalls != 0 || tokens.revokeCalls != 0 {
		t.Fatalf("non-admin call reached the handler: updates=%d revokes=%d",
			users.updateCalls, tokens.revokeCalls)
	}
}
