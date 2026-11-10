package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *knowledgeService) SearchDocuments(
	ctx context.Context,
	kbID, query string,
	offset int,
	backlink string,
) (*types.DocumentSearchPage, error) {
	query = strings.TrimSpace(query)
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 100 || offset < 0 || offset > 10000 {
		return nil, apperrors.NewBadRequestError("检索词或分页参数超出限制")
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.ID != kbID || kb.Type == types.KnowledgeBaseTypeFAQ {
		return nil, apperrors.NewNotFoundError("document knowledge base not found")
	}
	tenant, err := resolveKBReadTenant(ctx, kb, s.kbShareService)
	if err != nil {
		return nil, err
	}
	ctx = types.WithExecutionTenant(ctx, tenant)
	if backlink != "" {
		k, err := s.repo.GetKnowledgeByID(ctx, tenant, backlink)
		if err != nil {
			return nil, err
		}
		if k == nil || k.ID != backlink || k.TenantID != tenant || k.KnowledgeBaseID != kbID {
			return nil, apperrors.NewNotFoundError("document not found")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.repo.SearchDocuments(ctx, tenant, kbID, query, offset, backlink)
}
