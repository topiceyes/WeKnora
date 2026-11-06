package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/kbmutation"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type wsTransferTenantRepo struct {
	interfaces.TenantRepository
	tenants map[uint64]*types.Tenant
}

func (r *wsTransferTenantRepo) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	tenant, ok := r.tenants[id]
	if !ok || tenant == nil {
		return nil, errors.New("tenant not found")
	}
	return tenant, nil
}

type wsTransferStorageResolver struct {
	interfaces.StorageBackendResolver
	backend *types.StorageBackend
}

func (r *wsTransferStorageResolver) ResolveBackend(
	context.Context, *types.Tenant, string, string,
) (*types.StorageBackend, error) {
	return r.backend, nil
}

func TestBuildKnowledgeBaseWorkspaceTransferPlanBlockers(t *testing.T) {
	db := newKBWorkspaceTransferTestDB(t)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())

	sourceTenant := &types.Tenant{
		ID: 10000, Name: "source", Status: "active", StorageQuota: 1_000_000,
		RetrieverEngines: types.RetrieverEngines{Engines: []types.RetrieverEngineParams{
			{RetrieverEngineType: types.RetrieverEngineType("elasticsearch")},
		}},
	}
	targetTenant := &types.Tenant{
		ID: 10001, Name: "target", Status: "active", StorageQuota: 10,
	}
	require.NoError(t, db.Create(sourceTenant).Error)
	require.NoError(t, db.Create(targetTenant).Error)
	require.NoError(t, db.Create(&types.User{
		ID: "new", Username: "new", Email: "new@example.com", TenantID: 10001, IsActive: true,
	}).Error)

	vectorStore := "vs-1"
	kb := &types.KnowledgeBase{
		ID: "kb-1", TenantID: 10000, CreatorID: "creator", Name: "KB",
		VectorStoreID: &vectorStore, SummaryModelID: "private-model",
	}
	repo := &transferKBRepo{fakeKBRepo: *newFakeKBRepo(), casMatch: true}
	repo.rows[kb.ID] = kb
	members := &transferMemberService{membership: &types.TenantMember{
		UserID: "new", TenantID: 10001, Role: types.TenantRoleOwner,
		Status: types.TenantMemberStatusActive,
	}, membershipOK: true}
	svc := &knowledgeBaseService{
		repo:                repo,
		db:                  db,
		tenantRepo:          &wsTransferTenantRepo{tenants: map[uint64]*types.Tenant{10000: sourceTenant, 10001: targetTenant}},
		tenantMemberService: members,
		// No target backend: the target_storage_backend_missing blocker.
		storageResolver: &wsTransferStorageResolver{},
	}

	require.NoError(t, db.Create(&types.Knowledge{
		ID: "k-ok", TenantID: 10000, KnowledgeBaseID: kb.ID, ParseStatus: types.ParseStatusCompleted, StorageSize: 100,
	}).Error)
	require.NoError(t, db.Create(&types.Knowledge{
		ID: "k-proc", TenantID: 10000, KnowledgeBaseID: kb.ID, ParseStatus: types.ParseStatusProcessing, StorageSize: 500,
	}).Error)
	require.NoError(t, db.Create(&types.Knowledge{
		ID: "k-other", TenantID: 10000, KnowledgeBaseID: "kb-2", ParseStatus: types.ParseStatusCompleted,
	}).Error)
	require.NoError(t, db.Exec(`INSERT INTO task_pending_ops
		(tenant_id, task_type, scope, scope_id, op, payload, enqueued_at)
		VALUES (?, ?, ?, ?, ?, '{}', CURRENT_TIMESTAMP)`,
		10000, "wiki:ingest", types.TaskScopeKnowledgeBase, kb.ID, "ingest").Error)

	s3Path := "s3://bucket/k-ok.md"
	require.NoError(t, db.Create(&types.StoredResource{
		ID: "res-s3", Handle: "handle-s3", TenantID: 10000, Provider: "s3",
		PhysicalPath: s3Path, LocationHash: resourceLocationHash(s3Path),
	}).Error)
	sharedPath := "local://10000/exports/shared.png"
	require.NoError(t, db.Create(&types.StoredResource{
		ID: "res-shared", Handle: "handle-shared", TenantID: 10000, Provider: "local",
		PhysicalPath: sharedPath, LocationHash: resourceLocationHash(sharedPath),
	}).Error)
	for _, binding := range []*types.ResourceBinding{
		{ID: "b1", ResourceID: "res-s3", TenantID: 10000, OwnerType: types.ResourceOwnerKnowledge, OwnerID: "k-ok"},
		{ID: "b2", ResourceID: "res-shared", TenantID: 10000, OwnerType: types.ResourceOwnerKnowledge, OwnerID: "k-ok"},
		{ID: "b3", ResourceID: "res-shared", TenantID: 10000, OwnerType: types.ResourceOwnerKnowledge, OwnerID: "k-other"},
	} {
		require.NoError(t, db.Create(binding).Error)
	}

	require.NoError(t, db.Exec(`INSERT INTO im_channels (id, tenant_id, knowledge_base_id, enabled)
		VALUES ('im1', 10000, 'kb-1', 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO custom_agents (id, tenant_id, name, config)
		VALUES ('ag1', 10000, 'agent-a', '{"knowledge_bases":["kb-1"]}')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO tenant_api_keys (id, tenant_id, name, knowledge_base_ids)
		VALUES (1, 10000, 'key-a', '["kb-1"]')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO mcp_endpoints (id, tenant_id, name, knowledge_base_ids, enabled)
		VALUES ('mcp1', 10000, 'mcp-a', '["kb-1"]', 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO kb_shares
		(id, knowledge_base_id, organization_id, shared_by_user_id, source_tenant_id, permission)
		VALUES ('share1', 'kb-1', 'org1', 'creator', 10000, 'viewer')`).Error)

	plan, err := svc.BuildKnowledgeBaseWorkspaceTransferPlan(
		transferTestCtx("creator", types.TenantRoleContributor, 10000), kb.ID, "new")
	require.NoError(t, err)
	require.True(t, plan.MovesWorkspace)
	assert.Equal(t, uint64(10000), plan.SourceTenantID)
	assert.Equal(t, uint64(10001), plan.TargetTenantID)
	assert.Equal(t, "new", plan.TargetUserID)

	codes := make(map[string]bool, len(plan.Blockers))
	for _, blocker := range plan.Blockers {
		codes[blocker.Code] = true
	}
	for _, want := range []string{
		"kb_processing",
		"storage_provider_unsupported",
		"vector_store_bound",
		"retrieval_engine_mismatch",
		"target_storage_backend_missing",
		"im_channel_bound",
		"agent_bound",
		"api_key_bound",
		"mcp_endpoint_bound",
		"private_model_unavailable",
		"resource_shared",
		"target_quota_exceeded",
		"share_org_membership_missing",
	} {
		assert.True(t, codes[want], "missing blocker %s in %v", want, codes)
	}
	assert.Equal(t, int64(1), plan.SharesKept)
	assert.NotEmpty(t, plan.Warnings, "a missing local file should be a warning, not a blocker")
	assert.Equal(t, "source_file_missing", plan.Warnings[0].Code)
}

