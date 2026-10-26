package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// stubVersionUserService 只实现 enrichContentVersionCreatorNames 用到的
// GetUsersByIDs，其余方法靠接口内嵌（不会被调到）。
type stubVersionUserService struct {
	interfaces.UserService
	getByIDs func(ctx context.Context, ids []string) (map[string]*types.User, error)
}

func (s *stubVersionUserService) GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error) {
	return s.getByIDs(ctx, ids)
}

func TestEnrichContentVersionCreatorNames(t *testing.T) {
	versions := []*types.KnowledgeContentVersion{
		{ID: "v3", Version: 3, CreatedBy: "user-a"},
		{ID: "v2", Version: 2, CreatedBy: "user-b"},
		{ID: "v1", Version: 1, CreatedBy: "user-a"},
		{ID: "v0", Version: 0, CreatedBy: ""}, // API key 保存，保持无名
	}

	var batchCallCount int
	svc := &stubVersionUserService{
		getByIDs: func(_ context.Context, ids []string) (map[string]*types.User, error) {
			batchCallCount++
			if len(ids) != 2 {
				t.Fatalf("batch lookup should receive 2 distinct IDs, got %v", ids)
			}
			return map[string]*types.User{
				"user-a": {ID: "user-a", Username: "alice"},
				"user-b": {ID: "user-b", Email: "bob@example.com"}, // username 缺失退化到 email
			}, nil
		},
	}

	enrichContentVersionCreatorNames(context.Background(), svc, versions)

	if batchCallCount != 1 {
		t.Fatalf("expected exactly 1 batched GetUsersByIDs call, got %d", batchCallCount)
	}
	if versions[0].CreatorName != "alice" || versions[2].CreatorName != "alice" {
		t.Fatalf("user-a versions should resolve to alice, got %q / %q", versions[0].CreatorName, versions[2].CreatorName)
	}
	if versions[1].CreatorName != "bob@example.com" {
		t.Fatalf("user-b should fall back to email, got %q", versions[1].CreatorName)
	}
	if versions[3].CreatorName != "" {
		t.Fatalf("empty created_by must stay nameless, got %q", versions[3].CreatorName)
	}
}

// 解析失败只 Warnf：列表返回原样，creator_name 全部留空。
func TestEnrichContentVersionCreatorNamesSwallowsLookupFailure(t *testing.T) {
	versions := []*types.KnowledgeContentVersion{{ID: "v1", Version: 1, CreatedBy: "user-a"}}
	svc := &stubVersionUserService{
		getByIDs: func(context.Context, []string) (map[string]*types.User, error) {
			return nil, errors.New("db down")
		},
	}

	enrichContentVersionCreatorNames(context.Background(), svc, versions)

	if versions[0].CreatorName != "" {
		t.Fatalf("lookup failure must not set creator_name, got %q", versions[0].CreatorName)
	}
}
