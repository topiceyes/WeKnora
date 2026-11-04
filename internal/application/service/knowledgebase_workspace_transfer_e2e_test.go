package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransferKnowledgeBaseWorkspaceEndToEnd exercises the public entry point:
// preflight, file copy, the cross-table transaction, and post-commit source
// cleanup. It is the closest thing to the endpoint without an HTTP server.
func TestTransferKnowledgeBaseWorkspaceEndToEnd(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", baseDir)
	db := newKBWorkspaceTransferTestDB(t)

	sourceTenant := &types.Tenant{ID: 10000, Name: "source", Status: "active", StorageUsed: 100}
	targetTenant := &types.Tenant{ID: 10001, Name: "target", Status: "active"}
	require.NoError(t, db.Create(sourceTenant).Error)
	require.NoError(t, db.Create(targetTenant).Error)
	require.NoError(t, db.Create(&types.User{
		ID: "new", Username: "new", Email: "new@example.com", TenantID: 10001, IsActive: true,
	}).Error)

	writeFile := func(rel string) {
		path := filepath.Join(baseDir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("data-"+rel), 0o644))
	}
	writeFile("10000/k-1/report.pdf")
	writeFile("10000/exports/image.png")

	kb := &types.KnowledgeBase{
		ID: "kb-1", TenantID: 10000, CreatorID: "creator", Name: "KB",
	}
	require.NoError(t, db.Create(kb).Error)
	// The CAS compares updated_at, so the repo must return the persisted row
	// (production reads it from the database, not from a fixture).
	var loadedKB types.KnowledgeBase
	require.NoError(t, db.First(&loadedKB, "id = ?", kb.ID).Error)
	repo := &transferKBRepo{fakeKBRepo: *newFakeKBRepo(), casMatch: true}
	repo.rows[kb.ID] = &loadedKB

	require.NoError(t, db.Create(&types.Knowledge{
		ID: "k-1", TenantID: 10000, KnowledgeBaseID: kb.ID,
		ParseStatus: types.ParseStatusCompleted, StorageSize: 100,
	}).Error)
	oldBackend := "old-backend"
	require.NoError(t, db.Create(&types.StoredResource{
		ID: "res-1", Handle: "handle-1", TenantID: 10000, StorageBackendID: oldBackend,
		Provider: "local", PhysicalPath: "storage://old-backend/local://10000/exports/image.png",
		LocationHash: resourceLocationHash("storage://old-backend/local://10000/exports/image.png"),
	}).Error)
	require.NoError(t, db.Create(&types.ResourceBinding{
		ID: "bind-1", ResourceID: "res-1", TenantID: 10000,
		OwnerType: types.ResourceOwnerKnowledge, OwnerID: "k-1",
	}).Error)

	svc := &knowledgeBaseService{
		repo: repo,
		db:   db,
		tenantRepo: &wsTransferTenantRepo{tenants: map[uint64]*types.Tenant{
			10000: sourceTenant, 10001: targetTenant,
		}},
		tenantMemberService: &transferMemberService{membership: &types.TenantMember{
			UserID: "new", TenantID: 10001, Role: types.TenantRoleOwner,
			Status: types.TenantMemberStatusActive,
		}, membershipOK: true},
		storageResolver: &wsTransferStorageResolver{backend: &types.StorageBackend{
			ID: "new-backend", TenantID: 10001, Provider: "local",
			Status: types.StorageBackendStatusActive,
		}},
	}

	result, err := svc.TransferKnowledgeBaseWorkspace(
		transferTestCtx("creator", types.TenantRoleContributor, 10000), kb.ID, "new")
	require.NoError(t, err)
	assert.Equal(t, kb.ID, result.KnowledgeBaseID)
	assert.Equal(t, uint64(10000), result.FromTenantID)
	assert.Equal(t, uint64(10001), result.ToTenantID)
	assert.Equal(t, "new", result.NewOwnerUserID)
	assert.Equal(t, int64(1), result.Stats.KnowledgeCount)
	// FileBytes is the on-disk size of the copied files, not knowledge storage_size.
	assert.Positive(t, result.Stats.FileBytes)
	assert.Equal(t, int64(2), result.Stats.FileCount)

	var movedKB types.KnowledgeBase
	require.NoError(t, db.First(&movedKB, "id = ?", kb.ID).Error)
	assert.Equal(t, uint64(10001), movedKB.TenantID)
	assert.Equal(t, "new", movedKB.CreatorID)
	require.NotNil(t, movedKB.StorageBackendID)
	assert.Equal(t, "new-backend", *movedKB.StorageBackendID)

	var movedKnowledge types.Knowledge
	require.NoError(t, db.First(&movedKnowledge, "id = ?", "k-1").Error)
	assert.Equal(t, uint64(10001), movedKnowledge.TenantID)

	var movedResource types.StoredResource
	require.NoError(t, db.First(&movedResource, "id = ?", "res-1").Error)
	assert.Equal(t, uint64(10001), movedResource.TenantID)
	assert.Equal(t, "new-backend", movedResource.StorageBackendID)
	assert.Equal(t, "storage://new-backend/local://10001/exports/image.png", movedResource.PhysicalPath)
	assert.Equal(t, resourceLocationHash(movedResource.PhysicalPath), movedResource.LocationHash)

	// Files were copied under the new workspace and the sources removed.
	assert.FileExists(t, filepath.Join(baseDir, "10001", "k-1", "report.pdf"))
	assert.FileExists(t, filepath.Join(baseDir, "10001", "exports", "image.png"))
	assert.NoFileExists(t, filepath.Join(baseDir, "10000", "k-1", "report.pdf"))
	assert.NoFileExists(t, filepath.Join(baseDir, "10000", "exports", "image.png"))

	var updatedSource, updatedTarget types.Tenant
	require.NoError(t, db.First(&updatedSource, 10000).Error)
	require.NoError(t, db.First(&updatedTarget, 10001).Error)
	assert.Equal(t, int64(0), updatedSource.StorageUsed)
	assert.Equal(t, int64(100), updatedTarget.StorageUsed)
}
