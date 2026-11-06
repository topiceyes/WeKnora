package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/kbmutation"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

// Embedded in content services; the composition root installs one coordinator
// before starting HTTP, cron or workers. Repository-only unit fixtures can
// leave it unset; workspace transfers fail closed without a coordinator.
type knowledgeMutationGuard struct {
	mutations  *kbmutation.Coordinator
	mutationDB *gorm.DB
}

func (g *knowledgeMutationGuard) setMutationCoordinator(c *kbmutation.Coordinator, db *gorm.DB) {
	g.mutations, g.mutationDB = c, db
}

func (g *knowledgeMutationGuard) beginMutation(ctx context.Context) (context.Context, func(), error) {
	ctx, release, err := g.mutations.Begin(ctx)
	if errors.Is(err, kbmutation.ErrBusy) {
		return ctx, nil, apperrors.NewConflictError("知识库正在迁移，请稍后重试").WithDetails(map[string]any{"reason": "workspace_transfer_in_progress"})
	}
	if err != nil {
		return ctx, nil, fmt.Errorf("coordinate knowledge operation: %w", err)
	}
	return ctx, release, nil
}

// ConfigureKBMutationCoordination is called exactly once before services start.
// Reject unknown targets so a wiring mistake cannot silently leave a writer out.
func ConfigureKBMutationCoordination(c *kbmutation.Coordinator, db *gorm.DB, targets ...any) error {
	if c == nil {
		return errors.New("knowledge mutation coordinator is required")
	}
	for _, target := range targets {
		g, ok := target.(interface {
			setMutationCoordinator(*kbmutation.Coordinator, *gorm.DB)
		})
		if !ok {
			return fmt.Errorf("knowledge mutation coordinator: unsupported service %T", target)
		}
		g.setMutationCoordinator(c, db)
	}
	return nil
}

// BeginKnowledgeMutation coordinates lifecycle callbacks which update a
// repository directly, such as the worker's terminal-failure callback.
func BeginKnowledgeMutation(ctx context.Context, target any) (context.Context, func(), error) {
	if g, ok := target.(interface {
		beginMutation(context.Context) (context.Context, func(), error)
	}); ok {
		return g.beginMutation(ctx)
	}
	return ctx, func() {}, nil // test doubles do not run production callbacks
}

var errObsoleteKnowledgeTask = errors.New("knowledge task belongs to a previous workspace")

// Validate queued work after taking the shared lease. Tasks admitted before a
// completed transfer must never run with their old tenant/path snapshots.
func (g *knowledgeMutationGuard) beginTaskMutation(ctx context.Context, task *asynq.Task) (context.Context, func(), error) {
	ctx, release, err := g.mutations.WaitBegin(ctx)
	if err != nil {
		return ctx, nil, err
	}
	if g.mutationDB == nil {
		return ctx, release, nil
	}
	var p struct {
		TenantID        uint64   `json:"tenant_id"`
		KnowledgeBaseID string   `json:"knowledge_base_id"`
		KBID            string   `json:"kb_id"`
		SourceKBID      string   `json:"source_kb_id"`
		TargetKBID      string   `json:"target_kb_id"`
		TargetID        string   `json:"target_id"`
		SourceID        string   `json:"source_id"`
		KnowledgeID     string   `json:"knowledge_id"`
		KnowledgeIDs    []string `json:"knowledge_ids"`
		DataSourceID    string   `json:"data_source_id"`
		SyncLogID       string   `json:"sync_log_id"`
	}
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		release()
		return ctx, nil, err
	}
	if p.TenantID == 0 {
		return ctx, release, nil
	}
	if task.Type() == types.TypeKBClone {
		p.SourceKBID = p.SourceID
	}
	check := func(table string, ids []string) error {
		var mismatches int64
		err := g.mutationDB.WithContext(ctx).Table(table).
			Where("id IN ? AND tenant_id <> ?", ids, p.TenantID).Count(&mismatches).Error
		if err != nil {
			return err
		}
		if mismatches > 0 {
			return errObsoleteKnowledgeTask
		}
		return nil
	}
	for table, ids := range map[string][]string{
		"knowledge_bases": {p.KnowledgeBaseID, p.KBID, p.SourceKBID, p.TargetKBID, p.TargetID},
		"knowledges":      append(p.KnowledgeIDs, p.KnowledgeID),
		"data_sources":    {p.DataSourceID},
	} {
		var nonEmpty []string
		for _, id := range ids {
			if id != "" {
				nonEmpty = append(nonEmpty, id)
			}
		}
		if len(nonEmpty) == 0 {
			continue
		}
		if err := check(table, nonEmpty); err != nil {
			if err == errObsoleteKnowledgeTask && p.DataSourceID != "" && p.SyncLogID != "" {
				// A scheduled sync can be queued before the move. Its running log
				// moved with the source; close it so future syncs are not blocked.
				if updateErr := g.mutationDB.WithContext(ctx).Table("sync_logs").
					Where("id = ? AND data_source_id = ? AND status = ?", p.SyncLogID, p.DataSourceID, types.SyncLogStatusRunning).
					Updates(map[string]any{"status": types.SyncLogStatusCanceled, "finished_at": time.Now().UTC(), "error_message": "workspace changed before sync started"}).Error; updateErr != nil {
					release()
					return ctx, nil, updateErr
				}
			}
			release()
			return ctx, nil, err
		}
	}
	return ctx, release, nil
}

