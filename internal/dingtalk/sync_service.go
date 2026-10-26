package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// ErrSyncAlreadyRunning 已有同步进行中（手动触发与 cron 的并发保护）。
var ErrSyncAlreadyRunning = errors.New("dingtalk contact sync already running")

// unionIDPlaceholderPrefix 登录时拿不到企业内部 userid 的占位 ding_user_id 前缀
// （见 service.userService dingTalkIdentityKey）。占位记录无法用 userid 参与
// 通讯录对账，reconcile 阶段跳过，避免误禁用。
const unionIDPlaceholderPrefix = "union:"

// deptUserPageSize 部门成员分页大小（钉钉 /topapi/v2/user/list 上限 100）。
const deptUserPageSize = 100

// syncRunTimeout 单轮全量同步的硬上限。正常一轮约 1~2 分钟（326 人 /
// 193 部门）；跑到这个时长说明远端在持续超时或限流，继续挂着只会留下
// 一条永远 running 的记录——生产上观察到的 1h44m、9.5h 僵尸行即为此。
const syncRunTimeout = 30 * time.Minute

// finishRunTimeout 落终态的写库超时。
const finishRunTimeout = 10 * time.Second

// UserRepo 同步所需的本地用户读写最小接口（interfaces.UserRepository 已实现）。
// 本地定义小接口以避免 dingtalk 包反向依赖 application/repository。
type UserRepo interface {
	GetUserByID(ctx context.Context, id string) (*types.User, error)
	GetUserByEmail(ctx context.Context, email string) (*types.User, error)
	GetUserByUsername(ctx context.Context, username string) (*types.User, error)
	UpdateUser(ctx context.Context, user *types.User) error
}

// TokenRevoker 离职禁用时撤销用户全部会话令牌
// （interfaces.AuthTokenRepository 已实现）。
type TokenRevoker interface {
	RevokeTokensByUserID(ctx context.Context, userID string) error
}

// contactClient 同步所需的钉钉 API 最小接口（*Client 已实现），
// 便于单测注入 mock。
type contactClient interface {
	GetCorpAccessToken(ctx context.Context) (string, error)
	ListDepartmentTree(ctx context.Context) ([]SubDepartment, error)
	ListDeptUsers(ctx context.Context, deptID, cursor, size int64) (*DeptUserPage, error)
}

// SyncService 钉钉通讯录全量同步 + 离职自动禁用编排。
//
// 安全设计：
//   - 远端拉取任何一步失败 → 整轮 failed，绝不做禁用（避免网络抖动误伤）；
//   - 半量保护：远端人数低于本地活跃绑定的一半 → 判定远端数据异常，整批跳过禁用；
//   - SystemAdmin / CanAccessAllTenants 用户永不被同步禁用；
//   - "union:" 占位绑定（登录时未拿到企业 userid）不参与对账。
type SyncService struct {
	client       contactClient
	repo         *Repository
	userRepo     UserRepo
	tokenRevoker TokenRevoker
	corpID       string
}

// NewSyncService 构造 SyncService。corpID 为 identity 表的多企业隔离键。
func NewSyncService(client *Client, repo *Repository, userRepo UserRepo, tokenRevoker TokenRevoker, corpID string) *SyncService {
	return &SyncService{
		client:       client,
		repo:         repo,
		userRepo:     userRepo,
		tokenRevoker: tokenRevoker,
		corpID:       strings.TrimSpace(corpID),
	}
}

// RunFullSync 执行一次全量通讯录同步：
//
//  1. HasRunningSync 防并发；
//  2. 拉取远端全员（部门递归 + 部门成员分页，按 dingUserID 去重）；
//  3. 有绑定 → 更新资料快照；无绑定但 email 匹配本地账号 → 补绑定；否则跳过（不主动建号）；
//  4. 本地活跃绑定与远端求差集 → 疑似离职，带保护地禁用 + 撤销令牌；
//  5. 落终态计数。
//
// triggerType 取 SyncTriggerCron / SyncTriggerManual。
func (s *SyncService) RunFullSync(ctx context.Context, triggerType string) (*SyncRun, error) {
	// 总超时兜底；调用方已有更短的 deadline 时尊重之。
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, syncRunTimeout)
		defer cancel()
	}

	run, err := s.createSyncRun(ctx, triggerType)
	if err != nil {
		return nil, err
	}
	return s.executeSync(ctx, run)
}

