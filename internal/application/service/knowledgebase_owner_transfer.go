package service

import (
	"context"
	"errors"
	"strings"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Ownership-transfer sentinels. The handler maps each to its HTTP status so
// the UI can show a precise message instead of a generic 500.
var (
	// ErrKBOwnerTransferForbidden: caller is neither the KB creator (at the
	// Contributor floor) nor an Admin/Owner of the KB's workspace.
	ErrKBOwnerTransferForbidden = errors.New("kb owner transfer: only the knowledge base creator or a workspace admin may transfer it")
	// ErrKBOwnerTransferTargetSelf: target already owns the KB.
	ErrKBOwnerTransferTargetSelf = errors.New("kb owner transfer: target already owns this knowledge base")
	// ErrKBOwnerTransferSynthetic: the API-key synthetic user must never hold
	// ownership — otherwise no human can manage the row afterwards.
	ErrKBOwnerTransferSynthetic = errors.New("kb owner transfer: the system user cannot own a knowledge base")
	// ErrKBOwnerTransferNotMember: target is neither a member of the KB's
	// workspace nor a member of any organization the KB is shared into.
	ErrKBOwnerTransferNotMember = errors.New("kb owner transfer: target is not a member of this workspace or its shared spaces")
	// ErrKBOwnerTransferInactive: invited/suspended memberships cannot take
	// ownership.
	ErrKBOwnerTransferInactive = errors.New("kb owner transfer: target membership is not active")
	// ErrKBOwnerTransferTargetRole: a target in another workspace must already
	// be able to manage KBs there (Contributor+). Promoting someone's role in
	// their own workspace is outside the scope of a KB transfer.
	ErrKBOwnerTransferTargetRole = errors.New("kb owner transfer: target needs contributor permissions in their own workspace")
	// ErrKBOwnerTransferConflict: the CAS row predicate did not match — the
	// creator changed between load and update (concurrent transfer).
	ErrKBOwnerTransferConflict = errors.New("kb owner transfer: ownership changed concurrently")
)

// TransferKnowledgeBaseOwnership reassigns the KB creator (归宿) to another
// active person who can already see the KB — a member of its workspace or of a
// shared space (organization) the KB is shared into. Only creator_id and
// updated_at change; tenant, storage, vector store, shares, pins and content
// stay untouched.
//
// Ordering: the target membership is validated first, a same-workspace Viewer
// target is promoted to Contributor (audited as rbac.member_role_changed), and
// only then does the compare-and-swap commit the creator change. A failed CAS
// rolls the promotion back best-effort so a rejected transfer never leaves
// behind an unintended role grant; a failed rollback is logged, never fatal.
func (s *knowledgeBaseService) TransferKnowledgeBaseOwnership(
	ctx context.Context, kbID, newOwnerUserID string,
) (*types.KnowledgeBase, error) {
	kbID = strings.TrimSpace(kbID)
	newOwnerUserID = strings.TrimSpace(newOwnerUserID)
	if kbID == "" {
		return nil, errors.New("knowledge base ID cannot be empty")
	}
	if newOwnerUserID == "" {
		return nil, ErrKBOwnerTransferNotMember
	}
	if types.IsSyntheticUserID(newOwnerUserID) {
		return nil, ErrKBOwnerTransferSynthetic
	}
	// JWT-only defense in depth: the route is not declared to the API-key
	// authorizer (default-deny), and the service refuses a machine principal
	// that somehow reached it.
	if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		return nil, ErrKBOwnerTransferForbidden
	}

	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, ErrKBOwnerTransferForbidden
	}
	if s.tenantMemberService == nil {
		return nil, errors.New("kb owner transfer: tenant member service unavailable")
	}
	kb, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, kbID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeKBOwnershipTransfer(ctx, kb); err != nil {
		return nil, err
	}
	if newOwnerUserID == kb.CreatorID {
		return nil, ErrKBOwnerTransferTargetSelf
	}

	member, promote, err := s.resolveTransferTarget(ctx, kb, newOwnerUserID)
	if err != nil {
		return nil, err
	}

	// Below the contributor floor the new owner could not edit a KB in their
	// own workspace, so promote first. UpdateRole carries its own audit and
	// the last-Owner invariant; viewer→contributor is a safe transition.
	// Cross-workspace targets are never promoted here: their role in *their*
	// workspace is not ours to change.
	promoted := false
	if promote {
		if err := s.tenantMemberService.UpdateRole(
			ctx, member.UserID, kb.TenantID, types.TenantRoleContributor,
		); err != nil {
			return nil, err
		}
		promoted = true
	}

	oldCreatorID := kb.CreatorID
	matched, err := s.repo.UpdateKnowledgeBaseCreator(ctx, kb.ID, kb.TenantID, oldCreatorID, newOwnerUserID)
	if err != nil {
		if promoted {
			s.rollbackViewerPromotion(ctx, kb.ID, kb.TenantID, newOwnerUserID)
		}
		return nil, err
	}
	if !matched {
		if promoted {
			s.rollbackViewerPromotion(ctx, kb.ID, kb.TenantID, newOwnerUserID)
		}
		return nil, ErrKBOwnerTransferConflict
	}

	recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID,
		types.AuditActionKBOwnerTransferred, "user", newOwnerUserID,
		types.AuditOutcomeSuccess, map[string]interface{}{
			"old_creator_id": oldCreatorID,
			"new_creator_id": newOwnerUserID,
		})

	kb.CreatorID = newOwnerUserID
	// Best-effort fresh read so the response carries a bumped updated_at;
	// a read failure must not turn a committed transfer into a 500.
	if fresh, ferr := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, kb.ID, kb.TenantID); ferr == nil && fresh != nil {
		return fresh, nil
	}
	return kb, nil
}

