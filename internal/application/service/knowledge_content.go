package service

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
)

// knowledge_content.go implements the browse-mode online editing endpoints
// (P2): content save (PUT /knowledge/:id/content) and the explicit re-index
// trigger (POST /knowledge/:id/relearn).
//
// Semantics: saving writes the edited markdown into knowledge.metadata and
// marks index_stale=true, WITHOUT touching the index — the reader serves the
// fresh text immediately while search keeps returning the old chunks.
// RelearnKnowledge is the manual "重新学习" action: it resets the parse state
// and enqueues the shared manual:process pipeline (cleanup + re-chunk +
// re-embed from the edited content).

// UpdateKnowledgeContent saves the online-edited markdown for any knowledge
// type. Unlike UpdateManualKnowledge it never enqueues indexing and never
// rewrites type/file fields — for file/url documents the original file stays
// frozen as the provenance snapshot (preview/download keep serving it).
func (s *knowledgeService) UpdateKnowledgeContent(ctx context.Context,
	knowledgeID string, payload *types.KnowledgeContentPayload,
) (*types.Knowledge, error) {
	if payload == nil {
		return nil, werrors.NewBadRequestError("请求内容不能为空")
	}

	cleanContent := secutils.CleanMarkdown(payload.Content)
	if strings.TrimSpace(cleanContent) == "" {
		return nil, werrors.NewValidationError("内容不能为空")
	}
	if len([]rune(cleanContent)) > manualContentMaxLength {
		return nil, werrors.NewValidationError(fmt.Sprintf("内容长度超出限制（最多%d个字符）", manualContentMaxLength))
	}

	safeTitle := ""
	if strings.TrimSpace(payload.Title) != "" {
		var ok bool
		safeTitle, ok = secutils.ValidateInput(payload.Title)
		if !ok {
			return nil, werrors.NewValidationError("标题包含非法字符或超出长度限制")
		}
	}

	existing, kb, err := loadKnowledgeWrite(ctx, s.repo, s.kbService, knowledgeID)
	if err != nil {
		return nil, err
	}
	// FAQ knowledge is a set of Q&A pairs, not a markdown document; editing it
	// as prose and relearning would chunk it into ordinary text and destroy
	// the FAQ structure. FAQ entries have their own manager.
	if existing.Type == types.KnowledgeTypeFAQ {
		return nil, werrors.NewBadRequestError("FAQ 文档请使用 FAQ 管理功能编辑")
	}
	// An in-flight parse would race the save: the worker snapshots content at
	// chunk time, so edits landing mid-run would be silently lost from the
	// index while the reader shows them. Reject instead of reconciling.
	if isInFlightParseStatus(existing.ParseStatus) {
		return nil, werrors.NewBadRequestError("文档正在处理中，请完成后再编辑")
	}
	ctx, err = withKBWriteTenantInfo(ctx, kb, s.tenantRepo)
	if err != nil {
		return nil, err
	}
	tenantID := existing.TenantID

	// Only a prior EDITED content bumps the counter: file/url docs carry
	// pipeline metadata (process_overrides etc.) whose ManualMetadata()
	// normalizes Version to 1 despite there being no content snapshot yet.
	version := 1
	if meta, err := existing.ManualMetadata(); err == nil && meta != nil && meta.Content != "" {
		version = meta.Version + 1
	}

	// Draft documents have never been indexed, so nothing can be stale yet;
	// everything else gets the "待重建" marker until relearn runs.
	indexStale := existing.ParseStatus != types.ManualKnowledgeStatusDraft
	if err := existing.SetContentMetadata(cleanContent, version, indexStale); err != nil {
		logger.Errorf(ctx, "Failed to set content metadata: %v", err)
		return nil, err
	}

	if safeTitle != "" {
		existing.Title = safeTitle
		if existing.IsManual() {
			existing.FileName = ensureManualFileName(existing.Title)
		} else {
			// file/url: FileName doubles as the display name in the AI views
			// (browse tree shows Title), so renames must sync it. Keep the
			// original extension so a downloaded file keeps a usable suffix.
			existing.FileName = ensureDisplayFileName(safeTitle, existing.FileName)
		}
	}
	existing.UpdatedAt = time.Now()

	if err := s.saveWithContentVersion(ctx, existing, version, cleanContent); err != nil {
		logger.Errorf(ctx, "Failed to persist edited content: %v", err)
		return nil, err
	}
	s.bindContentResources(ctx, tenantID, existing.ID, cleanContent)
	recordKBActivity(ctx, s.audit, tenantID, existing.KnowledgeBaseID, types.AuditActionKnowledgeUpdated,
		"knowledge", existing.ID, types.AuditOutcomeSuccess, map[string]any{
			"title": existing.Title, "trigger": "online_edit", "index_stale": indexStale,
		})
	return existing, nil
}

