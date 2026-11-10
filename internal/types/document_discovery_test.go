package types

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

const discoveryID = "12345678-1234-4234-8234-123456789abc"

func TestDocumentLinksAreCanonicalAndOriginIndependent(t *testing.T) {
	href := DocumentLinkPrefix + discoveryID
	require.Equal(t, discoveryID, LinkedDocumentID(href))
	for _, bad := range []string{
		"https://other.invalid" + href, "//other.invalid" + href, href + "?x=1",
		href + "#x", href + "/", DocumentLinkPrefix + "invalid",
		DocumentLinkPrefix + strings.ToUpper(discoveryID), "/platform/documents/open/%31" + discoveryID[1:],
	} {
		require.Empty(t, LinkedDocumentID(bad), bad)
	}
}
func TestStructuredSearchTextAndBacklinks(t *testing.T) {
	doc := &StructuredDocument{SchemaVersion: 1, Content: EditorNode{Type: "doc", Content: []EditorNode{
		{
			Type: "paragraph",
			Content: []EditorNode{
				{Type: "text", Text: "跨格式"},
				{Type: "text", Text: "检索", Marks: []EditorMark{{Type: "bold"}}},
			},
		},
		{
			Type: "paragraph",
			Content: []EditorNode{
				{
					Type: "text",
					Text: "引用",
					Marks: []EditorMark{
						{Type: "link", Attrs: map[string]any{"href": DocumentLinkPrefix + discoveryID}},
					},
				},
			},
		},
	}}}
	require.Contains(t, doc.SearchText(), "跨格式检索")
	require.NotContains(t, doc.SearchText(), discoveryID)
	require.NotContains(t, doc.SearchText(), "检索引用", "paragraphs are separate")
	require.True(t, doc.ReferencesDocument(discoveryID))
	doc.Content.Content[1].Content[0].Marks[0].Attrs["href"] =
		"https://other.invalid" + DocumentLinkPrefix + discoveryID
	require.False(t, doc.ReferencesDocument(discoveryID))
	doc.Content.Content[1].Type = "codeBlock"
	doc.Content.Content[1].Content[0].Marks[0].Attrs["href"] = DocumentLinkPrefix + discoveryID
	require.False(t, doc.ReferencesDocument(discoveryID), "a code sample is not a rendered link")
}
func TestDocumentSnippetLiteralUnicodeAndBounds(t *testing.T) {
	text := strings.Repeat("中文", 100) + "操作%_示例" + strings.Repeat("中文", 100)
	snippet := DocumentSnippet(text, "操作%_示例")
	require.Contains(t, snippet, "操作%_示例")
	require.LessOrEqual(t, len([]rune(snippet)), 182)
	require.True(t, strings.HasPrefix(snippet, "…"))
	require.True(t, strings.HasSuffix(snippet, "…"))
	require.Equal(t, "正文", DocumentSnippet("正文", "missing"))
	require.Equal(t, 0, DocumentMatchIndex("ABC", "abc"))
}
