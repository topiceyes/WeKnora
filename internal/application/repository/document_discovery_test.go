package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDocumentSearchCurrentBodyChunkFallbackAndScope(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	require.NoError(
		t,
		db.Exec(
			`CREATE TABLE chunks(id TEXT, knowledge_id TEXT, tenant_id INTEGER, kno`+
				`wledge_base_id TEXT, content TEXT, chunk_type TEXT, chunk_index INTEGE`+
				`R, deleted_at DATETIME);`,
		).Error,
	)
	r := NewKnowledgeRepository(db).(*knowledgeRepository)
	ctx := context.Background()
	seed := func(id string, tenant uint64, kb, title, body, status string) {
		k := &types.Knowledge{
			ID:              id,
			TenantID:        tenant,
			KnowledgeBaseID: kb,
			Title:           title,
			Type:            "file",
			ParseStatus:     status,
			UpdatedAt:       time.Now(),
		}
		if body != "" {
			require.NoError(t, k.SetManualMetadata(types.NewManualKnowledgeMetadata(body, "draft", 1)))
		}
		require.NoError(t, db.Omit("custom_metadata").Create(k).Error)
	}
	chunk := func(id, doc, kb string, tenant uint64, text, kind string) {
		require.NoError(
			t,
			db.Exec(
				"INSERT INTO chunks(id, knowledge_id, tenant_id, knowledge_base_id, "+
					"content, chunk_type, chunk_index) VALUES (?,?,?,?,?,?,0)",
				id,
				doc,
				tenant,
				kb,
				text,
				kind,
			).Error,
		)
	}
	seed("edited", 1, "kb", "ordinary", "最新操作说明 100%_done", types.ParseStatusCompleted)
	chunk("stale", "edited", "kb", 1, "旧版隐藏词", "text")
	seed("imported", 1, "kb", "PDF", "", types.ParseStatusCompleted)
	chunk("first", "imported", "kb", 1, "导入正文操作说明", "text")
	chunk("second", "imported", "kb", 1, "导入正文操作说明 second", "text")
	chunk("summary", "imported", "kb", 1, "仅摘要中的隐藏词", "summary")
	seed("foreign", 2, "other", "操作说明", "操作说明", types.ParseStatusCompleted)
	seed("same-tenant-other", 1, "other", "操作说明", "操作说明", types.ParseStatusCompleted)
	seed("deleting", 1, "kb", "操作说明", "操作说明", types.ParseStatusDeleting)
	seed("soft-deleted", 1, "kb", "操作说明", "操作说明", types.ParseStatusCompleted)
	require.NoError(t, db.Where("id = ?", "soft-deleted").Delete(&types.Knowledge{}).Error)
	chunk("wrong-scope", "imported", "other", 1, "串库泄漏", "text")
	page, err := r.SearchDocuments(ctx, 1, "kb", "操作说明", 0, "")
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, "body", page.Items[0].MatchField)
	for _, q := range []string{"旧版隐藏词", "仅摘要中的隐藏词", "串库泄漏"} {
		p, err := r.SearchDocuments(ctx, 1, "kb", q, 0, "")
		require.NoError(t, err)
		require.Empty(t, p.Items, q)
	}
	page, err = r.SearchDocuments(ctx, 1, "kb", "100%_done", 0, "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	page, err = r.SearchDocuments(ctx, 1, "kb", "100%_missing", 0, "")
	require.NoError(t, err)
	require.Empty(t, page.Items)
	// Rich body text is searchable across formatting marks; URLs aren't content.
	rich := &types.StructuredDocument{
		SchemaVersion: 1,
		Content: types.EditorNode{
			Type: "doc",
			Content: []types.EditorNode{
				{
					Type: "paragraph",
					Content: []types.EditorNode{
						{Type: "text", Text: "跨格式"},
						{Type: "text", Text: "关键词", Marks: []types.EditorMark{{Type: "bold"}}},
					},
				},
			},
		},
	}
	meta := types.NewManualKnowledgeMetadata(rich.Markdown(), "draft", 1)
	meta.Document = rich
	k := &types.Knowledge{
		ID:              "rich",
		TenantID:        1,
		KnowledgeBaseID: "kb",
		Type:            "manual",
		Title:           "Rich",
		ParseStatus:     "draft",
	}
	require.NoError(t, k.SetManualMetadata(meta))
	require.NoError(t, db.Omit("custom_metadata").Create(k).Error)
	page, err = r.SearchDocuments(ctx, 1, "kb", "跨格式关键词", 0, "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "rich", page.Items[0].ID)
}
func TestDocumentSearchBacklinksCurrentReferencesAndPagination(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	require.NoError(
		t,
		db.Exec(
			`CREATE TABLE chunks(id TEXT, knowledge_id TEXT, tenant_id INTEGER, kno`+
				`wledge_base_id TEXT, content TEXT, chunk_type TEXT, chunk_index INTEGE`+
				`R, deleted_at DATETIME);`,
		).Error,
	)
	r := NewKnowledgeRepository(db).(*knowledgeRepository)
	ctx := context.Background()
	target := uuid.NewString()
	seed := func(id, kb, href string) {
		d := &types.StructuredDocument{
			SchemaVersion: 1,
			Content: types.EditorNode{
				Type: "doc",
				Content: []types.EditorNode{
					{
						Type: "paragraph",
						Content: []types.EditorNode{
							{
								Type:  "text",
								Text:  "See document",
								Marks: []types.EditorMark{{Type: "link", Attrs: map[string]any{"href": href}}},
							},
						},
					},
				},
			},
		}
		meta := types.NewManualKnowledgeMetadata(d.Markdown(), "draft", 1)
		meta.Document = d
		k := &types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: kb, Title: id, Type: "manual", ParseStatus: "draft"}
		require.NoError(t, k.SetManualMetadata(meta))
		require.NoError(t, db.Omit("custom_metadata").Create(k).Error)
	}
	href := types.DocumentLinkPrefix + target
	for i := 0; i < 23; i++ {
		seed(fmt.Sprintf("source-%02d", i), "kb", href)
	}
	seed(target, "kb", href)
	seed("foreign", "other", href)
	seed("external", "kb", "https://other.invalid"+href)
	page, err := r.SearchDocuments(ctx, 1, "kb", "", 0, target)
	require.NoError(t, err)
	require.Len(t, page.Items, 20)
	require.True(t, page.HasMore)
	next, err := r.SearchDocuments(ctx, 1, "kb", "", page.NextOffset, target)
	require.NoError(t, err)
	require.Len(t, next.Items, 3)
	require.False(t, next.HasMore)
	for _, i := range next.Items {
		for _, j := range page.Items {
			require.NotEqual(t, i.ID, j.ID)
		}
	}
	// Removing the link changes backlinks immediately without rebuilding AI data.
	require.NoError(
		t,
		db.Model(&types.Knowledge{}).
			Where("id = ?", "source-00").
			UpdateColumn("metadata", `{"content":"no reference","version":2}`).
			Error,
	)
	next, err = r.SearchDocuments(ctx, 1, "kb", "", 20, target)
	require.NoError(t, err)
	require.Len(t, next.Items, 2)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = r.SearchDocuments(cancelled, 1, "kb", "", 0, "")
	require.Error(t, err)
}
