package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Cross-workspace transfer sentinels. The handler maps each to an HTTP status
// and, for blockers, a structured 409 body the UI renders item by item.
var (
	// ErrKBWorkspaceTransferRequired tells the legacy transfer-owner endpoint
	// that the target lives in another workspace, so only the whole-KB move
	// (transfer-workspace) can satisfy the request.
	ErrKBWorkspaceTransferRequired = errors.New(
		"kb transfer: target lives in another workspace; use workspace transfer")
	// ErrKBWorkspaceTransferSameTenant: transfer-workspace was called for a
	// target that already lives in the KB's workspace. The plain
	// transfer-owner endpoint is the right call there.
	ErrKBWorkspaceTransferSameTenant = errors.New(
		"kb workspace transfer: target already belongs to the knowledge base workspace")
	// ErrKBWorkspaceTransferTargetInactive: the target account or workspace is
	// not active.
	ErrKBWorkspaceTransferTargetInactive = errors.New(
		"kb workspace transfer: target account or workspace is not active")
	// ErrKBWorkspaceTransferUnsupported: the runtime cannot perform a move
	// (no database handle, or a storage layout outside local).
	ErrKBWorkspaceTransferUnsupported = errors.New(
		"kb workspace transfer: this deployment cannot move knowledge bases")
	// ErrKBWorkspaceTransferInProgress: another transfer holds the per-KB lock.
	ErrKBWorkspaceTransferInProgress = errors.New(
		"kb workspace transfer: another transfer is already running for this knowledge base")
)

// KBWorkspaceTransferBlockedError carries the hard preflight blockers. It is a
// 409 at the HTTP layer: the request is well-formed but the KB cannot be moved
// as it stands.
type KBWorkspaceTransferBlockedError struct {
	Blockers []types.KBWorkspaceTransferBlocker
}

func (e *KBWorkspaceTransferBlockedError) Error() string {
	if e == nil || len(e.Blockers) == 0 {
		return "kb workspace transfer: blocked"
	}
	codes := make([]string, 0, len(e.Blockers))
	for _, blocker := range e.Blockers {
		codes = append(codes, blocker.Code)
	}
	return "kb workspace transfer blocked: " + strings.Join(codes, ", ")
}

const (
	kbWorkspaceTransferMaxBytes     int64 = 2 << 30 // 2 GiB
	kbWorkspaceTransferMaxKnowledge       = 20000
	kbWorkspaceTransferDiskHeadroom int64 = 64 << 20
)

// kbWorkspaceTransferLocks serialises transfers of one KB inside this process.
// A move is a multi-second file copy plus a cross-table transaction; two
// concurrent requests would otherwise race on the same destination paths.
var kbWorkspaceTransferLocks sync.Map // map[string]*sync.Mutex

