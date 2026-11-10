package types

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStructuredDocumentProjectionContract(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/structured-documents.json")
	require.NoError(t, err)
	var cases []struct {
		Name     string
		Document StructuredDocument
		Markdown string
	}
	require.NoError(t, json.Unmarshal(raw, &cases))
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			require.NoError(t, c.Document.Validate())
			require.Equal(t, c.Markdown, c.Document.Markdown())
		})
	}
}

func TestStructuredDocumentRejectsUnsupportedOrDangerousStructure(t *testing.T) {
	cases := []string{
		`{"schema_version":1,"content":{"type":"doc","content":[{"type":"paragr` +
			`aph","content":[{"type":"text","text":"link","marks":[{"type":"link","` +
			`attrs":{"href":"https://example.com","title":42}}]}]}]}}`,
		`{"schema_version":2,"content":{"type":"doc","content":[{"type":"paragraph"}]}}`,
		`{"schema_version":1,"content":{"type":"doc","content":[{"type":"iframe` +
			`","attrs":{"src":"https://example.com"}}]}}`,
		`{"schema_version":1,"content":{"type":"doc","content":[{"type":"image"` +
			`,"attrs":{"src":"javascript:alert(1)"}}]}}`,
		`{"schema_version":1,"content":{"type":"doc","content":[{"type":"image","attrs":{"src":"blob:temporary"}}]}}`,
		`{"schema_version":1,"content":{"type":"doc","content":[{"type":"paragr` +
			`aph","content":[{"type":"text","text":"link","marks":[{"type":"link","` +
			`attrs":{"href":"data:text/html,test"}}]}]}]}}`,
		`{"schema_version":1,"content":{"type":"doc","content":[{"type":"paragr` +
			`aph","content":[{"type":"text","text":"style","marks":[{"type":"highli` +
			`ght","attrs":{"color":"red;background:url(javascript:evil)"}}]}]}]}}`,
		`{"schema_version":1,"content":{"type":"doc","content":[{"type":"paragraph","attrs":{"onclick":"evil"}}]}}`,
		`{"schema_version":1,"content":{"type":"doc","content":[{"type":"table"` +
			`,"content":[{"type":"tableRow","content":[{"type":"tableCell","attrs":` +
			`{"rowspan":2},"content":[{"type":"paragraph"}]}]}]}]}}`,
	}
	for _, raw := range cases {
		var d StructuredDocument
		require.NoError(t, json.Unmarshal([]byte(raw), &d))
		require.Error(t, d.Validate(), raw)
	}
	d := StructuredDocument{
		SchemaVersion: 1,
		Content: EditorNode{
			Type: "doc",
			Content: []EditorNode{
				{
					Type:    "paragraph",
					Content: []EditorNode{{Type: "text", Text: strings.Repeat("a", StructuredDocumentMaxBytes)}},
				},
			},
		},
	}
	require.Error(t, d.Validate())
}

func TestStructuredDocumentPreservedAcrossContentAndIndexMetadataChanges(t *testing.T) {
	doc := &StructuredDocument{
		SchemaVersion: 1,
		Content: EditorNode{
			Type:    "doc",
			Content: []EditorNode{{Type: "paragraph", Content: []EditorNode{{Type: "text", Text: "body"}}}},
		},
	}
	k := &Knowledge{Type: "file", Metadata: JSON(`{"datasource_id":"source"}`)}
	require.NoError(t, k.SetContentMetadata("body", 1, true))
	require.NoError(t, k.SetStructuredDocument(doc))
	require.NoError(t, k.SetContentMetadata("body", 1, false))
	require.NoError(t, k.ClearIndexStale())
	meta, err := k.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, doc, meta.Document)
	fields, err := k.Metadata.Map()
	require.NoError(t, err)
	require.Equal(t, "source", fields["datasource_id"])
	require.NoError(t, k.SetStructuredDocument(nil))
	meta, err = k.ManualMetadata()
	require.NoError(t, err)
	require.Nil(t, meta.Document)
}

func TestStructuredDocumentNestedListProjection(t *testing.T) {
	n := EditorNode{Type: "paragraph", Content: []EditorNode{{Type: "text", Text: "leaf"}}}
	for i := 0; i < 24; i++ {
		n = EditorNode{Type: "bulletList", Content: []EditorNode{{Type: "listItem", Content: []EditorNode{
			{Type: "paragraph", Content: []EditorNode{{Type: "text", Text: "level"}}}, n,
		}}}}
	}
	d := StructuredDocument{SchemaVersion: 1, Content: EditorNode{Type: "doc", Content: []EditorNode{n}}}
	require.NoError(t, d.Validate())
	md := d.Markdown()
	require.Equal(t, 24, strings.Count(md, "level"))
	require.Equal(t, 1, strings.Count(md, "leaf"))
}

func TestStructuredDocumentListSpacingValidation(t *testing.T) {
	paragraph := EditorNode{Type: "paragraph", Content: []EditorNode{{Type: "text", Text: "Pasted list"}}}
	list := EditorNode{
		Type:    "bulletList",
		Attrs:   map[string]any{"tight": true},
		Content: []EditorNode{{Type: "listItem", Content: []EditorNode{paragraph}}},
	}
	d := StructuredDocument{SchemaVersion: 1, Content: EditorNode{Type: "doc", Content: []EditorNode{list}}}
	require.NoError(t, d.Validate())
	d.Content.Content[0].Attrs["tight"] = "invalid"
	require.ErrorContains(t, d.Validate(), "invalid list spacing")
}
