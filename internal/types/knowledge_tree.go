package types

import "time"

// KnowledgeTreeDocument is a slim knowledge entry as exposed by the browse-mode
// document tree endpoint. It intentionally carries only what the tree needs to
// render a row and open the reader — full payloads stay on the per-document
// endpoints so this list stays cheap even for large knowledge bases.
type KnowledgeTreeDocument struct {
	ID          string `json:"id"            gorm:"column:id"          extensions:"x-agent=knowledge_id"`
	Title       string `json:"title"         gorm:"column:title"       extensions:"x-agent"`
	Type        string `json:"type"          gorm:"column:type"        extensions:"x-agent=knowledge_type"`
	FileType    string `json:"file_type"     gorm:"column:file_type"   extensions:"x-agent"`
	FileName    string `json:"file_name"     gorm:"column:file_name"   extensions:"x-agent"`
	FolderPath  string `json:"folder_path"   gorm:"column:folder_path" extensions:"x-agent=folder_path"`
	ParseStatus string `json:"parse_status"  gorm:"column:parse_status" extensions:"x-agent"`
	// IndexStale mirrors metadata.index_stale (edited content not yet relearnt)
	// without dragging the whole metadata column into the tree query.
	IndexStale bool      `json:"index_stale"   gorm:"column:index_stale" extensions:"x-agent"`
	UpdatedAt  time.Time `json:"updated_at"    gorm:"column:updated_at"  extensions:"x-agent"`
}

// KnowledgeTree is the response payload of the browse-mode tree endpoint: the
// folder hierarchy (with counts) plus every document as a flat list. Clients
// group documents by folder_path to hang them under the folder nodes.
type KnowledgeTree struct {
	*KnowledgeFolderTree
	Documents []*KnowledgeTreeDocument `json:"documents"`
}
