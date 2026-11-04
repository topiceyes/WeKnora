package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transferMemberService records role updates and serves membership lookups.
type transferMemberService struct {
	interfaces.TenantMemberService
	membership   *types.TenantMember
	membershipOK bool
	// extraMemberships models the cross-workspace pool: users whose home
	// tenant is a member of one of the KB's shared spaces.
	extraMemberships []*types.TenantMember
	membersPage      []*types.TenantMember
	roleCalls        []transferRoleCall
	roleErr          error
}

type transferRoleCall struct {
	userID   string
	tenantID uint64
	role     types.TenantRole
}

func (m *transferMemberService) GetMembership(_ context.Context, userID string, tenantID uint64) (*types.TenantMember, error) {
	if !m.membershipOK {
		return nil, nil
	}
	if m.membership.UserID != userID || m.membership.TenantID != tenantID {
		return nil, nil
	}
	return m.membership, nil
}

func (m *transferMemberService) ListByUser(_ context.Context, userID string) ([]*types.TenantMember, error) {
	var out []*types.TenantMember
	if m.membershipOK && m.membership.UserID == userID {
		out = append(out, m.membership)
	}
	for _, extra := range m.extraMemberships {
		if extra.UserID == userID {
			out = append(out, extra)
		}
	}
	return out, nil
}

func (m *transferMemberService) ListMembersPage(
	_ context.Context, tenantID uint64, query string, _, limit int,
) ([]*types.TenantMember, int64, error) {
	var out []*types.TenantMember
	for _, candidate := range m.membersPage {
		if candidate.TenantID != tenantID {
			continue
		}
		if query != "" && candidate.UserID != query {
			continue
		}
		out = append(out, candidate)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, int64(len(out)), nil
}

func (m *transferMemberService) ListActiveByTenantIDs(
	_ context.Context, tenantIDs []uint64, query string, limit int,
) ([]*types.TenantMember, error) {
	allowed := make(map[uint64]struct{}, len(tenantIDs))
	for _, id := range tenantIDs {
		allowed[id] = struct{}{}
	}
	var out []*types.TenantMember
	for _, candidate := range m.extraMemberships {
		if candidate.Status != types.TenantMemberStatusActive {
			continue
		}
		if _, ok := allowed[candidate.TenantID]; !ok {
			continue
		}
		if query != "" && candidate.UserID != query {
			continue
		}
		out = append(out, candidate)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *transferMemberService) UpdateRole(_ context.Context, userID string, tenantID uint64, role types.TenantRole) error {
	if m.roleErr != nil {
		return m.roleErr
	}
	m.roleCalls = append(m.roleCalls, transferRoleCall{userID: userID, tenantID: tenantID, role: role})
	if m.membership != nil && m.membership.UserID == userID {
		m.membership.Role = role
	}
	return nil
}

// transferAuditLog captures every audit entry so tests can assert both the
// ownership-transfer row and the viewer-promotion role-change row.
type transferAuditLog struct {
	entries []*types.AuditLog
}

func (a *transferAuditLog) Log(_ context.Context, entry *types.AuditLog) error {
	a.entries = append(a.entries, entry)
	return nil
}

func (*transferAuditLog) LogDenied(context.Context, *gin.Context, uint64, string, string, types.TenantRole) error {
	return nil
}

func (*transferAuditLog) List(context.Context, uint64, *interfaces.AuditLogQuery) ([]*types.AuditLog, error) {
	return nil, nil
}

func (*transferAuditLog) Purge(context.Context, int) (int64, error) { return 0, nil }

// transferKBRepo wraps fakeKBRepo and lets tests force a CAS mismatch.
type transferKBRepo struct {
	fakeKBRepo
	casMatch bool
	// concurrentWinner, when set, makes every read after the first observe the
	// given creator — simulating another transfer that committed between this
	// request's load and its CAS.
	concurrentWinner string
	reads            int
}

func (r *transferKBRepo) GetKnowledgeBaseByIDAndTenant(
	ctx context.Context, id string, tenantID uint64,
) (*types.KnowledgeBase, error) {
	kb, err := r.fakeKBRepo.GetKnowledgeBaseByIDAndTenant(ctx, id, tenantID)
	if err != nil || kb == nil {
		return kb, err
	}
	r.reads++
	if r.concurrentWinner != "" && r.reads > 1 {
		clone := *kb
		clone.CreatorID = r.concurrentWinner
		return &clone, nil
	}
	return kb, nil
}

func (r *transferKBRepo) UpdateKnowledgeBaseCreator(
	_ context.Context, id string, tenantID uint64, expectedCreatorID, newCreatorID string,
) (bool, error) {
	if !r.casMatch {
		return false, nil
	}
	return r.fakeKBRepo.UpdateKnowledgeBaseCreator(context.Background(), id, tenantID, expectedCreatorID, newCreatorID)
}

// transferShareService exposes the shared-space tenant pool used by the
// candidate and target-validation paths.
type transferShareService struct {
	interfaces.KBShareService
	tenantIDs []uint64
	err       error
}

func (s *transferShareService) ListShareMemberTenantIDs(context.Context, string) ([]uint64, error) {
	return s.tenantIDs, s.err
}

func transferTestCtx(userID string, role types.TenantRole, tenantID uint64) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
	return types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: userID, Role: role})
}

