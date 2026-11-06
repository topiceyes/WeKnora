package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/kbmutation"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRewriteKBWorkspaceResourcePath(t *testing.T) {
	t.Run("scoped backend path is re-tenanted and re-wrapped", func(t *testing.T) {
		got, err := rewriteKBWorkspaceResourcePath(
			"storage://old-backend/local://10000/abc/file.pdf", 10000, 10001, "new-backend")
		require.NoError(t, err)
		assert.Equal(t, "storage://new-backend/local://10001/abc/file.pdf", got)
	})

	t.Run("legacy bare local path keeps its shape", func(t *testing.T) {
		got, err := rewriteKBWorkspaceResourcePath("local://10000/exports/a.png", 10000, 10001, "new-backend")
		require.NoError(t, err)
		assert.Equal(t, "local://10001/exports/a.png", got)
	})

	t.Run("path outside the source workspace is refused", func(t *testing.T) {
		_, err := rewriteKBWorkspaceResourcePath("local://10009/a.png", 10000, 10001, "new-backend")
		require.Error(t, err)
	})
}

func TestKBWorkspaceResourceRelPath(t *testing.T) {
	rel, err := kbWorkspaceResourceRelPath(
		"storage://old/local://10000/exports/a.png", 10000)
	require.NoError(t, err)
	assert.Equal(t, "10000/exports/a.png", rel)

	_, err = kbWorkspaceResourceRelPath("storage://old/s3://bucket/key", 10000)
	require.Error(t, err)

	_, err = kbWorkspaceResourceRelPath("local://10001/exports/a.png", 10000)
	require.Error(t, err)
}

func TestKBWorkspaceFileSetCopyAndCleanup(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", baseDir)

	writeFile := func(rel string) {
		path := filepath.Join(baseDir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("data-"+rel), 0o644))
	}
	writeFile("10000/knowledge-1/report.pdf")
	writeFile("10000/exports/image.png")

	resources := []kbWorkspaceTransferResourceRow{
		{ID: "r1", PhysicalPath: "storage://old/local://10000/knowledge-1/report.pdf"},
		{ID: "r2", PhysicalPath: "storage://old/local://10000/exports/image.png"},
		{ID: "r3", PhysicalPath: "storage://old/local://10000/exports/gone.png"},
	}
	set, err := buildKBWorkspaceFileSet(baseDir, 10000, resources, []string{"knowledge-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"10000/exports/image.png",
		"10000/knowledge-1/report.pdf",
	}, set.rels)
	assert.Equal(t, []string{"10000/exports/gone.png"}, set.missing)
	assert.Positive(t, set.totalBytes)

	created, err := copyKBWorkspaceTransferFiles(baseDir, set, 10000, 10001)
	require.NoError(t, err)
	assert.Len(t, created, 2)
	for _, rel := range created {
		info, statErr := os.Stat(filepath.Join(baseDir, filepath.FromSlash(rel)))
		require.NoError(t, statErr)
		assert.Positive(t, info.Size())
	}
	assert.FileExists(t, filepath.Join(baseDir, "10001", "knowledge-1", "report.pdf"))

	// A retry against the same destination is idempotent.
	createdAgain, err := copyKBWorkspaceTransferFiles(baseDir, set, 10000, 10001)
	require.NoError(t, err)
	assert.Empty(t, createdAgain)

	require.NoError(t, removeKBWorkspaceTransferSources(baseDir, set))
	assert.NoFileExists(t, filepath.Join(baseDir, "10000", "exports", "image.png"))
	assert.NoFileExists(t, filepath.Join(baseDir, "10000", "knowledge-1", "report.pdf"))
	assert.FileExists(t, filepath.Join(baseDir, "10001", "knowledge-1", "report.pdf"))

	removeKBWorkspaceTransferCopies(baseDir, created)
	assert.NoFileExists(t, filepath.Join(baseDir, "10001", "knowledge-1", "report.pdf"))
	assert.NoFileExists(t, filepath.Join(baseDir, "10001", "exports", "image.png"))
}

func TestKBWorkspaceCopyRejectsSameSizeDifferentContent(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	require.NoError(t, os.WriteFile(src, []byte("original"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("different"[:8]), 0o600))
	created, err := copyKBWorkspaceFileExclusive(src, dst)
	require.Error(t, err)
	assert.False(t, created)
	contents, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "differen", string(contents))
}