// StartFullSync 异步触发全量同步：前奏（并发保护 + 建 run 记录）在调用方
// ctx 内同步完成，409/建记录失败的语义立即返回；执行体在脱离请求生命周期的
// goroutine 里跑。手动触发的 HTTP 请求 30s 超时/断开不应杀掉同步——生产曾
// 因此三轮 manual 全部 context canceled、快照始终为空。
func (s *SyncService) StartFullSync(ctx context.Context, triggerType string) (*SyncRun, error) {
	run, err := s.createSyncRun(ctx, triggerType)
	if err != nil {
		return nil, err
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		runCtx, cancel := context.WithTimeout(bg, syncRunTimeout)
		defer cancel()
		if _, err := s.executeSync(runCtx, run); err != nil {
			logger.Warnf(bg, "DingTalk sync: async run %d failed: %v", run.ID, err)
		}
	}()
	return run, nil
}

// createSyncRun 前奏：并发互斥（409）+ 僵尸 running 清理 + 建 run 记录。
func (s *SyncService) createSyncRun(ctx context.Context, triggerType string) (*SyncRun, error) {
	running, err := s.repo.HasRunningSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("check running sync: %w", err)
	}
	if running {
		return nil, ErrSyncAlreadyRunning
	}
	// 僵尸 running（进程重启中断的旧 run）顺手标记 failed，同步历史不留悬案。
	if err := s.repo.FailStaleRuns(ctx); err != nil {
		logger.Warnf(ctx, "DingTalk sync: fail stale runs: %v", err)
	}
	run, err := s.repo.CreateSyncRun(ctx, triggerType)
	if err != nil {
		return nil, fmt.Errorf("create sync run: %w", err)
	}
	return run, nil
}

// executeSync 执行体（run 已由 createSyncRun 建立）：拉取远端全员 → 部门树
// 与全员快照落库 → 资料对账 → 离职禁用 → 落终态计数。ctx 的超时由调用方保证。
func (s *SyncService) executeSync(ctx context.Context, run *SyncRun) (*SyncRun, error) {
	remote, deptTree, err := s.fetchRemoteUsers(ctx)
	if err != nil {
		// 拉取失败：整轮 failed，不做任何禁用。
		s.finishRun(ctx, run.ID, SyncStatusFailed, 0, 0, 0, 0, err.Error())
		run.Status = SyncStatusFailed
		run.ErrorMessage = err.Error()
		return run, err
	}

	// 部门树落库（组织架构本地化）。失败仅告警不 fail 整轮：
	// 对账禁用是关键路径，部门展示数据可等下一次同步补上。
	if err := s.repo.ReplaceDepartments(ctx, s.corpID, deptTree); err != nil {
		logger.Warnf(ctx, "DingTalk sync: persist departments failed: %v", err)
	}

	// 全员快照落库（dingtalk_contacts，含未绑定的人）——共享空间「从钉钉
	// 组织架构添加」的浏览/导入数据源。与部门树同理 warn-only；只有拉取
	// 成功后才走到这里，接口异常不会误清快照。
	if err := s.repo.ReplaceContacts(ctx, s.corpID, remote); err != nil {
		logger.Warnf(ctx, "DingTalk sync: persist contacts snapshot failed: %v", err)
	}

	created, updated := s.upsertRemoteUsers(ctx, remote)
	deactivated, reconcileErr := s.reconcileDeparted(ctx, remote)

	errMsg := ""
	if reconcileErr != nil {
		// 对账异常（含半量保护触发）不掩盖已完成的资料同步：
		// 记录 error_message，run 仍记 success。
		errMsg = reconcileErr.Error()
	}
	s.finishRun(ctx, run.ID, SyncStatusSuccess, len(remote), created, updated, deactivated, errMsg)

	now := time.Now()
	run.Status = SyncStatusSuccess
	run.FinishedAt = &now
	run.TotalRemote = len(remote)
	run.CreatedCount = created
	run.UpdatedCount = updated
	run.DeactivatedCount = deactivated
	run.ErrorMessage = errMsg
	return run, nil
}