func newTransferService(kb *types.KnowledgeBase, members *transferMemberService) (*knowledgeBaseService, *transferAuditLog) {
	repo := &transferKBRepo{fakeKBRepo: *newFakeKBRepo(), casMatch: true}
	repo.rows[kb.ID] = kb
	audit := &transferAuditLog{}
	return &knowledgeBaseService{
		repo:                repo,
		audit:               audit,
		tenantMemberService: members,
		kbShareService:      &transferShareService{},
	}, audit
}

func activeMember(userID string, role types.TenantRole) *types.TenantMember {
	return &types.TenantMember{
		UserID:   userID,
		TenantID: 1,
		Role:     role,
		Status:   types.TenantMemberStatusActive,
	}
}

func TestTransferKnowledgeBaseOwnership_CreatorToContributor(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, audit := newTransferService(kb, members)

	got, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "target")
	require.NoError(t, err)
	assert.Equal(t, "target", got.CreatorID)
	assert.Empty(t, members.roleCalls, "contributor target must not trigger a promotion")

	require.Len(t, audit.entries, 1)
	entry := audit.entries[0]
	assert.Equal(t, types.AuditActionKBOwnerTransferred, entry.Action)
	assert.Equal(t, "kb-1", entry.ScopeID)
	assert.Equal(t, "user", entry.TargetType)
	assert.Equal(t, "target", entry.TargetID)
	assert.Equal(t, "creator", entry.ActorUserID)
}

func TestTransferKnowledgeBaseOwnership_AdminTransfersSomeoneElsesKB(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleAdmin), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("admin", types.TenantRoleAdmin, 1), "kb-1", "target")
	require.NoError(t, err)
	assert.Equal(t, "target", kb.CreatorID)
}

func TestTransferKnowledgeBaseOwnership_AdminClaimsTenantOwnedKB(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("admin", types.TenantRoleAdmin, 1), "kb-1", "target")
	require.NoError(t, err)
	assert.Equal(t, "target", kb.CreatorID)
}

func TestTransferKnowledgeBaseOwnership_NonCreatorContributorForbidden(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("other", types.TenantRoleContributor, 1), "kb-1", "target")
	assert.ErrorIs(t, err, ErrKBOwnerTransferForbidden)
	assert.Equal(t, "creator", kb.CreatorID)
}

func TestTransferKnowledgeBaseOwnership_DemotedCreatorForbidden(t *testing.T) {
	// CreatorRoleFloor: a creator demoted to Viewer may no longer manage
	// their own KB, including transferring it.
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleViewer, 1), "kb-1", "target")
	assert.ErrorIs(t, err, ErrKBOwnerTransferForbidden)
}