func TestKBWorkspaceTransferLockRejectsConcurrentInstance(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", baseDir)
	db := newKBWorkspaceTransferTestDB(t)
	first := &knowledgeBaseService{db: db}
	second := &knowledgeBaseService{db: db}
	require.NoError(t, ConfigureKBMutationCoordination(&kbmutation.Coordinator{BaseDir: baseDir}, db, first, second))
	err := first.withWorkspaceTransferLock("kb-1", func() error {
		return second.withWorkspaceTransferLock("kb-1", func() error {
			t.Fatal("second transfer acquired the lock")
			return nil
		})
	})
	require.ErrorIs(t, err, ErrKBWorkspaceTransferInProgress)
	require.NoError(t, second.withWorkspaceTransferLock("kb-1", func() error { return nil }))
}

func newKBWorkspaceTransferTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "weknora.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&types.KnowledgeBase{},
		&types.Knowledge{},
		&types.Chunk{},
		&types.KnowledgeTag{},
		&types.KnowledgeContentVersion{},
		&types.WikiPage{},
		&types.WikiFolder{},
		&types.WikiPageIssue{},
		&types.WikiPageRevision{},
		&types.DataSource{},
		&types.SyncLog{},
		&types.StoredResource{},
		&types.ResourceBinding{},
		&types.ResourceAccessGrant{},
		&types.Tenant{},
		&types.User{},
		&types.TaskPendingOp{},
		&types.UserResourceFavorite{},
		&types.MemoryDocAffinity{},
	))
	for _, ddl := range []string{
		`CREATE TABLE im_channels (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT 1, deleted_at DATETIME)`,
		`CREATE TABLE custom_agents (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
			config TEXT NOT NULL DEFAULT '{}', deleted_at DATETIME)`,
		`CREATE TABLE tenant_api_keys (
			id INTEGER PRIMARY KEY, tenant_id INTEGER, name TEXT NOT NULL,
			knowledge_base_ids TEXT NOT NULL DEFAULT '[]', revoked_at DATETIME, expires_at DATETIME)`,
		`CREATE TABLE mcp_endpoints (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
			knowledge_base_ids TEXT NOT NULL DEFAULT '[]', enabled BOOLEAN NOT NULL DEFAULT 1,
			deleted_at DATETIME)`,
		`CREATE TABLE models (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, is_builtin BOOLEAN NOT NULL DEFAULT 0,
			deleted_at DATETIME)`,
		`CREATE TABLE organization_tenant_members (
			id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, tenant_id INTEGER NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer')`,
		`CREATE TABLE chunk_images (
			chunk_id TEXT NOT NULL, image_index INTEGER NOT NULL, tenant_id INTEGER NOT NULL,
			knowledge_base_id TEXT NOT NULL, url TEXT NOT NULL DEFAULT '', original_url TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (chunk_id, image_index))`,
		`CREATE TABLE chunk_revisions (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
			knowledge_id TEXT NOT NULL, chunk_id TEXT NOT NULL, revision INTEGER NOT NULL,
			content TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE kb_shares (
			id TEXT PRIMARY KEY, knowledge_base_id TEXT NOT NULL, organization_id TEXT NOT NULL,
			shared_by_user_id TEXT NOT NULL, source_tenant_id INTEGER NOT NULL,
			permission TEXT NOT NULL, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE user_kb_pins (
			tenant_id INTEGER NOT NULL, user_id TEXT NOT NULL, kb_id TEXT NOT NULL,
			pinned_at DATETIME NOT NULL, PRIMARY KEY (tenant_id, user_id, kb_id))`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	return db
}