// fetchRemoteUsers 拉取企业全员 + 部门树：token → 递归部门（带名称）→
// 逐部门分页成员，按 dingUserID 去重聚合。任何一步失败都返回 error
// （调用方整轮 failed）。部门树随返回值交给调用方落库。
func (s *SyncService) fetchRemoteUsers(ctx context.Context) (map[string]*DeptUserDetail, []SubDepartment, error) {
	if _, err := s.client.GetCorpAccessToken(ctx); err != nil {
		return nil, nil, fmt.Errorf("get corp access token: %w", err)
	}
	deptTree, err := s.client.ListDepartmentTree(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list department tree: %w", err)
	}
	// 根部门（1）不在树里（ListDepartmentTree 只返回子部门），但根部门
	// 直挂成员（未加入任何子部门的人）必须参与对账，与旧行为一致。
	deptIDs := make([]int64, 0, len(deptTree)+1)
	deptIDs = append(deptIDs, 1)
	for _, d := range deptTree {
		deptIDs = append(deptIDs, d.DeptID)
	}

	remote := make(map[string]*DeptUserDetail)
	for _, deptID := range deptIDs {
		var cursor int64
		for {
			page, err := s.client.ListDeptUsers(ctx, deptID, cursor, deptUserPageSize)
			if err != nil {
				return nil, nil, fmt.Errorf("list dept users (dept=%d): %w", deptID, err)
			}
			// 请求间隔：钉钉 user/list 有全平台 QPS 限流（subcode 90002），
			// 连续翻页容易触发。193 个部门多花约 29s，但把突发压到限额内。
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(150 * time.Millisecond):
			}
			for i := range page.List {
				u := page.List[i]
				if strings.TrimSpace(u.UserID) == "" {
					continue
				}
				if _, dup := remote[u.UserID]; !dup {
					remote[u.UserID] = &u
				}
			}
			if !page.HasMore {
				break
			}
			cursor = page.NextCursor
		}
	}
	return remote, deptTree, nil
}

// upsertRemoteUsers 第一遍对账：更新已有绑定的资料快照；无 userid 绑定
// 但 unionid 能对上占位记录 → 升级成真实 userid；否则 email 匹配本地
// 账号时补绑定（不主动为陌生人建账号）。
func (s *SyncService) upsertRemoteUsers(ctx context.Context, remote map[string]*DeptUserDetail) (created, updated int) {
	now := time.Now()
	for dingUserID, du := range remote {
		identity, err := s.repo.GetByDingUserID(ctx, s.corpID, dingUserID)
		switch {
		case err == nil:
			identity.Nick = du.Name
			identity.Mobile = du.Mobile
			identity.Email = du.Email
			identity.Title = du.Title
			identity.JobNumber = du.JobNumber
			identity.DeptIDList = Int64List(du.DeptIDList)
			identity.LastSyncedAt = &now
			if err := s.repo.Upsert(ctx, identity); err != nil {
				logger.Warnf(ctx, "DingTalk sync: update identity %s failed: %v", dingUserID, err)
				continue
			}
			s.maybeRenameLegacyUsername(ctx, identity, du)
			updated++
		case errors.Is(err, ErrIdentityNotFound):
			// user/list 的返回带 unionid：登录时因权限缺失只留下 "union:"
			// 占位记录的，这里直接升级成真实 userid，无需等本人再扫码。
			if s.repairPlaceholder(ctx, du, now) {
				updated++
				continue
			}
			if s.bindByEmail(ctx, du) {
				created++
			}
		default:
			logger.Warnf(ctx, "DingTalk sync: lookup identity %s failed: %v", dingUserID, err)
		}
	}
	return created, updated
}

