package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// knowledgeContentFixture wires a real sqlite-backed repository with the
// access-grant context the write path requires, mirroring newFAQWriteFixture.
type knowledgeContentFixture struct {
	svc  *knowledgeService
	db   *gorm.DB
	ctx  context.Context
	kb   *types.KnowledgeBase
	task *createKnowledgeTaskEnqueuerStub
}

func newKnowledgeContentFixture(t *testing.T) *knowledgeContentFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeContentVersion{}))
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	task := &createKnowledgeTaskEnqueuerStub{}
	svc := &knowledgeService{
		repo:               repository.NewKnowledgeRepository(db),
		kbService:          &writeKBLookup{kb: kb},
		task:               task,
		contentVersionRepo: repository.NewKnowledgeContentVersionRepository(db),
	}
	base := types.WithCaller(
		context.Background(),
		types.Caller{TenantID: 1, UserID: "caller", Role: types.TenantRoleAdmin},
	)
	grant := &access.KBAccess{
		KnowledgeBase:     kb,
		Caller:            types.CallerFromContext(base),
		EffectiveTenantID: 7,
		Permission:        types.OrgRoleEditor,
	}
	ctx := context.WithValue(grant.Context(base), types.TenantInfoContextKey, &types.Tenant{ID: 7})
	return &knowledgeContentFixture{svc: svc, db: db, ctx: ctx, kb: kb, task: task}
}

func (f *knowledgeContentFixture) seed(t *testing.T, k *types.Knowledge) {
	t.Helper()
	require.NoError(t, f.db.Create(k).Error)
}

func (f *knowledgeContentFixture) reload(t *testing.T, id string) *types.Knowledge {
	t.Helper()
	var k types.Knowledge
	require.NoError(t, f.db.First(&k, "id = ?", id).Error)
	return &k
}

func TestUpdateKnowledgeContentMarksStaleAndKeepsParseStatus(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "doc", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", FileType: "pdf", FileName: "a.pdf", Title: "旧标题",
		ParseStatus: types.ParseStatusCompleted,
	})

	updated, err := f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{
		Title:   "新标题",
		Content: "# 新正文\n\nhello",
	})
	require.NoError(t, err)
	require.Equal(t, "新标题", updated.Title)
	require.Equal(t, "新标题.pdf", updated.FileName, "改名同步展示名并保留原扩展名")

	stored := f.reload(t, "doc")
	require.Equal(t, types.ParseStatusCompleted, stored.ParseStatus, "保存不触碰索引状态")
	meta, err := stored.ManualMetadata()
	require.NoError(t, err)
	require.NotNil(t, meta)
	require.Equal(t, "# 新正文\n\nhello", meta.Content)
	require.Equal(t, 1, meta.Version)
	require.True(t, meta.IndexStale, "已索引文档编辑后必须标记 index_stale")
	require.True(t, stored.HasEditedContent())

	// 第二次保存版本递增。
	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "doc", &types.KnowledgeContentPayload{Content: "v2"})
	require.NoError(t, err)
	meta, err = f.reload(t, "doc").ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, 2, meta.Version)
	require.Equal(t, "v2", meta.Content)
}

func TestUpdateKnowledgeContentDraftIsNotStale(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "draft", TenantID: 7, KnowledgeBaseID: "kb",
		Type: types.KnowledgeTypeManual, Title: "草稿",
		ParseStatus: types.ManualKnowledgeStatusDraft,
	})

	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "draft", &types.KnowledgeContentPayload{Content: "正文"})
	require.NoError(t, err)
	meta, err := f.reload(t, "draft").ManualMetadata()
	require.NoError(t, err)
	require.False(t, meta.IndexStale, "草稿从未入索引，不存在过期一说")
}

func TestUpdateKnowledgeContentRejectsInFlightAndEmpty(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "busy", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", Title: "解析中", ParseStatus: types.ParseStatusProcessing,
	})
	f.seed(t, &types.Knowledge{
		ID: "plain", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", Title: "正常", ParseStatus: types.ParseStatusCompleted,
	})

	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "busy", &types.KnowledgeContentPayload{Content: "x"})
	require.Error(t, err, "解析中的文档必须拒绝编辑")

	_, err = f.svc.UpdateKnowledgeContent(f.ctx, "plain", &types.KnowledgeContentPayload{Content: "   "})
	require.Error(t, err, "空内容必须拒绝")

	require.Zero(t, f.task.calls, "内容保存绝不触发索引任务")
}

func TestRelearnKnowledgeEnqueuesManualPipelineAndClearsStale(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	doc := &types.Knowledge{
		ID: "doc", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", FileType: "pdf", FileName: "a.pdf", Title: "文档",
		ParseStatus: types.ParseStatusCompleted,
	}
	require.NoError(t, doc.SetContentMetadata("# 编辑后的正文", 2, true))
	f.seed(t, doc)

	updated, err := f.svc.RelearnKnowledge(f.ctx, "doc")
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, updated.ParseStatus)
	require.Equal(t, 1, f.task.calls, "必须投递一次 manual:process 任务")

	stored := f.reload(t, "doc")
	meta, err := stored.ManualMetadata()
	require.NoError(t, err)
	require.False(t, meta.IndexStale, "重新学习提交后解除 stale")
	require.Equal(t, "# 编辑后的正文", meta.Content, "编辑内容必须保留，等待 worker 消费")
	require.Equal(t, 2, meta.Version)
}

func TestRelearnKnowledgeRejectsWithoutContent(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	f.seed(t, &types.Knowledge{
		ID: "raw", TenantID: 7, KnowledgeBaseID: "kb",
		Type: "file", Title: "未编辑", ParseStatus: types.ParseStatusCompleted,
	})

	_, err := f.svc.RelearnKnowledge(f.ctx, "raw")
	require.Error(t, err, "没有编辑内容的文档不能重新学习")
	require.Zero(t, f.task.calls)
}

// FAQ 知识是问答对集合，不是 Markdown 文档：在线编辑/重新学习会把它切块成
// 普通文本、摧毁 FAQ 结构，两个入口都必须拒绝。
func TestContentEditAndRelearnRejectFAQ(t *testing.T) {
	f := newKnowledgeContentFixture(t)
	faq := &types.Knowledge{
		ID: "faq", TenantID: 7, KnowledgeBaseID: "kb",
		Type: types.KnowledgeTypeFAQ, Title: "常见问题", ParseStatus: types.ParseStatusCompleted,
	}
	require.NoError(t, faq.SetContentMetadata("Q: 什么？\nA: 这样。", 1, true))
	f.seed(t, faq)

	_, err := f.svc.UpdateKnowledgeContent(f.ctx, "faq", &types.KnowledgeContentPayload{Content: "改写"})
	require.Error(t, err, "FAQ 必须拒绝在线编辑")
	_, err = f.svc.RelearnKnowledge(f.ctx, "faq")
	require.Error(t, err, "FAQ 必须拒绝重新学习")
	require.Zero(t, f.task.calls)
}
