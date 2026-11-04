package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// A cross-workspace ownership transfer only moves creator_id: the KB keeps
// living in its original workspace. These tests pin the consequence — every
// share path must treat creator_id, not the caller's tenant, as ownership,
// while the KB's data tenant still drives counts and audit.
const (
	kbDataTenant  = uint64(10000) // workspace the KB's data lives in
	ownerTenant   = uint64(10001) // the transferred owner's own workspace
	kbCreatorID   = "owner-user"
	creatorKBID   = "kb-product"
	creatorOrgID  = "org-product"
	otherTenantID = uint64(10002)
)

type creatorKBRepo struct {
	interfaces.KnowledgeBaseRepository
	kb *types.KnowledgeBase
}

func (r creatorKBRepo) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	if r.kb == nil {
		return nil, repository.ErrKnowledgeBaseNotFound
	}
	copied := *r.kb
	return &copied, nil
}

type creatorOrgRepo struct {
	interfaces.OrganizationRepository
	memberRoles map[uint64]types.OrgMemberRole
	memberErr   error
}

func (r creatorOrgRepo) GetByID(context.Context, string) (*types.Organization, error) {
	return &types.Organization{ID: creatorOrgID, Name: "产品技术中心"}, nil
}

func (r creatorOrgRepo) GetTenantMember(
	_ context.Context, _ string, tenantID uint64,
) (*types.OrganizationTenantMember, error) {
	if r.memberErr != nil {
		return nil, r.memberErr
	}
	role, ok := r.memberRoles[tenantID]
	if !ok {
		return nil, repository.ErrOrgMemberNotFound
	}
	return &types.OrganizationTenantMember{TenantID: tenantID, Role: role}, nil
}

type creatorShareRepo struct {
	interfaces.KBShareRepository
	shares   []*types.KnowledgeBaseShare
	created  []*types.KnowledgeBaseShare
	updated  []*types.KnowledgeBaseShare
	deleted  []string
	perOrg   []*types.KnowledgeBaseShare
	perTenan []*types.KnowledgeBaseShare
}

func (r *creatorShareRepo) Create(_ context.Context, share *types.KnowledgeBaseShare) error {
	r.created = append(r.created, share)
	return nil
}

func (r *creatorShareRepo) GetByID(_ context.Context, id string) (*types.KnowledgeBaseShare, error) {
	for _, share := range r.shares {
		if share.ID == id {
			copied := *share
			return &copied, nil
		}
	}
	return nil, repository.ErrKBShareNotFound
}

func (r *creatorShareRepo) Update(_ context.Context, share *types.KnowledgeBaseShare) error {
	r.updated = append(r.updated, share)
	return nil
}

func (r *creatorShareRepo) Delete(_ context.Context, id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func (r *creatorShareRepo) ListByKnowledgeBase(
	_ context.Context, _ string,
) ([]*types.KnowledgeBaseShare, error) {
	return r.shares, nil
}

func (r *creatorShareRepo) ListByOrganization(
	_ context.Context, _ string,
) ([]*types.KnowledgeBaseShare, error) {
	return r.perOrg, nil
}

func (r *creatorShareRepo) ListSharedKBsForTenant(
	_ context.Context, _ uint64,
) ([]*types.KnowledgeBaseShare, error) {
	return r.perTenan, nil
}

type countingKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	tenants []uint64
	count   int64
}

func (r *countingKnowledgeRepo) CountKnowledgeByKnowledgeBaseID(
	_ context.Context, tenantID uint64, _ string,
) (int64, error) {
	r.tenants = append(r.tenants, tenantID)
	return r.count, nil
}

type countingChunkRepo struct {
	interfaces.ChunkRepository
	tenants []uint64
	count   int64
}

func (r *countingChunkRepo) CountChunksByKnowledgeBaseID(
	_ context.Context, tenantID uint64, _ string,
) (int64, error) {
	r.tenants = append(r.tenants, tenantID)
	return r.count, nil
}

func transferredKB() *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID:        creatorKBID,
		Name:      "产品技术",
		TenantID:  kbDataTenant,
		CreatorID: kbCreatorID,
		Type:      types.KnowledgeBaseTypeDocument,
	}
}

