package service

import (
	"context"
	"strings"
	"unicode/utf8"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

func (s *knowledgeService) ListKnowledgeComments(
	ctx context.Context,
	id string,
	offset int,
	status string,
) (*types.KnowledgeCommentPage, error) {
	k, err := s.loadKnowledgeForContentVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	kb, err := knowledgeWriteKB(ctx, s.kbService, k)
	if err != nil {
		return nil, err
	}
	if _, err = resolveKBReadTenant(ctx, kb, s.kbShareService); err != nil {
		return nil, err
	}
	if offset < 0 || offset > 1000000 || (status != "all" && status != "open" && status != "resolved") {
		return nil, apperrors.NewBadRequestError("评论筛选参数不合法")
	}
	return s.repo.ListKnowledgeComments(ctx, id, offset, status)
}
func commentUser(ctx context.Context) (string, error) {
	user := types.CallerFromContext(ctx).UserID
	if user == "" || types.IsSyntheticUserID(user) {
		return "", apperrors.NewForbiddenError("评论操作需要登录用户")
	}
	return user, nil
}

func (s *knowledgeService) CreateKnowledgeComment(
	ctx context.Context,
	id string,
	input *types.KnowledgeCommentInput,
	name string,
) (*types.KnowledgeComment, error) {
	user, err := commentUser(ctx)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, apperrors.NewBadRequestError("评论不能为空")
	}
	body := strings.TrimSpace(input.Body)
	if body == "" || !utf8.ValidString(body) || utf8.RuneCountInString(body) > 5000 ||
		utf8.RuneCountInString(input.Quote) > 1000 ||
		utf8.RuneCountInString(input.Prefix) > 64 ||
		utf8.RuneCountInString(input.Suffix) > 64 ||
		input.ContentVersion < 0 {
		return nil, apperrors.NewBadRequestError("评论或引用超出限制")
	}
	if input.ParentID != "" && (input.Quote != "" || input.Prefix != "" || input.Suffix != "") {
		return nil, apperrors.NewBadRequestError("回复不能附加选区")
	}
	if input.Quote == "" && (input.Prefix != "" || input.Suffix != "") {
		return nil, apperrors.NewBadRequestError("选区引用不能为空")
	}
	requestID := input.ClientID
	if requestID == "" {
		requestID = uuid.NewString()
	}
	parsedID, parseErr := uuid.Parse(requestID)
	if parseErr != nil {
		return nil, apperrors.NewBadRequestError("评论请求 ID 不合法")
	}
	requestID = parsedID.String()
	ctx, finish, err := s.beginMutation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	k, _, err := loadKnowledgeWrite(ctx, s.repo, s.kbService, id)
	if err != nil {
		return nil, err
	}
	if k.Type == types.KnowledgeTypeFAQ {
		return nil, apperrors.NewBadRequestError("FAQ 不支持文档讨论")
	}
	return s.repo.CreateKnowledgeComment(ctx, k, &types.KnowledgeComment{
		ID: requestID, KnowledgeID: id, AuthorID: user, AuthorName: name,
		Body: body, ParentID: input.ParentID, Quote: input.Quote,
		Prefix: input.Prefix, Suffix: input.Suffix, ContentVersion: input.ContentVersion,
	})
}

func (s *knowledgeService) MutateKnowledgeComment(
	ctx context.Context,
	id, commentID string,
	m *types.KnowledgeCommentMutation,
) error {
	user, err := commentUser(ctx)
	if err != nil {
		return err
	}
	if m == nil || m.BaseRevision < 1 || (m.Action != "delete" && m.Action != "resolve") {
		return apperrors.NewBadRequestError("评论操作不合法")
	}
	ctx, finish, err := s.beginMutation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	k, _, err := loadKnowledgeWrite(ctx, s.repo, s.kbService, id)
	if err != nil {
		return err
	}
	return s.repo.MutateKnowledgeComment(ctx, k, commentID, user, m)
}
