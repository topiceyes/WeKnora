package service

import (
	"fmt"
	"net/http"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// knowledge_content_version_test.go covers the P3 version history: snapshot on
// save, cross-path counter continuity, list/get, retention prune, restore
// semantics, conflict rollback and isolation.

func TestContentVersionSnapshotOnSave(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "doc", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", FileType: "pdf", FileName: "a.pdf", Title: "标题",
		ParseStatus: types.ParseStatusCompleted,
	})

	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Title: "第一版", Content: "v1 内容",
	})
	require.NoError(t, err)
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{Content: "v2 内容"})
	require.NoError(t, err)

	versions, err := f.svc.ListKnowledgeContentVersions(f.ctx, "doc")
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, 2, versions[0].Version, "新→旧排序")
	require.Equal(t, 1, versions[1].Version)
	require.Empty(t, versions[0].Content, "列表不含正文")
	require.Equal(t, "第一版", versions[1].Title, "快照存的是该版本保存时的标题")
	require.Equal(t, "caller", versions[0].CreatedBy, "记录保存人")

	full, err := f.svc.GetKnowledgeContentVersion(f.ctx, "doc", 1)
	require.NoError(t, err)
	require.Equal(t, "v1 内容", full.Content)
}

// 两条保存路径（旧 manual 接口 + 新在线编辑接口）共用同一计数器。
func TestContentVersionCounterSharedAcrossSavePaths(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "manual", TenantID: 7, KnowledgeBaseID: "kb",
		Type: types.KnowledgeTypeManual, FileName: "m.md", Title: "手册",
		ParseStatus: types.ManualKnowledgeStatusDraft,
	})

	_, err := f.svc.UpdateManualKnowledge(f.ctx, "manual", &types.ManualKnowledgePayload{
		Title: "手册", Content: "manual 保存", Status: types.ManualKnowledgeStatusDraft,
	})
	require.NoError(t, err)
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "manual", &types.KnowledgeContentPayload{Content: "在线编辑"})
	require.NoError(t, err)

	versions, err := f.svc.ListKnowledgeContentVersions(f.ctx, "manual")
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, 2, versions[0].Version)
	require.Equal(t, "在线编辑", mustGetVersionContent(t, f, "manual", 2))
	require.Equal(t, "manual 保存", mustGetVersionContent(t, f, "manual", 1))
}

func mustGetVersionContent(t *testing.T, f *knowledgeContentFixture, id string, version int) string {
	t.Helper()
	row, err := f.svc.GetKnowledgeContentVersion(f.ctx, id, version)
	require.NoError(t, err)
	return row.Content
}

func TestContentVersionPruneKeepsLatest50(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	repo := repository.NewKnowledgeContentVersionRepository(f.db)
	for v := 1; v <= 55; v++ {
		require.NoError(t, repo.CreateSnapshot(f.ctx, &types.KnowledgeContentVersion{
			ID: fmt.Sprintf("snap-%d", v), TenantID: 7, KnowledgeBaseID: "kb",
			KnowledgeID: "doc", Version: v, Title: "t", Content: "c",
		}))
	}
	require.NoError(t, repo.Prune(f.ctx, "doc", types.KnowledgeMaxContentVersions))

	versions, err := repo.ListByKnowledge(f.ctx, "kb", "doc", 100)
	require.NoError(t, err)
	require.Len(t, versions, 50)
	require.Equal(t, 55, versions[0].Version)
	require.Equal(t, 6, versions[49].Version, "最旧的 5 个版本被清理")
}

