package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// POST /models and DELETE /models/:id were widened from Admin to
// AdminOrSystemAdmin so platform administrators can manage built-in models
// without holding an admin role in the active workspace. The service layer
// still restricts the builtin flag/row itself to system admins.
func TestModelMutationRoutesAdmitSystemAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enforce := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enforce}}
	g := &rbacGuards{cfg: cfg}

	cases := []struct {
		name        string
		systemAdmin bool
		role        types.TenantRole
		wantGate    bool
	}{
		{"system admin with viewer workspace role passes", true, types.TenantRoleViewer, true},
		{"workspace admin passes", false, types.TenantRoleAdmin, true},
		{"workspace viewer rejected", false, types.TenantRoleViewer, false},
	}

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/models"},
		{http.MethodDelete, "/api/v1/models/some-id"},
	} {
		for _, tc := range cases {
			t.Run(route.method+" "+route.path+" "+tc.name, func(t *testing.T) {
				r := gin.New()
				r.Use(gin.Recovery())
				v1 := r.Group("/api/v1", func(c *gin.Context) {
					ctx := c.Request.Context()
					ctx = context.WithValue(ctx, types.TenantRoleContextKey, tc.role)
					ctx = context.WithValue(ctx, types.SystemAdminContextKey, tc.systemAdmin)
					c.Request = c.Request.WithContext(ctx)
					c.Next()
				})
				RegisterModelRoutes(v1, &handler.ModelHandler{}, &handler.ModelCredentialsHandler{}, g)

				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
				if tc.wantGate {
					require.NotEqual(t, http.StatusForbidden, w.Code,
						"role gate must admit the caller (handler failure codes are fine)")
				} else {
					require.Equal(t, http.StatusForbidden, w.Code)
				}
			})
		}
	}
}
