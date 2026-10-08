package dingtalk

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrIdentityNotFound 绑定记录不存在。
var ErrIdentityNotFound = errors.New("dingtalk identity not found")

// Repository 钉钉身份绑定表与同步记录表的数据访问层。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造 Repository。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ---------------------------------------------------------------------------
// Identity 绑定表
// ---------------------------------------------------------------------------

// GetByUnionID 按 union_id + corp_id 查绑定。
func (r *Repository) GetByUnionID(ctx context.Context, corpID, unionID string) (*Identity, error) {
	var id Identity
	err := r.db.WithContext(ctx).
		Where("corp_id = ? AND union_id = ?", corpID, unionID).
		First(&id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrIdentityNotFound
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// GetByDingUserID 按 ding_user_id + corp_id 查绑定（对账主键）。
func (r *Repository) GetByDingUserID(ctx context.Context, corpID, dingUserID string) (*Identity, error) {
	var id Identity
	err := r.db.WithContext(ctx).
		Where("corp_id = ? AND ding_user_id = ?", corpID, dingUserID).
		First(&id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrIdentityNotFound
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// GetByUserID 按本地 user_id 查绑定。
func (r *Repository) GetByUserID(ctx context.Context, userID string) (*Identity, error) {
	var id Identity
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrIdentityNotFound
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// Upsert 按 (ding_user_id, corp_id) 唯一键写入或更新绑定。
func (r *Repository) Upsert(ctx context.Context, id *Identity) error {
	var existing Identity
	err := r.db.WithContext(ctx).
		Where("corp_id = ? AND ding_user_id = ?", id.CorpID, id.DingUserID).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(id).Error
	}
	if err != nil {
		return err
	}
	id.ID = existing.ID
	id.CreatedAt = existing.CreatedAt
	return r.db.WithContext(ctx).Save(id).Error
}

// Update 按主键保存整行。用于升级 "union:" 占位记录：占位 ding_user_id
// 即将被替换成真实 userid，Upsert 的 (ding_user_id, corp_id) 匹配键会
// miss 并误插新行，所以这里必须走主键。
func (r *Repository) Update(ctx context.Context, id *Identity) error {
	return r.db.WithContext(ctx).Save(id).Error
}

// BindToUser 把 ding_user_id 绑定到指定本地账号（幂等，同时更新 profile 快照）。
func (r *Repository) BindToUser(ctx context.Context, corpID, dingUserID, unionID, localUserID, nick, mobile, email string) error {
	now := time.Now()
	id := &Identity{
		UserID:      localUserID,
		UnionID:     unionID,
		DingUserID:  dingUserID,
		CorpID:      corpID,
		Nick:        nick,
		Mobile:      mobile,
		Email:       email,
		LastLoginAt: &now,
	}
	return r.Upsert(ctx, id)
}

// TouchLogin 更新最近登录时间。
func (r *Repository) TouchLogin(ctx context.Context, corpID, dingUserID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&Identity{}).
		Where("corp_id = ? AND ding_user_id = ?", corpID, dingUserID).
		Update("last_login_at", now).Error
}

// ActiveBinding 对账用的本地钉钉来源用户投影（绑定 + 本地账号激活态）。
type ActiveBinding struct {
	Identity
	UserIsActive       bool `gorm:"column:user_is_active"`
	UserIsSystemAdmin  bool `gorm:"column:user_is_system_admin"`
	UserAccessAll      bool `gorm:"column:user_can_access_all_tenants"`
}

// ListActiveBindings 返回所有已绑定且本地账号仍激活的记录（join users）。
func (r *Repository) ListActiveBindings(ctx context.Context) ([]*ActiveBinding, error) {
	var out []*ActiveBinding
	err := r.db.WithContext(ctx).
		Table("user_dingtalk_identities AS i").
		Select("i.*, u.is_active AS user_is_active, u.is_system_admin AS user_is_system_admin, u.can_access_all_tenants AS user_can_access_all_tenants").
		Joins("JOIN users u ON u.id = i.user_id").
		Where("i.deleted_at IS NULL AND u.deleted_at IS NULL").
		Scan(&out).Error
	return out, err
}

// ---------------------------------------------------------------------------
// Department 部门表
// ---------------------------------------------------------------------------

// ReplaceDepartments 全量替换某企业的部门树（事务内 delete + insert）。
// 部门量级很小（几十到几百行），整批替换最简单也最正确：钉钉侧删除/改名
// 的部门在下一次同步后自然消失或更新，无需 diff。落库时间戳统一为本次
// 同步时间，供前端展示「最近同步」。
func (r *Repository) ReplaceDepartments(ctx context.Context, corpID string, depts []SubDepartment) error {
	if len(depts) == 0 {
		// 空树也执行清空：钉钉侧确实可能只剩根部门。但为防接口异常返回
		// 空集导致误清，调用方（sync）负责在拉取成功后才调到这里。
		return r.db.WithContext(ctx).
			Where("corp_id = ?", corpID).
			Delete(&Department{}).Error
	}
	now := time.Now()
	rows := make([]Department, 0, len(depts))
	for _, d := range depts {
		rows = append(rows, Department{
			DeptID:       d.DeptID,
			CorpID:       corpID,
			Name:         d.Name,
			ParentID:     d.ParentID,
			LastSyncedAt: now,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("corp_id = ?", corpID).Delete(&Department{}).Error; err != nil {
			return err
		}
		return tx.Create(&rows).Error
	})
}

// ListDepartments 平铺返回部门列表（含各自 last_synced_at）。
// 不过滤 corp_id：单企业部署，同步写入的企业即全部数据。
func (r *Repository) ListDepartments(ctx context.Context) ([]Department, error) {
	var out []Department
	err := r.db.WithContext(ctx).
		Order("parent_id ASC, dept_id ASC").
		Find(&out).Error
	return out, err
}

// ---------------------------------------------------------------------------
// SyncRun 同步记录
// ---------------------------------------------------------------------------

// CreateSyncRun 创建一条 running 状态的同步记录。
func (r *Repository) CreateSyncRun(ctx context.Context, triggerType string) (*SyncRun, error) {
	run := &SyncRun{
		TriggerType: triggerType,
		Status:      SyncStatusRunning,
		StartedAt:   time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}
	return run, nil
}

// FinishSyncRun 落终态（success/failed + 计数 + 错误）。
func (r *Repository) FinishSyncRun(ctx context.Context, runID uint64, status string, totalRemote, created, updated, deactivated int, errMsg string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&SyncRun{}).
		Where("id = ?", runID).
		Updates(map[string]interface{}{
			"status":            status,
			"finished_at":       now,
			"total_remote":      totalRemote,
			"created_count":     created,
			"updated_count":     updated,
			"deactivated_count": deactivated,
			"error_message":     errMsg,
		}).Error
}

// syncRunStaleTimeout running 状态超过该时长视为僵尸（进程重启中断）。
// 全量同步 326 人实测约 1 分钟，1 小时余量充足。
const syncRunStaleTimeout = time.Hour

// HasRunningSync 是否有进行中的同步（防 cron 与手动并发）。
// 僵尸 running（进程在同步中途被杀，run 永远停在 running）不算，
// 否则一次进程重启就会永久阻塞后续同步。
func (r *Repository) HasRunningSync(ctx context.Context) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&SyncRun{}).
		Where("status = ? AND started_at > ?", SyncStatusRunning, time.Now().Add(-syncRunStaleTimeout)).
		Count(&count).Error
	return count > 0, err
}

// FailStaleRuns 把僵尸 running 记录标记为 failed（带中断说明），
// 让同步历史里不会永远挂着一条 running。幂等，可随新同步顺手调用。
func (r *Repository) FailStaleRuns(ctx context.Context) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&SyncRun{}).
		Where("status = ? AND started_at <= ?", SyncStatusRunning, now.Add(-syncRunStaleTimeout)).
		Updates(map[string]interface{}{
			"status":        SyncStatusFailed,
			"finished_at":   now,
			"error_message": "interrupted: process restarted mid-sync",
		}).Error
}

