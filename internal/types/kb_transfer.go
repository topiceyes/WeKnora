package types

// KBTransferCandidate is one person who may receive a knowledge base during an
// ownership transfer. Candidates come from two pools:
//
//   - active members of the knowledge base's own workspace (TenantID == KB tenant), and
//   - active members of every workspace that belongs to an organization the KB
//     is shared into (the "shared space" the user sees in the UI).
//
// User-facing fields (username/email/avatar) are enriched by the handler; the
// service only resolves membership identity so the search query can be pushed
// into SQL.
type KBTransferCandidate struct {
	UserID     string             `json:"user_id"`
	TenantID   uint64             `json:"tenant_id"`
	TenantName string             `json:"tenant_name,omitempty"`
	Role       TenantRole         `json:"role"`
	Status     TenantMemberStatus `json:"status"`
}