func TestTransferKnowledgeBaseOwnership_TenantOwnedKBRequiresAdmin(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("contrib", types.TenantRoleContributor, 1), "kb-1", "target")
	assert.ErrorIs(t, err, ErrKBOwnerTransferForbidden)
}

func TestTransferKnowledgeBaseOwnership_SelfTransferRejected(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("creator", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "creator")
	assert.ErrorIs(t, err, ErrKBOwnerTransferTargetSelf)
}

func TestTransferKnowledgeBaseOwnership_SyntheticTargetRejected(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	svc, _ := newTransferService(kb, &transferMemberService{})

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "system-1")
	assert.ErrorIs(t, err, ErrKBOwnerTransferSynthetic)
}

func TestTransferKnowledgeBaseOwnership_TargetNotMember(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	svc, _ := newTransferService(kb, &transferMemberService{})

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "outsider")
	assert.ErrorIs(t, err, ErrKBOwnerTransferNotMember)
}

func TestTransferKnowledgeBaseOwnership_InactiveMembershipsRejected(t *testing.T) {
	for _, status := range []types.TenantMemberStatus{types.TenantMemberStatusInvited, types.TenantMemberStatusSuspended} {
		kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
		member := activeMember("target", types.TenantRoleContributor)
		member.Status = status
		members := &transferMemberService{membership: member, membershipOK: true}
		svc, _ := newTransferService(kb, members)

		_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "target")
		assert.ErrorIs(t, err, ErrKBOwnerTransferInactive, "status=%s", status)
	}
}

func TestTransferKnowledgeBaseOwnership_ViewerTargetPromoted(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	member := activeMember("target", types.TenantRoleViewer)
	members := &transferMemberService{membership: member, membershipOK: true}
	svc, audit := newTransferService(kb, members)

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "target")
	require.NoError(t, err)
	assert.Equal(t, "target", kb.CreatorID)

	require.Len(t, members.roleCalls, 1)
	assert.Equal(t, transferRoleCall{userID: "target", tenantID: 1, role: types.TenantRoleContributor}, members.roleCalls[0])
	// The real tenantMemberService.UpdateRole emits rbac.member_role_changed
	// into the same audit table (covered by its own tests); here we assert
	// only the ownership row written by this service.
	require.Len(t, audit.entries, 1)
	assert.Equal(t, types.AuditActionKBOwnerTransferred, audit.entries[0].Action)
	assert.Equal(t, "target", audit.entries[0].TargetID)
}

func TestTransferKnowledgeBaseOwnership_ViewerPromotionRolledBackOnConflict(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	member := activeMember("target", types.TenantRoleViewer)
	members := &transferMemberService{membership: member, membershipOK: true}
	svc, audit := newTransferService(kb, members)
	svc.repo.(*transferKBRepo).casMatch = false

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "target")
	assert.ErrorIs(t, err, ErrKBOwnerTransferConflict)
	assert.Equal(t, "creator", kb.CreatorID, "rejected transfer must not change the creator")

	// promotion + best-effort rollback, and no ownership audit row.
	require.Len(t, members.roleCalls, 2)
	assert.Equal(t, types.TenantRoleContributor, members.roleCalls[0].role)
	assert.Equal(t, types.TenantRoleViewer, members.roleCalls[1].role)
	for _, entry := range audit.entries {
		assert.NotEqual(t, types.AuditActionKBOwnerTransferred, entry.Action)
	}
}