func kbWorkspaceTransferMutex(kbID string) *sync.Mutex {
	value, _ := kbWorkspaceTransferLocks.LoadOrStore(kbID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// kbWorkspaceTransferContext is everything preflight resolved and execution
// needs: the KB row used for the CAS, the target identity/workspace, the
// resolved destination storage backend and the data snapshot.
type kbWorkspaceTransferContext struct {
	kb            *types.KnowledgeBase
	caller        types.Caller
	sourceTenant  *types.Tenant
	targetTenant  *types.Tenant
	targetUser    *types.User
	targetBackend *types.StorageBackend
	snapshot      *kbWorkspaceTransferSnapshot
	fileSet       *kbWorkspaceFileSet
	plan          *types.KBWorkspaceTransferPlan
}

type kbWorkspaceTransferSnapshot struct {
	stats           types.KBWorkspaceTransferStats
	storageBytes    int64
	knowledgeIDs    []string // every row, including soft-deleted, for bindings
	resourceRows    []kbWorkspaceTransferResourceRow
	shareOrgIDs     []string
	sharesKept      int64
	processingCount int64
	pendingOps      int64
}

type kbWorkspaceTransferResourceRow struct {
	ID               string  `gorm:"column:id"`
	PhysicalPath     string  `gorm:"column:physical_path"`
	TenantID         uint64  `gorm:"column:tenant_id"`
	StorageBackendID *string `gorm:"column:storage_backend_id"`
	Provider         string  `gorm:"column:provider"`
	Size             int64   `gorm:"column:size"`
}

// BuildKnowledgeBaseWorkspaceTransferPlan is the read-only preflight. Authz
// mirrors the transfer itself; the returned plan is safe to show verbatim.
func (s *knowledgeBaseService) BuildKnowledgeBaseWorkspaceTransferPlan(
	ctx context.Context, kbID, newOwnerUserID string,
) (*types.KBWorkspaceTransferPlan, error) {
	tc, err := s.prepareWorkspaceTransfer(ctx, kbID, newOwnerUserID)
	if err != nil {
		return nil, err
	}
	return tc.plan, nil
}

// TransferKnowledgeBaseWorkspace moves the whole KB — row, content rows, files
// and share anchors — into the target user's home workspace. Files are copied
// before the database transaction so a failed copy leaves the row and its
// content untouched; after a failed transaction the copies are removed.
func (s *knowledgeBaseService) TransferKnowledgeBaseWorkspace(
	ctx context.Context, kbID, newOwnerUserID string,
) (*types.KBWorkspaceTransferResult, error) {
	if s.db == nil {
		return nil, ErrKBWorkspaceTransferUnsupported
	}
	lock := kbWorkspaceTransferMutex(strings.TrimSpace(kbID))
	if !lock.TryLock() {
		return nil, ErrKBWorkspaceTransferInProgress
	}
	defer lock.Unlock()

	tc, err := s.prepareWorkspaceTransfer(ctx, kbID, newOwnerUserID)
	if err != nil {
		return nil, err
	}
	if !tc.plan.MovesWorkspace {
		return nil, ErrKBWorkspaceTransferSameTenant
	}
	if len(tc.plan.Blockers) > 0 {
		return nil, &KBWorkspaceTransferBlockedError{Blockers: tc.plan.Blockers}
	}

	logger.Infof(ctx,
		"[KBWorkspaceTransfer] moving kb=%s tenant=%d->%d creator=%s->%s files=%d bytes=%d",
		tc.kb.ID, tc.kb.TenantID, tc.targetTenant.ID, tc.kb.CreatorID,
		tc.targetUser.ID, tc.plan.Stats.FileCount, tc.plan.Stats.FileBytes)

	baseDir := kbWorkspaceTransferBaseDir()
	copied, err := copyKBWorkspaceTransferFiles(
		baseDir, tc.fileSet, tc.kb.TenantID, tc.targetTenant.ID,
	)
	if err != nil {
		removeKBWorkspaceTransferCopies(baseDir, copied)
		return nil, fmt.Errorf("copy workspace files: %w", err)
	}

	sharesReanchored, err := s.moveKnowledgeBaseWorkspaceTx(ctx, tc)
	if err != nil {
		removeKBWorkspaceTransferCopies(baseDir, copied)
		return nil, err
	}

	// Post-commit work is idempotent best effort: the database already points
	// at the new workspace, so a failure here only leaves harmless residue.
	postCtx := context.WithoutCancel(ctx)
	if err := removeKBWorkspaceTransferSources(baseDir, tc.fileSet); err != nil {
		logger.Warnf(postCtx, "[KBWorkspaceTransfer] source file cleanup for kb=%s: %v", tc.kb.ID, err)
	}
	s.reregisterKBWorkspaceTransferDataSources(postCtx, tc.kb.ID)
	s.recordKBWorkspaceTransferAudit(postCtx, tc, sharesReanchored)

	return &types.KBWorkspaceTransferResult{
		KnowledgeBaseID:  tc.kb.ID,
		FromTenantID:     tc.kb.TenantID,
		ToTenantID:       tc.targetTenant.ID,
		NewOwnerUserID:   tc.targetUser.ID,
		Stats:            tc.plan.Stats,
		SharesReanchored: sharesReanchored,
	}, nil
}

// prepareWorkspaceTransfer runs authz, target resolution and the read-only
// preflight. The executing endpoint re-runs it so a stale UI cannot move a KB
// that changed since its plan was shown.
func (s *knowledgeBaseService) prepareWorkspaceTransfer(
	ctx context.Context, kbID, newOwnerUserID string,
) (*kbWorkspaceTransferContext, error) {
	kbID = strings.TrimSpace(kbID)
	newOwnerUserID = strings.TrimSpace(newOwnerUserID)
	if kbID == "" {
		return nil, errors.New("knowledge base ID cannot be empty")
	}
	if newOwnerUserID == "" {
		return nil, ErrKBOwnerTransferNotMember
	}
	if types.IsSyntheticUserID(newOwnerUserID) {
		return nil, ErrKBOwnerTransferSynthetic
	}
	// JWT-only defense in depth: the route is not declared to the API-key
	// authorizer (default-deny), and the service refuses a machine principal
	// that somehow reached it.
	if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		return nil, ErrKBOwnerTransferForbidden
	}
	if s.db == nil || s.tenantRepo == nil || s.tenantMemberService == nil {
		return nil, ErrKBWorkspaceTransferUnsupported
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, ErrKBOwnerTransferForbidden
	}

	kb, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, kbID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeKBOwnershipTransfer(ctx, kb); err != nil {
		return nil, err
	}

	caller := types.CallerFromContext(ctx)
	if newOwnerUserID == kb.CreatorID && caller.UserID != newOwnerUserID {
		// Prevents an Admin from "transferring" a KB to the person who
		// already owns it; the self-move repair path belongs to the creator.
		return nil, ErrKBOwnerTransferTargetSelf
	}

	targetUser, err := s.getWorkspaceTransferTargetUser(ctx, newOwnerUserID)
	if err != nil {
		return nil, err
	}
	if !targetUser.IsActive {
		return nil, ErrKBWorkspaceTransferTargetInactive
	}
	targetTenant, err := s.tenantRepo.GetTenantByID(ctx, targetUser.TenantID)
	if err != nil {
		return nil, err
	}
	sourceTenant, err := s.tenantRepo.GetTenantByID(ctx, kb.TenantID)
	if err != nil {
		return nil, err
	}

	tc := &kbWorkspaceTransferContext{
		kb:           kb,
		caller:       caller,
		sourceTenant: sourceTenant,
		targetTenant: targetTenant,
		targetUser:   targetUser,
	}
	plan := &types.KBWorkspaceTransferPlan{
		SourceTenantID:   kb.TenantID,
		SourceTenantName: sourceTenant.Name,
		TargetTenantID:   targetTenant.ID,
		TargetTenantName: targetTenant.Name,
		TargetUserID:     targetUser.ID,
		TargetUsername:   targetUser.Username,
		Blockers:         []types.KBWorkspaceTransferBlocker{},
	}
	tc.plan = plan

	if targetTenant.ID == kb.TenantID {
		if newOwnerUserID == kb.CreatorID {
			return nil, ErrKBOwnerTransferTargetSelf
		}
		// Same workspace: the plain transfer-owner endpoint changes only the
		// creator. The plan still reports what the transfer would touch.
		plan.MovesWorkspace = false
		snapshot, snapErr := s.collectWorkspaceTransferSnapshot(ctx, kb)
		if snapErr != nil {
			return nil, snapErr
		}
		tc.snapshot = snapshot
		plan.Stats = snapshot.stats
		plan.SharesKept = snapshot.sharesKept
		return tc, nil
	}
	plan.MovesWorkspace = true

	if targetTenant.Status != "active" {
		return nil, ErrKBWorkspaceTransferTargetInactive
	}
	member, err := s.tenantMemberService.GetMembership(ctx, targetUser.ID, targetTenant.ID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, ErrKBOwnerTransferNotMember
	}
	if member.Status != types.TenantMemberStatusActive {
		return nil, ErrKBOwnerTransferInactive
	}
	if !member.Role.HasPermission(types.TenantRoleContributor) {
		// Never promote someone's role in a workspace that is not ours to
		// change; their own workspace admin has to do it first.
		return nil, ErrKBOwnerTransferTargetRole
	}

	snapshot, err := s.collectWorkspaceTransferSnapshot(ctx, kb)
	if err != nil {
		return nil, err
	}
	tc.snapshot = snapshot
	plan.Stats = snapshot.stats
	plan.SharesKept = snapshot.sharesKept

	backend, err := s.resolveWorkspaceTransferBackend(ctx, targetTenant)
	if err != nil {
		return nil, err
	}
	tc.targetBackend = backend

	fileSet, err := buildKBWorkspaceFileSet(
		kbWorkspaceTransferBaseDir(), kb.TenantID, snapshot.resourceRows,
		snapshot.knowledgeIDs,
	)
	if err != nil {
		return nil, err
	}
	tc.fileSet = fileSet
	plan.Stats.FileCount = int64(len(fileSet.rels))
	plan.Stats.FileBytes = fileSet.totalBytes
	if len(fileSet.missing) > 0 {
		plan.Warnings = append(plan.Warnings, types.KBWorkspaceTransferBlocker{
			Code:    "source_file_missing",
			Message: "some stored files are missing on disk; they will stay missing after the move",
			Details: map[string]any{"count": len(fileSet.missing), "sample": sampleStrings(fileSet.missing, 10)},
		})
	}

	blockers, err := s.workspaceTransferBlockers(ctx, tc)
	if err != nil {
		return nil, err
	}
	plan.Blockers = blockers
	return tc, nil
}

func (s *knowledgeBaseService) getWorkspaceTransferTargetUser(
	ctx context.Context, userID string,
) (*types.User, error) {
	var user types.User
	err := s.db.WithContext(ctx).
		Select("id", "username", "email", "tenant_id", "is_active").
		Where("id = ?", userID).
		Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrKBOwnerTransferNotMember
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// resolveWorkspaceTransferBackend finds the destination workspace's active
// local backend. A missing backend is a preflight blocker, not an error here.
func (s *knowledgeBaseService) resolveWorkspaceTransferBackend(
	ctx context.Context, targetTenant *types.Tenant,
) (*types.StorageBackend, error) {
	if s.storageResolver == nil {
		return nil, nil
	}
	backend, err := s.storageResolver.ResolveBackend(ctx, targetTenant, "", "local")
	if err != nil {
		logger.Warnf(ctx, "[KBWorkspaceTransfer] resolve storage backend tenant=%d: %v", targetTenant.ID, err)
		return nil, nil
	}
	return backend, nil
}

func (s *knowledgeBaseService) collectWorkspaceTransferSnapshot(
	ctx context.Context, kb *types.KnowledgeBase,
) (*kbWorkspaceTransferSnapshot, error) {
	db := s.db.WithContext(ctx)
	snapshot := &kbWorkspaceTransferSnapshot{}

	live := "knowledge_base_id = ? AND deleted_at IS NULL"
	if err := db.Table("knowledges").Where(live, kb.ID).Count(&snapshot.stats.KnowledgeCount).Error; err != nil {
		return nil, err
	}
	if err := db.Table("chunks").Where(live, kb.ID).Count(&snapshot.stats.ChunkCount).Error; err != nil {
		return nil, err
	}
	if err := db.Table("wiki_pages").Where(live, kb.ID).Count(&snapshot.stats.WikiPageCount).Error; err != nil {
		return nil, err
	}
	if err := db.Table("knowledges").Where(live, kb.ID).
		Select("COALESCE(SUM(storage_size), 0)").Scan(&snapshot.storageBytes).Error; err != nil {
		return nil, err
	}
	if err := db.Table("knowledges").Where("knowledge_base_id = ?", kb.ID).
		Pluck("id", &snapshot.knowledgeIDs).Error; err != nil {
		return nil, err
	}

	knowledgeIDs := db.Table("knowledges").Select("id").Where("knowledge_base_id = ?", kb.ID)
	if err := db.Table("resources AS r").
		Select("r.id, r.physical_path, r.tenant_id, r.storage_backend_id, r.provider, r.size").
		Joins("JOIN resource_bindings AS b ON b.resource_id = r.id").
		Where("b.owner_type = ? AND b.owner_id IN (?)", types.ResourceOwnerKnowledge, knowledgeIDs).
		Group("r.id, r.physical_path, r.tenant_id, r.storage_backend_id, r.provider, r.size").
		Scan(&snapshot.resourceRows).Error; err != nil {
		return nil, err
	}

	var shareCount int64
	if err := db.Table("kb_shares").Where("knowledge_base_id = ? AND deleted_at IS NULL", kb.ID).
		Count(&shareCount).Error; err != nil {
		return nil, err
	}
	snapshot.sharesKept = shareCount
	if err := db.Table("kb_shares").Where("knowledge_base_id = ? AND deleted_at IS NULL", kb.ID).
		Distinct().Pluck("organization_id", &snapshot.shareOrgIDs).Error; err != nil {
		return nil, err
	}

	if err := db.Table("knowledges").
		Where("knowledge_base_id = ? AND deleted_at IS NULL AND parse_status IN ?", kb.ID,
			[]string{
				types.ParseStatusPending, types.ParseStatusProcessing,
				types.ParseStatusFinalizing, types.ParseStatusDeleting,
			}).
		Count(&snapshot.processingCount).Error; err != nil {
		return nil, err
	}
	if err := db.Table("task_pending_ops").
		Where("scope = ? AND scope_id = ?", types.TaskScopeKnowledgeBase, kb.ID).
		Count(&snapshot.pendingOps).Error; err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *knowledgeBaseService) workspaceTransferBlockers(
	ctx context.Context, tc *kbWorkspaceTransferContext,
) ([]types.KBWorkspaceTransferBlocker, error) {
	snapshot := tc.snapshot
	kb := tc.kb
	blockers := make([]types.KBWorkspaceTransferBlocker, 0, 6)
	add := func(code, message string, details map[string]any) {
		blockers = append(blockers, types.KBWorkspaceTransferBlocker{
			Code: code, Message: message, Details: details,
		})
	}

	if snapshot.processingCount > 0 || snapshot.pendingOps > 0 {
		add("kb_processing",
			"the knowledge base still has documents being processed or queued work",
			map[string]any{
				"documents":   snapshot.processingCount,
				"pending_ops": snapshot.pendingOps,
			})
	}

	if providers := unsupportedResourceProviders(snapshot.resourceRows); len(providers) > 0 {
		add("storage_provider_unsupported",
			"only local storage can be moved between workspaces",
			map[string]any{"providers": providers})
	}

	if tc.fileSet != nil && len(tc.fileSet.outside) > 0 {
		add("resource_path_outside_workspace",
			"some stored files live outside the workspace they belong to",
			map[string]any{"count": len(tc.fileSet.outside), "sample": sampleStrings(tc.fileSet.outside, 10)})
	}

	if kb.VectorStoreID != nil && strings.TrimSpace(*kb.VectorStoreID) != "" {
		add("vector_store_bound",
			"the knowledge base is bound to a dedicated vector store",
			map[string]any{"vector_store_id": strings.TrimSpace(*kb.VectorStoreID)})
	}

	if !sameRetrieverEngines(tc.sourceTenant, tc.targetTenant) {
		add("retrieval_engine_mismatch",
			"the two workspaces use different retrieval engines",
			nil)
	}

	if tc.targetBackend == nil || strings.ToLower(strings.TrimSpace(tc.targetBackend.Provider)) != "local" {
		add("target_storage_backend_missing",
			"the target workspace has no active local storage backend", nil)
	}

	var imCount int64
	if err := s.db.WithContext(ctx).Table("im_channels").
		Where("tenant_id = ? AND knowledge_base_id = ? AND enabled = ? AND deleted_at IS NULL",
			kb.TenantID, kb.ID, true).
		Count(&imCount).Error; err != nil {
		return nil, err
	}
	if imCount > 0 {
		add("im_channel_bound",
			"an enabled IM channel in the current workspace points at this knowledge base",
			map[string]any{"count": imCount})
	}

	agents, err := s.agentsReferencingKB(ctx, kb)
	if err != nil {
		return nil, err
	}
	if len(agents) > 0 {
		add("agent_bound", "custom agents in the current workspace reference this knowledge base",
			map[string]any{"count": len(agents), "agents": agents})
	}

	apiKeys, err := s.apiKeysReferencingKB(ctx, kb)
	if err != nil {
		return nil, err
	}
	if len(apiKeys) > 0 {
		add("api_key_bound", "API keys in the current workspace are scoped to this knowledge base",
			map[string]any{"count": len(apiKeys), "api_keys": apiKeys})
	}

	mcpEndpoints, err := s.mcpEndpointsReferencingKB(ctx, kb)
	if err != nil {
		return nil, err
	}
	if len(mcpEndpoints) > 0 {
		add("mcp_endpoint_bound", "MCP endpoints in the current workspace expose this knowledge base",
			map[string]any{"count": len(mcpEndpoints), "endpoints": mcpEndpoints})
	}

	missingModels, err := s.modelsUnavailableInTarget(ctx, kb, tc.targetTenant.ID)
	if err != nil {
		return nil, err
	}
	if len(missingModels) > 0 {
		add("private_model_unavailable",
			"the knowledge base references private models the target workspace does not have",
			map[string]any{"model_ids": missingModels})
	}

	shared, err := s.resourceSharedOutsideKB(ctx, kb.ID)
	if err != nil {
		return nil, err
	}
	if shared > 0 {
		add("resource_shared", "some stored files are also used by another owner",
			map[string]any{"bindings": shared})
	}

	if quota := tc.targetTenant.StorageQuota; quota > 0 &&
		tc.targetTenant.StorageUsed+snapshot.storageBytes > quota {
		add("target_quota_exceeded", "the target workspace does not have enough storage quota",
			map[string]any{
				"quota": quota, "used": tc.targetTenant.StorageUsed, "required": snapshot.storageBytes,
			})
	}

	missingOrgs, err := s.shareOrganizationsMissingTarget(ctx, snapshot.shareOrgIDs, tc.targetTenant.ID)
	if err != nil {
		return nil, err
	}
	if len(missingOrgs) > 0 {
		add("share_org_membership_missing",
			"the target workspace is not a member of every space this knowledge base is shared into",
			map[string]any{"organization_ids": missingOrgs})
	}

	if tc.fileSet != nil && (tc.fileSet.totalBytes > kbWorkspaceTransferMaxBytes ||
		len(snapshot.knowledgeIDs) > kbWorkspaceTransferMaxKnowledge) {
		add("too_large", "the knowledge base exceeds the single-move size limit",
			map[string]any{
				"file_bytes":      tc.fileSet.totalBytes,
				"max_file_bytes":  kbWorkspaceTransferMaxBytes,
				"knowledge_count": len(snapshot.knowledgeIDs),
				"max_knowledge":   kbWorkspaceTransferMaxKnowledge,
			})
	}

	if tc.fileSet != nil && tc.fileSet.totalBytes > 0 {
		if free, ok := localDiskFreeBytes(kbWorkspaceTransferBaseDir()); ok &&
			free < tc.fileSet.totalBytes+kbWorkspaceTransferDiskHeadroom {
			add("disk_space_insufficient", "the storage volume does not have enough free space",
				map[string]any{"free_bytes": free, "required": tc.fileSet.totalBytes})
		}
	}

	return blockers, nil
}

func unsupportedResourceProviders(rows []kbWorkspaceTransferResourceRow) []string {
	seen := make(map[string]struct{}, 2)
	providers := make([]string, 0, 2)
	for _, row := range rows {
		provider := strings.ToLower(strings.TrimSpace(row.Provider))
		if provider == "" {
			provider = strings.ToLower(types.ParseProviderScheme(row.PhysicalPath))
		}
		if provider == "local" || provider == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		providers = append(providers, provider)
	}
	return providers
}

func sameRetrieverEngines(source, target *types.Tenant) bool {
	if source == nil || target == nil {
		return false
	}
	left, err := json.Marshal(source.GetEffectiveEngines())
	if err != nil {
		return false
	}
	right, err := json.Marshal(target.GetEffectiveEngines())
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

func (s *knowledgeBaseService) agentsReferencingKB(
	ctx context.Context, kb *types.KnowledgeBase,
) ([]string, error) {
	var rows []struct {
		ID     string `gorm:"column:id"`
		Name   string `gorm:"column:name"`
		Config string `gorm:"column:config"`
	}
	if err := s.db.WithContext(ctx).Table("custom_agents").
		Select("id, name, config").
		Where("tenant_id = ? AND deleted_at IS NULL", kb.TenantID).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	names := make([]string, 0, 2)
	for _, row := range rows {
		var config struct {
			KnowledgeBases []string `json:"knowledge_bases"`
		}
		if err := json.Unmarshal([]byte(row.Config), &config); err != nil {
			continue
		}
		if kbTransferContainsString(config.KnowledgeBases, kb.ID) {
			names = append(names, firstNonEmpty(row.Name, row.ID))
		}
	}
	return names, nil
}

func (s *knowledgeBaseService) apiKeysReferencingKB(
	ctx context.Context, kb *types.KnowledgeBase,
) ([]string, error) {
	var rows []struct {
		ID               uint64 `gorm:"column:id"`
		Name             string `gorm:"column:name"`
		KnowledgeBaseIDs string `gorm:"column:knowledge_base_ids"`
	}
	if err := s.db.WithContext(ctx).Table("tenant_api_keys").
		Select("id, name, knowledge_base_ids").
		Where("tenant_id = ? AND revoked_at IS NULL", kb.TenantID).
		Where("(expires_at IS NULL OR expires_at > ?)", time.Now()).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	names := make([]string, 0, 2)
	for _, row := range rows {
		var ids []string
		if err := json.Unmarshal([]byte(row.KnowledgeBaseIDs), &ids); err != nil {
			continue
		}
		if kbTransferContainsString(ids, kb.ID) {
			names = append(names, firstNonEmpty(row.Name, strconv.FormatUint(row.ID, 10)))
		}
	}
	return names, nil
}

func (s *knowledgeBaseService) mcpEndpointsReferencingKB(
	ctx context.Context, kb *types.KnowledgeBase,
) ([]string, error) {
	var rows []struct {
		ID               string `gorm:"column:id"`
		Name             string `gorm:"column:name"`
		KnowledgeBaseIDs string `gorm:"column:knowledge_base_ids"`
	}
	if err := s.db.WithContext(ctx).Table("mcp_endpoints").
		Select("id, name, knowledge_base_ids").
		Where("tenant_id = ? AND deleted_at IS NULL AND enabled = ?", kb.TenantID, true).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	names := make([]string, 0, 2)
	for _, row := range rows {
		var ids []string
		if err := json.Unmarshal([]byte(row.KnowledgeBaseIDs), &ids); err != nil {
			continue
		}
		if kbTransferContainsString(ids, kb.ID) {
			names = append(names, firstNonEmpty(row.Name, row.ID))
		}
	}
	return names, nil
}

// modelsUnavailableInTarget lists non-builtin model ids the KB references that
// the target workspace cannot resolve. Builtin models are global by design.
func (s *knowledgeBaseService) modelsUnavailableInTarget(
	ctx context.Context, kb *types.KnowledgeBase, targetTenantID uint64,
) ([]string, error) {
	ids := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(kb.EmbeddingModelID)
	add(kb.SummaryModelID)
	add(kb.VLMConfig.ModelID)
	add(kb.ASRConfig.ModelID)
	add(kb.ImageProcessingConfig.ModelID)
	if kb.AutoTagConfig != nil {
		add(kb.AutoTagConfig.ModelID)
	}
	if kb.WikiConfig != nil {
		add(kb.WikiConfig.SynthesisModelID)
	}
	var knowledgeModelIDs []string
	if err := s.db.WithContext(ctx).Table("knowledges").
		Where("knowledge_base_id = ? AND deleted_at IS NULL AND embedding_model_id <> ''", kb.ID).
		Distinct().Pluck("embedding_model_id", &knowledgeModelIDs).Error; err != nil {
		return nil, err
	}
	for _, id := range knowledgeModelIDs {
		add(id)
	}

	missing := make([]string, 0, 2)
	db := s.db.WithContext(ctx)
	for _, id := range ids {
		var count int64
		if err := db.Table("models").
			Where("id = ? AND (tenant_id = ? OR is_builtin = ?) AND deleted_at IS NULL",
				id, targetTenantID, true).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

func (s *knowledgeBaseService) resourceSharedOutsideKB(
	ctx context.Context, kbID string,
) (int64, error) {
	db := s.db.WithContext(ctx)
	resourceIDs := db.Table("resource_bindings AS b2").
		Select("b2.resource_id").
		Joins("JOIN knowledges AS k ON k.id = b2.owner_id").
		Where("b2.owner_type = ? AND k.knowledge_base_id = ?", types.ResourceOwnerKnowledge, kbID)
	knowledgeIDs := db.Table("knowledges").Select("id").Where("knowledge_base_id = ?", kbID)
	var count int64
	err := db.Table("resource_bindings AS b").
		Where("b.resource_id IN (?)", resourceIDs).
		Where("NOT (b.owner_type = ? AND b.owner_id IN (?))", types.ResourceOwnerKnowledge, knowledgeIDs).
		Count(&count).Error
	return count, err
}

func (s *knowledgeBaseService) shareOrganizationsMissingTarget(
	ctx context.Context, orgIDs []string, targetTenantID uint64,
) ([]string, error) {
	if len(orgIDs) == 0 {
		return nil, nil
	}
	db := s.db.WithContext(ctx)
	missing := make([]string, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		var count int64
		if err := db.Table("organization_tenant_members").
			Where("organization_id = ? AND tenant_id = ?", orgID, targetTenantID).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			missing = append(missing, orgID)
		}
	}
	return missing, nil
}

func kbTransferContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sampleStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func kbWorkspaceTransferBaseDir() string {
	baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	if baseDir == "" {
		baseDir = "/data/files"
	}
	return baseDir
}

// moveKnowledgeBaseWorkspaceTx performs the single transaction that re-tenants
// the KB and every row that belongs to it. The KB update is a compare-and-swap
// on (tenant, creator, updated_at) so a concurrent edit or transfer aborts the
// move before any child row is touched.
func (s *knowledgeBaseService) moveKnowledgeBaseWorkspaceTx(
	ctx context.Context, tc *kbWorkspaceTransferContext,
) (int64, error) {
	oldTenant := tc.kb.TenantID
	newTenant := tc.targetTenant.ID
	newBackendID := tc.targetBackend.ID
	sharesReanchored := tc.snapshot.sharesKept

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&types.KnowledgeBase{}).
			Where("id = ? AND tenant_id = ? AND COALESCE(creator_id, '') = ? AND updated_at = ?",
				tc.kb.ID, oldTenant, tc.kb.CreatorID, tc.kb.UpdatedAt).
			Updates(map[string]any{
				"tenant_id":          newTenant,
				"creator_id":         tc.targetUser.ID,
				"storage_backend_id": newBackendID,
				"updated_at":         time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrKBOwnerTransferConflict
		}

		for _, table := range []string{
			"knowledges",
			"chunks",
			"chunk_revisions",
			"knowledge_content_versions",
			"knowledge_tags",
			"wiki_folders",
			"wiki_pages",
			"wiki_page_issues",
			"wiki_page_revisions",
			"data_sources",
		} {
			if err := updateKBWorkspaceTenant(tx, table, tc.kb.ID, newTenant); err != nil {
				return err
			}
		}
		// The chunks trigger rewrites chunk_images on PostgreSQL/SQLite; run
		// the explicit update afterwards so a trigger-less or failed
		// projection still ends up in the new workspace.
		if err := updateKBWorkspaceTenant(tx, "chunk_images", tc.kb.ID, newTenant); err != nil {
			return err
		}
		if err := tx.Table("sync_logs").
			Where("data_source_id IN (?)",
				tx.Table("data_sources").Select("id").Where("knowledge_base_id = ?", tc.kb.ID)).
			Update("tenant_id", newTenant).Error; err != nil {
			return err
		}

		if err := s.rewriteKBWorkspaceResources(tx, tc, newBackendID); err != nil {
			return err
		}
		if err := rewriteKBWorkspacePathColumns(tx, tc, newBackendID); err != nil {
			return err
		}

		if err := tx.Table("resource_bindings").
			Where("resource_id IN (?)",
				tx.Table("resource_bindings AS b").Select("b.resource_id").
					Joins("JOIN knowledges AS k ON k.id = b.owner_id").
					Where("b.owner_type = ? AND k.knowledge_base_id = ?",
						types.ResourceOwnerKnowledge, tc.kb.ID)).
			Update("tenant_id", newTenant).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM resource_access_grants WHERE resource_id IN (
			SELECT b.resource_id FROM resource_bindings b
			JOIN knowledges k ON k.id = b.owner_id
			WHERE b.owner_type = ? AND k.knowledge_base_id = ?)`,
			types.ResourceOwnerKnowledge, tc.kb.ID).Error; err != nil {
			return err
		}

		if err := tx.Table("kb_shares").
			Where("knowledge_base_id = ?", tc.kb.ID).
			Update("source_tenant_id", newTenant).Error; err != nil {
			return err
		}

		// Per-user state is workspace-scoped: it cannot follow the KB without
		// granting old-workspace members a row in the new workspace.
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{"DELETE FROM user_kb_pins WHERE tenant_id = ? AND kb_id = ?", []any{oldTenant, tc.kb.ID}},
			{
				"DELETE FROM user_resource_favorites WHERE tenant_id = ? AND resource_type = ? AND resource_id = ?",
				[]any{oldTenant, "kb", tc.kb.ID},
			},
			{
				"DELETE FROM memory_doc_affinity WHERE tenant_id = ? AND knowledge_base_id = ?",
				[]any{oldTenant, tc.kb.ID},
			},
		} {
			if err := tx.Exec(statement.query, statement.args...).Error; err != nil {
				return err
			}
		}

		return adjustKBWorkspaceTransferQuota(tx, oldTenant, newTenant, tc.snapshot.storageBytes)
	})
	if err != nil {
		return 0, err
	}
	return sharesReanchored, nil
}

func updateKBWorkspaceTenant(tx *gorm.DB, table, kbID string, tenantID uint64) error {
	return tx.Table(table).Where("knowledge_base_id = ?", kbID).
		Update("tenant_id", tenantID).Error
}

func (s *knowledgeBaseService) rewriteKBWorkspaceResources(
	tx *gorm.DB, tc *kbWorkspaceTransferContext, newBackendID string,
) error {
	for _, row := range tc.snapshot.resourceRows {
		physicalPath, err := rewriteKBWorkspaceResourcePath(
			row.PhysicalPath, tc.kb.TenantID, tc.targetTenant.ID, newBackendID,
		)
		if err != nil {
			return err
		}
		if err := tx.Table("resources").Where("id = ?", row.ID).
			Updates(map[string]any{
				"tenant_id":          tc.targetTenant.ID,
				"storage_backend_id": newBackendID,
				"physical_path":      physicalPath,
				"location_hash":      resourceLocationHash(physicalPath),
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

func rewriteKBWorkspaceResourcePath(
	path string, oldTenant, newTenant uint64, newBackendID string,
) (string, error) {
	backendID, providerPath, scoped := types.ParseStorageBackendPath(path)
	inner := path
	if scoped {
		inner = providerPath
	}
	oldPrefix := fmt.Sprintf("local://%d/", oldTenant)
	if !strings.HasPrefix(inner, oldPrefix) {
		return "", fmt.Errorf("resource path %q is outside workspace %d", path, oldTenant)
	}
	newInner := fmt.Sprintf("local://%d/", newTenant) + strings.TrimPrefix(inner, oldPrefix)
	if scoped && backendID != "" {
		return "storage://" + newBackendID + "/" + newInner, nil
	}
	return newInner, nil
}

// rewriteKBWorkspacePathColumns rewrites legacy provider paths embedded in text
// columns. Current rows reference resources by stable resource:// handles, so
// this is a no-op on modern data; it keeps older rows (raw local:// URLs in
// chunk content, image_info or revisions) working after the move.
func rewriteKBWorkspacePathColumns(
	tx *gorm.DB, tc *kbWorkspaceTransferContext, newBackendID string,
) error {
	backendIDs := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	addBackend := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		backendIDs = append(backendIDs, id)
	}
	if tc.kb.StorageBackendID != nil {
		addBackend(*tc.kb.StorageBackendID)
	}
	for _, row := range tc.snapshot.resourceRows {
		if backendID, _, ok := types.ParseStorageBackendPath(row.PhysicalPath); ok {
			addBackend(backendID)
		}
	}

	columns := []struct {
		table   string
		columns []string
	}{
		{"knowledges", []string{"file_path"}},
		{"chunks", []string{"content", "source_content", "image_info"}},
		{"knowledge_content_versions", []string{"content"}},
		{"wiki_pages", []string{"content"}},
		{"wiki_page_revisions", []string{"content"}},
		{"chunk_images", []string{"url", "original_url"}},
	}
	oldPrefix := fmt.Sprintf("local://%d/", tc.kb.TenantID)
	newPrefix := fmt.Sprintf("local://%d/", tc.targetTenant.ID)
	for _, entry := range columns {
		for _, column := range entry.columns {
			expression := column
			args := make([]any, 0, len(backendIDs)*2+2)
			for _, backendID := range backendIDs {
				expression = fmt.Sprintf("REPLACE(%s, ?, ?)", expression)
				args = append(args,
					"storage://"+backendID+"/"+oldPrefix,
					"storage://"+newBackendID+"/"+newPrefix)
			}
			expression = fmt.Sprintf("REPLACE(%s, ?, ?)", expression)
			args = append(args, oldPrefix, newPrefix)

			if err := tx.Table(entry.table).
				Where("knowledge_base_id = ?", tc.kb.ID).
				Where(fmt.Sprintf("%s LIKE ?", column), "%"+oldPrefix+"%").
				Update(column, gorm.Expr(expression, args...)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func adjustKBWorkspaceTransferQuota(tx *gorm.DB, oldTenant, newTenant uint64, size int64) error {
	if size == 0 {
		return nil
	}
	oldUsed, err := readKBWorkspaceTenantStorageUsed(tx, oldTenant)
	if err != nil {
		return err
	}
	newUsed, err := readKBWorkspaceTenantStorageUsed(tx, newTenant)
	if err != nil {
		return err
	}
	oldUsed -= size
	if oldUsed < 0 {
		oldUsed = 0
	}
	newUsed += size
	if err := tx.Table("tenants").Where("id = ?", oldTenant).
		Update("storage_used", oldUsed).Error; err != nil {
		return err
	}
	return tx.Table("tenants").Where("id = ?", newTenant).
		Update("storage_used", newUsed).Error
}

func readKBWorkspaceTenantStorageUsed(tx *gorm.DB, tenantID uint64) (int64, error) {
	var used int64
	row := tx.Table("tenants").Select("storage_used").Where("id = ?", tenantID).Row()
	if err := row.Scan(&used); err != nil {
		return 0, err
	}
	return used, nil
}

func (s *knowledgeBaseService) reregisterKBWorkspaceTransferDataSources(
	ctx context.Context, kbID string,
) {
	if s.dsScheduler == nil || s.dsRepo == nil {
		return
	}
	sources, err := s.dsRepo.FindByKnowledgeBase(ctx, kbID)
	if err != nil {
		logger.Warnf(ctx, "[KBWorkspaceTransfer] list data sources for kb=%s: %v", kbID, err)
		return
	}
	for _, source := range sources {
		if source == nil {
			continue
		}
		// The cron closure captures TenantID at registration time, so the old
		// schedule would keep running as the source workspace.
		s.dsScheduler.Remove(source.ID)
		if source.Status != types.DataSourceStatusActive || source.SyncSchedule == "" {
			continue
		}
		if err := s.dsScheduler.AddOrUpdate(source); err != nil {
			logger.Warnf(ctx, "[KBWorkspaceTransfer] re-register data source %s: %v", source.ID, err)
		}
	}
}

func (s *knowledgeBaseService) recordKBWorkspaceTransferAudit(
	ctx context.Context, tc *kbWorkspaceTransferContext, sharesReanchored int64,
) {
	details := map[string]any{
		"old_tenant_id":     tc.kb.TenantID,
		"new_tenant_id":     tc.targetTenant.ID,
		"old_creator_id":    tc.kb.CreatorID,
		"new_creator_id":    tc.targetUser.ID,
		"knowledge_count":   tc.plan.Stats.KnowledgeCount,
		"chunk_count":       tc.plan.Stats.ChunkCount,
		"wiki_page_count":   tc.plan.Stats.WikiPageCount,
		"file_count":        tc.plan.Stats.FileCount,
		"file_bytes":        tc.plan.Stats.FileBytes,
		"shares_reanchored": sharesReanchored,
	}
	recordKBActivity(ctx, s.audit, tc.kb.TenantID, tc.kb.ID,
		types.AuditActionKBWorkspaceTransferred, "knowledge_base", tc.kb.ID,
		types.AuditOutcomeSuccess, details)
	if tc.targetTenant.ID != tc.kb.TenantID {
		recordKBActivity(ctx, s.audit, tc.targetTenant.ID, tc.kb.ID,
			types.AuditActionKBWorkspaceTransferred, "knowledge_base", tc.kb.ID,
			types.AuditOutcomeSuccess, details)
	}
}

// kbWorkspaceFileSet is the union of files the KB owns on local disk, keyed by
// paths relative to the storage base dir (e.g. "10000/<knowledgeID>/<file>").
type kbWorkspaceFileSet struct {
	rels       []string
	missing    []string
	outside    []string
	totalBytes int64
}
