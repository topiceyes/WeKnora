package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"testing"
)

type navigationRepoStub struct {
	interfaces.KnowledgeRepository
	calls  int
	tenant uint64
}

func (r *navigationRepoStub) MutateKnowledgeNavigation(
	_ context.Context,
	tenant uint64,
	_ string,
	_ *types.KnowledgeNavigationMutation,
) (int64, error) {
	r.calls++
	r.tenant = tenant
	return 1, nil
}
func TestKnowledgeNavigationRequiresEffectiveWriteGrant(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	repo := &navigationRepoStub{}
	svc := &knowledgeService{repo: repo, kbService: &writeKBLookup{kb: kb}}
	revision := int64(0)
	m := &types.KnowledgeNavigationMutation{Action: "create_folder", To: "New", BaseRevision: &revision}
	base := types.WithCaller(context.Background(), types.Caller{UserID: "me", TenantID: 1, Role: types.TenantRoleAdmin})
	grant := &access.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.CallerFromContext(base),
		EffectiveTenantID: 7,
		Permission:        types.OrgRoleViewer,
	}
	_, err := svc.UpdateKnowledgeNavigation(grant.Context(base), "kb", m)
	require.Error(t, err)
	require.Zero(t, repo.calls)
	grant.Permission = types.OrgRoleEditor
	_, err = svc.UpdateKnowledgeNavigation(grant.Context(base), "kb", m)
	require.NoError(t, err)
	require.Equal(t, uint64(7), repo.tenant)
	_, err = svc.UpdateKnowledgeNavigation(grant.Context(base), "other", m)
	require.Error(t, err)
	require.Equal(t, 1, repo.calls)
	m.BaseRevision = nil
	_, err = svc.UpdateKnowledgeNavigation(grant.Context(base), "kb", m)
	require.Error(t, err)
	require.Equal(t, 1, repo.calls)
}
