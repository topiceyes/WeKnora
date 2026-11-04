package handler

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The workspace-transfer endpoints rely on this mapping so the UI can tell a
// retryable conflict from a hard blocker. The handler itself is a thin shell;
// covering the mapping keeps the status contract honest without spinning up a
// database-backed service.
func TestWriteKBWorkspaceTransferError(t *testing.T) {
	blocked := &service.KBWorkspaceTransferBlockedError{
		Blockers: []types.KBWorkspaceTransferBlocker{
			{Code: "im_channel_bound", Message: "an enabled IM channel references this knowledge base"},
		},
	}
	cases := []struct {
		name         string
		err          error
		wantStatus   int
		wantBlockers bool
	}{
		{"knowledge base missing", repository.ErrKnowledgeBaseNotFound, http.StatusNotFound, false},
		{"tenant missing", repository.ErrTenantNotFound, http.StatusNotFound, false},
		{"forbidden caller", service.ErrKBOwnerTransferForbidden, http.StatusForbidden, false},
		{"target already owns", service.ErrKBOwnerTransferTargetSelf, http.StatusBadRequest, false},
		{"synthetic owner", service.ErrKBOwnerTransferSynthetic, http.StatusBadRequest, false},
		{"target not a member", service.ErrKBOwnerTransferNotMember, http.StatusBadRequest, false},
		{"target inactive", service.ErrKBOwnerTransferInactive, http.StatusBadRequest, false},
		{"target lacks contributor", service.ErrKBOwnerTransferTargetRole, http.StatusBadRequest, false},
		{"same workspace", service.ErrKBWorkspaceTransferSameTenant, http.StatusBadRequest, false},
		{"target workspace inactive", service.ErrKBWorkspaceTransferTargetInactive, http.StatusBadRequest, false},
		{"service unsupported", service.ErrKBWorkspaceTransferUnsupported, http.StatusBadRequest, false},
		{"concurrent owner change", service.ErrKBOwnerTransferConflict, http.StatusConflict, false},
		{"transfer in progress", service.ErrKBWorkspaceTransferInProgress, http.StatusConflict, false},
		{"preflight blockers", blocked, http.StatusConflict, true},
	}

	handler := &KnowledgeBaseHandler{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			handled := handler.writeKBWorkspaceTransferError(c, tc.err)
			require.True(t, handled)
			require.Len(t, c.Errors, 1)

			var appErr *apperrors.AppError
			require.ErrorAs(t, c.Errors[0].Err, &appErr)
			assert.Equal(t, tc.wantStatus, appErr.HTTPCode)
			if tc.wantBlockers {
				details, ok := appErr.Details.(map[string]any)
				require.True(t, ok, "blocked transfers must expose structured blockers")
				blockers, ok := details["blockers"].([]types.KBWorkspaceTransferBlocker)
				require.True(t, ok)
				require.Len(t, blockers, 1)
				assert.Equal(t, "im_channel_bound", blockers[0].Code)
			}
		})
	}

	// Anything outside the transfer contract is left to the generic 500 path.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	assert.False(t, handler.writeKBWorkspaceTransferError(c, stderrors.New("boom")))
	assert.Empty(t, c.Errors)
}

// The handler-level mirror of OwnedKBOrAdmin must use the caller's own tenant
// (gin key, preserved by KBAccess) rather than the rewritten execution tenant.
func TestAuthorizeKBWorkspaceTransfer(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 10000, CreatorID: "creator"}

	cases := []struct {
		name       string
		callerID   string
		role       types.TenantRole
		callerTent uint64
		want       bool
	}{
		{"creator from another workspace", "creator", types.TenantRoleContributor, 10001, true},
		{"creator below the floor", "creator", types.TenantRoleViewer, 10001, false},
		{"workspace admin", "admin", types.TenantRoleAdmin, 10000, true},
		{"workspace owner", "owner", types.TenantRoleOwner, 10000, true},
		{"workspace contributor", "contrib", types.TenantRoleContributor, 10000, false},
		{"admin of a foreign workspace", "foreign-admin", types.TenantRoleAdmin, 10001, false},
		{"no caller tenant", "nobody", types.TenantRoleOwner, 0, false},
	}

	handler := &KnowledgeBaseHandler{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), types.UserIDContextKey, tc.callerID)
			ctx = context.WithValue(ctx, types.TenantRoleContextKey, tc.role)
			req := httptest.NewRequest(http.MethodPost, "/transfer-workspace", nil).WithContext(ctx)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req
			if tc.callerTent != 0 {
				c.Set(types.TenantIDContextKey.String(), tc.callerTent)
			}

			assert.Equal(t, tc.want, handler.authorizeKBWorkspaceTransfer(c, kb))
			if !tc.want {
				require.Len(t, c.Errors, 1)
			} else {
				assert.Empty(t, c.Errors)
			}
		})
	}
}
