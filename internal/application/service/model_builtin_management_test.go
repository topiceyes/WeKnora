package service

import (
	"context"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func builtinModelContext(systemAdmin bool) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	return context.WithValue(ctx, types.SystemAdminContextKey, systemAdmin)
}

func TestCreateBuiltinModel_RequiresSystemAdmin(t *testing.T) {
	created := false
	svc := NewModelService(&stubModelRepoForDelete{
		create: func(*types.Model) error {
			created = true
			return nil
		},
	}, nil, nil, nil, nil, nil)

	model := &types.Model{
		Name: "shared-chat", Type: types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote, IsBuiltin: true,
	}
	err := svc.CreateModel(builtinModelContext(false), model)
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrForbidden, appErr.Code)
	assert.False(t, created)
}

func TestCreateBuiltinModel_SystemAdmin(t *testing.T) {
	var saved *types.Model
	svc := NewModelService(&stubModelRepoForDelete{
		create: func(model *types.Model) error {
			copy := *model
			saved = &copy
			return nil
		},
	}, nil, nil, nil, nil, nil)

	model := &types.Model{
		TenantID: 7, // caller's tenant; must be overridden
		Name:     "shared-chat", Type: types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote, IsBuiltin: true,
	}
	require.NoError(t, svc.CreateModel(builtinModelContext(true), model))
	require.NotNil(t, saved)
	assert.Equal(t, types.DefaultBuiltinModelTenantID, saved.TenantID)
	assert.True(t, saved.IsBuiltin)
	assert.Empty(t, saved.ManagedBy, "runtime-created builtin rows must survive the YAML reconciler sweep")
	assert.False(t, saved.IsDefault)
}

func TestCreateModel_NonBuiltinUnaffectedByBuiltinBranch(t *testing.T) {
	var saved *types.Model
	svc := NewModelService(&stubModelRepoForDelete{
		create: func(model *types.Model) error {
			copy := *model
			saved = &copy
			return nil
		},
	}, nil, nil, nil, nil, nil)

	model := &types.Model{
		TenantID: 7, Name: "own-chat", Type: types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote,
	}
	require.NoError(t, svc.CreateModel(builtinModelContext(false), model))
	require.NotNil(t, saved)
	assert.Equal(t, uint64(7), saved.TenantID)
	assert.False(t, saved.IsBuiltin)
}

func TestDeleteBuiltinModel_RequiresSystemAdmin(t *testing.T) {
	stored := &types.Model{ID: "builtin-chat", TenantID: 10000, IsBuiltin: true}
	deleted := false
	svc := NewModelService(&stubModelRepoForDelete{
		model: stored,
		delete: func(string) error {
			deleted = true
			return nil
		},
	}, &stubKBRepoForModelDelete{}, &stubAgentRepoForModelDelete{}, nil, nil, nil)

	err := svc.DeleteModel(builtinModelContext(false), stored.ID)
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrForbidden, appErr.Code)
	assert.False(t, deleted)
}

func TestDeleteBuiltinModel_SystemAdminSucceeds(t *testing.T) {
	stored := &types.Model{ID: "builtin-chat", TenantID: 10000, IsBuiltin: true}
	deleted := false
	var deleteTenant uint64
	svc := NewModelService(&stubModelRepoForDelete{
		model:        stored,
		deleteTenant: &deleteTenant,
		delete: func(id string) error {
			assert.Equal(t, stored.ID, id)
			deleted = true
			return nil
		},
	}, &stubKBRepoForModelDelete{}, &stubAgentRepoForModelDelete{}, nil, nil, nil)

	// Caller tenant is 7; the row hangs off the builtin tenant 10000.
	require.NoError(t, svc.DeleteModel(builtinModelContext(true), stored.ID))
	assert.True(t, deleted)
	assert.Equal(t, uint64(10000), deleteTenant, "repo.Delete must target the row's own tenant")
}

