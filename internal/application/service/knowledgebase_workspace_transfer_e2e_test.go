package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/kbmutation"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransferKnowledgeBaseWorkspaceEndToEnd exercises the public entry point:
// preflight, file copy, the cross-table transaction, and post-commit source
// cleanup. It is the closest thing to the endpoint without an HTTP server.
func TestTransferKnowledgeBaseWorkspaceEndToEnd(t *testing.T) {
	svc, db, baseDir, kb := newWorkspaceTransferIntegrationFixture(t)
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

func newWorkspaceTransferIntegrationFixture(t *testing.T) (*knowledgeBaseService, *gorm.DB, string, *types.KnowledgeBase) {
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

	require.NoError(t, ConfigureKBMutationCoordination(&kbmutation.Coordinator{BaseDir: baseDir}, db, svc))
	return svc, db, baseDir, kb
}

func TestWorkspaceTransferWaitsForAdmittedWrite(t *testing.T) {
	svc, db, baseDir, kb := newWorkspaceTransferIntegrationFixture(t)
	_, releaseWrite, err := svc.mutations.Begin(context.Background())
	require.NoError(t, err)
	defer releaseWrite()
	ctx := transferTestCtx("creator", types.TenantRoleContributor, 10000)
	_, err = svc.TransferKnowledgeBaseWorkspace(ctx, kb.ID, "new")
	require.ErrorIs(t, err, ErrKBWorkspaceTransferInProgress)
	var persisted types.KnowledgeBase
	require.NoError(t, db.First(&persisted, "id = ?", kb.ID).Error)
	assert.Equal(t, uint64(10000), persisted.TenantID)
	assert.FileExists(t, filepath.Join(baseDir, "10000/k-1/report.pdf"))
	assert.NoFileExists(t, filepath.Join(baseDir, "10001/k-1/report.pdf"))
	releaseWrite()
	_, err = svc.TransferKnowledgeBaseWorkspace(ctx, kb.ID, "new")
	require.NoError(t, err)
}

func TestWorkspaceTransferBlocksServiceWriters(t *testing.T) {
	svc, db, baseDir, _ := newWorkspaceTransferIntegrationFixture(t)
	knowledge := &knowledgeService{}
	chunks := &chunkService{}
	sources := &DataSourceService{}
	pages := &wikiPageService{}
	tags := &knowledgeTagService{}
	resources := &resourceCatalog{}
	wiki := &wikiIngestService{}
	require.NoError(t, ConfigureKBMutationCoordination(svc.mutations, db, knowledge, chunks, sources, pages, tags, resources, wiki))
	unlock, err := svc.mutations.Exclusive()
	require.NoError(t, err)
	defer unlock()
	ctx := context.Background()
	checks := map[string]func() error{
		"upload": func() error {
			_, err := knowledge.CreateKnowledgeFromFile(ctx, "kb-1", nil, nil, nil, "", nil, "", nil)
			return err
		},
		"edit":                         func() error { _, err := knowledge.UpdateKnowledgeContent(ctx, "k-1", nil); return err },
		"content image":                func() error { _, err := knowledge.UploadContentImage(ctx, "kb-1", nil); return err },
		"batch delete":                 func() error { return knowledge.DeleteKnowledgeList(ctx, []string{"k-1"}) },
		"clone":                        func() error { return knowledge.CloneKnowledgeBase(ctx, "kb-1", "kb-2") },
		"chunks":                       func() error { return chunks.UpdateChunks(ctx, nil) },
		"wiki edit":                    func() error { _, err := pages.UpdatePage(ctx, nil); return err },
		"wiki get which creates index": func() error { _, err := pages.GetIndex(ctx, "kb-1"); return err },
		"tags":                         func() error { _, err := tags.CreateTag(ctx, "kb-1", "tag", "", 0); return err },
		"resources":                    func() error { return resources.Bind(ctx, "ref", "knowledge", "k-1", "image") },
		"manual sync":                  func() error { _, err := sources.ManualSync(ctx, "ds-1"); return err },
	}
	for name, call := range checks {
		t.Run(name, func(t *testing.T) {
			err := call()
			var appErr *apperrors.AppError
			require.True(t, errors.As(err, &appErr), "expected conflict before touching repositories: %v", err)
			assert.Equal(t, 409, appErr.HTTPCode)
		})
	}
	assert.NoFileExists(t, filepath.Join(baseDir, "10001/k-1/report.pdf"))
}

func TestWorkspaceTransferDiscardsOldTenantTasks(t *testing.T) {
	svc, db, _, kb := newWorkspaceTransferIntegrationFixture(t)
	ctx := transferTestCtx("creator", types.TenantRoleContributor, 10000)
	_, err := svc.TransferKnowledgeBaseWorkspace(ctx, kb.ID, "new")
	require.NoError(t, err)
	knowledge := &knowledgeService{}
	profile := &KnowledgeBaseProfileService{}
	wiki := &wikiIngestService{}
	require.NoError(t, ConfigureKBMutationCoordination(svc.mutations, db, knowledge, profile, wiki))
	payload, err := json.Marshal(map[string]any{"tenant_id": 10000, "knowledge_base_id": kb.ID, "knowledge_id": "k-1"})
	require.NoError(t, err)
	// Each handler has nil dependencies: stale tasks must exit before accessing
	// a model, file, index or repository through those dependencies.
	require.NoError(t, knowledge.ProcessDocument(ctx, asynq.NewTask(types.TypeDocumentProcess, payload)))
	require.NoError(t, knowledge.ProcessSummaryGeneration(ctx, asynq.NewTask(types.TypeSummaryGeneration, payload)))
	require.NoError(t, profile.Handle(ctx, asynq.NewTask(types.TypeKnowledgeBaseProfile, payload)))
	require.NoError(t, wiki.Handle(ctx, asynq.NewTask(types.TypeWikiIngest, payload)))
	var row types.Knowledge
	require.NoError(t, db.First(&row, "id = ?", "k-1").Error)
	assert.Equal(t, uint64(10001), row.TenantID)
	assert.Equal(t, types.ParseStatusCompleted, row.ParseStatus)
}

func TestWorkspaceTransferRequiresCoordinator(t *testing.T) {
	svc, _, baseDir, kb := newWorkspaceTransferIntegrationFixture(t)
	svc.mutations = nil
	ctx := transferTestCtx("creator", types.TenantRoleContributor, 10000)
	plan, err := svc.BuildKnowledgeBaseWorkspaceTransferPlan(ctx, kb.ID, "new")
	require.NoError(t, err)
	require.Len(t, plan.Blockers, 1)
	assert.Equal(t, "write_barrier_unavailable", plan.Blockers[0].Code)
	_, err = svc.TransferKnowledgeBaseWorkspace(ctx, kb.ID, "new")
	require.ErrorIs(t, err, ErrKBWorkspaceTransferUnsupported)
	assert.FileExists(t, filepath.Join(baseDir, "10000/k-1/report.pdf"))
	assert.NoFileExists(t, filepath.Join(baseDir, "10001/k-1/report.pdf"))
}

func TestWorkspaceTransferParksWorkerUntilCommitAndCleanup(t *testing.T) {
	svc, db, _, kb := newWorkspaceTransferIntegrationFixture(t)
	knowledge := &knowledgeService{}
	require.NoError(t, ConfigureKBMutationCoordination(svc.mutations, db, knowledge))
	ctx := transferTestCtx("creator", types.TenantRoleContributor, 10000)
	payload := []byte(`{"tenant_id":10000,"knowledge_base_id":"kb-1","knowledge_id":"k-1"}`)
	workerDone := make(chan error, 1)
	workerCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := svc.withWorkspaceTransferLock(kb.ID, func() error {
		go func() {
			workerDone <- knowledge.ProcessDocument(workerCtx, asynq.NewTask(types.TypeDocumentProcess, payload))
		}()
		select {
		case err := <-workerDone:
			t.Fatalf("worker entered before transfer released its lease: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		_, err := svc.transferKnowledgeBaseWorkspaceLocked(ctx, kb.ID, "new")
		return err
	})
	require.NoError(t, err)
	select {
	case err := <-workerDone:
		// The old task is acknowledged without touching its nil dependencies.
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not resume after transfer")
	}
}

type workspaceTransferQueueProbe struct {
	interfaces.TaskInspector
	queued bool
	err    error
}

func (p *workspaceTransferQueueProbe) HasQueuedTasksForKnowledgeBase(context.Context, string, []string, []string) (bool, error) {
	return p.queued, p.err
}

func TestWorkspaceTransferRejectsQueuedWorkAndInspectionFailure(t *testing.T) {
	for _, failProbe := range []bool{false, true} {
		t.Run(fmt.Sprint(failProbe), func(t *testing.T) {
			svc, db, baseDir, kb := newWorkspaceTransferIntegrationFixture(t)
			probe := &workspaceTransferQueueProbe{queued: true}
			if failProbe {
				probe.err = errors.New("queue unavailable")
			}
			svc.taskInspector = probe
			_, err := svc.TransferKnowledgeBaseWorkspace(transferTestCtx("creator", types.TenantRoleContributor, 10000), kb.ID, "new")
			require.Error(t, err)
			if !failProbe {
				var blocked *KBWorkspaceTransferBlockedError
				require.ErrorAs(t, err, &blocked)
				require.Len(t, blocked.Blockers, 1)
				assert.Equal(t, "kb_processing", blocked.Blockers[0].Code)
			}
			var row types.KnowledgeBase
			require.NoError(t, db.First(&row, "id = ?", kb.ID).Error)
			assert.Equal(t, uint64(10000), row.TenantID)
			assert.NoFileExists(t, filepath.Join(baseDir, "10001/k-1/report.pdf"))
		})
	}
}

func TestWorkspaceTransferRejectsStalePageSnapshotAndClosesOldSyncLog(t *testing.T) {
	svc, db, _, kb := newWorkspaceTransferIntegrationFixture(t)
	ctx := transferTestCtx("creator", types.TenantRoleContributor, 10000)
	_, err := svc.TransferKnowledgeBaseWorkspace(ctx, kb.ID, "new")
	require.NoError(t, err)
	pages := &wikiPageService{}
	sources := &DataSourceService{}
	require.NoError(t, ConfigureKBMutationCoordination(svc.mutations, db, pages, sources))
	_, err = pages.UpdatePage(ctx, &types.WikiPage{TenantID: 10000, KnowledgeBaseID: kb.ID, Slug: "stale", Content: "old"})
	var appErr *apperrors.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 409, appErr.HTTPCode)
	require.NoError(t, db.Create(&types.DataSource{ID: "ds-1", TenantID: 10001, KnowledgeBaseID: kb.ID, Name: "source"}).Error)
	require.NoError(t, db.Create(&types.SyncLog{ID: "sl-1", TenantID: 10001, DataSourceID: "ds-1", Status: types.SyncLogStatusRunning}).Error)
	require.NoError(t, sources.ProcessSync(ctx, asynq.NewTask(types.TypeDataSourceSync, []byte(`{"tenant_id":10000,"data_source_id":"ds-1","sync_log_id":"sl-1"}`))))
	var log types.SyncLog
	require.NoError(t, db.First(&log, "id = ?", "sl-1").Error)
	assert.Equal(t, types.SyncLogStatusCanceled, log.Status)
	assert.NotNil(t, log.FinishedAt)
}

func TestWorkspaceTransferFailurePreservesSourceAndReleasesGate(t *testing.T) {
	svc, db, baseDir, kb := newWorkspaceTransferIntegrationFixture(t)
	// Simulate a change which invalidated the loaded KB snapshot. The copy
	// succeeds, then the compare-and-swap must fail and clean only new copies.
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id = ?", kb.ID).Update("creator_id", "changed").Error)
	_, err := svc.TransferKnowledgeBaseWorkspace(transferTestCtx("creator", types.TenantRoleContributor, 10000), kb.ID, "new")
	require.ErrorIs(t, err, ErrKBOwnerTransferConflict)
	assert.FileExists(t, filepath.Join(baseDir, "10000/k-1/report.pdf"))
	assert.NoFileExists(t, filepath.Join(baseDir, "10001/k-1/report.pdf"))
	_, release, err := svc.mutations.Begin(context.Background())
	require.NoError(t, err, "failed transfer must not leave the write barrier locked")
	release()
	var row types.Knowledge
	require.NoError(t, db.First(&row, "id = ?", "k-1").Error)
	assert.Equal(t, uint64(10000), row.TenantID)
}