func TestMoveKnowledgeBaseWorkspaceTx(t *testing.T) {
	db := newKBWorkspaceTransferTestDB(t)
	ctx := context.Background()

	require.NoError(t, db.Create(&types.Tenant{ID: 10000, Name: "source", Status: "active", StorageUsed: 1000}).Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 10001, Name: "target", Status: "active", StorageUsed: 500}).Error)

	oldBackend := "old-backend"
	updatedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	kb := &types.KnowledgeBase{
		ID: "kb-1", Name: "KB", TenantID: 10000, CreatorID: "user-old", StorageBackendID: &oldBackend,
	}
	require.NoError(t, db.Create(kb).Error)
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id = ?", kb.ID).
		Update("updated_at", updatedAt).Error)
	require.NoError(t, db.First(kb, "id = ?", kb.ID).Error)

	knowledges := []*types.Knowledge{
		{ID: "k-live", TenantID: 10000, KnowledgeBaseID: kb.ID, StorageSize: 100, ParseStatus: types.ParseStatusCompleted},
		{ID: "k-deleted", TenantID: 10000, KnowledgeBaseID: kb.ID, StorageSize: 50, ParseStatus: types.ParseStatusCompleted},
	}
	for _, knowledge := range knowledges {
		require.NoError(t, db.Create(knowledge).Error)
	}
	require.NoError(t, db.Exec("UPDATE knowledges SET deleted_at = ? WHERE id = ?", time.Now(), "k-deleted").Error)

	require.NoError(t, db.Create(&types.Chunk{
		ID: "c1", TenantID: 10000, KnowledgeID: "k-live", KnowledgeBaseID: kb.ID, Content: "text",
	}).Error)
	require.NoError(t, db.Create(&types.Chunk{
		ID: "c2", TenantID: 10000, KnowledgeID: "k-deleted", KnowledgeBaseID: kb.ID, Content: "deleted",
	}).Error)
	require.NoError(t, db.Exec("UPDATE chunks SET deleted_at = ? WHERE id = ?", time.Now(), "c2").Error)
	require.NoError(t, db.Exec(`INSERT INTO chunk_images
		(chunk_id, image_index, tenant_id, knowledge_base_id, url) VALUES ('c1', 0, 10000, 'kb-1', 'local://10000/exports/a.png')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO chunk_revisions
		(id, tenant_id, knowledge_base_id, knowledge_id, chunk_id, revision) VALUES ('cr1', 10000, 'kb-1', 'k-live', 'c1', 1)`).Error)
	require.NoError(t, db.Create(&types.KnowledgeContentVersion{
		ID: "kcv1", TenantID: 10000, KnowledgeBaseID: kb.ID, KnowledgeID: "k-live", Version: 1,
	}).Error)
	require.NoError(t, db.Create(&types.KnowledgeTag{
		ID: "tag1", TenantID: 10000, KnowledgeBaseID: kb.ID, Name: "tag",
	}).Error)
	require.NoError(t, db.Create(&types.WikiFolder{
		ID: "wf1", TenantID: 10000, KnowledgeBaseID: kb.ID, Name: "folder",
	}).Error)
	require.NoError(t, db.Create(&types.WikiPage{
		ID: "wp1", TenantID: 10000, KnowledgeBaseID: kb.ID, Slug: "page", Title: "Page",
	}).Error)
	require.NoError(t, db.Create(&types.WikiPageRevision{
		ID: "wpr1", TenantID: 10000, KnowledgeBaseID: kb.ID, PageID: "wp1", Slug: "page", Version: 1,
	}).Error)
	require.NoError(t, db.Create(&types.WikiPageIssue{
		ID: "wpi1", TenantID: 10000, KnowledgeBaseID: kb.ID, Slug: "page", IssueType: "stale",
	}).Error)
	require.NoError(t, db.Create(&types.DataSource{
		ID: "ds1", TenantID: 10000, KnowledgeBaseID: kb.ID, Name: "ds", Status: types.DataSourceStatusActive,
	}).Error)
	require.NoError(t, db.Create(&types.SyncLog{
		ID: "sl1", DataSourceID: "ds1", TenantID: 10000, Status: types.SyncLogStatusSuccess,
	}).Error)

	resources := []*types.StoredResource{
		{
			ID: "res1", Handle: "handle1", TenantID: 10000, StorageBackendID: oldBackend, Provider: "local",
			PhysicalPath: "storage://old-backend/local://10000/k-live/file.pdf",
			LocationHash: resourceLocationHash("storage://old-backend/local://10000/k-live/file.pdf"),
		},
		{
			ID: "res2", Handle: "handle2", TenantID: 10000, StorageBackendID: oldBackend, Provider: "local",
			PhysicalPath: "local://10000/exports/a.png",
			LocationHash: resourceLocationHash("local://10000/exports/a.png"),
		},
	}
	for _, resource := range resources {
		require.NoError(t, db.Create(resource).Error)
		require.NoError(t, db.Create(&types.ResourceBinding{
			ID: "bind-" + resource.ID, ResourceID: resource.ID, TenantID: 10000,
			OwnerType: types.ResourceOwnerKnowledge, OwnerID: "k-live",
		}).Error)
	}
	require.NoError(t, db.Create(&types.ResourceAccessGrant{
		ID: "grant1", TokenHash: "hash1", ResourceID: "res1",
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error)
	require.NoError(t, db.Exec(`INSERT INTO kb_shares
		(id, knowledge_base_id, organization_id, shared_by_user_id, source_tenant_id, permission)
		VALUES ('share1', 'kb-1', 'org1', 'user-old', 10000, 'viewer')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_kb_pins (tenant_id, user_id, kb_id, pinned_at)
		VALUES (10000, 'user-old', 'kb-1', CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.Create(&types.UserResourceFavorite{
		UserID: "user-old", TenantID: 10000, ResourceType: types.ResourceTypeKB, ResourceID: kb.ID,
	}).Error)
	require.NoError(t, db.Create(&types.MemoryDocAffinity{
		ID: "aff1", TenantID: 10000, SubjectID: "user-old", KnowledgeID: "k-live", KnowledgeBaseID: kb.ID,
	}).Error)

	tc := &kbWorkspaceTransferContext{
		kb:           kb,
		targetTenant: &types.Tenant{ID: 10001, Name: "target", Status: "active", StorageUsed: 500},
		targetUser:   &types.User{ID: "user-new", TenantID: 10001, Username: "new"},
		targetBackend: &types.StorageBackend{
			ID: "new-backend", TenantID: 10001, Provider: "local", Status: types.StorageBackendStatusActive,
		},
		snapshot: &kbWorkspaceTransferSnapshot{
			storageBytes: 100,
			knowledgeIDs: []string{"k-live", "k-deleted"},
			resourceRows: []kbWorkspaceTransferResourceRow{
				{
					ID: "res1", TenantID: 10000, StorageBackendID: &oldBackend, Provider: "local",
					PhysicalPath: "storage://old-backend/local://10000/k-live/file.pdf",
				},
				{
					ID: "res2", TenantID: 10000, StorageBackendID: &oldBackend, Provider: "local",
					PhysicalPath: "local://10000/exports/a.png",
				},
			},
			sharesKept: 1,
		},
		plan: &types.KBWorkspaceTransferPlan{},
	}
	svc := &knowledgeBaseService{db: db}

	shares, err := svc.moveKnowledgeBaseWorkspaceTx(ctx, tc)
	require.NoError(t, err)
	assert.Equal(t, int64(1), shares)

	var movedKB types.KnowledgeBase
	require.NoError(t, db.Unscoped().First(&movedKB, "id = ?", kb.ID).Error)
	assert.Equal(t, uint64(10001), movedKB.TenantID)
	assert.Equal(t, "user-new", movedKB.CreatorID)
	assert.Equal(t, "new-backend", *movedKB.StorageBackendID)

	for _, table := range []string{
		"knowledges", "chunks", "chunk_revisions", "knowledge_content_versions",
		"knowledge_tags", "wiki_folders", "wiki_pages", "wiki_page_issues",
		"wiki_page_revisions", "data_sources", "chunk_images",
	} {
		var stale int64
		require.NoError(t, db.Table(table).Where("knowledge_base_id = ? AND tenant_id = ?", kb.ID, 10000).
			Count(&stale).Error, table)
		assert.Zero(t, stale, table)
	}
	var staleLogs int64
	require.NoError(t, db.Table("sync_logs").Where("tenant_id = ?", 10000).Count(&staleLogs).Error)
	assert.Zero(t, staleLogs)

	var res1 types.StoredResource
	require.NoError(t, db.First(&res1, "id = ?", "res1").Error)
	assert.Equal(t, uint64(10001), res1.TenantID)
	assert.Equal(t, "new-backend", res1.StorageBackendID)
	assert.Equal(t, "storage://new-backend/local://10001/k-live/file.pdf", res1.PhysicalPath)
	assert.Equal(t, resourceLocationHash(res1.PhysicalPath), res1.LocationHash)

	var res2 types.StoredResource
	require.NoError(t, db.First(&res2, "id = ?", "res2").Error)
	assert.Equal(t, "local://10001/exports/a.png", res2.PhysicalPath)

	var bindingTenants []uint64
	require.NoError(t, db.Table("resource_bindings").Pluck("tenant_id", &bindingTenants).Error)
	assert.Equal(t, []uint64{10001, 10001}, bindingTenants)

	var grants int64
	require.NoError(t, db.Table("resource_access_grants").Count(&grants).Error)
	assert.Zero(t, grants)

	var shareSource uint64
	require.NoError(t, db.Table("kb_shares").Select("source_tenant_id").
		Where("id = ?", "share1").Scan(&shareSource).Error)
	assert.Equal(t, uint64(10001), shareSource)

	for _, table := range []string{"user_kb_pins", "user_resource_favorites", "memory_doc_affinity"} {
		var count int64
		require.NoError(t, db.Table(table).Where("tenant_id = ?", 10000).Count(&count).Error, table)
		assert.Zero(t, count, table)
	}

	var sourceTenant, targetTenant types.Tenant
	require.NoError(t, db.First(&sourceTenant, 10000).Error)
	require.NoError(t, db.First(&targetTenant, 10001).Error)
	assert.Equal(t, int64(900), sourceTenant.StorageUsed)
	assert.Equal(t, int64(600), targetTenant.StorageUsed)

	// Replaying the same move with a stale row must fail the CAS and leave
	// every row untouched.
	_, err = svc.moveKnowledgeBaseWorkspaceTx(ctx, tc)
	require.ErrorIs(t, err, ErrKBOwnerTransferConflict)
}
