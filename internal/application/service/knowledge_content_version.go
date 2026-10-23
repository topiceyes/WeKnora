package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// knowledge_content_version.go implements the browse-mode version history (P3).
// Every content save (UpdateKnowledgeContent / UpdateManualKnowledge) stores a
// full snapshot keyed (knowledge_id, version) in the same transaction as the
// knowledge row update, so the history can never silently miss a version. The
// current state always equals the highest version. Restoring an old version is
// a normal save of that content — the pre-restore state is snapshotted too.

// buildContentVersionSnapshot assembles the snapshot row for the state being
// persisted. CreatedBy is empty for synthetic (API-key) identities.
func buildContentVersionSnapshot(ctx context.Context,
	k *types.Knowledge, version int, content string,
) *types.KnowledgeContentVersion {
	createdBy := types.CallerFromContext(ctx).UserID
	if types.IsSyntheticUserID(createdBy) {
		createdBy = ""
	}
	return &types.KnowledgeContentVersion{
		ID:              uuid.NewString(),
		TenantID:        k.TenantID,
		KnowledgeBaseID: k.KnowledgeBaseID,
		KnowledgeID:     k.ID,
		Version:         version,
		Title:           k.Title,
		Content:         content,
		CreatedBy:       createdBy,
		CreatedAt:       time.Now(),
	}
}

// saveWithContentVersion persists the knowledge row together with its content
// snapshot atomically, then prunes beyond the retention cap (best-effort).
func (s *knowledgeService) saveWithContentVersion(ctx context.Context,
	k *types.Knowledge, version int, content string,
) error {
	// A nil repo only occurs in hand-built test fixtures — the dig container
	// fails at startup when the provider is missing, so production saves
	// always snapshot.
	if s.contentVersionRepo == nil {
		return s.repo.UpdateKnowledge(ctx, k)
	}
	snapshot := buildContentVersionSnapshot(ctx, k, version, content)
	err := s.contentVersionRepo.CreateWithKnowledgeUpdate(ctx, k, snapshot)
	if err != nil {
		if errors.Is(err, repository.ErrKnowledgeContentVersionConflict) {
			return werrors.NewBadRequestError("保存冲突，请刷新后重试")
		}
		return err
	}
	if perr := s.contentVersionRepo.Prune(ctx, k.ID, types.KnowledgeMaxContentVersions); perr != nil {
		logger.Warnf(ctx, "Failed to prune content versions for %s: %v", k.ID, perr)
	}
	return nil
}

// loadKnowledgeForContentVersions loads the document for the read-only version
// endpoints. The handler middleware has already validated KB read access; the
// tenant-scoped load is defense in depth and falls back to the unscoped getter
// when no execution tenant is present (test harnesses).
func (s *knowledgeService) loadKnowledgeForContentVersions(ctx context.Context,
	knowledgeID string,
) (*types.Knowledge, error) {
	var (
		knowledge *types.Knowledge
		err       error
	)
	if tenantID, ok := types.TenantIDFromContext(ctx); ok {
		knowledge, err = s.repo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
	} else {
		knowledge, err = s.repo.GetKnowledgeByIDOnly(ctx, knowledgeID)
	}
	if err != nil {
		return nil, err
	}
	if knowledge == nil || knowledge.ID != knowledgeID {
		return nil, werrors.NewNotFoundError("knowledge not found")
	}
	return knowledge, nil
}

// ListKnowledgeContentVersions returns the document's snapshots, newest first,
// without the content payload.
func (s *knowledgeService) ListKnowledgeContentVersions(ctx context.Context,
	knowledgeID string,
) ([]*types.KnowledgeContentVersion, error) {
	knowledge, err := s.loadKnowledgeForContentVersions(ctx, knowledgeID)
	if err != nil {
		return nil, err
	}
	return s.contentVersionRepo.ListByKnowledge(
		ctx, knowledge.KnowledgeBaseID, knowledgeID, types.KnowledgeMaxContentVersions)
}

// GetKnowledgeContentVersion returns one snapshot including its content.
func (s *knowledgeService) GetKnowledgeContentVersion(ctx context.Context,
	knowledgeID string, version int,
) (*types.KnowledgeContentVersion, error) {
	knowledge, err := s.loadKnowledgeForContentVersions(ctx, knowledgeID)
	if err != nil {
		return nil, err
	}
	row, err := s.contentVersionRepo.GetByVersion(ctx, knowledge.KnowledgeBaseID, knowledgeID, version)
	if err != nil {
		if errors.Is(err, repository.ErrKnowledgeContentVersionNotFound) {
			return nil, werrors.NewNotFoundError("knowledge content version not found")
		}
		return nil, err
	}
	return row, nil
}

// RestoreKnowledgeContentVersion saves the snapshot's title/content as the new
// current version by running the normal online-edit save path — inheriting FAQ
// and in-flight rejection, index_stale marking, FileName sync, resource
// rebinding and audit, and snapshotting the pre-restore state in the process.
func (s *knowledgeService) RestoreKnowledgeContentVersion(ctx context.Context,
	knowledgeID string, version int,
) (*types.Knowledge, error) {
	existing, _, err := loadKnowledgeWrite(ctx, s.repo, s.kbService, knowledgeID)
	if err != nil {
		return nil, err
	}
	if existing.Type == types.KnowledgeTypeFAQ {
		return nil, werrors.NewBadRequestError("FAQ 文档不支持版本恢复")
	}
	if meta, err := existing.ManualMetadata(); err == nil && meta != nil && meta.Version == version {
		return nil, werrors.NewBadRequestError("该版本已是当前版本")
	}
	rev, err := s.contentVersionRepo.GetByVersion(ctx, existing.KnowledgeBaseID, knowledgeID, version)
	if err != nil {
		if errors.Is(err, repository.ErrKnowledgeContentVersionNotFound) {
			return nil, werrors.NewNotFoundError("knowledge content version not found")
		}
		return nil, err
	}
	return s.UpdateKnowledgeContent(ctx, knowledgeID, &types.KnowledgeContentPayload{
		Title:   rev.Title,
		Content: rev.Content,
	})
}