// RelearnKnowledge rebuilds the index from the edited metadata content — the
// manual "重新学习" trigger that resolves the index_stale state. It reuses the
// manual:process pipeline, which is type-agnostic (chunks a provided markdown
// string with the KB's effective config).
func (s *knowledgeService) RelearnKnowledge(ctx context.Context,
	knowledgeID string,
) (*types.Knowledge, error) {
	existing, kb, err := loadKnowledgeWrite(ctx, s.repo, s.kbService, knowledgeID)
	if err != nil {
		return nil, err
	}
	if existing.Type == types.KnowledgeTypeFAQ {
		return nil, werrors.NewBadRequestError("FAQ 文档不支持重新学习")
	}
	meta, err := existing.ManualMetadata()
	if err != nil || meta == nil || strings.TrimSpace(meta.Content) == "" {
		return nil, werrors.NewBadRequestError("该文档没有可学习的编辑内容")
	}
	if isInFlightParseStatus(existing.ParseStatus) {
		return nil, werrors.NewBadRequestError("文档正在处理中，请稍后重试")
	}
	ctx, err = withKBWriteTenantInfo(ctx, kb, s.tenantRepo)
	if err != nil {
		return nil, err
	}
	tenantID := existing.TenantID

	resetKnowledgeForReparse(existing, kb)
	if existing.IsManual() {
		// A draft manual document that gets relearnt is published by definition —
		// the user explicitly asked for it to be indexed. SetManualMetadata
		// rewrites the whole metadata doc, which is the established behavior for
		// manual knowledge.
		meta.Status = types.ManualKnowledgeStatusPublish
		meta.IndexStale = false
		if err := existing.SetManualMetadata(meta); err != nil {
			logger.Errorf(ctx, "Failed to update metadata for relearn: %v", err)
			return nil, err
		}
	} else {
		// Merge path: clears index_stale without dropping pipeline-owned keys
		// (process_overrides, transfer state) from the metadata document.
		if err := existing.SetContentMetadata(meta.Content, meta.Version, false); err != nil {
			logger.Errorf(ctx, "Failed to clear index_stale: %v", err)
			return nil, err
		}
	}

	if err := s.repo.UpdateKnowledge(ctx, existing); err != nil {
		logger.Errorf(ctx, "Failed to persist knowledge state before relearn: %v", err)
		return nil, err
	}
	if err := s.repo.UpdateKnowledgeColumn(ctx, existing.ID, "pending_subtasks_count", 0); err != nil {
		logger.Errorf(ctx, "Failed to reset pending_subtasks_count before relearn: %v", err)
		return nil, err
	}

	if _, err := s.enqueueManualProcessing(ctx, existing, meta.Content, true); err != nil {
		logger.Errorf(ctx, "Failed to enqueue relearn task: %v", err)
		s.markKnowledgeEnqueueFailed(ctx, existing)
		recordKBActivity(ctx, s.audit, tenantID, existing.KnowledgeBaseID, types.AuditActionKnowledgeReparseStarted,
			"knowledge", existing.ID, types.AuditOutcomeFailed, map[string]any{
				"title": existing.Title, "trigger": "relearn", "failure_stage": "enqueue",
			})
		return nil, werrors.NewInternalServerError("Failed to submit relearn task")
	}
	recordKBActivity(ctx, s.audit, tenantID, existing.KnowledgeBaseID, types.AuditActionKnowledgeReparseStarted,
		"knowledge", existing.ID, types.AuditOutcomeAccepted, map[string]any{
			"title": existing.Title, "trigger": "relearn", "processing_status": "pending",
		})
	return existing, nil
}

