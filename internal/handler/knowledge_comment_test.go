package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type commentKBLookup struct {
	interfaces.KnowledgeBaseService
}

func (*commentKBLookup) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "kb", TenantID: 1}, nil
}

type commentUserLookup struct{ interfaces.UserService }

func (*commentUserLookup) GetUserByID(_ context.Context, id string) (*types.User, error) {
	return &types.User{ID: id, Username: "Trusted name"}, nil
}
func TestKnowledgeCommentsHTTPPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeComment{}, &types.Chunk{}))
	require.NoError(
		t,
		db.Create(
			&types.Knowledge{
				ID:              "doc",
				TenantID:        1,
				KnowledgeBaseID: "kb",
				Type:            "manual",
				Metadata:        types.JSON(`{"content":"Body","version":1}`),
			},
		).Error,
	)
	kbLookup := &commentKBLookup{}
	svc, err := service.NewKnowledgeService(
		nil,
		repository.NewKnowledgeRepository(db),
		nil,
		kbLookup,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	h := &KnowledgeHandler{kgService: svc, kbService: kbLookup, userSvc: &commentUserLookup{}}
	tenant := uint64(1)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	engine.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenant)
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleAdmin)
		ctx = context.WithValue(ctx, types.UserIDContextKey, "me")
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), tenant)
		c.Next()
	})
	engine.GET("/knowledge/:id/comments", h.ListKnowledgeComments)
	engine.GET("/knowledge-bases/:id/knowledge/document-search", h.SearchDocuments)
	engine.GET("/knowledge/:id/backlinks", h.DocumentBacklinks)
	engine.POST("/knowledge/:id/comments", h.CreateKnowledgeComment)
	engine.PUT("/knowledge/:id/comments/:comment_id", h.MutateKnowledgeComment)
	call := func(method, path string, input any, want int) map[string]any {
		raw, _ := json.Marshal(input)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		require.Equal(t, want, rec.Code, "%s", rec.Body.String())
		var result map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		return result
	}
	root := call(http.MethodPost, "/knowledge/doc/comments", map[string]any{
		"body": "Discussion", "author_id": "spoof", "author_name": "spoof",
		"quote": "Body", "content_version": 1,
	}, 200)["data"].(map[string]any)
	require.Equal(t, "me", root["author_id"])
	require.Equal(t, "Trusted name", root["author_name"])
	id := root["id"].(string)
	call(http.MethodPost, "/knowledge/doc/comments", map[string]any{"body": "Reply", "parent_id": id}, 200)
	page := call(http.MethodGet, "/knowledge/doc/comments", nil, 200)["data"].(map[string]any)
	items := page["items"].([]any)
	require.Len(t, items, 1)
	require.Len(t, items[0].(map[string]any)["replies"].([]any), 1)
	call(
		http.MethodPut,
		"/knowledge/doc/comments/"+id,
		map[string]any{"action": "resolve", "base_revision": 1, "resolved": true},
		200,
	)
	call(
		http.MethodPut,
		"/knowledge/doc/comments/"+id,
		map[string]any{"action": "resolve", "base_revision": 1, "resolved": false},
		409,
	)
	call(http.MethodPost, "/knowledge/doc/comments", map[string]any{"body": "late", "parent_id": id}, 409)
	call(
		http.MethodPost,
		"/knowledge/doc/comments",
		map[string]any{"body": "stale", "quote": "Body", "content_version": 0},
		409,
	)
	call(http.MethodGet, "/knowledge/doc/comments?offset=bad", nil, 400)
	foundResponse := call(http.MethodGet, "/knowledge-bases/kb/knowledge/document-search?query=Body", nil, 200)
	found := foundResponse["data"].(map[string]any)
	require.Len(t, found["items"].([]any), 1)
	call(http.MethodGet, "/knowledge/doc/backlinks", nil, 200)
	call(http.MethodGet, "/knowledge-bases/kb/knowledge/document-search?offset=bad", nil, 400)
	call(http.MethodGet, "/knowledge-bases/kb/knowledge/document-search?offset=-1", nil, 400)
	tenant = 2
	call(http.MethodGet, "/knowledge-bases/kb/knowledge/document-search?query=Body", nil, 403)
	call(http.MethodGet, "/knowledge/doc/backlinks", nil, 403)
	call(http.MethodGet, "/knowledge/doc/comments", nil, 403)
	var stored types.Knowledge
	require.NoError(t, db.First(&stored, "id = ?", "doc").Error)
	require.JSONEq(
		t,
		`{"content":"Body","version":1}`,
		string(stored.Metadata),
		"comments do not change document versions",
	)
}
