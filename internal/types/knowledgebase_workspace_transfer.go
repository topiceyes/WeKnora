package types

// KBWorkspaceTransferBlocker is a reason a knowledge base may not be moved to
// another workspace. The UI renders these verbatim; every blocker is hard —
// the transfer service refuses to start while any is present.
type KBWorkspaceTransferBlocker struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// KBWorkspaceTransferStats summarises what an ownership transfer will move.
type KBWorkspaceTransferStats struct {
	KnowledgeCount int64 `json:"knowledge_count"`
	ChunkCount     int64 `json:"chunk_count"`
	WikiPageCount  int64 `json:"wiki_page_count"`
	FileCount      int64 `json:"file_count"`
	FileBytes      int64 `json:"file_bytes"`
}

// KBWorkspaceTransferPlan is the read-only preflight shown before a
// cross-workspace transfer. When MovesWorkspace is false the target lives in
// the KB's own workspace and the caller should use the plain transfer-owner
// endpoint instead.
type KBWorkspaceTransferPlan struct {
	MovesWorkspace   bool                         `json:"moves_workspace"`
	SourceTenantID   uint64                       `json:"source_tenant_id"`
	SourceTenantName string                       `json:"source_tenant_name"`
	TargetTenantID   uint64                       `json:"target_tenant_id"`
	TargetTenantName string                       `json:"target_tenant_name"`
	TargetUserID     string                       `json:"target_user_id"`
	TargetUsername   string                       `json:"target_username"`
	Stats            KBWorkspaceTransferStats     `json:"stats"`
	SharesKept       int64                        `json:"shares_kept"`
	Blockers         []KBWorkspaceTransferBlocker `json:"blockers"`
	Warnings         []KBWorkspaceTransferBlocker `json:"warnings,omitempty"`
}

// KBWorkspaceTransferResult is returned after a successful whole-KB move.
type KBWorkspaceTransferResult struct {
	KnowledgeBaseID  string                   `json:"knowledge_base_id"`
	FromTenantID     uint64                   `json:"from_tenant_id"`
	ToTenantID       uint64                   `json:"to_tenant_id"`
	NewOwnerUserID   string                   `json:"new_owner_user_id"`
	Stats            KBWorkspaceTransferStats `json:"stats"`
	SharesReanchored int64                    `json:"shares_reanchored"`
}