func TestRestoreContentVersion(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "doc", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", FileType: "pdf", FileName: "a.pdf", Title: "标题",
		ParseStatus: types.ParseStatusCompleted,
	})
	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Title: "第一版", Content: "v1 内容",
	})
	require.NoError(t, err)
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Title: "第二版", Content: "v2 内容",
	})
	require.NoError(t, err)

	// 恢复当前版本必须拒绝。
	_, err = f.svc.RestoreKnowledgeContentVersion(f.ctx, "doc", 2)
	require.Error(t, err, "恢复当前版本无意义")

	restored, err := f.svc.RestoreKnowledgeContentVersion(f.ctx, "doc", 1)
	require.NoError(t, err)
	require.Equal(t, "第一版", restored.Title)
	require.Equal(t, "第一版.pdf", restored.FileName, "恢复同样同步展示名")

	meta, err := f.reload(t, "doc").ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, 3, meta.Version, "恢复是一次新保存，版本递增")
	require.Equal(t, "v1 内容", meta.Content)
	require.True(t, meta.IndexStale, "已索引文档恢复旧版本必须标记待重建")

	// 恢复前的状态（v2）仍在历史里，且恢复产生了 v3 快照。
	versions, err := f.svc.ListKnowledgeContentVersions(f.ctx, "doc")
	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "v2 内容", mustGetVersionContent(t, f, "doc", 2))
	require.Equal(t, "v1 内容", mustGetVersionContent(t, f, "doc", 3))
}

func TestRestoreContentVersionRejectsFAQ(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	faq := &types.Knowledge{
		ID: "faq", TenantID: 7, KnowledgeBaseID: "kb",
		Type: types.KnowledgeTypeFAQ, Title: "常见问题", ParseStatus: types.ParseStatusCompleted,
	}
	require.NoError(t, faq.SetContentMetadata("Q: 什么？\nA: 这样。", 1, true))
	f.seed(t, faq)

	_, err := f.svc.RestoreKnowledgeContentVersion(f.ctx, "faq", 1)
	require.Error(t, err, "FAQ 必须拒绝版本恢复")
}

// 并发保存算出相同版本号时，后到者整体回滚，不留半个保存。
func TestContentVersionConflictRollsBack(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "doc", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", Title: "原标题", ParseStatus: types.ParseStatusCompleted,
	})
	repo := repository.NewKnowledgeContentVersionRepository(f.db)
	require.NoError(t, repo.CreateSnapshot(f.ctx, &types.KnowledgeContentVersion{
		ID: "existing", TenantID: 7, KnowledgeBaseID: "kb",
		KnowledgeID: "doc", Version: 1, Title: "t", Content: "c",
	}))

	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Title: "并发保存", Content: "冲突内容",
	})
	require.Error(t, err, "(knowledge_id, version) 冲突必须失败")
	appErr, ok := err.(*werrors.AppError)
	require.True(t, ok, "竞态冲突必须是 AppError（透传 409）")
	require.Equal(t, werrors.ErrConflict, appErr.Code)
	require.Equal(t, http.StatusConflict, appErr.HTTPCode)
	details, ok := appErr.Details.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "version_conflict", details["reason"])
	require.Equal(t, 1, details["current_version"], "赢家提交的就是失败方算出的同一个版本号")

	stored := f.reload(t, "doc")
	require.Equal(t, "原标题", stored.Title, "冲突时 knowledge 行一并回滚")
	require.False(t, stored.HasEditedContent())
}

func TestContentVersionIsolationAndDelete(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	repo := repository.NewKnowledgeContentVersionRepository(f.db)
	require.NoError(t, repo.CreateSnapshot(f.ctx, &types.KnowledgeContentVersion{
		ID: "snap", TenantID: 7, KnowledgeBaseID: "kb",
		KnowledgeID: "doc", Version: 1, Title: "t", Content: "c",
	}))

	// 跨知识库作用域取不到同一版本。
	_, err := repo.GetByVersion(f.ctx, "other-kb", "doc", 1)
	require.ErrorIs(t, err, repository.ErrKnowledgeContentVersionNotFound)
	_, err = f.svc.GetKnowledgeContentVersion(f.ctx, "missing-doc", 1)
	require.Error(t, err, "文档不存在必须报错")

	// 移动到另一个知识库后，版本历史跟随文档。
	require.NoError(t, repo.UpdateKnowledgeBaseID(f.ctx, "doc", "kb2"))
	row, err := repo.GetByVersion(f.ctx, "kb2", "doc", 1)
	require.NoError(t, err)
	require.Equal(t, "kb2", row.KnowledgeBaseID)
	_, err = repo.GetByVersion(f.ctx, "kb", "doc", 1)
	require.ErrorIs(t, err, repository.ErrKnowledgeContentVersionNotFound)

	// 删除清理。
	require.NoError(t, repo.DeleteByKnowledge(f.ctx, "doc"))
	_, err = repo.GetByVersion(f.ctx, "kb2", "doc", 1)
	require.ErrorIs(t, err, repository.ErrKnowledgeContentVersionNotFound)
}

