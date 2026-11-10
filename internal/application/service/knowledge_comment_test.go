package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type commentRepoStub struct {
	interfaces.KnowledgeRepository
	k      *types.Knowledge
	writes int
}

func (r *commentRepoStub) GetKnowledgeByID(_ context.Context, tenant uint64, id string) (*types.Knowledge, error) {
	if tenant != r.k.TenantID || id != r.k.ID {
		return nil, nil
	}
	return r.k, nil
}

func (r *commentRepoStub) CreateKnowledgeComment(
	_ context.Context,
	_ *types.Knowledge,
	row *types.KnowledgeComment,
) (*types.KnowledgeComment, error) {
	r.writes++
	return row, nil
}

func (r *commentRepoStub) MutateKnowledgeComment(
	context.Context,
	*types.Knowledge,
	string,
	string,
	*types.KnowledgeCommentMutation,
) error {
	r.writes++
	return nil
}

func (r *commentRepoStub) ListKnowledgeComments(
	context.Context,
	string,
	int,
	string,
) (*types.KnowledgeCommentPage, error) {
	return &types.KnowledgeCommentPage{}, nil
}
func TestKnowledgeCommentsRequireExactWriteAndUser(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	repo := &commentRepoStub{k: &types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "kb"}}
	svc := &knowledgeService{repo: repo, kbService: &writeKBLookup{kb: kb}}
	base := types.WithCaller(context.Background(), types.Caller{UserID: "me", TenantID: 1, Role: types.TenantRoleAdmin})
	grant := &access.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.CallerFromContext(base),
		EffectiveTenantID: 7,
		Permission:        types.OrgRoleViewer,
	}
	_, err := svc.ListKnowledgeComments(grant.Context(base), "doc", 0, "all")
	require.NoError(t, err)
	_, err = svc.CreateKnowledgeComment(grant.Context(base), "doc", &types.KnowledgeCommentInput{Body: "Hi"}, "Me")
	require.Error(t, err)
	require.Zero(t, repo.writes)
	require.Error(
		t,
		svc.MutateKnowledgeComment(
			grant.Context(base),
			"doc",
			"comment",
			&types.KnowledgeCommentMutation{Action: "resolve", BaseRevision: 1},
		),
	)
	grant.Permission = types.OrgRoleEditor
	row, err := svc.CreateKnowledgeComment(grant.Context(base), "doc", &types.KnowledgeCommentInput{Body: " Hi "}, "Me")
	require.NoError(t, err)
	require.Equal(t, "Hi", row.Body)
	require.Equal(t, "me", row.AuthorID)
	require.Equal(t, "Me", row.AuthorName)
	_, err = svc.CreateKnowledgeComment(
		grant.Context(base),
		"different",
		&types.KnowledgeCommentInput{Body: "Hi"},
		"Me",
	)
	require.Error(t, err)
	require.Equal(t, 1, repo.writes)
	for _, user := range []string{"", "system-7"} {
		ctx := types.WithCaller(
			grant.Context(base),
			types.Caller{TenantID: 1, UserID: user, Role: types.TenantRoleAdmin},
		)
		_, err = svc.CreateKnowledgeComment(ctx, "doc", &types.KnowledgeCommentInput{Body: "Hi"}, "Me")
		require.Error(t, err)
	}
}
func TestKnowledgeCommentInputLimits(t *testing.T) {
	svc := &knowledgeService{}
	ctx := types.WithCaller(context.Background(), types.Caller{UserID: "me"})
	invalid := []*types.KnowledgeCommentInput{
		nil,
		{Body: "  "},
		{Body: strings.Repeat("字", 5001)},
		{Body: "Hi", Quote: strings.Repeat("字", 1001)},
		{Body: "Hi", Quote: "q", Prefix: strings.Repeat("a", 65)},
		{Body: "Hi", Prefix: "orphan"},
		{Body: "Hi", ParentID: "reply", Quote: "nested"},
		{Body: "Hi", ClientID: "invalid"},
		{Body: "Hi", ContentVersion: -1},
	}
	for i, input := range invalid {
		_, err := svc.CreateKnowledgeComment(ctx, "doc", input, "Me")
		require.Error(t, err, "case %d", i)
	}
}