func TestTransferKnowledgeBaseOwnership_ConcurrentWinnerKeepsPromotion(t *testing.T) {
	// Two requests transfer the same KB to the same Viewer target. Both see
	// the old creator, so both promote; one CAS commits, the loser must not
	// demote the winner — otherwise the new owner cannot manage the KB.
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	member := activeMember("target", types.TenantRoleViewer)
	members := &transferMemberService{membership: member, membershipOK: true}
	svc, _ := newTransferService(kb, members)
	repo := svc.repo.(*transferKBRepo)
	repo.casMatch = false
	repo.concurrentWinner = "target"

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "target")
	assert.ErrorIs(t, err, ErrKBOwnerTransferConflict)
	require.Len(t, members.roleCalls, 1, "loser must not demote the concurrent winner")
	assert.Equal(t, types.TenantRoleContributor, members.roleCalls[0].role)
}

func TestTransferKnowledgeBaseOwnership_PromotionFailureAborts(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	member := activeMember("target", types.TenantRoleViewer)
	members := &transferMemberService{membership: member, membershipOK: true, roleErr: assert.AnError}
	svc, _ := newTransferService(kb, members)

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "target")
	require.Error(t, err)
	assert.Equal(t, "creator", kb.CreatorID, "failed promotion must abort before the CAS")
}

func TestTransferKnowledgeBaseOwnership_APIKeyScopeForbidden(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	ctx := types.WithTenantAPIKeyScope(transferTestCtx("creator", types.TenantRoleContributor, 1), types.TenantAPIKeyScope{FullAccess: true})
	_, err := svc.TransferKnowledgeBaseOwnership(ctx, "kb-1", "target")
	assert.ErrorIs(t, err, ErrKBOwnerTransferForbidden)
}

func TestTransferKnowledgeBaseOwnership_SharedKBCrossTenantCallerForbidden(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	// Caller lives in tenant 2 while the KB belongs to tenant 1: the shared-KB
	// scenario where KBAccess* rewrote the execution tenant.
	ctx := types.WithExecutionTenant(transferTestCtx("editor", types.TenantRoleContributor, 2), 1)
	_, err := svc.TransferKnowledgeBaseOwnership(ctx, "kb-1", "target")
	assert.ErrorIs(t, err, ErrKBOwnerTransferForbidden)
}

