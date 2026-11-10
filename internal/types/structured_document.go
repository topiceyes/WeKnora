package types

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// StructuredDocument is the versioned, authoritative editor body. Content in
// metadata remains a server-generated Markdown projection for existing clients,
// indexing, resource binding and exports.
type StructuredDocument struct {
	SchemaVersion int        `json:"schema_version"`
	Content       EditorNode `json:"content"`
}

// EditorNode represents one supported block or text node in the editor tree.
type EditorNode struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []EditorNode   `json:"content,omitempty"`
	Marks   []EditorMark   `json:"marks,omitempty"`
	Text    string         `json:"text,omitempty"`
}

// EditorMark carries inline formatting attributes for an editor node.
type EditorMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// StructuredDocumentMaxBytes bounds the serialized editor body size.
const StructuredDocumentMaxBytes = 2 << 20

var editorColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate rejects unsupported structure rather than silently dropping it. The
// allowlist is shared conceptually with the editor schema; no arbitrary HTML,
// style strings or executable URL schemes are accepted.
func (d *StructuredDocument) Validate() error {
	if d == nil {
		return nil
	}
	if d.SchemaVersion != 1 || d.Content.Type != "doc" {
		return fmt.Errorf("unsupported document schema")
	}
	raw, err := json.Marshal(d)
	if err != nil || len(raw) > StructuredDocumentMaxBytes {
		return fmt.Errorf("document exceeds size limit")
	}
	count := 0
	return validateEditorNode(d.Content, "", 0, &count)
}

func nodeIn(value, options string) bool { return strings.Contains(" "+options+" ", " "+value+" ") }

const editorBlocks = "paragraph heading blockquote bulletList orderedList taskList " +
	"codeBlock horizontalRule image blockMath callout table"
const editorInline = "text hardBreak inlineMath"

func validateEditorNode(n EditorNode, parent string, depth int, count *int) error {
	*count++
	if depth > 64 || *count > 20000 {
		return fmt.Errorf("document too complex")
	}
	allowed := false
	switch parent {
	case "":
		allowed = n.Type == "doc"
	case "doc", "blockquote", "callout", "listItem", "taskItem", "tableCell", "tableHeader":
		allowed = nodeIn(n.Type, editorBlocks)
	case "paragraph", "heading":
		allowed = nodeIn(n.Type, editorInline)
	case "codeBlock":
		allowed = n.Type == "text" && len(n.Marks) == 0
	case "bulletList", "orderedList":
		allowed = n.Type == "listItem"
	case "taskList":
		allowed = n.Type == "taskItem"
	case "table":
		allowed = n.Type == "tableRow"
	case "tableRow":
		allowed = nodeIn(n.Type, "tableCell tableHeader")
	}
	if !allowed {
		return fmt.Errorf("unsupported node %q in %q", n.Type, parent)
	}
	if n.Type == "text" {
		if n.Text == "" || len(n.Content) != 0 || len(n.Attrs) != 0 {
			return fmt.Errorf("invalid text node")
		}
	} else if n.Text != "" {
		return fmt.Errorf("text on non-text node")
	}
	if nodeIn(n.Type, "image hardBreak horizontalRule inlineMath blockMath") && len(n.Content) != 0 {
		return fmt.Errorf("invalid leaf node")
	}
	if nodeIn(
		n.Type,
		"doc blockquote callout bulletList orderedList taskList listItem taskItem table tableRow tableCell tableHeader",
	) &&
		len(n.Content) == 0 {
		return fmt.Errorf("empty container %q", n.Type)
	}
	if nodeIn(n.Type, "listItem taskItem tableCell tableHeader") && n.Content[0].Type != "paragraph" {
		return fmt.Errorf("container must start with paragraph")
	}
	attrs := ""
	if nodeIn(n.Type, "bulletList orderedList") && n.Attrs["tight"] != nil {
		if _, ok := n.Attrs["tight"].(bool); !ok {
			return fmt.Errorf("invalid list spacing")
		}
	}
	switch n.Type {
	case "heading":
		attrs = "level"
		if !editorInt(n.Attrs["level"], 1, 4) {
			return fmt.Errorf("invalid heading")
		}
	case "bulletList":
		attrs = "tight"
	case "orderedList":
		attrs = "start type tight"
		if v := n.Attrs["type"]; v != nil && !nodeIn(editorStringValue(v), "1 a A i I") {
			return fmt.Errorf("invalid list type")
		}
		if n.Attrs["start"] != nil && !editorInt(n.Attrs["start"], 1, 1000000) {
			return fmt.Errorf("invalid list start")
		}
	case "taskItem":
		attrs = "checked"
		if _, ok := n.Attrs["checked"].(bool); !ok {
			return fmt.Errorf("invalid task state")
		}
	case "codeBlock":
		attrs = "language"
		if v := n.Attrs["language"]; v != nil && !editorString(v, 64) {
			return fmt.Errorf("invalid language")
		}
	case "image":
		attrs = "src alt title width height align"
		if !safeEditorURL(editorStringValue(n.Attrs["src"]), true) {
			return fmt.Errorf("invalid image URL")
		}
		for _, key := range []string{"alt", "title"} {
			if n.Attrs[key] != nil && !editorString(n.Attrs[key], 2000) {
				return fmt.Errorf("invalid image description")
			}
		}
		for _, key := range []string{"width", "height"} {
			if n.Attrs[key] != nil && !editorInt(n.Attrs[key], 1, 4096) {
				return fmt.Errorf("invalid image size")
			}
		}
		if n.Attrs["align"] != nil && !nodeIn(editorStringValue(n.Attrs["align"]), "left center right") {
			return fmt.Errorf("invalid image alignment")
		}
	case "inlineMath", "blockMath":
		attrs = "latex"
		if !editorString(n.Attrs["latex"], 10000) {
			return fmt.Errorf("invalid formula")
		}
	case "callout":
		attrs = "kind"
		if !nodeIn(editorStringValue(n.Attrs["kind"]), "info tip warning danger") {
			return fmt.Errorf("invalid callout kind")
		}
	case "tableCell", "tableHeader":
		attrs = "colspan rowspan colwidth align"
		if v := n.Attrs["align"]; v != nil && !nodeIn(editorStringValue(v), "left center right justify") {
			return fmt.Errorf("invalid cell alignment")
		}
		for _, key := range []string{"colspan", "rowspan"} {
			if n.Attrs[key] != nil && !editorInt(n.Attrs[key], 1, 100) {
				return fmt.Errorf("invalid table span")
			}
		}
		if width := n.Attrs["colwidth"]; width != nil {
			values, ok := width.([]any)
			if !ok || len(values) != editorNumber(n.Attrs["colspan"], 1) {
				return fmt.Errorf("invalid column widths")
			}
			for _, value := range values {
				if !editorInt(value, 0, 4096) {
					return fmt.Errorf("invalid column width")
				}
			}
		}
	}
	for key := range n.Attrs {
		if !nodeIn(key, attrs) {
			return fmt.Errorf("unsupported attribute %q", key)
		}
	}
	if len(n.Marks) > 10 || (len(n.Marks) > 0 && !nodeIn(n.Type, editorInline)) {
		return fmt.Errorf("invalid marks")
	}
	for _, mark := range n.Marks {
		if err := validateEditorMark(mark); err != nil {
			return err
		}
	}
	for _, child := range n.Content {
		if err := validateEditorNode(child, n.Type, depth+1, count); err != nil {
			return err
		}
	}
	if n.Type == "table" {
		_, err := editorTableGrid(n)
		return err
	}
	return nil
}