// ListKnowledgeBaseTransferCandidates returns the people who may receive the
// KB. The pool is exactly the validation set used by the transfer itself, so
// the UI cannot offer a target the backend would reject:
//
//   - active members of the KB's own workspace, and
//   - active members of every organization (shared space) the KB is shared into.
//
// The same-workspace members come first. `query` is matched against username
// and email in SQL, which keeps email search working even though the response
// does not expose other workspaces' email addresses.
func (s *knowledgeBaseService) ListKnowledgeBaseTransferCandidates(
	ctx context.Context, kbID, query string, limit int,
) ([]*types.KBTransferCandidate, error) {
	kbID = strings.TrimSpace(kbID)
	if kbID == "" {
		return nil, errors.New("knowledge base ID cannot be empty")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, ErrKBOwnerTransferForbidden
	}
	if s.tenantMemberService == nil {
		return nil, errors.New("kb owner transfer: tenant member service unavailable")
	}
	kb, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, kbID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeKBOwnershipTransfer(ctx, kb); err != nil {
		return nil, err
	}

	query = strings.TrimSpace(query)
	sameWorkspace, _, err := s.tenantMemberService.ListMembersPage(ctx, kb.TenantID, query, 1, limit)
	if err != nil {
		return nil, err
	}
	otherTenantIDs, err := s.transferSharedSpaceTenantIDs(ctx, kb)
	if err != nil {
		return nil, err
	}
	var sharedSpace []*types.TenantMember
	if len(otherTenantIDs) > 0 {
		sharedSpace, err = s.tenantMemberService.ListActiveByTenantIDs(ctx, otherTenantIDs, query, limit)
		if err != nil {
			return nil, err
		}
	}

	candidates := make([]*types.KBTransferCandidate, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendMember := func(m *types.TenantMember) {
		if m == nil || m.UserID == "" || m.UserID == kb.CreatorID {
			return
		}
		if m.Status != types.TenantMemberStatusActive {
			return
		}
		if _, dup := seen[m.UserID]; dup {
			return
		}
		seen[m.UserID] = struct{}{}
		candidates = append(candidates, &types.KBTransferCandidate{
			UserID:   m.UserID,
			TenantID: m.TenantID,
			Role:     m.Role,
			Status:   m.Status,
		})
	}
	for _, m := range sameWorkspace {
		if len(candidates) >= limit {
			break
		}
		appendMember(m)
	}
	for _, m := range sharedSpace {
		if len(candidates) >= limit {
			break
		}
		appendMember(m)
	}

	s.fillCandidateTenantNames(ctx, candidates)
	return candidates, nil
}

// authorizeKBOwnershipTransfer applies the same matrix as the route guards:
// the creator at the Contributor floor, or an Admin+ of the KB's workspace.
// The creator may live in another workspace after a cross-workspace transfer;
// workspace admins are limited to the KB's own workspace.
func (s *knowledgeBaseService) authorizeKBOwnershipTransfer(
	ctx context.Context, kb *types.KnowledgeBase,
) error {
	role := types.TenantRoleFromContext(ctx)
	caller := types.CallerFromContext(ctx)
	if caller.UserID != "" && kb.CreatorID == caller.UserID &&
		role.HasPermission(types.TenantRoleContributor) {
		return nil
	}
	if caller.TenantID != 0 && caller.TenantID == kb.TenantID && role.HasPermission(types.TenantRoleAdmin) {
		return nil
	}
	return ErrKBOwnerTransferForbidden
}

// resolveTransferTarget accepts two populations:
//
//  1. an active member of the KB's workspace (same tenant), returning
//     promote=true when they are below Contributor; or
//  2. an active member of any workspace inside an organization the KB is
//     shared into. Those users must already be Contributor+ in their own
//     workspace, because the transfer does not modify it.
func (s *knowledgeBaseService) resolveTransferTarget(
	ctx context.Context, kb *types.KnowledgeBase, userID string,
) (*types.TenantMember, bool, error) {
	member, err := s.tenantMemberService.GetMembership(ctx, userID, kb.TenantID)
	if err != nil {
		return nil, false, err
	}
	if member != nil {
		if member.Status != types.TenantMemberStatusActive {
			return nil, false, ErrKBOwnerTransferInactive
		}
		return member, !member.Role.HasPermission(types.TenantRoleContributor), nil
	}

	allowedTenantIDs, err := s.transferCandidateTenantIDs(ctx, kb)
	if err != nil {
		return nil, false, err
	}
	memberships, err := s.tenantMemberService.ListByUser(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	for _, m := range memberships {
		if m == nil || m.Status != types.TenantMemberStatusActive {
			continue
		}
		if _, ok := allowedTenantIDs[m.TenantID]; !ok {
			continue
		}
		if !m.Role.HasPermission(types.TenantRoleContributor) {
			return nil, false, ErrKBOwnerTransferTargetRole
		}
		return m, false, nil
	}
	return nil, false, ErrKBOwnerTransferNotMember
}

// transferCandidateTenantIDs returns the KB's workspace plus every workspace
// that belongs to an organization the KB is shared into.
func (s *knowledgeBaseService) transferCandidateTenantIDs(
	ctx context.Context, kb *types.KnowledgeBase,
) (map[uint64]struct{}, error) {
	allowed := map[uint64]struct{}{kb.TenantID: {}}
	if s.kbShareService == nil {
		return allowed, nil
	}
	shared, err := s.kbShareService.ListShareMemberTenantIDs(ctx, kb.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range shared {
		if id != 0 {
			allowed[id] = struct{}{}
		}
	}
	return allowed, nil
}

// transferSharedSpaceTenantIDs returns the shared-space member tenants,
// excluding the KB's own workspace (already covered by the same-workspace
// member query).
func (s *knowledgeBaseService) transferSharedSpaceTenantIDs(
	ctx context.Context, kb *types.KnowledgeBase,
) ([]uint64, error) {
	allowed, err := s.transferCandidateTenantIDs(ctx, kb)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(allowed))
	for id := range allowed {
		if id != kb.TenantID {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// fillCandidateTenantNames resolves display names for the candidate rows. A
// lookup failure only costs the tenant label, so it is swallowed.
func (s *knowledgeBaseService) fillCandidateTenantNames(
	ctx context.Context, candidates []*types.KBTransferCandidate,
) {
	if len(candidates) == 0 || s.tenantRepo == nil {
		return
	}
	idSet := make(map[uint64]struct{}, len(candidates))
	ids := make([]uint64, 0, len(candidates))
	for _, c := range candidates {
		if c.TenantID == 0 {
			continue
		}
		if _, ok := idSet[c.TenantID]; ok {
			continue
		}
		idSet[c.TenantID] = struct{}{}
		ids = append(ids, c.TenantID)
	}
	tenants, err := s.tenantRepo.GetTenantsByIDs(ctx, ids)
	if err != nil {
		logger.Warnf(ctx, "[kb.owner_transfer] failed to resolve candidate tenant names: %v", err)
		return
	}
	for _, c := range candidates {
		if t, ok := tenants[c.TenantID]; ok && t != nil {
			c.TenantName = t.Name
		}
	}
}

// rollbackViewerPromotion reverts a just-performed viewer→contributor
// promotion when the subsequent ownership CAS failed, so a rejected transfer
// leaves no unintended role grant behind. Best-effort by design: a rollback
// failure is logged and swallowed (the transfer row itself was never written).
//
// Two concurrent transfers to the same Viewer target race: both promote, one
// CAS wins, the loser reaches this rollback. Re-reading the KB first skips the
// demotion when a concurrent request already made this user the owner —
// otherwise the winner's new owner would keep ownership but lose Contributor,
// leaving them unable to manage the KB they just received.
func (s *knowledgeBaseService) rollbackViewerPromotion(
	ctx context.Context, kbID string, tenantID uint64, userID string,
) {
	if fresh, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, kbID, tenantID); err == nil &&
		fresh != nil && fresh.CreatorID == userID {
		return
	}
	if err := s.tenantMemberService.UpdateRole(ctx, userID, tenantID, types.TenantRoleViewer); err != nil {
		if errors.Is(err, apprepo.ErrLastOwner) || errors.Is(err, ErrMembershipNotFound) {
			return
		}
		logger.Warnf(ctx,
			"[kb.owner_transfer] failed to roll back viewer promotion: user=%s tenant=%d err=%v",
			userID, tenantID, err)
	}
}