func creatorCtx() context.Context {
	return types.WithCaller(context.Background(), types.Caller{
		TenantID: ownerTenant, UserID: kbCreatorID, Role: types.TenantRoleOwner,
	})
}

func newCreatorShareService(
	shares *creatorShareRepo, kb *types.KnowledgeBase,
) (*kbShareService, *countingKnowledgeRepo) {
	kgRepo := &countingKnowledgeRepo{count: 7}
	return &kbShareService{
		shareRepo: shares,
		orgRepo: creatorOrgRepo{memberRoles: map[uint64]types.OrgMemberRole{
			ownerTenant: types.OrgRoleEditor,
		}},
		kbRepo: creatorKBRepo{kb: kb},
		kgRepo: kgRepo,
	}, kgRepo
}

func TestShareKnowledgeBaseAllowsCrossWorkspaceCreator(t *testing.T) {
	shares := &creatorShareRepo{}
	svc, _ := newCreatorShareService(shares, transferredKB())

	share, err := svc.ShareKnowledgeBase(
		creatorCtx(), creatorKBID, creatorOrgID, kbCreatorID, ownerTenant, types.OrgRoleEditor,
	)
	require.NoError(t, err)
	require.Len(t, shares.created, 1)
	// The share's lifecycle anchor is the owner's own tenant, not the KB's
	// data tenant: repository queries drop a share once its anchor leaves the
	// organization, and the data tenant may not even be a member.
	require.Equal(t, ownerTenant, share.SourceTenantID)
	require.Equal(t, creatorKBID, share.KnowledgeBaseID)
}

func TestShareKnowledgeBaseRejectsCrossWorkspaceNonCreator(t *testing.T) {
	shares := &creatorShareRepo{}
	svc, _ := newCreatorShareService(shares, transferredKB())

	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: otherTenantID, UserID: "someone-else", Role: types.TenantRoleOwner,
	})
	_, err := svc.ShareKnowledgeBase(
		ctx, creatorKBID, creatorOrgID, "someone-else", otherTenantID, types.OrgRoleEditor,
	)
	require.ErrorIs(t, err, ErrNotKBOwner)
	require.Empty(t, shares.created)
}

func TestShareKnowledgeBaseKeepsOrgRoleRequirementForCreator(t *testing.T) {
	shares := &creatorShareRepo{}
	svc, _ := newCreatorShareService(shares, transferredKB())
	svc.orgRepo = creatorOrgRepo{memberRoles: map[uint64]types.OrgMemberRole{
		ownerTenant: types.OrgRoleViewer,
	}}

	_, err := svc.ShareKnowledgeBase(
		creatorCtx(), creatorKBID, creatorOrgID, kbCreatorID, ownerTenant, types.OrgRoleEditor,
	)
	require.ErrorIs(t, err, ErrOrgRoleCannotShare)
}

func TestListSharesByKnowledgeBaseAllowsCrossWorkspaceCreator(t *testing.T) {
	shares := &creatorShareRepo{shares: []*types.KnowledgeBaseShare{{ID: "share-1"}}}
	svc, _ := newCreatorShareService(shares, transferredKB())

	list, err := svc.ListSharesByKnowledgeBase(creatorCtx(), creatorKBID, ownerTenant)
	require.NoError(t, err)
	require.Len(t, list, 1)

	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: otherTenantID, UserID: "someone-else", Role: types.TenantRoleOwner,
	})
	_, err = svc.ListSharesByKnowledgeBase(ctx, creatorKBID, otherTenantID)
	require.ErrorIs(t, err, ErrNotKBOwner)
}

