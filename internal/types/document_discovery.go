package types

import (
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const DocumentLinkPrefix = "/platform/documents/open/"

// Internal links intentionally have no hostname or KB ID. A path on another
// origin must not appear as an internal backlink.
func LinkedDocumentID(href string) string {
	u, err := url.Parse(href)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" ||
		!strings.HasPrefix(u.Path, DocumentLinkPrefix) {
		return ""
	}
	id := strings.TrimPrefix(u.Path, DocumentLinkPrefix)
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id || u.Path != href {
		return ""
	}
	return id
}

type DocumentSearchItem struct {
	ID              string    `json:"id"`
	KnowledgeBaseID string    `json:"knowledge_base_id"`
	Title           string    `json:"title"`
	FolderPath      string    `json:"folder_path"`
	UpdatedAt       time.Time `json:"updated_at"`
	Snippet         string    `json:"snippet"`
	MatchField      string    `json:"match_field"`
}
type DocumentSearchPage struct {
	Items      []*DocumentSearchItem `json:"items"`
	NextOffset int                   `json:"next_offset"`
	HasMore    bool                  `json:"has_more"`
}

// Preserve text split across marks, with block boundaries to prevent a match
// spanning unrelated paragraphs. Formatting and link destinations aren't prose.
func (d *StructuredDocument) SearchText() string {
	if d == nil {
		return ""
	}
	var out strings.Builder
	var visit func(EditorNode)
	visit = func(n EditorNode) {
		switch n.Type {
		case "text":
			out.WriteString(n.Text)
		case "image":
			out.WriteString(editorStringValue(n.Attrs["alt"]))
		case "inlineMath", "blockMath":
			out.WriteString(editorStringValue(n.Attrs["latex"]))
		case "hardBreak":
			out.WriteByte('\n')
		}
		for _, child := range n.Content {
			visit(child)
		}
		if nodeIn(n.Type, "paragraph heading codeBlock blockMath tableCell tableHeader") {
			out.WriteByte('\n')
		}
	}
	visit(d.Content)
	return out.String()
}
func (d *StructuredDocument) ReferencesDocument(id string) bool {
	if d == nil {
		return false
	}
	var visit func(EditorNode) bool
	visit = func(n EditorNode) bool {
		if n.Type == "codeBlock" {
			return false
		}
		for _, mark := range n.Marks {
			if mark.Type == "link" && LinkedDocumentID(editorStringValue(mark.Attrs["href"])) == id {
				return true
			}
		}
		for _, child := range n.Content {
			if visit(child) {
				return true
			}
		}
		return false
	}
	return visit(d.Content)
}

// Rune-based normalization preserves Chinese and literal SQL wildcard characters.
func DocumentMatchIndex(text, query string) int {
	folded := strings.Map(unicode.ToLower, text)
	needle := strings.Map(unicode.ToLower, query)
	index := strings.Index(folded, needle)
	if index < 0 {
		return -1
	}
	runeIndex := len([]rune(folded[:index]))
	for byteIndex := range text {
		if runeIndex == 0 {
			return byteIndex
		}
		runeIndex--
	}
	return len(text)
}
func DocumentSnippet(text, query string) string {
	// Compact whitespace only after locating in the original text. A code snippet
	// remains plain text and is never rendered as HTML by the client.
	text = strings.TrimSpace(text)
	index := DocumentMatchIndex(text, query)
	runes := []rune(text)
	start := 0
	if index >= 0 {
		start = len([]rune(text[:index])) - 45
		if start < 0 {
			start = 0
		}
	}
	end := start + 180
	if end > len(runes) {
		end = len(runes)
	}
	snippet := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, string(runes[start:end]))
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet
}
