package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// knowledgebase_admin_test.go pins the two properties that make
// ListKnowledgeBasesByTenantForAdmin safe to expose at the platform tier:
//
//  1. the workspace it reads is the one named in the URL, never the
//     caller's own (that's the entire point — and the entire risk), and
//  2. the rows render through the cross-tenant "shared" store display, so
//     an administrator browsing from outside cannot read another
//     workspace's vector-store configuration out of the list.
//
// Route-level authorization (RequireSystemAdmin) is exercised in
// middleware/rbac_test.go; these tests mount the handler directly so a
// failure points at the handler rather than at the guard.

// stubKBByTenantService implements just enough of KnowledgeBaseService to
// drive the admin listing. Embedding the interface keeps every other method
// nil-panicky on purpose, so a future test reaching outside the contract
// fails loudly instead of silently exercising a zero value.
type stubKBByTenantService struct {
	interfaces.KnowledgeBaseService
	listByTenant    func(ctx context.Context, tenantID uint64) ([]*types.KnowledgeBase, error)
	requestedTenant uint64
}

func (s *stubKBByTenantService) ListKnowledgeBasesByTenantID(
	ctx context.Context, tenantID uint64,
) ([]*types.KnowledgeBase, error) {
	s.requestedTenant = tenantID
	return s.listByTenant(ctx, tenantID)
}

// newSpaceAdminKBTestRouter mirrors the production wiring: the real
// ErrorHandler (so c.Error renders the standard envelope) and a context
// carrying the CALLER's home workspace, which must differ from the
// workspace named in the path for these tests to mean anything.
const callerHomeTenant = uint64(7)
const inspectedTenant = uint64(99)

func newSpaceAdminKBTestRouter(svc interfaces.KnowledgeBaseService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), callerHomeTenant)
		c.Set(types.UserIDContextKey.String(), "u-admin")
		c.Next()
	})
	h := &KnowledgeBaseHandler{service: svc}
	r.GET("/system/admin/spaces/:id/knowledge-bases", h.ListKnowledgeBasesByTenantForAdmin)
	return r
}

func doAdminKBRequest(t *testing.T, path string, svc interfaces.KnowledgeBaseService) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	newSpaceAdminKBTestRouter(svc).ServeHTTP(w, req)
	return w
}

func TestAdminKBList_RejectsMalformedSpaceID(t *testing.T) {
	svc := &stubKBByTenantService{
		listByTenant: func(context.Context, uint64) ([]*types.KnowledgeBase, error) {
			t.Fatal("service must not be reached for a malformed workspace id")
			return nil, nil
		},
	}
	w := doAdminKBRequest(t, "/system/admin/spaces/abc/knowledge-bases", svc)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric workspace id must map to 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminKBList_RejectsZeroSpaceID(t *testing.T) {
	svc := &stubKBByTenantService{
		listByTenant: func(context.Context, uint64) ([]*types.KnowledgeBase, error) {
			t.Fatal("service must not be reached for workspace id 0")
			return nil, nil
		},
	}
	w := doAdminKBRequest(t, "/system/admin/spaces/0/knowledge-bases", svc)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("zero workspace id must map to 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminKBList_ReadsTheWorkspaceNamedInThePath(t *testing.T) {
	// The core assertion: the id that reaches the service is the PATH id,
	// not the caller's contextual workspace. A regression here would turn
	// the endpoint into a silent no-op (always returning the admin's own
	// knowledge bases) — a failure mode that looks like success in the UI.
	svc := &stubKBByTenantService{
		listByTenant: func(_ context.Context, tenantID uint64) ([]*types.KnowledgeBase, error) {
			return []*types.KnowledgeBase{
				{ID: "kb-target", Name: "Target KB", TenantID: tenantID},
			}, nil
		},
	}
	w := doAdminKBRequest(t, "/system/admin/spaces/99/knowledge-bases", svc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if svc.requestedTenant != inspectedTenant {
		t.Fatalf("service must be asked for workspace %d, got %d", inspectedTenant, svc.requestedTenant)
	}

	var body struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if !body.Success || len(body.Data) != 1 {
		t.Fatalf("expected success with 1 item, got success=%v items=%d", body.Success, len(body.Data))
	}
	if got := body.Data[0]["id"]; got != "kb-target" {
		t.Fatalf("expected kb-target, got %v", got)
	}
}

func TestAdminKBList_CrossTenantRowsRenderAsSharedStore(t *testing.T) {
	// A KB belonging to the inspected workspace, with a bound vector store.
	// Because the caller's home workspace differs, the row must render via
	// SharedStoreDisplay: vector_store_source == "shared" and no
	// vector_store_id. This is the same suppression contract
	// dto.NewTenantResponsesCrossTenant applies to /tenants/all.
	storeID := "vs-of-tenant-99"
	svc := &stubKBByTenantService{
		listByTenant: func(_ context.Context, tenantID uint64) ([]*types.KnowledgeBase, error) {
			return []*types.KnowledgeBase{
				{ID: "kb-target", Name: "Target KB", TenantID: tenantID, VectorStoreID: &storeID},
			}, nil
		},
	}
	w := doAdminKBRequest(t, "/system/admin/spaces/99/knowledge-bases", svc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var body struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(body.Data))
	}
	row := body.Data[0]
	if got := row["vector_store_source"]; got != types.StoreSourceShared {
		t.Fatalf("cross-tenant row must render as shared, got vector_store_source=%v", got)
	}
	if _, leaked := row["vector_store_id"]; leaked {
		t.Fatalf("cross-tenant row must not expose vector_store_id, body=%s", w.Body.String())
	}
}

func TestAdminKBList_PropagatesServiceError(t *testing.T) {
	svc := &stubKBByTenantService{
		listByTenant: func(context.Context, uint64) ([]*types.KnowledgeBase, error) {
			return nil, errStubListFailure
		},
	}
	w := doAdminKBRequest(t, "/system/admin/spaces/99/knowledge-bases", svc)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("service failure must map to 500, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "success") {
		t.Fatalf("error must use the standard envelope, body=%s", w.Body.String())
	}
}

var errStubListFailure = stubListError("list failed")

type stubListError string

func (e stubListError) Error() string { return string(e) }