func validateEditorMark(m EditorMark) error {
	attrs := ""
	switch m.Type {
	case "bold", "italic", "strike", "code", "underline":
	case "highlight", "textColor":
		attrs = "color"
		if !editorColor.MatchString(editorStringValue(m.Attrs["color"])) {
			return fmt.Errorf("invalid text color")
		}
	case "link":
		attrs = "href target rel class title"
		if !safeEditorURL(editorStringValue(m.Attrs["href"]), false) {
			return fmt.Errorf("invalid link URL")
		}
		for _, key := range []string{"target", "rel", "class", "title"} {
			if m.Attrs[key] != nil && !editorString(m.Attrs[key], 200) {
				return fmt.Errorf("invalid link attributes")
			}
		}
	default:
		return fmt.Errorf("unsupported mark %q", m.Type)
	}
	for key := range m.Attrs {
		if !nodeIn(key, attrs) {
			return fmt.Errorf("unsupported mark attribute")
		}
	}
	return nil
}

func editorStringValue(v any) string { s, _ := v.(string); return s }
func editorString(v any, maxBytes int) bool {
	s, ok := v.(string)
	return ok && len(s) <= maxBytes && !strings.ContainsRune(s, 0)
}
func editorNumber(v any, fallback int) int {
	switch value := v.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		n, _ := strconv.Atoi(string(value))
		return n
	}
	return fallback
}
func editorInt(v any, lower, upper int) bool {
	switch value := v.(type) {
	case float64:
		return value >= float64(lower) && value <= float64(upper) && value == float64(int(value))
	case int:
		return value >= lower && value <= upper
	case json.Number:
		n, err := strconv.Atoi(string(value))
		return err == nil && n >= lower && n <= upper
	}
	return false
}

func safeEditorURL(value string, image bool) bool {
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n\t\\") {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return !strings.HasPrefix(value, "//")
	}
	if !image && strings.HasPrefix(value, "#") {
		return true
	}
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "resource", "storage", "local", "minio", "cos", "tos", "s3", "oss", "ks3", "obs":
		return u.Host != ""
	case "mailto", "tel":
		return !image && u.Opaque != ""
	}
	return false
}

