package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCreateModelService struct {
	interfaces.ModelService
	captured *types.Model
	err      error
}

func (s *stubCreateModelService) CreateModel(_ context.Context, model *types.Model) error {
	s.captured = model
	return s.err
}

func createModelRouter(svc interfaces.ModelService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewModelHandler(svc)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.POST("/models", func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		h.CreateModel(c)
	})
	return router
}

func postModel(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	// The handler SSRF-checks base_url with real DNS; whitelist the fixture
	// host so the test does not depend on network resolution.
	t.Setenv("SSRF_WHITELIST_EXTRA", "api.example.com")
	req := httptest.NewRequest(http.MethodPost, "/models", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), types.TenantIDContextKey, uint64(7)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

const createModelBody = `{
	"name": "shared-chat",
	"type": "KnowledgeQA",
	"source": "remote",
	"is_builtin": %s,
	"parameters": {"base_url": "https://api.example.com/v1", "api_key": "sk-x", "provider": "generic"}
}`

// The is_builtin flag must reach the service untouched; the service is the
// authority that rejects non-system-admins.
func TestCreateModelRequestPassesBuiltinFlag(t *testing.T) {
	stub := &stubCreateModelService{}
	rec := postModel(t, createModelRouter(stub), fmt.Sprintf(createModelBody, "true"))

	require.Equal(t, http.StatusCreated, rec.Code)
	require.NotNil(t, stub.captured)
	assert.True(t, stub.captured.IsBuiltin)
	assert.Equal(t, uint64(7), stub.captured.TenantID,
		"handler leaves the builtin tenant override to the service")
}

func TestCreateModelRequestBuiltinDefaultsFalse(t *testing.T) {
	stub := &stubCreateModelService{}
	rec := postModel(t, createModelRouter(stub), fmt.Sprintf(createModelBody, "false"))

	require.Equal(t, http.StatusCreated, rec.Code)
	require.NotNil(t, stub.captured)
	assert.False(t, stub.captured.IsBuiltin)
}

// A 403 from the service (non-system-admin asking for a builtin model) must
// surface as 403, not be rewrapped into a 500.
func TestCreateModelPropagatesServiceForbidden(t *testing.T) {
	stub := &stubCreateModelService{
		err: apperrors.NewForbiddenError("only system administrators can create builtin models"),
	}
	rec := postModel(t, createModelRouter(stub), fmt.Sprintf(createModelBody, "true"))

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "system administrators")
}