// contentImageMaxSize bounds one online-editor image upload (10 MiB).
const contentImageMaxSize = 10 << 20

// ensureDisplayFileName keeps a renamed non-manual document's display name
// aligned with its original extension so a downloaded file keeps a usable
// suffix. A title that already carries an extension wins as-is.
func ensureDisplayFileName(title, current string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return current
	}
	if filepath.Ext(trimmed) != "" {
		return trimmed
	}
	if ext := filepath.Ext(current); ext != "" {
		return trimmed + ext
	}
	return trimmed
}

// contentImageExts whitelists the image extensions the editor accepts. SVG is
// included even though content sniffing cannot recognize it — it is a vector
// text format — so the extension check is the gate and the sniffed type must
// still not contradict it with a non-image payload (see below).
var contentImageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".bmp": true, ".svg": true, ".avif": true,
}

// UploadContentImage stores one image pasted/picked in the online editor and
// returns its stable serving reference (resource:// when the catalog-backed
// file service is active, otherwise the provider URL — the reader hydrates
// both). The image is registered under the KB owner tenant but deliberately
// NOT bound to any knowledge yet: the body does not reference it until the
// next save, and bindContentResources claims it at that point.
func (s *knowledgeService) UploadContentImage(ctx context.Context,
	kbID string, fileHeader *multipart.FileHeader,
) (string, error) {
	if fileHeader == nil {
		return "", werrors.NewBadRequestError("请选择要上传的图片")
	}
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if !contentImageExts[ext] {
		return "", werrors.NewValidationError("仅支持 png/jpg/jpeg/gif/webp/bmp/svg/avif 图片")
	}
	if fileHeader.Size <= 0 || fileHeader.Size > contentImageMaxSize {
		return "", werrors.NewValidationError(
			fmt.Sprintf("图片大小需在 0~%dMB 之间", contentImageMaxSize>>20))
	}

	src, err := fileHeader.Open()
	if err != nil {
		logger.Errorf(ctx, "Failed to open uploaded image: %v", err)
		return "", werrors.NewBadRequestError("无法读取上传的图片")
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, contentImageMaxSize+1))
	if err != nil || len(data) == 0 || len(data) > contentImageMaxSize {
		return "", werrors.NewValidationError("图片读取失败或超出大小限制")
	}
	// Sniffed type must agree it is an image; SVG sniffs as text, so exempt the
	// extension that carries no reliable signature.
	if sniffed := http.DetectContentType(data); !strings.HasPrefix(sniffed, "image/") && ext != ".svg" {
		return "", werrors.NewValidationError("文件内容不是有效的图片")
	}

	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge base: %v", err)
		return "", err
	}
	if err := s.checkStorageEngineConfigured(ctx, kb); err != nil {
		return "", err
	}
	ctx, err = withKBWriteTenantInfo(ctx, kb, s.tenantRepo)
	if err != nil {
		return "", err
	}

	fileSvc := s.resolveFileService(ctx, kb)
	ref, err := fileSvc.SaveBytes(ctx, data, kb.TenantID, uuid.NewString()+ext, false)
	if err != nil {
		logger.Errorf(ctx, "Failed to store content image: %v", err)
		return "", werrors.NewInternalServerError("图片存储失败")
	}
	logger.Infof(ctx, "Content image stored: kb=%s ref=%s size=%d", kbID, ref, len(data))
	return ref, nil
}
