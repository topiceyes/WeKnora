package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeCommentsLifecycleIsolationAndRetry(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	r := NewKnowledgeRepository(db).(*knowledgeRepository)
	ctx := context.Background()
	id := insertKnowledgeInFolder(t, db, 1, "kb", "", "Doc")
	k, err := r.GetKnowledgeByID(ctx, 1, id)
	require.NoError(t, err)
	require.NoError(t, db.Model(k).UpdateColumn("metadata", `{"content":"Body","version":2}`).Error)
	root := &types.KnowledgeComment{
		ID:             uuid.NewString(),
		KnowledgeID:    id,
		AuthorID:       "me",
		AuthorName:     "Me",
		Body:           "Discuss",
		Quote:          "Body",
		ContentVersion: 2,
	}
	_, err = r.CreateKnowledgeComment(ctx, k, root)
	require.NoError(t, err)
	_, err = r.CreateKnowledgeComment(ctx, k, root)
	require.NoError(t, err, "retry does not duplicate")
	changed := *root
	changed.Body = "Changed"
	_, err = r.CreateKnowledgeComment(ctx, k, &changed)
	require.Error(t, err)
	foreignID := insertKnowledgeInFolder(t, db, 2, "other", "", "Other")
	foreign, err := r.GetKnowledgeByID(ctx, 2, foreignID)
	require.NoError(t, err)
	_, err = r.CreateKnowledgeComment(
		ctx,
		foreign,
		&types.KnowledgeComment{
			ID:          uuid.NewString(),
			KnowledgeID: foreignID,
			ParentID:    root.ID,
			AuthorID:    "me",
			Body:        "Cross-document",
		},
	)
	require.Error(t, err)
	reply := &types.KnowledgeComment{
		ID:          uuid.NewString(),
		KnowledgeID: id,
		ParentID:    root.ID,
		AuthorID:    "other",
		Body:        "Reply",
	}
	_, err = r.CreateKnowledgeComment(ctx, k, reply)
	require.NoError(t, err)
	page, err := r.ListKnowledgeComments(ctx, id, 0, "all")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Len(t, page.Items[0].Replies, 1)
	require.NoError(
		t,
		r.MutateKnowledgeComment(
			ctx,
			k,
			root.ID,
			"me",
			&types.KnowledgeCommentMutation{Action: "resolve", BaseRevision: 1, Resolved: true},
		),
	)
	require.Error(
		t,
		r.MutateKnowledgeComment(
			ctx,
			k,
			root.ID,
			"me",
			&types.KnowledgeCommentMutation{Action: "resolve", BaseRevision: 1, Resolved: false},
		),
		"stale revision cannot reopen",
	)
	_, err = r.CreateKnowledgeComment(
		ctx,
		k,
		&types.KnowledgeComment{
			ID:          uuid.NewString(),
			KnowledgeID: id,
			ParentID:    root.ID,
			AuthorID:    "me",
			Body:        "Late reply",
		},
	)
	require.Error(t, err)
	page, err = r.ListKnowledgeComments(ctx, id, 0, "open")
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Error(
		t,
		r.MutateKnowledgeComment(
			ctx,
			k,
			reply.ID,
			"me",
			&types.KnowledgeCommentMutation{Action: "delete", BaseRevision: 1},
		),
		"only author can delete",
	)
	require.Error(
		t,
		r.MutateKnowledgeComment(
			ctx,
			k,
			reply.ID,
			"other",
			&types.KnowledgeCommentMutation{Action: "resolve", BaseRevision: 1},
		),
		"reply is not a thread",
	)
	require.Error(
		t,
		r.MutateKnowledgeComment(
			ctx,
			foreign,
			root.ID,
			"me",
			&types.KnowledgeCommentMutation{Action: "delete", BaseRevision: 2},
		),
	)
	require.NoError(
		t,
		r.MutateKnowledgeComment(
			ctx,
			k,
			root.ID,
			"me",
			&types.KnowledgeCommentMutation{Action: "delete", BaseRevision: 2},
		),
	)
	page, err = r.ListKnowledgeComments(ctx, id, 0, "all")
	require.NoError(t, err)
	require.True(t, page.Items[0].Deleted)
	require.Empty(t, page.Items[0].Body)
	require.Empty(t, page.Items[0].Quote)
	require.Len(t, page.Items[0].Replies, 1)
	// Comments follow the stable document ID when its owning KB/workspace changes.
	require.NoError(
		t,
		db.Model(&types.Knowledge{}).
			Where("id = ?", id).
			Updates(map[string]any{"knowledge_base_id": "moved", "tenant_id": 3}).
			Error,
	)
	_, err = r.CreateKnowledgeComment(
		ctx,
		k,
		&types.KnowledgeComment{ID: uuid.NewString(), KnowledgeID: id, AuthorID: "me", Body: "Old binding"},
	)
	require.Error(t, err)
	page, err = r.ListKnowledgeComments(ctx, id, 0, "all")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NoError(t, r.DeleteKnowledgeList(ctx, 2, []string{id}))
	page, err = r.ListKnowledgeComments(ctx, id, 0, "all")
	require.NoError(t, err)
	require.Len(t, page.Items, 1, "wrong tenant cannot purge comments")
	require.NoError(t, r.DeleteKnowledge(ctx, 3, id))
	page, err = r.ListKnowledgeComments(ctx, id, 0, "all")
	require.NoError(t, err)
	require.Empty(t, page.Items)
}
func TestKnowledgeCommentsPaginationQuoteVersionsAndReplyLimit(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	r := NewKnowledgeRepository(db).(*knowledgeRepository)
	ctx := context.Background()
	id := insertKnowledgeInFolder(t, db, 1, "kb", "", "Doc")
	k, err := r.GetKnowledgeByID(ctx, 1, id)
	require.NoError(t, err)
	require.NoError(t, db.Model(k).UpdateColumn("metadata", `{"content":"Body","version":3}`).Error)
	_, err = r.CreateKnowledgeComment(
		ctx,
		k,
		&types.KnowledgeComment{
			ID:             uuid.NewString(),
			KnowledgeID:    id,
			AuthorID:       "me",
			Body:           "stale",
			Quote:          "Body",
			ContentVersion: 2,
		},
	)
	require.Error(t, err)
	rows := []types.KnowledgeComment{}
	for i := 0; i < 23; i++ {
		rows = append(
			rows,
			types.KnowledgeComment{
				ID:          uuid.NewString(),
				KnowledgeID: id,
				AuthorID:    "me",
				Body:        fmt.Sprint(i),
				Resolved:    i%2 == 0,
			},
		)
	}
	require.NoError(t, db.Create(&rows).Error)
	p, err := r.ListKnowledgeComments(ctx, id, 0, "all")
	require.NoError(t, err)
	require.Len(t, p.Items, 20)
	require.True(t, p.HasMore)
	last, err := r.ListKnowledgeComments(ctx, id, p.NextOffset, "all")
	require.NoError(t, err)
	require.Len(t, last.Items, 3)
	require.False(t, last.HasMore)
	for _, item := range last.Items {
		for _, first := range p.Items {
			require.NotEqual(t, first.ID, item.ID)
		}
	}
	replies := []types.KnowledgeComment{}
	for i := 0; i < 100; i++ {
		replies = append(
			replies,
			types.KnowledgeComment{
				ID:          uuid.NewString(),
				KnowledgeID: id,
				ParentID:    rows[1].ID,
				AuthorID:    "me",
				Body:        "reply",
			},
		)
	}
	require.NoError(t, db.Create(&replies).Error)
	_, err = r.CreateKnowledgeComment(
		ctx,
		k,
		&types.KnowledgeComment{
			ID:          uuid.NewString(),
			KnowledgeID: id,
			ParentID:    rows[1].ID,
			AuthorID:    "me",
			Body:        "overflow",
		},
	)
	require.Error(t, err)
	require.NoError(t, db.Model(k).UpdateColumn("parse_status", types.ParseStatusDeleting).Error)
	_, err = r.CreateKnowledgeComment(
		ctx,
		k,
		&types.KnowledgeComment{ID: uuid.NewString(), KnowledgeID: id, AuthorID: "me", Body: "deleting"},
	)
	require.Error(t, err)
}
func TestKnowledgeBaseDeleteCleansDocumentComments(t *testing.T) {
	db := setupKBTestDB(t)
	kb := makeKB(nil)
	require.NoError(t, db.Create(kb).Error)
	require.NoError(
		t,
		db.Exec(
			"INSERT INTO knowledges(id, tenant_id, knowledge_base_id) VALUES (?, ?, ?)",
			"doc",
			kb.TenantID,
			kb.ID,
		).Error,
	)
	require.NoError(
		t,
		db.Create(
			&types.KnowledgeComment{ID: uuid.NewString(), KnowledgeID: "doc", AuthorID: "me", Body: "discussion"},
		).Error,
	)
	require.NoError(t, NewKnowledgeBaseRepository(db).DeleteKnowledgeBase(context.Background(), kb.ID))
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeComment{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestImportedDocumentQuoteUsesUneditedVersionZero(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	r := NewKnowledgeRepository(db).(*knowledgeRepository)
	id := insertKnowledgeInFolder(t, db, 1, "kb", "", "Imported.pdf")
	// Parser-only metadata normalizes ManualMetadata.Version to 1 even though
	// there has never been an online content save. The reader uses version 0.
	require.NoError(
		t,
		db.Model(&types.Knowledge{}).
			Where("id = ?", id).
			UpdateColumn("metadata", `{"process_overrides":{"chunk_size":512}}`).
			Error,
	)
	k, err := r.GetKnowledgeByID(context.Background(), 1, id)
	require.NoError(t, err)
	_, err = r.CreateKnowledgeComment(
		context.Background(),
		k,
		&types.KnowledgeComment{
			ID:             uuid.NewString(),
			KnowledgeID:    id,
			AuthorID:       "me",
			Body:           "Discuss imported text",
			Quote:          "Parsed passage",
			ContentVersion: 0,
		},
	)
	require.NoError(t, err)
}
