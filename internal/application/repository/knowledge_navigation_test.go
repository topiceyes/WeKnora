package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeNavigationPersistenceAndAtomicity(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	r := NewKnowledgeRepository(db).(*knowledgeRepository)
	ctx := context.Background()
	a := insertKnowledgeInFolder(t, db, 1, "kb", "legacy/deep", "Alpha.md")
	b := insertKnowledgeInFolder(t, db, 1, "kb", "", "Beta.md")
	foreign := insertKnowledgeInFolder(t, db, 2, "other", "", "Foreign.md")
	revision := int64(0)
	mutate := func(m types.KnowledgeNavigationMutation) error {
		m.BaseRevision = &revision
		_, err := r.MutateKnowledgeNavigation(ctx, 1, "kb", &m)
		if err == nil {
			revision++
		}
		return err
	}
	require.NoError(t, mutate(types.KnowledgeNavigationMutation{Action: "create_folder", To: "empty/child"}))
	counts, err := r.ListKnowledgeFolderCounts(ctx, 1, "kb")
	require.NoError(t, err)
	tree := types.BuildKnowledgeFolderTree(counts)
	require.Equal(t, int64(2), tree.TotalDocumentCount)
	require.Len(t, tree.Folders, 2)
	nav, err := r.GetKnowledgeNavigation(ctx, 1, "kb")
	require.NoError(t, err)
	require.Contains(t, nav.Folders, "empty/child")
	require.Contains(t, nav.Folders, "legacy/deep")
	require.Error(
		t,
		mutate(types.KnowledgeNavigationMutation{Action: "move_documents", IDs: []string{b, foreign}, To: "empty"}),
	)
	require.Equal(t, "", folderPathOf(t, db, b), "failed mixed-KB batch changes nothing")
	stale := int64(0)
	_, err = r.MutateKnowledgeNavigation(
		ctx,
		1,
		"kb",
		&types.KnowledgeNavigationMutation{Action: "delete_folder", From: "empty", BaseRevision: &stale},
	)
	require.Error(t, err)
	require.NoError(
		t,
		mutate(types.KnowledgeNavigationMutation{Action: "move_documents", IDs: []string{a, b}, To: "empty/child"}),
	)
	require.Error(t, mutate(types.KnowledgeNavigationMutation{Action: "delete_folder", From: "empty"}))
	require.Error(
		t,
		mutate(types.KnowledgeNavigationMutation{Action: "move_folder", From: "empty", To: "empty/child/nested"}),
	)
	require.NoError(
		t,
		mutate(
			types.KnowledgeNavigationMutation{
				Action: "reorder",
				From:   "empty/child",
				Items:  []string{"d:" + b, "d:" + a},
			},
		),
	)
	require.Error(
		t,
		mutate(
			types.KnowledgeNavigationMutation{
				Action: "reorder",
				From:   "empty/child",
				Items:  []string{"d:" + a, "d:" + a},
			},
		),
	)
	require.NoError(t, mutate(types.KnowledgeNavigationMutation{Action: "move_folder", From: "empty", To: "renamed"}))
	nav, err = r.GetKnowledgeNavigation(ctx, 1, "kb")
	require.NoError(t, err)
	require.Equal(t, []string{"d:" + b, "d:" + a}, nav.Orders["renamed/child"])
	require.Equal(t, "renamed/child", folderPathOf(t, db, a))
	require.NotContains(t, nav.Folders, "empty")
	require.NoError(t, mutate(types.KnowledgeNavigationMutation{Action: "move_documents", IDs: []string{a, b}, To: ""}))
	require.NoError(t, mutate(types.KnowledgeNavigationMutation{Action: "delete_folder", From: "renamed"}))
	nav, err = r.GetKnowledgeNavigation(ctx, 1, "kb")
	require.NoError(t, err)
	require.NotContains(t, nav.Folders, "renamed/child")
	require.Error(t, mutate(types.KnowledgeNavigationMutation{Action: "create_folder", To: "../bad"}))
	require.Error(t, mutate(types.KnowledgeNavigationMutation{Action: "create_folder", To: "a//b"}))
}

func TestLegacyFolderOperationsPreserveEmptyFoldersAndManualOrder(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	r := NewKnowledgeRepository(db).(*knowledgeRepository)
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	a := "stale-save"
	require.NoError(
		t,
		db.Create(
			&types.Knowledge{
				ID:              a,
				TenantID:        1,
				KnowledgeBaseID: "kb",
				Type:            "file",
				Title:           "A.md",
				FolderPath:      "source",
				ParseStatus:     types.ParseStatusCompleted,
				CustomMetadata:  types.JSON(`{}`),
			},
		).Error,
	)
	_, err := r.UpdateKnowledgeFolderPath(ctx, 1, "kb", []string{a}, "target")
	require.NoError(t, err)
	nav, err := r.GetKnowledgeNavigation(ctx, 1, "kb")
	require.NoError(t, err)
	require.Contains(t, nav.Folders, "source")
	_, err = r.RenameKnowledgeFolderPath(ctx, 1, "kb", "source", "emptyRenamed")
	require.NoError(t, err)
	nav, err = r.GetKnowledgeNavigation(ctx, 1, "kb")
	require.NoError(t, err)
	require.Contains(t, nav.Folders, "emptyRenamed")
	// Content processing may save a stale struct after a navigation operation.
	var stale types.Knowledge
	require.NoError(t, db.First(&stale, "id = ?", a).Error)
	_, err = r.UpdateKnowledgeFolderPath(ctx, 1, "kb", []string{a}, "moved")
	require.NoError(t, err)
	stale.Title = "Updated title"
	require.NoError(t, r.UpdateKnowledge(ctx, &stale))
	require.Equal(t, "moved", folderPathOf(t, db, a))
}

func TestKnowledgeBaseDeleteCleansNavigation(t *testing.T) {
	db := setupKBTestDB(t)
	kb := makeKB(nil)
	require.NoError(t, db.Create(kb).Error)
	nav := types.KnowledgeNavigation{
		TenantID:        kb.TenantID,
		KnowledgeBaseID: kb.ID,
		Folders:         []string{"Empty"},
		Orders:          map[string][]string{},
	}
	require.NoError(t, db.Create(&nav).Error)
	require.NoError(t, NewKnowledgeBaseRepository(db).DeleteKnowledgeBase(context.Background(), kb.ID))
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeNavigation{}).Where("knowledge_base_id = ?", kb.ID).Count(&count).Error)
	require.Zero(t, count)
}