// maybeRenameLegacyUsername 存量旧格式用户名改成规范格式 <姓名>-<工号>
// （如 汪利辉-0003）。历史两代格式（dt-<unionId>、dt-<工号><随机>）都以
// "dt-" 开头——这是识别「自动注册账号」的唯一标志。
//
// 改名规则：
//   - 用户名不以 "dt-" 开头（管理员手建后补绑 / 已是规范格式）→ 永不动；
//   - 已等于 ExpectedUsername(姓名, 工号) → 达标，不动；
//   - 姓名或工号缺失（没有规范名）→ 不动；
//   - 其余 dt- 旧格式 → 改成规范名；撞名追加 4 位随机，仍兜底时间戳。
//
// 代价：钉钉侧后续改名/改工号不会同步到用户名（规范格式无 dt- 前缀，
// 无法与手建账号安全区分），要跟随时手动改。改名失败仅 warn。
func (s *SyncService) maybeRenameLegacyUsername(ctx context.Context, identity *Identity, du *DeptUserDetail) {
	expected := ExpectedUsername(du.Name, du.JobNumber)
	if expected == "" || identity.UserID == "" || s.userRepo == nil {
		return
	}
	user, err := s.userRepo.GetUserByID(ctx, identity.UserID)
	if err != nil || user == nil || user.Username == "" {
		return
	}
	if !strings.HasPrefix(user.Username, usernameLegacyPrefix) || user.Username == expected {
		return
	}

	// 规范名查重（同名同工号重复录入时撞名）。repo 查不到返回
	// ErrUserNotFound 或 (nil, nil)，两种约定都视为「可用」。
	newName := expected
	for i := 0; i < 6; i++ {
		if existing, gerr := s.userRepo.GetUserByUsername(ctx, newName); gerr != nil || existing == nil {
			break
		}
		newName = expected + "-" + randomUsernameChars(usernameRandomSuffixLen)
	}
	if newName == expected+"-" || newName == "" {
		// crypto/rand 持续失败 / 持续撞名：时间戳兜底保证唯一。
		newName = fmt.Sprintf("%s-%d", expected, time.Now().UnixNano())
	}

	oldName := user.Username
	user.Username = newName
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		logger.Warnf(ctx, "DingTalk sync: rename user %s to %q failed: %v", user.ID, newName, err)
		return
	}
	logger.Infof(ctx, "DingTalk sync: renamed user %s username %q -> %q",
		user.ID, oldName, newName)
}

// repairPlaceholder 按 unionid 找到 "union:" 占位绑定并升级成真实 userid。
// 走主键 Update 而非 Upsert：占位 ding_user_id 被替换后 Upsert 的匹配键
// 会 miss，变成插新行 + 孤儿占位记录。
func (s *SyncService) repairPlaceholder(ctx context.Context, du *DeptUserDetail, now time.Time) bool {
	unionID := strings.TrimSpace(du.UnionID)
	if unionID == "" {
		return false
	}
	identity, err := s.repo.GetByUnionID(ctx, s.corpID, unionID)
	if err != nil {
		return false
	}
	identity.DingUserID = du.UserID
	identity.Nick = du.Name
	identity.Mobile = du.Mobile
	identity.Email = du.Email
	identity.Title = du.Title
	identity.JobNumber = du.JobNumber
	identity.DeptIDList = Int64List(du.DeptIDList)
	identity.LastSyncedAt = &now
	if err := s.repo.Update(ctx, identity); err != nil {
		logger.Warnf(ctx, "DingTalk sync: repair placeholder for union %s failed: %v", unionID, err)
		return false
	}
	logger.Infof(ctx, "DingTalk sync: upgraded placeholder binding (union %s) to userid %s", unionID, du.UserID)
	return true
}

// bindByEmail 按 email 匹配本地账号补绑定。匹配不到 / 查询失败均跳过（下个周期再试）。
// 本地账号已有绑定（如占位记录 unionid 与远端不一致）时改为原行覆盖，
// 避免触碰 user_id 唯一索引。
func (s *SyncService) bindByEmail(ctx context.Context, du *DeptUserDetail) bool {
	email := strings.TrimSpace(du.Email)
	if email == "" || s.userRepo == nil {
		return false
	}
	user, err := s.userRepo.GetUserByEmail(ctx, email)
	if err != nil || user == nil {
		return false
	}
	// 本地账号已有绑定行（unionid 与远端不一致的占位记录等）时覆盖原行，
	// 避免插入第二行触碰 user_id 唯一索引。
	if existing, gerr := s.repo.GetByUserID(ctx, user.ID); gerr == nil && existing != nil {
		now := time.Now()
		existing.UnionID = du.UnionID
		existing.DingUserID = du.UserID
		existing.Nick = du.Name
		existing.Mobile = du.Mobile
		existing.Email = email
		existing.LastSyncedAt = &now
		if err := s.repo.Update(ctx, existing); err != nil {
			logger.Warnf(ctx, "DingTalk sync: rebind user %s failed: %v", user.ID, err)
			return false
		}
		logger.Infof(ctx, "DingTalk sync: rebound local user %s to ding user %s", user.ID, du.UserID)
		return true
	}
	if err := s.repo.BindToUser(ctx, s.corpID, du.UserID, du.UnionID, user.ID, du.Name, du.Mobile, email); err != nil {
		logger.Warnf(ctx, "DingTalk sync: bind %s to user %s failed: %v", du.UserID, user.ID, err)
		return false
	}
	logger.Infof(ctx, "DingTalk sync: bound ding user %s to local user %s by email", du.UserID, user.ID)
	return true
}