func TestBuildKnowledgeBaseWorkspaceTransferPlanSameTenant(t *testing.T) {
	db := newKBWorkspaceTransferTestDB(t)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())

	tenant := &types.Tenant{ID: 10000, Name: "source", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	require.NoError(t, db.Create(&types.User{
		ID: "member", Username: "member", Email: "member@example.com", TenantID: 10000, IsActive: true,
	}).Error)
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 10000, CreatorID: "creator", Name: "KB"}
	repo := &transferKBRepo{fakeKBRepo: *newFakeKBRepo(), casMatch: true}
	repo.rows[kb.ID] = kb
	svc := &knowledgeBaseService{
		repo:       repo,
		db:         db,
		tenantRepo: &wsTransferTenantRepo{tenants: map[uint64]*types.Tenant{10000: tenant}},
		tenantMemberService: &transferMemberService{membership: &types.TenantMember{
			UserID: "member", TenantID: 10000, Role: types.TenantRoleContributor,
			Status: types.TenantMemberStatusActive,
		}, membershipOK: true},
	}

	plan, err := svc.BuildKnowledgeBaseWorkspaceTransferPlan(
		transferTestCtx("creator", types.TenantRoleContributor, 10000), kb.ID, "member")
	require.NoError(t, err)
	assert.False(t, plan.MovesWorkspace)
	assert.Empty(t, plan.Blockers)
	require.NoError(t, ConfigureKBMutationCoordination(&kbmutation.Coordinator{BaseDir: kbWorkspaceTransferBaseDir()}, db, svc))

	_, err = svc.TransferKnowledgeBaseWorkspace(
		transferTestCtx("creator", types.TenantRoleContributor, 10000), kb.ID, "member")
	require.ErrorIs(t, err, ErrKBWorkspaceTransferSameTenant)
}

