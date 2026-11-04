package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func insertKBWithCreator(t *testing.T, db *gorm.DB, tenantID uint64, creatorID *string) string {
	t.Helper()
	id := uuid.New().String()
	var creator interface{}
	if creatorID != nil {
		creator = *creatorID
	}
	require.NoError(t, db.Exec(
		`INSERT INTO knowledge_bases (id, name, tenant_id, embedding_model_id, summary_model_id, creator_id)
		 VALUES (?, 'transfer-kb', ?, 'embed', 'summary', ?)`,
		id, tenantID, creator).Error)
	return id
}

func TestUpdateKnowledgeBaseCreator_HappyPath(t *testing.T) {
	db := setupKBTestDB(t)
	repo := &knowledgeBaseRepository{db: db}
	ctx := context.Background()
	id := insertKBWithCreator(t, db, 1, strPtr("user-a"))
	before := reloadKB(t, db, id)

	matched, err := repo.UpdateKnowledgeBaseCreator(ctx, id, 1, "user-a", "user-b")
	require.NoError(t, err)
	assert.True(t, matched)

	got := reloadKB(t, db, id)
	assert.Equal(t, "user-b", got.CreatorID)
	assert.Equal(t, before.Name, got.Name)
	assert.Equal(t, before.VectorStoreID, got.VectorStoreID)
	assert.Equal(t, before.TenantID, got.TenantID)
	assert.True(t, !got.UpdatedAt.Before(before.UpdatedAt))
}

func TestUpdateKnowledgeBaseCreator_StaleExpectedCreator(t *testing.T) {
	db := setupKBTestDB(t)
	repo := &knowledgeBaseRepository{db: db}
	ctx := context.Background()
	id := insertKBWithCreator(t, db, 1, strPtr("user-a"))

	matched, err := repo.UpdateKnowledgeBaseCreator(ctx, id, 1, "user-z", "user-b")
	require.NoError(t, err)
	assert.False(t, matched, "expected creator changed concurrently: CAS must not match")
	assert.Equal(t, "user-a", reloadKB(t, db, id).CreatorID)
}

func TestUpdateKnowledgeBaseCreator_WrongTenant(t *testing.T) {
	db := setupKBTestDB(t)
	repo := &knowledgeBaseRepository{db: db}
	ctx := context.Background()
	id := insertKBWithCreator(t, db, 1, strPtr("user-a"))

	matched, err := repo.UpdateKnowledgeBaseCreator(ctx, id, 2, "user-a", "user-b")
	require.NoError(t, err)
	assert.False(t, matched, "a leaked KB id must never move ownership across workspaces")
	assert.Equal(t, "user-a", reloadKB(t, db, id).CreatorID)
}

func TestUpdateKnowledgeBaseCreator_NullLegacyCreator(t *testing.T) {
	db := setupKBTestDB(t)
	repo := &knowledgeBaseRepository{db: db}
	ctx := context.Background()
	// Legacy/API-key-created rows store NULL creator_id.
	id := insertKBWithCreator(t, db, 1, nil)

	matched, err := repo.UpdateKnowledgeBaseCreator(ctx, id, 1, "", "user-b")
	require.NoError(t, err)
	assert.True(t, matched, "COALESCE must cover NULL creators")
	assert.Equal(t, "user-b", reloadKB(t, db, id).CreatorID)
}

func TestUpdateKnowledgeBaseCreator_SoftDeletedRow(t *testing.T) {
	db := setupKBTestDB(t)
	repo := &knowledgeBaseRepository{db: db}
	ctx := context.Background()
	id := insertKBWithCreator(t, db, 1, strPtr("user-a"))
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET deleted_at = ? WHERE id = ?",
		time.Now(), id).Error)

	matched, err := repo.UpdateKnowledgeBaseCreator(ctx, id, 1, "user-a", "user-b")
	require.NoError(t, err)
	assert.False(t, matched, "soft-deleted rows are out of scope")
}

func TestUpdateKnowledgeBaseCreator_MissingKeys(t *testing.T) {
	db := setupKBTestDB(t)
	repo := &knowledgeBaseRepository{db: db}
	ctx := context.Background()

	_, err := repo.UpdateKnowledgeBaseCreator(ctx, "", 1, "a", "b")
	require.Error(t, err)
	_, err = repo.UpdateKnowledgeBaseCreator(ctx, "id", 0, "a", "b")
	require.Error(t, err)
	_, err = repo.UpdateKnowledgeBaseCreator(ctx, "id", 1, "a", "")
	require.Error(t, err)
}

func strPtr(s string) *string { return &s }