// reconcileDeparted 第二遍对账：本地活跃绑定与远端求差集 = 疑似离职，逐个禁用。
// 返回 (禁用数, 对账异常)。半量保护触发 / ListActiveBindings 失败时返回 error，
// 由调用方记入 run.error_message。
func (s *SyncService) reconcileDeparted(ctx context.Context, remote map[string]*DeptUserDetail) (int, error) {
	bindings, err := s.repo.ListActiveBindings(ctx)
	if err != nil {
		return 0, fmt.Errorf("list active bindings: %w", err)
	}

	// 对账范围：仅真实 userid 的绑定；"union:" 占位记录无法与通讯录 userid
	// 对账，跳过（待其下次登录补齐真实 userid 后再参与）。
	reconcilable := make([]*ActiveBinding, 0, len(bindings))
	for _, b := range bindings {
		if strings.HasPrefix(b.DingUserID, unionIDPlaceholderPrefix) {
			continue
		}
		reconcilable = append(reconcilable, b)
	}
	if len(reconcilable) == 0 {
		return 0, nil
	}

	// 半量保护：远端人数低于本地活跃绑定的一半，判定远端数据异常
	// （如应用权限被收紧、部门被裁撤），整批跳过禁用以防误伤。
	if len(remote)*2 < len(reconcilable) {
		return 0, fmt.Errorf(
			"remote user count %d below 50%% of %d active bindings; skip deactivation as data anomaly",
			len(remote), len(reconcilable))
	}

	deactivated := 0
	for _, b := range reconcilable {
		if _, ok := remote[b.DingUserID]; ok {
			continue
		}
		// 保护：平台级特权账号永不被通讯录同步禁用。
		if b.UserIsSystemAdmin || b.UserAccessAll {
			logger.Infof(ctx,
				"DingTalk sync: skip deactivating protected user %s (system_admin=%v access_all=%v)",
				b.UserID, b.UserIsSystemAdmin, b.UserAccessAll)
			continue
		}
		if err := s.deactivateUser(ctx, b.UserID); err != nil {
			logger.Warnf(ctx, "DingTalk sync: deactivate user %s failed: %v", b.UserID, err)
			continue
		}
		deactivated++
	}
	return deactivated, nil
}

// deactivateUser 禁用本地账号并撤销其全部令牌。令牌撤销失败不掩盖禁用结果
// （禁用本身才是安全关键动作），记 warn 后仍视为已禁用。
func (s *SyncService) deactivateUser(ctx context.Context, userID string) error {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user %s: %w", userID, err)
	}
	if user == nil || !user.IsActive {
		return nil
	}
	user.IsActive = false
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return fmt.Errorf("update user %s: %w", userID, err)
	}
	if s.tokenRevoker != nil {
		if err := s.tokenRevoker.RevokeTokensByUserID(ctx, userID); err != nil {
			logger.Warnf(ctx, "DingTalk sync: revoke tokens for user %s failed: %v", userID, err)
		}
	}
	logger.Infof(ctx, "DingTalk sync: deactivated departed user %s", userID)
	return nil
}

// finishRun 落终态，失败仅记日志（不影响主流程返回值）。
// finishRun 落同步终态。写库用脱离取消的 ctx：这一轮很可能正是因为
// 超时或被取消才失败的，若沿用同一个 ctx，这条 failed 会跟着写不进去，
// 于是又留下一条永远 running 的僵尸记录。
func (s *SyncService) finishRun(ctx context.Context, runID uint64, status string, totalRemote, created, updated, deactivated int, errMsg string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishRunTimeout)
	defer cancel()
	if err := s.repo.FinishSyncRun(writeCtx, runID, status, totalRemote, created, updated, deactivated, errMsg); err != nil {
		logger.Errorf(ctx, "DingTalk sync: finish run %d failed: %v", runID, err)
	}
}
