package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *knowledgeRepository) ListKnowledgeComments(
	ctx context.Context,
	id string,
	offset int,
	status string,
) (*types.KnowledgeCommentPage, error) {
	page := &types.KnowledgeCommentPage{Items: []*types.KnowledgeComment{}}
	q := r.db.WithContext(ctx).Where("knowledge_id = ? AND parent_id = ?", id, "")
	if status != "all" {
		q = q.Where("resolved = ?", status == "resolved")
	}
	if err := q.Order("created_at DESC, id DESC").Offset(offset).Limit(21).Find(&page.Items).Error; err != nil {
		return nil, err
	}
	page.HasMore = len(page.Items) > 20
	if page.HasMore {
		page.Items = page.Items[:20]
	}
	page.NextOffset = offset + len(page.Items)
	ids := []string{}
	byID := map[string]*types.KnowledgeComment{}
	for _, item := range page.Items {
		ids = append(ids, item.ID)
		item.Replies = []*types.KnowledgeComment{}
		byID[item.ID] = item
	}
	if len(ids) != 0 {
		var replies []*types.KnowledgeComment
		if err := r.db.WithContext(ctx).
			Where("knowledge_id = ? AND parent_id IN ?", id, ids).
			Order("created_at ASC, id ASC").
			Find(&replies).Error; err != nil {
			return nil, err
		}
		for _, reply := range replies {
			byID[reply.ParentID].Replies = append(byID[reply.ParentID].Replies, reply)
		}
	}
	return page, nil
}

// Serialize comment mutations on the document row, also checking the exact
// binding validated by the service. Moving/deleting documents reject writes.
func commentDocumentLock(tx *gorm.DB, k *types.Knowledge) (*types.Knowledge, error) {
	var current types.Knowledge
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", k.ID, k.TenantID, k.KnowledgeBaseID).
		First(&current).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("document not found")
	}
	if err != nil {
		return nil, err
	}
	if current.ParseStatus == types.ParseStatusDeleting {
		return nil, apperrors.NewConflictError("文档正在移动或删除，请刷新后重试")
	}
	if err := access.RejectMovingKnowledge(&current); err != nil {
		return nil, err
	}
	return &current, nil
}

func (r *knowledgeRepository) CreateKnowledgeComment(
	ctx context.Context,
	k *types.Knowledge,
	row *types.KnowledgeComment,
) (*types.KnowledgeComment, error) {
	var result *types.KnowledgeComment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := commentDocumentLock(tx, k)
		if err != nil {
			return err
		}
		var prior types.KnowledgeComment
		err = tx.Where("id = ?", row.ID).First(&prior).Error
		if err == nil {
			if prior.KnowledgeID != k.ID || prior.AuthorID != row.AuthorID || prior.ParentID != row.ParentID ||
				prior.Body != row.Body ||
				prior.Quote != row.Quote ||
				prior.Prefix != row.Prefix ||
				prior.Suffix != row.Suffix ||
				prior.ContentVersion != row.ContentVersion {
				return apperrors.NewConflictError("评论请求已使用，请刷新后重试")
			}
			result = &prior
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if row.ParentID != "" {
			var parent types.KnowledgeComment
			if err := tx.Where("id = ? AND knowledge_id = ? AND parent_id = ?", row.ParentID, k.ID, "").
				First(&parent).Error; err != nil {
				return apperrors.NewNotFoundError("讨论不存在")
			}
			if parent.Deleted || parent.Resolved {
				return apperrors.NewConflictError("讨论已删除或解决，请刷新后重试")
			}
			var count int64
			if err := tx.Model(&types.KnowledgeComment{}).
				Where("knowledge_id = ? AND parent_id = ?", k.ID, parent.ID).
				Count(&count).Error; err != nil {
				return err
			}
			if count >= 100 {
				return apperrors.NewBadRequestError("单条讨论最多支持 100 条回复，请新建讨论")
			}
		} else if row.Quote != "" {
			version := 0
			if meta, err := current.ManualMetadata(); err == nil && meta != nil && meta.Content != "" {
				version = meta.Version
				if version == 0 && meta.Content != "" {
					version = 1
				}
			}
			if version != row.ContentVersion {
				return apperrors.NewConflictError("引用正文已变化，请刷新并重新选择")
			}
		}
		row.CreatedAt = time.Now()
		row.Revision = 1
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		result = row
		return nil
	})
	return result, err
}

func (r *knowledgeRepository) MutateKnowledgeComment(
	ctx context.Context,
	k *types.Knowledge,
	id, user string,
	m *types.KnowledgeCommentMutation,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := commentDocumentLock(tx, k); err != nil {
			return err
		}
		var row types.KnowledgeComment
		if err := tx.Where("knowledge_id = ? AND id = ?", k.ID, id).First(&row).Error; err != nil {
			return apperrors.NewNotFoundError("评论不存在")
		}
		if m.BaseRevision != row.Revision {
			return apperrors.NewConflictError("评论已变化，请刷新后重试")
		}
		update := map[string]any{"revision": row.Revision + 1}
		switch m.Action {
		case "delete":
			if row.AuthorID != user {
				return apperrors.NewForbiddenError("只能删除自己的评论")
			}
			update["deleted"] = true
			update["body"] = ""
			update["quote"] = ""
			update["prefix"] = ""
			update["suffix"] = ""
		case "resolve":
			if row.ParentID != "" || row.Deleted {
				return apperrors.NewBadRequestError("只能修改有效讨论的解决状态")
			}
			update["resolved"] = m.Resolved
		default:
			return apperrors.NewBadRequestError("评论操作不合法")
		}
		return tx.Model(&types.KnowledgeComment{}).Where("id = ? AND knowledge_id = ?", id, k.ID).Updates(update).Error
	})
}
