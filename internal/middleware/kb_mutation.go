package middleware

import (
	"errors"
	"net/http"
	"strings"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/kbmutation"
	"github.com/gin-gonic/gin"
)

// KnowledgeMutationAdmission holds the gate before request authorization loads
// KB/document snapshots. Service guards cover direct and background callers.
func KnowledgeMutationAdmission(coordinator *kbmutation.Coordinator) gin.HandlerFunc {
	return func(c *gin.Context) {
		if coordinator == nil || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		path := c.FullPath()
		if path == "/api/v1/knowledge-bases/:id/transfer-workspace" {
			c.Next()
			return
		}
		guard := false
		for _, prefix := range []string{"/api/v1/knowledge-bases", "/api/v1/knowledge", "/api/v1/chunks", "/api/v1/data-sources", "/api/v1/initialization"} {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				guard = true
				break
			}
		}
		if !guard {
			c.Next()
			return
		}
		ctx, release, err := coordinator.Begin(c.Request.Context())
		if err != nil {
			if errors.Is(err, kbmutation.ErrBusy) {
				_ = c.Error(apperrors.NewConflictError("知识库正在迁移，请稍后重试").WithDetails(map[string]any{"reason": "workspace_transfer_in_progress"}))
			} else {
				_ = c.Error(apperrors.NewServiceUnavailableError("暂时无法协调知识库写入"))
			}
			c.Abort()
			return
		}
		defer release()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
