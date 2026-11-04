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
	// ErrKBOwnerTransferForbidden: caller is neither the KB creator (at
	// Contributor floor) nor a workspace Admin/Owner, or the KB belongs to a
	// different workspace than the caller.
	ErrKBOwnerTransferForbidden = errors.New("kb owner transfer: only the knowledge base creator or an admin may transfer it")
	// ErrKBOwnerTransferTargetSelf: target already owns the KB.
	ErrKBOwnerTransferTargetSelf = errors.New("kb owner transfer: target already owns this knowledge base")
	// ErrKBOwnerTransferSynthetic: the API-key synthetic user must never hold
	// ownership — otherwise no human can manage the row afterwards.
	ErrKBOwnerTransferSynthetic = errors.New("kb owner transfer: the system user cannot own a knowledge base")
	// ErrKBOwnerTransferNotMember: target has no membership in the KB's
	// workspace (this also catches cross-workspace targets).
	ErrKBOwnerTransferNotMember = errors.New("kb owner transfer: target is not a member of this workspace")
	// ErrKBOwnerTransferInactive: invited/suspended memberships cannot take
	// ownership.
	ErrKBOwnerTransferInactive = errors.New("kb owner transfer: target membership is not active")
	// ErrKBOwnerTransferConflict: the CAS row predicate did not match — the
	// creator changed between load and update (concurrent transfer).
	ErrKBOwnerTransferConflict = errors.New("kb owner transfer: ownership changed concurrently")
)

// TransferKnowledgeBaseOwnership reassigns the KB creator (归宿) to another
// active member of the same workspace. Only creator_id/updated_at change;
// tenant, storage, vector store, shares, pins and content stay untouched.
//
// Ordering: the target membership is validated first, a Viewer target is
// promoted to Contributor (audited as rbac.member_role_changed), and only
// then does the compare-and-swap commit the creator change. A failed CAS
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

	// KBAccess* middleware rewrites the execution tenant to the KB owner's
	// tenant for shared KBs; authorization always uses the caller identity.
	caller := types.CallerFromContext(ctx)
	if caller.TenantID != 0 && caller.TenantID != kb.TenantID {
		return nil, ErrKBOwnerTransferForbidden
	}

	role := types.TenantRoleFromContext(ctx)
	callerID, _ := types.UserIDFromContext(ctx)
	isCreator := callerID != "" && kb.CreatorID == callerID &&
		role.HasPermission(types.TenantRoleContributor)
	if !isCreator && !role.HasPermission(types.TenantRoleAdmin) {
		// Tenant-owned rows (legacy/API-key-created) only reach Admin+ here.
		return nil, ErrKBOwnerTransferForbidden
	}

	if newOwnerUserID == kb.CreatorID {
		return nil, ErrKBOwnerTransferTargetSelf
	}

	member, err := s.tenantMemberService.GetMembership(ctx, newOwnerUserID, kb.TenantID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, ErrKBOwnerTransferNotMember
	}
	if member.Status != types.TenantMemberStatusActive {
		return nil, ErrKBOwnerTransferInactive
	}

	// Below the contributor floor the new owner could not edit the KB they
	// just received, so promote first. UpdateRole carries its own audit and
	// the last-Owner invariant; viewer→contributor is a safe transition.
	promoted := false
	if !member.Role.HasPermission(types.TenantRoleContributor) {
		if err := s.tenantMemberService.UpdateRole(
			ctx, newOwnerUserID, kb.TenantID, types.TenantRoleContributor,
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
