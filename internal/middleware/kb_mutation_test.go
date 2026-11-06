package middleware

import (
	"github.com/Tencent/WeKnora/internal/kbmutation"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKnowledgeMutationAdmissionHoldsLockThroughAuthorizationAndHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	coordinator := &kbmutation.Coordinator{BaseDir: t.TempDir()}
	r := gin.New()
	r.Use(ErrorHandler(), KnowledgeMutationAdmission(coordinator))
	assertLocked := func(c *gin.Context) {
		_, err := coordinator.Exclusive()
		require.ErrorIs(t, err, kbmutation.ErrBusy)
		c.Next()
	}
	r.POST("/api/v1/knowledge-bases/:id/knowledge/file", assertLocked, func(c *gin.Context) {
		_, nested, err := coordinator.Begin(c.Request.Context())
		require.NoError(t, err)
		nested()
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/one/knowledge/file", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	unlock, err := coordinator.Exclusive()
	require.NoError(t, err)
	require.NoError(t, unlock())
}

func TestKnowledgeMutationAdmissionRejectsWritesAndPermitsTransferAndReads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	coordinator := &kbmutation.Coordinator{BaseDir: t.TempDir()}
	unlock, err := coordinator.Exclusive()
	require.NoError(t, err)
	defer unlock()
	r := gin.New()
	r.Use(ErrorHandler(), KnowledgeMutationAdmission(coordinator))
	for _, path := range []string{"/api/v1/knowledge/batch-delete", "/api/v1/knowledge/move", "/api/v1/data-sources/:id/sync"} {
		r.POST(path, func(c *gin.Context) { t.Error("write handler ran during transfer") })
	}
	r.POST("/api/v1/knowledge-bases/:id/transfer-workspace", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/api/v1/knowledge-bases/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, path := range []string{"/api/v1/knowledge/batch-delete", "/api/v1/knowledge/move", "/api/v1/data-sources/ds/sync"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusConflict, w.Code)
		require.Contains(t, w.Body.String(), "workspace_transfer_in_progress")
	}
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases/one", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/one/transfer-workspace", nil),
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Code)
	}
}