func TestTransferKnowledgeBaseOwnership_SharedSpaceTarget(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{extraMemberships: []*types.TenantMember{
		{UserID: "remote", TenantID: 2, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
	}}
	svc, audit := newTransferService(kb, members)
	svc.kbShareService = &transferShareService{tenantIDs: []uint64{2}}

	got, err := svc.TransferKnowledgeBaseOwnership(
		transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "remote")
	require.NoError(t, err)
	assert.Equal(t, "remote", got.CreatorID)
	assert.Empty(t, members.roleCalls, "a cross-space target's own workspace role must not be changed")
	require.Len(t, audit.entries, 1)
	assert.Equal(t, types.AuditActionKBOwnerTransferred, audit.entries[0].Action)
}

func TestTransferKnowledgeBaseOwnership_SharedSpaceViewerTargetRejected(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{extraMemberships: []*types.TenantMember{
		{UserID: "remote-viewer", TenantID: 2, Role: types.TenantRoleViewer, Status: types.TenantMemberStatusActive},
	}}
	svc, _ := newTransferService(kb, members)
	svc.kbShareService = &transferShareService{tenantIDs: []uint64{2}}

	_, err := svc.TransferKnowledgeBaseOwnership(
		transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "remote-viewer")
	assert.ErrorIs(t, err, ErrKBOwnerTransferTargetRole)
	assert.Equal(t, "creator", kb.CreatorID)
}

func TestTransferKnowledgeBaseOwnership_TargetOutsideSharedSpacesRejected(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{extraMemberships: []*types.TenantMember{
		{UserID: "outsider", TenantID: 3, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
	}}
	svc, _ := newTransferService(kb, members)
	svc.kbShareService = &transferShareService{tenantIDs: []uint64{2}}

	_, err := svc.TransferKnowledgeBaseOwnership(
		transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "outsider")
	assert.ErrorIs(t, err, ErrKBOwnerTransferNotMember)
}

func TestTransferKnowledgeBaseOwnership_CrossWorkspaceCreatorMayTransferAgain(t *testing.T) {
	// The KB stays in tenant 1 after an earlier cross-workspace transfer; the
	// creator now lives in tenant 2 and must still be allowed to hand it on.
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "remote-creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)

	ctx := types.WithExecutionTenant(
		transferTestCtx("remote-creator", types.TenantRoleOwner, 2), 1)
	got, err := svc.TransferKnowledgeBaseOwnership(ctx, "kb-1", "target")
	require.NoError(t, err)
	assert.Equal(t, "target", got.CreatorID)
}

func TestListKnowledgeBaseTransferCandidates_MergesWorkspaceAndSharedSpace(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{
		membersPage: []*types.TenantMember{
			{UserID: "creator", TenantID: 1, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
			{UserID: "local", TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive},
			{UserID: "invited", TenantID: 1, Role: types.TenantRoleViewer, Status: types.TenantMemberStatusInvited},
		},
		extraMemberships: []*types.TenantMember{
			{UserID: "remote", TenantID: 2, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
			{UserID: "remote-suspended", TenantID: 2, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusSuspended},
		},
	}
	svc, _ := newTransferService(kb, members)
	svc.kbShareService = &transferShareService{tenantIDs: []uint64{2, 3}}

	got, err := svc.ListKnowledgeBaseTransferCandidates(
		transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "", 50)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "local", got[0].UserID, "workspace members come first")
	assert.Equal(t, "remote", got[1].UserID)
	assert.Equal(t, uint64(2), got[1].TenantID)
}

func TestListKnowledgeBaseTransferCandidates_SearchAndLimit(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{
		membersPage: []*types.TenantMember{
			{UserID: "alice", TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive},
			{UserID: "bob", TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive},
		},
		extraMemberships: []*types.TenantMember{
			{UserID: "carol", TenantID: 2, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
		},
	}
	svc, _ := newTransferService(kb, members)
	svc.kbShareService = &transferShareService{tenantIDs: []uint64{2}}

	got, err := svc.ListKnowledgeBaseTransferCandidates(
		transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "carol", 50)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "carol", got[0].UserID)

	limited, err := svc.ListKnowledgeBaseTransferCandidates(
		transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "", 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
	assert.Equal(t, "alice", limited[0].UserID)
}

func TestListKnowledgeBaseTransferCandidates_RequiresCreatorOrAdmin(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{}
	svc, _ := newTransferService(kb, members)

	_, err := svc.ListKnowledgeBaseTransferCandidates(
		transferTestCtx("someone-else", types.TenantRoleContributor, 1), "kb-1", "", 50)
	assert.ErrorIs(t, err, ErrKBOwnerTransferForbidden)
}

func TestListKnowledgeBaseTransferCandidates_CrossWorkspaceCreatorAllowed(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "remote-creator", Name: "kb"}
	members := &transferMemberService{
		membersPage: []*types.TenantMember{
			{UserID: "local", TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive},
		},
	}
	svc, _ := newTransferService(kb, members)

	ctx := types.WithExecutionTenant(
		transferTestCtx("remote-creator", types.TenantRoleOwner, 2), 1)
	got, err := svc.ListKnowledgeBaseTransferCandidates(ctx, "kb-1", "", 50)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "local", got[0].UserID)
}

func TestTransferKnowledgeBaseOwnership_RepoErrorPropagates(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1, CreatorID: "creator", Name: "kb"}
	members := &transferMemberService{membership: activeMember("target", types.TenantRoleContributor), membershipOK: true}
	svc, _ := newTransferService(kb, members)
	delete(svc.repo.(*transferKBRepo).rows, "kb-1")

	_, err := svc.TransferKnowledgeBaseOwnership(transferTestCtx("creator", types.TenantRoleContributor, 1), "kb-1", "target")
	require.Error(t, err)
}