// A bounded span grid validates merged cells and provides a lossless textual
// projection of all cell contents. Continuations of merged cells stay empty.
func editorTableGrid(n EditorNode) ([][]string, error) {
	if len(n.Content) > 500 {
		return nil, fmt.Errorf("too many table rows")
	}
	grid := make([][]string, len(n.Content))
	occupied := make([][]bool, len(n.Content))
	for i := range grid {
		grid[i] = make([]string, 100)
		occupied[i] = make([]bool, 100)
	}
	width := 0
	for row, r := range n.Content {
		col := 0
		for _, cell := range r.Content {
			for col < 100 && occupied[row][col] {
				col++
			}
			cs, rs := editorNumber(cell.Attrs["colspan"], 1), editorNumber(cell.Attrs["rowspan"], 1)
			if cs < 1 || rs < 1 || col+cs > 100 || row+rs > len(grid) {
				return nil, fmt.Errorf("invalid table geometry")
			}
			for y := row; y < row+rs; y++ {
				for x := col; x < col+cs; x++ {
					if occupied[y][x] {
						return nil, fmt.Errorf("overlapping table cells")
					}
					occupied[y][x] = true
				}
			}
			grid[row][col] = strings.Join(strings.Fields(strings.TrimSpace(editorMarkdown(cell))), " ")
			col += cs
			if col > width {
				width = col
			}
		}
	}
	for row := range grid {
		for col := 0; col < width; col++ {
			if !occupied[row][col] {
				return nil, fmt.Errorf("ragged table")
			}
		}
		grid[row] = grid[row][:width]
	}
	return grid, nil
}

func editorEscape(s string) string {
	return strings.NewReplacer(
		"\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]",
		"<", "&lt;", ">", "&gt;", "|", "\\|", "#", "\\#",
	).Replace(s)
}
func editorTicks(s string, minimum int) string {
	longest, current := 0, 0
	for _, r := range s {
		if r == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	if longest+1 > minimum {
		minimum = longest + 1
	}
	return strings.Repeat("`", minimum)
}
func editorMarkdown(n EditorNode) string {
	var b strings.Builder
	if !nodeIn(n.Type, "table codeBlock bulletList orderedList taskList") {
		for _, child := range n.Content {
			b.WriteString(editorMarkdown(child))
		}
	}
	body := b.String()
	switch n.Type {
	case "text":
		body = editorEscape(n.Text)
		for _, m := range n.Marks {
			switch m.Type {
			case "bold":
				body = "**" + body + "**"
			case "italic":
				body = "*" + body + "*"
			case "strike":
				body = "~~" + body + "~~"
			case "code":
				ticks := editorTicks(n.Text, 1)
				body = ticks + " " + n.Text + " " + ticks
			case "link":
				body = "[" + body + "](" + strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").
					Replace(editorStringValue(m.Attrs["href"])) +
					")"
			}
		}
		return body
	case "paragraph", "tableCell", "tableHeader":
		return body + "\n\n"
	case "heading":
		return strings.Repeat("#", editorNumber(n.Attrs["level"], 1)) + " " + body + "\n\n"
	case "hardBreak":
		return "  \n"
	case "horizontalRule":
		return "---\n\n"
	case "image":
		return "![" + editorEscape(
			editorStringValue(n.Attrs["alt"]),
		) + "](" + strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").
			Replace(editorStringValue(n.Attrs["src"])) +
			")\n\n"
	case "inlineMath":
		return "$" + editorStringValue(n.Attrs["latex"]) + "$"
	case "blockMath":
		return "$$\n" + editorStringValue(n.Attrs["latex"]) + "\n$$\n\n"
	case "codeBlock":
		var text strings.Builder
		for _, c := range n.Content {
			text.WriteString(c.Text)
		}
		ticks := editorTicks(text.String(), 3)
		return ticks + editorStringValue(n.Attrs["language"]) + "\n" + text.String() + "\n" + ticks + "\n\n"
	case "blockquote", "callout":
		body = strings.TrimSpace(body)
		if n.Type == "callout" {
			body = "[!" + strings.ToUpper(editorStringValue(n.Attrs["kind"])) + "]\n" + body
		}
		return "> " + strings.ReplaceAll(body, "\n", "\n> ") + "\n\n"
	case "bulletList", "orderedList", "taskList":
		var out strings.Builder
		for index, c := range n.Content {
			prefix := "- "
			if n.Type == "orderedList" {
				prefix = strconv.Itoa(editorNumber(n.Attrs["start"], 1)+index) + ". "
			}
			if n.Type == "taskList" {
				prefix = "- [ ] "
				if checked, _ := c.Attrs["checked"].(bool); checked {
					prefix = "- [x] "
				}
			}
			text := strings.TrimSpace(editorMarkdown(c))
			out.WriteString(prefix + strings.ReplaceAll(text, "\n", "\n"+strings.Repeat(" ", len(prefix))) + "\n")
		}
		return out.String() + "\n"
	case "table":
		grid, err := editorTableGrid(n)
		if err != nil || len(grid) == 0 {
			return ""
		}
		var out strings.Builder
		for index, row := range grid {
			out.WriteString("| " + strings.Join(row, " | ") + " |\n")
			if index == 0 {
				out.WriteString("| " + strings.TrimSuffix(strings.Repeat("--- | ", len(row)), " | ") + " |\n")
			}
		}
		return out.String() + "\n"
	}
	return body
}

func (d *StructuredDocument) Markdown() string {
	if d == nil {
		return ""
	}
	return strings.TrimSpace(editorMarkdown(d.Content))
}
