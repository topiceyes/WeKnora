package service

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func testRichDocument(t *testing.T, text string) *types.StructuredDocument {
	t.Helper()
	var d types.StructuredDocument
	raw := `{"schema_version":1,"content":{"type":"doc","content":[{"type":"callou` +
		`t","attrs":{"kind":"warning"},"content":[{"type":"paragraph","content"` +
		`:[{"type":"text","text":` + string(
		mustMarshalRich(t, text),
	) + `,"marks":[{"type":"highlight","attrs":{"color":"#fff3a3"}}]}]}]}]}}`
	require.NoError(t, json.Unmarshal([]byte(raw), &d))
	return &d
}
func mustMarshalRich(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestStructuredSaveProjectionHistoryAndRestoration(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(
		t,
		&types.Knowledge{
			ID:              "doc",
			TenantID:        7,
			KnowledgeBaseID: "kb",
			Type:            "file",
			Title:           "Title",
			ParseStatus:     types.ParseStatusCompleted,
		},
	)
	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{Content: "legacy"})
	require.NoError(t, err)
	d := testRichDocument(t, "Important")
	base := 1
	updated, err := f.svc.UpdateKnowledgeContent(
		f.ctx,
		"doc",
		&types.KnowledgeContentPayload{Content: "client text must be ignored", Document: d, BaseVersion: &base},
	)
	require.NoError(t, err)
	meta, err := updated.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, "> [!WARNING]\n> Important", meta.Content)
	require.Equal(t, d, meta.Document)
	version, err := f.svc.GetKnowledgeContentVersion(f.ctx, "doc", 2)
	require.NoError(t, err)
	require.Equal(t, d, version.Document)
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{Content: "flatten rich content"})
	require.Error(t, err, "legacy clients cannot silently discard rich formatting")
	_, err = f.svc.RestoreKnowledgeContentVersion(f.ctx, "doc", 1)
	require.NoError(t, err)
	plain, err := f.reload(t, "doc").ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, "legacy", plain.Content)
	require.Nil(t, plain.Document)
	_, err = f.svc.RestoreKnowledgeContentVersion(f.ctx, "doc", 2)
	require.NoError(t, err)
	restored, err := f.reload(t, "doc").ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, d, restored.Document)
	require.Equal(t, d.Markdown(), restored.Content)
	_, err = f.svc.UpdateKnowledgeContent(
		f.ctx,
		"doc",
		&types.KnowledgeContentPayload{Content: "stale", Document: d, BaseVersion: &base},
	)
	require.Error(t, err, "rich content still uses the existing version guard")
}

func TestStructuredSaveRejectsInvalidBeforeChangingRow(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(
		t,
		&types.Knowledge{
			ID:              "doc",
			TenantID:        7,
			KnowledgeBaseID: "kb",
			Type:            "file",
			Title:           "Title",
			ParseStatus:     types.ParseStatusCompleted,
		},
	)
	d := testRichDocument(t, "Safe")
	d.Content.Content[0].Attrs["kind"] = "<script>"
	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{Document: d})
	require.Error(t, err)
	require.Empty(t, f.reload(t, "doc").Metadata)
}

func TestStructuredManualStatusOnlyUpdateKeepsDocument(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	d := testRichDocument(t, "Manual")
	k := &types.Knowledge{
		ID:              "doc",
		TenantID:        7,
		KnowledgeBaseID: "kb",
		Type:            types.KnowledgeTypeManual,
		Title:           "Title",
		ParseStatus:     "draft",
	}
	meta := types.NewManualKnowledgeMetadata(d.Markdown(), "draft", 1)
	meta.Document = d
	require.NoError(t, k.SetManualMetadata(meta))
	f.seed(t, k)
	_, err := f.svc.UpdateManualKnowledge(
		f.ctx,
		"doc",
		&types.ManualKnowledgePayload{Title: "Title", Content: "different", Status: "draft"},
	)
	require.Error(t, err)
	updated, err := f.svc.UpdateManualKnowledge(
		f.ctx,
		"doc",
		&types.ManualKnowledgePayload{Title: "Title", Content: d.Markdown(), Status: "draft"},
	)
	require.NoError(t, err)
	after, err := updated.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, d, after.Document)
}
