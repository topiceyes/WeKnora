package types

// KnowledgeNavigation persists empty folders and sibling order independently
// of document content. A revision protects edits made from multiple tabs.
type KnowledgeNavigation struct {
	TenantID        uint64              `gorm:"primaryKey" json:"-"`
	KnowledgeBaseID string              `gorm:"primaryKey;type:varchar(36)" json:"-"`
	Revision        int64               `json:"revision"`
	Folders         []string            `gorm:"serializer:json;type:jsonb" json:"folders"`
	Orders          map[string][]string `gorm:"serializer:json;type:jsonb" json:"orders"`
}

func (KnowledgeNavigation) TableName() string { return "knowledge_navigation" }

type KnowledgeNavigationMutation struct {
	Action       string   `json:"action" binding:"required"`
	BaseRevision *int64   `json:"base_revision"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	IDs          []string `json:"knowledge_ids"`
	Items        []string `json:"items"`
	// Legacy path-only rename retains its documented merge behavior.
	AllowMerge bool `json:"-"`
}