func TestPrepareWorkspaceTransferRejectsAPIKeyAndNonMember(t *testing.T) {
	db := newKBWorkspaceTransferTestDB(t)
	tenant := &types.Tenant{ID: 10000, Name: "source", Status: "active"}
	target := &types.Tenant{ID: 10001, Name: "target", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	require.NoError(t, db.Create(target).Error)
	require.NoError(t, db.Create(&types.User{
		ID: "new", Username: "new", Email: "new@example.com", TenantID: 10001, IsActive: true,
	}).Error)
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 10000, CreatorID: "creator", Name: "KB"}
	repo := &transferKBRepo{fakeKBRepo: *newFakeKBRepo(), casMatch: true}
	repo.rows[kb.ID] = kb
	svc := &knowledgeBaseService{
		repo:                repo,
		db:                  db,
		tenantRepo:          &wsTransferTenantRepo{tenants: map[uint64]*types.Tenant{10000: tenant, 10001: target}},
		tenantMemberService: &transferMemberService{},
	}

	baseCtx := transferTestCtx("creator", types.TenantRoleContributor, 10000)
	apiKeyCtx := types.WithTenantAPIKeyScope(baseCtx, types.TenantAPIKeyScope{FullAccess: true})
	_, err := svc.BuildKnowledgeBaseWorkspaceTransferPlan(apiKeyCtx, kb.ID, "new")
	require.ErrorIs(t, err, ErrKBOwnerTransferForbidden)

	_, err = svc.BuildKnowledgeBaseWorkspaceTransferPlan(baseCtx, kb.ID, "new")
	require.ErrorIs(t, err, ErrKBOwnerTransferNotMember)

	// Self-move is the repair path for rows the legacy transfer left behind.
	svc.tenantMemberService = &transferMemberService{membership: &types.TenantMember{
		UserID: "creator", TenantID: 10001, Role: types.TenantRoleOwner,
		Status: types.TenantMemberStatusActive,
	}, membershipOK: true}
	require.NoError(t, db.Create(&types.User{
		ID: "creator", Username: "creator", Email: "creator@example.com", TenantID: 10001, IsActive: true,
	}).Error)
	selfPlan, err := svc.BuildKnowledgeBaseWorkspaceTransferPlan(baseCtx, kb.ID, "creator")
	require.NoError(t, err)
	assert.True(t, selfPlan.MovesWorkspace)

	// A workspace admin may not trigger the creator's self-move; only the
	// creator can repair their own legacy row.
	_, err = svc.BuildKnowledgeBaseWorkspaceTransferPlan(
		transferTestCtx("other-admin", types.TenantRoleAdmin, 10000), kb.ID, "creator")
	require.ErrorIs(t, err, ErrKBOwnerTransferTargetSelf)
}
