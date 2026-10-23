package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrKnowledgeContentVersionNotFound is returned when a requested snapshot does
// not exist (or is scoped to a different knowledge base).
var ErrKnowledgeContentVersionNotFound = errors.New("knowledge content version not found")

// ErrKnowledgeContentVersionConflict is returned when a snapshot with the same
// (knowledge_id, version) already exists — two concurrent saves computed the
// same next version. The save is rolled back; the caller should reload and
// retry.
var ErrKnowledgeContentVersionConflict = errors.New("knowledge content version conflict")

// knowledgeContentVersionRepository implements
// interfaces.KnowledgeContentVersionRepository.
type knowledgeContentVersionRepository struct {
	db *gorm.DB
}

// NewKnowledgeContentVersionRepository creates a KnowledgeContentVersionRepository.
func NewKnowledgeContentVersionRepository(db *gorm.DB) interfaces.KnowledgeContentVersionRepository {
	return &knowledgeContentVersionRepository{db: db}
}

// CreateSnapshot inserts one snapshot, idempotent on (knowledge_id, version)
// conflicts — used by creation paths where the knowledge row is persisted in
// its own step.
func (r *knowledgeContentVersionRepository) CreateSnapshot(ctx context.Context,
	version *types.KnowledgeContentVersion,
) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "knowledge_id"}, {Name: "version"}},
		DoNothing: true,
	}).Create(version).Error
}

// CreateWithKnowledgeUpdate inserts the snapshot and saves the knowledge row
// in one transaction, mirroring wikiPageRepository.UpdateWithRevision: a
// history that can silently miss versions is untrustworthy, so a snapshot
// failure fails the save.
func (r *knowledgeContentVersionRepository) CreateWithKnowledgeUpdate(ctx context.Context,
	knowledge *types.Knowledge, version *types.KnowledgeContentVersion,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "knowledge_id"}, {Name: "version"}},
			DoNothing: true,
		}).Create(version)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrKnowledgeContentVersionConflict
		}
		return saveKnowledgeRow(tx, knowledge)
	})
}

// ListByKnowledge returns newest-first slim rows (without the content payload).
func (r *knowledgeContentVersionRepository) ListByKnowledge(ctx context.Context,
	kbID, knowledgeID string, limit int,
) ([]*types.KnowledgeContentVersion, error) {
	var versions []*types.KnowledgeContentVersion
	err := r.db.WithContext(ctx).
		Select("id, tenant_id, knowledge_base_id, knowledge_id, version, title, created_by, created_at").
		Where("knowledge_base_id = ? AND knowledge_id = ?", kbID, knowledgeID).
		Order("version DESC").
		Limit(limit).
		Find(&versions).Error
	if err != nil {
		return nil, err
	}
	return versions, nil
}

// GetByVersion returns one full snapshot including the content.
func (r *knowledgeContentVersionRepository) GetByVersion(ctx context.Context,
	kbID, knowledgeID string, version int,
) (*types.KnowledgeContentVersion, error) {
	var row types.KnowledgeContentVersion
	err := r.db.WithContext(ctx).
		Where("knowledge_base_id = ? AND knowledge_id = ? AND version = ?", kbID, knowledgeID, version).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKnowledgeContentVersionNotFound
		}
		return nil, err
	}
	return &row, nil
}

// Prune deletes all but the newest `keep` versions of a document. The subquery
// form stays dialect-neutral (PostgreSQL and SQLite).
func (r *knowledgeContentVersionRepository) Prune(ctx context.Context, knowledgeID string, keep int) error {
	if keep <= 0 {
		return nil
	}
	latest := r.db.Model(&types.KnowledgeContentVersion{}).
		Select("version").
		Where("knowledge_id = ?", knowledgeID).
		Order("version DESC").
		Limit(keep)
	return r.db.WithContext(ctx).
		Where("knowledge_id = ? AND version NOT IN (?)", knowledgeID, latest).
		Delete(&types.KnowledgeContentVersion{}).Error
}

// DeleteByKnowledge removes every snapshot of a document.
func (r *knowledgeContentVersionRepository) DeleteByKnowledge(ctx context.Context, knowledgeID string) error {
	return r.db.WithContext(ctx).
		Where("knowledge_id = ?", knowledgeID).
		Delete(&types.KnowledgeContentVersion{}).Error
}

// UpdateKnowledgeBaseID re-scopes a document's snapshots after an in-place
// move to another knowledge base.
func (r *knowledgeContentVersionRepository) UpdateKnowledgeBaseID(ctx context.Context,
	knowledgeID, kbID string,
) error {
	return r.db.WithContext(ctx).Model(&types.KnowledgeContentVersion{}).
		Where("knowledge_id = ?", knowledgeID).
		Update("knowledge_base_id", kbID).Error
}