func TestDeleteBuiltinModel_InUseAcrossTenantsBlocked(t *testing.T) {
	stored := &types.Model{ID: "builtin-chat", TenantID: 10000, IsBuiltin: true}
	deleted := false
	// Usage only visible in the cross-tenant lookup — the caller's own
	// tenant (7) has none.
	svc := NewModelService(&stubModelRepoForDelete{
		model: stored,
		delete: func(string) error {
			deleted = true
			return nil
		},
	},
		&stubKBRepoForModelDelete{crossUsages: []types.ModelUsageResource{
			{ID: "kb-9", Name: "Other tenant KB", TenantID: 10001,
				Bindings: []types.ModelUsageBinding{types.ModelUsageBindingSummaryModel}},
		}},
		&stubAgentRepoForModelDelete{},
		nil, nil, nil,
	)

	err := svc.DeleteModel(builtinModelContext(true), stored.ID)
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrModelInUse, appErr.Code)
	assert.False(t, deleted)
	details, ok := appErr.Details.(types.ModelUsageDetails)
	require.True(t, ok)
	require.Len(t, details.KnowledgeBases, 1)
	assert.Equal(t, uint64(10001), details.KnowledgeBases[0].TenantID)
}

func TestUpdateBuiltinModel_RequiresSystemAdmin(t *testing.T) {
	stored := &types.Model{
		ID: "builtin-chat", TenantID: 10000, IsBuiltin: true,
		ManagedBy: types.BuiltinModelManagedBy,
	}
	updated := false
	svc := NewModelService(&stubModelRepoForDelete{
		model: stored,
		update: func(*types.Model) error {
			updated = true
			return nil
		},
	}, nil, nil, nil, nil, nil)

	err := svc.UpdateModel(builtinModelContext(false), &types.Model{ID: stored.ID})
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrForbidden, appErr.Code)
	assert.False(t, updated)
}

func TestUpdateBuiltinModel_SystemAdminCreatesRuntimeOverride(t *testing.T) {
	stored := &types.Model{
		ID: "builtin-chat", TenantID: 10000, IsBuiltin: true,
		ManagedBy: types.BuiltinModelManagedBy,
	}
	var saved *types.Model
	svc := NewModelService(&stubModelRepoForDelete{
		model: stored,
		update: func(model *types.Model) error {
			copy := *model
			saved = &copy
			return nil
		},
	}, nil, nil, nil, nil, nil)

	input := &types.Model{ID: stored.ID, Name: "edited"}
	require.NoError(t, svc.UpdateModel(builtinModelContext(true), input))
	require.NotNil(t, saved)
	assert.Equal(t, uint64(10000), saved.TenantID)
	assert.True(t, saved.IsBuiltin)
	assert.Empty(t, saved.ManagedBy, "UI edit must stop later YAML reconciliation")
}

func TestUpdateBuiltinModelCredentials_SystemAdminOnly(t *testing.T) {
	newKey := "sk-new"

	t.Run("tenant admin denied", func(t *testing.T) {
		stored := &types.Model{ID: "builtin-chat", TenantID: 10000, IsBuiltin: true}
		svc := NewModelService(&stubModelRepoForDelete{model: stored}, nil, nil, nil, nil, nil)
		_, err := svc.UpdateModelCredentials(builtinModelContext(false), stored.ID, &newKey, nil)
		require.Error(t, err)
		appErr, ok := apperrors.IsAppError(err)
		require.True(t, ok)
		assert.Equal(t, apperrors.ErrForbidden, appErr.Code)
	})

	t.Run("system admin saves runtime override", func(t *testing.T) {
		stored := &types.Model{
			ID: "builtin-chat", TenantID: 10000, IsBuiltin: true,
			ManagedBy: types.BuiltinModelManagedBy,
		}
		var saved *types.Model
		svc := NewModelService(&stubModelRepoForDelete{
			model: stored,
			update: func(model *types.Model) error {
				copy := *model
				saved = &copy
				return nil
			},
		}, nil, nil, nil, nil, nil)

		updated, err := svc.UpdateModelCredentials(
			builtinModelContext(true), stored.ID, &newKey, nil,
		)
		require.NoError(t, err)
		assert.Equal(t, newKey, updated.Parameters.APIKey)
		require.NotNil(t, saved)
		assert.Empty(t, saved.ManagedBy)
	})
}