// ListSyncRuns 分页返回同步历史（按开始时间倒序）。
func (r *Repository) ListSyncRuns(ctx context.Context, offset, limit int) ([]*SyncRun, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&SyncRun{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var runs []*SyncRun
	err := r.db.WithContext(ctx).
		Order("started_at DESC").
		Offset(offset).Limit(limit).
		Find(&runs).Error
	return runs, total, err
}

// ListIdentities 分页返回绑定关系（join users 取用户名/邮箱/激活态）。
type IdentityWithUser struct {
	Identity
	Username string `gorm:"column:username" json:"username"`
	UserEmail string `gorm:"column:user_email" json:"user_email"`
	IsActive bool   `gorm:"column:is_active" json:"is_active"`
}

// ListIdentities 绑定关系分页列表，keyword 匹配 nick/mobile/email/username。
func (r *Repository) ListIdentities(ctx context.Context, offset, limit int, keyword string) ([]*IdentityWithUser, int64, error) {
	base := r.db.WithContext(ctx).
		Table("user_dingtalk_identities AS i").
		Joins("JOIN users u ON u.id = i.user_id").
		Where("i.deleted_at IS NULL AND u.deleted_at IS NULL")
	if keyword != "" {
		like := "%" + keyword + "%"
		base = base.Where("i.nick LIKE ? OR i.mobile LIKE ? OR i.email LIKE ? OR u.username LIKE ?",
			like, like, like, like)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []*IdentityWithUser
	err := base.
		Select("i.*, u.username AS username, u.email AS user_email, u.is_active AS is_active").
		Order("i.updated_at DESC").
		Offset(offset).Limit(limit).
		Scan(&out).Error
	return out, total, err
}