// Object-based APIs may be called after an agent spent time generating a new
// page. Reject snapshots from the old workspace even if the move completed
// before this particular method acquired its lease.
func (g *knowledgeMutationGuard) validateMutationWorkspace(ctx context.Context, kbID string, snapshotTenant uint64) error {
	if g.mutationDB == nil || kbID == "" {
		return nil
	}
	var row struct{ TenantID uint64 }
	if err := g.mutationDB.WithContext(ctx).Table("knowledge_bases").Select("tenant_id").Where("id = ?", kbID).Take(&row).Error; err != nil {
		return err
	}
	executionTenant, _ := types.TenantIDFromContext(ctx)
	if (snapshotTenant != 0 && snapshotTenant != row.TenantID) ||
		(snapshotTenant == 0 && executionTenant != 0 && executionTenant != row.TenantID &&
			!access.HasKBGrant(ctx, kbID, row.TenantID, types.OrgRoleEditor)) {
		return apperrors.NewConflictError("知识库所属空间已变更，请刷新后重试")
	}
	return nil
}

func (g *knowledgeMutationGuard) beginScopedMutation(ctx context.Context, kbID string) (context.Context, func(), error) {
	ctx, release, err := g.beginMutation(ctx)
	if err != nil {
		return ctx, nil, err
	}
	if err := g.validateMutationWorkspace(ctx, kbID, 0); err != nil {
		release()
		return ctx, nil, err
	}
	return ctx, release, nil
}

func (g *knowledgeMutationGuard) beginObjectMutation(ctx context.Context, object any) (context.Context, func(), error) {
	ctx, release, err := g.beginMutation(ctx)
	if err != nil {
		return ctx, nil, err
	}
	var kbID string
	var tenantID uint64
	switch v := object.(type) {
	case *types.WikiPage:
		if v != nil {
			kbID, tenantID = v.KnowledgeBaseID, v.TenantID
		}
	case *types.WikiPageIssue:
		if v != nil {
			kbID, tenantID = v.KnowledgeBaseID, v.TenantID
		}
	case *types.DataSource:
		if v != nil {
			kbID, tenantID = v.KnowledgeBaseID, v.TenantID
		}
	case *types.KnowledgeBase:
		if v != nil {
			kbID, tenantID = v.ID, v.TenantID
		}
	}
	if err := g.validateMutationWorkspace(ctx, kbID, tenantID); err != nil {
		release()
		return ctx, nil, err
	}
	return ctx, release, nil
}