// 带管线 metadata（process_overrides 等，无编辑内容）的 file/url 文档，
// ManualMetadata 会把 Version 归一为 1——首次在线编辑仍必须是 v1，
// 否则首个快照从 v2 开始编号。
func TestContentVersionFirstEditOfPipelineDocIsV1(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "pipelined", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", FileType: "pdf", FileName: "p.pdf", Title: "管线文档",
		ParseStatus: types.ParseStatusCompleted,
		Metadata:    types.JSON(`{"process_overrides":{"chunking":{"size":512}}}`),
	})

	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "pipelined", &types.KnowledgeContentPayload{Content: "首次编辑"})
	require.NoError(t, err)
	meta, err := f.reload(t, "pipelined").ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, 1, meta.Version, "首次编辑必须是 v1")

	versions, err := f.svc.ListKnowledgeContentVersions(f.ctx, "pipelined")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, 1, versions[0].Version)
}

func intPtr(v int) *int { return &v }

// base_version 乐观并发：过期 base 必须 409 且不动 knowledge 行；匹配/缺省
// base 正常保存。未编辑文档的期望 base 是 0。
func TestUpdateKnowledgeContentCAS(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "doc", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", FileType: "pdf", FileName: "a.pdf", Title: "原标题",
		ParseStatus: types.ParseStatusCompleted,
	})

	// 未编辑文档 base 0 → 首次编辑成 v1。
	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Content: "v1 内容", BaseVersion: intPtr(0),
	})
	require.NoError(t, err)

	// base 仍是 0（过期，当前已是 v1）→ 409，行不动。
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Content: "过期编辑", BaseVersion: intPtr(0),
	})
	appErr, ok := err.(*werrors.AppError)
	require.True(t, ok, "CAS 冲突必须是 AppError")
	require.Equal(t, werrors.ErrConflict, appErr.Code)
	require.Equal(t, http.StatusConflict, appErr.HTTPCode)
	details, ok := appErr.Details.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "version_conflict", details["reason"])
	require.Equal(t, 1, details["current_version"])
	meta, mErr := f.reload(t, "doc").ManualMetadata()
	require.NoError(t, mErr)
	require.Equal(t, "v1 内容", meta.Content, "冲突保存不得改行")
	require.Equal(t, 1, meta.Version)

	// base 匹配（当前 v1）→ 保存成 v2。
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Content: "v2 内容", BaseVersion: intPtr(1),
	})
	require.NoError(t, err)
	meta, mErr = f.reload(t, "doc").ManualMetadata()
	require.NoError(t, mErr)
	require.Equal(t, 2, meta.Version)

	// nil base（restore、普通 API 调用方）→ 跳过检查直接保存。
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Content: "v3 内容",
	})
	require.NoError(t, err)
	meta, mErr = f.reload(t, "doc").ManualMetadata()
	require.NoError(t, mErr)
	require.Equal(t, 3, meta.Version)

	// 负数 base → 参数错误。
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Content: "x", BaseVersion: intPtr(-1),
	})
	require.Error(t, err)
	appErr, ok = err.(*werrors.AppError)
	require.True(t, ok)
	require.Equal(t, werrors.ErrValidation, appErr.Code)
}