func TestUpdateAndRemoveShareAllowCrossWorkspaceCreator(t *testing.T) {
	shares := &creatorShareRepo{shares: []*types.KnowledgeBaseShare{{
		ID: "share-1", KnowledgeBaseID: creatorKBID, OrganizationID: creatorOrgID,
		SharedByUserID: "original-sharer", SourceTenantID: kbDataTenant,
		Permission: types.OrgRoleEditor,
	}}}
	svc, _ := newCreatorShareService(shares, transferredKB())

	// The creator is neither the sharer nor a member of the source tenant, so
	// only the creator path can authorize this.
	require.NoError(t, svc.UpdateSharePermission(
		creatorCtx(), "share-1", types.OrgRoleViewer, kbCreatorID, ownerTenant,
	))
	require.Len(t, shares.updated, 1)
	require.Equal(t, types.OrgRoleViewer, shares.updated[0].Permission)

	require.NoError(t, svc.RemoveShare(creatorCtx(), "share-1", kbCreatorID, ownerTenant))
	require.Equal(t, []string{"share-1"}, shares.deleted)

	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: otherTenantID, UserID: "someone-else", Role: types.TenantRoleOwner,
	})
	require.ErrorIs(t, svc.UpdateSharePermission(
		ctx, "share-1", types.OrgRoleEditor, "someone-else", otherTenantID,
	), ErrSharePermissionDenied)
}

func TestListSharedKnowledgeBasesUsesKBDataTenant(t *testing.T) {
	// The share anchor is the owner's tenant, but the KB lives in 10000.
	// Listing must key "is this mine" and the counts on the data tenant.
	shares := &creatorShareRepo{perTenan: []*types.KnowledgeBaseShare{{
		ID: "share-1", KnowledgeBaseID: creatorKBID, OrganizationID: creatorOrgID,
		SourceTenantID: ownerTenant, Permission: types.OrgRoleEditor,
		KnowledgeBase: transferredKB(),
	}}}
	svc, kgRepo := newCreatorShareService(shares, transferredKB())
	svc.orgRepo = creatorOrgRepo{memberRoles: map[uint64]types.OrgMemberRole{
		ownerTenant: types.OrgRoleEditor,
	}}
	svc.chunkRepo = &countingChunkRepo{count: 3}

	list, err := svc.ListSharedKnowledgeBases(creatorCtx(), ownerTenant, types.TenantRoleOwner)
	require.NoError(t, err)
	require.Len(t, list, 1, "creator must still see the KB when shared from their own tenant")
	require.Equal(t, types.OrgRoleAdmin, list[0].Permission)
	require.Equal(t, []uint64{kbDataTenant}, kgRepo.tenants)
	require.Equal(t, int64(7), list[0].KnowledgeBase.KnowledgeCount)
}

func TestListSharedKnowledgeBasesInOrganizationCreatorFlags(t *testing.T) {
	kb := transferredKB()
	shares := &creatorShareRepo{perOrg: []*types.KnowledgeBaseShare{
		{
			ID: "share-creator", KnowledgeBaseID: creatorKBID, OrganizationID: creatorOrgID,
			SourceTenantID: ownerTenant, Permission: types.OrgRoleEditor, KnowledgeBase: kb,
		},
		{
			ID: "share-source", KnowledgeBaseID: "kb-other", OrganizationID: creatorOrgID,
			SourceTenantID: kbDataTenant, Permission: types.OrgRoleEditor,
			KnowledgeBase: &types.KnowledgeBase{
				ID: "kb-other", TenantID: kbDataTenant, CreatorID: "colleague",
				Type: types.KnowledgeBaseTypeDocument,
			},
		},
	}}
	svc, kgRepo := newCreatorShareService(shares, kb)

	list, err := svc.ListSharedKnowledgeBasesInOrganization(
		creatorCtx(), creatorOrgID, ownerTenant, types.TenantRoleOwner,
	)
	require.NoError(t, err)
	require.Len(t, list, 2)

	byKB := map[string]*types.OrganizationSharedKnowledgeBaseItem{}
	for _, item := range list {
		byKB[item.KnowledgeBase.ID] = item
	}
	require.True(t, byKB[creatorKBID].IsCreator)
	require.True(t, byKB[creatorKBID].IsMine, "IsMine keeps its meaning: shared out by the caller's own tenant")
	require.Equal(t, types.OrgRoleAdmin, byKB[creatorKBID].Permission)
	require.False(t, byKB["kb-other"].IsCreator)
	require.False(t, byKB["kb-other"].IsMine, "source tenant check stays anchored to share.SourceTenantID")

	// Counts use the KB's data tenant, never the share anchor.
	require.Equal(t, []uint64{kbDataTenant, kbDataTenant}, kgRepo.tenants)
}
