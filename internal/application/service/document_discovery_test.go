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

type discoveryRepo struct {
	interfaces.KnowledgeRepository
	k      *types.Knowledge
	calls  int
	tenant uint64
}

func (r *discoveryRepo) SearchDocuments(
	_ context.Context,
	tenant uint64,
	_, _ string,
	_ int,
	_ string,
) (*types.DocumentSearchPage, error) {
	r.calls++
	r.tenant = tenant
	return &types.DocumentSearchPage{}, nil
}
func (r *discoveryRepo) GetKnowledgeByID(context.Context, uint64, string) (*types.Knowledge, error) {
	return r.k, nil
}
func TestDocumentDiscoveryRequiresReadScopeAndExactTarget(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	r := &discoveryRepo{k: &types.Knowledge{ID: "target", TenantID: 7, KnowledgeBaseID: "kb"}}
	svc := &knowledgeService{repo: r, kbService: &writeKBLookup{kb: kb}}
	base := types.WithCaller(context.Background(), types.Caller{UserID: "me", TenantID: 1, Role: types.TenantRoleAdmin})
	// Execution scope cannot fabricate a read grant.
	_, err := svc.SearchDocuments(types.WithExecutionTenant(base, 7), "kb", "q", 0, "")
	require.Error(t, err)
	require.Zero(t, r.calls)
	grant := &access.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.CallerFromContext(base),
		EffectiveTenantID: 7,
		Permission:        types.OrgRoleViewer,
	}
	ctx := grant.Context(base)
	_, err = svc.SearchDocuments(ctx, "kb", "q", 0, "")
	require.NoError(t, err)
	require.Equal(t, uint64(7), r.tenant)
	_, err = svc.SearchDocuments(ctx, "other", "q", 0, "")
	require.Error(t, err)
	_, err = svc.SearchDocuments(ctx, "kb", "", 0, "target")
	require.NoError(t, err)
	r.k.KnowledgeBaseID = "different"
	_, err = svc.SearchDocuments(ctx, "kb", "", 0, "target")
	require.Error(t, err)
	for _, offset := range []int{-1, 10001} {
		_, err = svc.SearchDocuments(ctx, "kb", "q", offset, "")
		require.Error(t, err)
	}
	_, err = svc.SearchDocuments(ctx, "kb", strings.Repeat("字", 101), 0, "")
	require.Error(t, err)
}
