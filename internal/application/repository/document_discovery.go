package repository

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
)

// Read the current saved body, not the AI retrieval index. Untouched imported
// files fall back to their readable text chunks. One row at a time bounds memory
// to a single body/chunk; search stops once the requested page plus one is found.
func (r *knowledgeRepository) SearchDocuments(
	ctx context.Context,
	tenant uint64,
	kb, query string,
	offset int,
	backlink string,
) (*types.DocumentSearchPage, error) {
	bodyExpr := "COALESCE(k.metadata->>'content', '')"
	if r.db.Dialector.Name() != "postgres" {
		bodyExpr = "COALESCE(k.metadata->>'$.content', '')"
	}
	q := r.db.WithContext(ctx).Table("knowledges k").
		Select("k.id, k.knowledge_base_id, k.title, k.file_name, k.folder_path, "+
			"k.updated_at, k.metadata, COALESCE(c.content, '') AS chunk_content").
		Joins("LEFT JOIN chunks c ON c.knowledge_id = k.id AND c.tenant_id = "+
			"k.tenant_id AND c.knowledge_base_id = k.knowledge_base_id AND "+
			"c.deleted_at IS NULL AND c.chunk_type IN ? AND "+bodyExpr+" = ''",
			[]string{types.ChunkTypeText, types.ChunkTypeParentText}).
		Where("k.tenant_id = ? AND k.knowledge_base_id = ? AND k.deleted_at IS NULL "+
			"AND k.type <> ? AND k.parse_status <> ?", tenant, kb, types.KnowledgeTypeFAQ, types.ParseStatusDeleting).
		Where("("+bodyExpr+" <> '' OR k.parse_status NOT IN ?)",
			[]string{"pending", "processing", "finalizing", "failed"}).
		Order("k.updated_at DESC, k.id ASC, c.chunk_index ASC, c.id ASC")
	if backlink != "" {
		q = q.Where("k.id <> ? AND "+bodyExpr+" <> ''", backlink)
	}
	rows, err := q.Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &types.DocumentSearchPage{Items: []*types.DocumentSearchItem{}}
	seen := ""
	accepted := ""
	count := 0
	var plain, raw string
	var structure *types.StructuredDocument
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var row struct {
			types.DocumentSearchItem
			FileName     string
			Metadata     types.JSON
			ChunkContent string
		}
		if err := r.db.ScanRows(rows, &row); err != nil {
			return nil, err
		}
		if row.ID == accepted {
			continue
		}
		if row.ID != seen {
			seen = row.ID
			var meta struct {
				Content  string                    `json:"content"`
				Document *types.StructuredDocument `json:"document"`
			}
			if len(row.Metadata) > 0 {
				if err := json.Unmarshal(row.Metadata, &meta); err != nil {
					return nil, err
				}
			}
			raw = meta.Content
			plain = raw
			structure = meta.Document
			if structure != nil {
				plain = structure.SearchText()
			}
		}
		if row.Title == "" {
			row.Title = row.FileName
		}
		if backlink != "" {
			if structure == nil || !structure.ReferencesDocument(backlink) {
				continue
			}
			row.MatchField = "reference"
		} else {
			if raw == "" {
				plain = row.ChunkContent
			}
			switch {
			case query == "" || types.DocumentMatchIndex(row.Title, query) >= 0:
				row.MatchField = "title"
			case types.DocumentMatchIndex(plain, query) >= 0:
				row.MatchField = "body"
			// Legacy Markdown has no canonical tree. Search its saved representation;
			// rich documents use visible text so URLs/formatting aren't false matches.
			default:
				continue
			}
		}
		accepted = row.ID
		if count < offset {
			count++
			continue
		}
		item := row.DocumentSearchItem
		item.Snippet = types.DocumentSnippet(plain, query)
		page.Items = append(page.Items, &item)
		count++
		if len(page.Items) > 20 {
			page.HasMore = true
			page.Items = page.Items[:20]
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	page.NextOffset = offset + len(page.Items)
	return page, nil
}
