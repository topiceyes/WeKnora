package service

import (
	"context"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *knowledgeService) UpdateKnowledgeNavigation(
	ctx context.Context,
	kbID string,
	m *types.KnowledgeNavigationMutation,
) (int64, error) {
	ctx, finish, err := s.beginScopedMutation(ctx, kbID)
	if err != nil {
		return 0, err
	}
	defer finish()
	if m == nil || m.BaseRevision == nil || *m.BaseRevision < 0 {
		return 0, apperrors.NewBadRequestError("base_revision 必须是非负整数")
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return 0, err
	}
	if kb == nil || kb.ID != kbID {
		return 0, apperrors.NewNotFoundError("knowledge base not found")
	}
	ctx, err = requireKBWrite(ctx, kb)
	if err != nil {
		return 0, err
	}
	if kb.Type == types.KnowledgeBaseTypeFAQ {
		return 0, apperrors.NewBadRequestError("FAQ 不支持文档目录操作")
	}
	affected, err := s.repo.MutateKnowledgeNavigation(ctx, kb.TenantID, kbID, m)
	if err != nil {
		return 0, err
	}
	recordKBActivity(
		ctx,
		s.audit,
		kb.TenantID,
		kbID,
		types.AuditActionKnowledgeUpdated,
		"knowledge_base",
		kbID,
		types.AuditOutcomeSuccess,
		map[string]any{
			"trigger": "document_navigation",
			"action":  m.Action,
			"from":    m.From,
			"to":      m.To,
			"count":   affected,
		},
	)
	return affected, nil
}
