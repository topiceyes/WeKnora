package dingtalk

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Identity 钉钉身份绑定表（user_dingtalk_identities）的 GORM model。
// 一条记录 = 一个本地账号绑定一个钉钉身份。「用户是否钉钉来源」由此表有无记录判断。
type Identity struct {
	ID              uint64          `gorm:"primaryKey"                       json:"id"`
	UserID          string          `gorm:"column:user_id;size:36;index"     json:"user_id"`
	UnionID         string          `gorm:"column:union_id;size:128"         json:"union_id"`
	DingUserID      string          `gorm:"column:ding_user_id;size:128"     json:"ding_user_id"`
	CorpID          string          `gorm:"column:corp_id;size:128"          json:"corp_id"`
	Nick            string          `gorm:"column:nick;size:255"             json:"nick"`
	Mobile          string          `gorm:"column:mobile;size:64"            json:"mobile"`
	Email           string          `gorm:"column:email;size:255"            json:"email"`
	DeptIDList      Int64List       `gorm:"column:dept_id_list;type:jsonb"   json:"dept_id_list"`
	JobNumber       string          `gorm:"column:job_number;size:64"        json:"job_number"`
	Title           string          `gorm:"column:title;size:255"            json:"title"`
	UserAccessToken string          `gorm:"column:user_access_token;type:text" json:"-"` // 加密存储，不序列化
	TokenExpiresAt  *time.Time      `gorm:"column:token_expires_at"          json:"token_expires_at,omitempty"`
	LastLoginAt     *time.Time      `gorm:"column:last_login_at"             json:"last_login_at,omitempty"`
	LastSyncedAt    *time.Time      `gorm:"column:last_synced_at"            json:"last_synced_at,omitempty"`
	CreatedAt       time.Time       `gorm:"column:created_at"                json:"created_at"`
	UpdatedAt       time.Time       `gorm:"column:updated_at"                json:"updated_at"`
	DeletedAt       gorm.DeletedAt  `gorm:"column:deleted_at;index"          json:"-"`
}

// TableName 绑定表名。
func (Identity) TableName() string { return "user_dingtalk_identities" }

// SyncRun 通讯录同步运行记录（dingtalk_sync_runs）。
type SyncRun struct {
	ID              uint64     `gorm:"primaryKey"                json:"id"`
	TriggerType     string     `gorm:"column:trigger_type;size:20" json:"trigger_type"` // cron | manual
	Status          string     `gorm:"column:status;size:20"       json:"status"`       // running | success | failed
	StartedAt       time.Time  `gorm:"column:started_at"           json:"started_at"`
	FinishedAt      *time.Time `gorm:"column:finished_at"          json:"finished_at,omitempty"`
	TotalRemote     int        `gorm:"column:total_remote"         json:"total_remote"`
	CreatedCount    int        `gorm:"column:created_count"        json:"created_count"`
	UpdatedCount    int        `gorm:"column:updated_count"        json:"updated_count"`
	DeactivatedCount int       `gorm:"column:deactivated_count"    json:"deactivated_count"`
	ErrorMessage    string     `gorm:"column:error_message;type:text" json:"error_message"`
	CreatedAt       time.Time  `gorm:"column:created_at"           json:"created_at"`
}

// TableName 同步记录表名。
func (SyncRun) TableName() string { return "dingtalk_sync_runs" }

// Department 钉钉部门表（dingtalk_departments）的 GORM model。
// 每日通讯录同步时全量替换；查询端点只读本地，不实时调钉钉。
// 根部门（dept_id=1）不落库，由前端虚拟「全公司」根节点表示。
type Department struct {
	DeptID       int64     `gorm:"primaryKey"                     json:"dept_id"`
	CorpID       string    `gorm:"column:corp_id;size:128"        json:"corp_id"`
	Name         string    `gorm:"column:name;size:255"           json:"name"`
	ParentID     int64     `gorm:"column:parent_id"               json:"parent_id"`
	LastSyncedAt time.Time `gorm:"column:last_synced_at"          json:"last_synced_at"`
	CreatedAt    time.Time `gorm:"column:created_at"              json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"              json:"updated_at"`
}

// TableName 部门表名。
func (Department) TableName() string { return "dingtalk_departments" }

// SyncRun 状态常量。
const (
	SyncTriggerCron   = "cron"
	SyncTriggerManual = "manual"

	SyncStatusRunning = "running"
	SyncStatusSuccess = "success"
	SyncStatusFailed  = "failed"
)

// Int64List JSONB 部门 id 列表，实现 driver.Valuer / sql.Scanner 兼容 PG 与 SQLite。
type Int64List []int64

// Value 实现 driver.Valuer。
func (l Int64List) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("marshal dept_id_list: %w", err)
	}
	return string(b), nil
}

// Scan 实现 sql.Scanner。
func (l *Int64List) Scan(src interface{}) error {
	if src == nil {
		*l = []int64{}
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("unsupported dept_id_list type %T", src)
	}
	if len(raw) == 0 {
		*l = []int64{}
		return nil
	}
	return json.Unmarshal(raw, l)
}

// DingTalkUserProfile 扫码登录解析出的钉钉侧用户资料（登录编排的产物）。
type DingTalkUserProfile struct {
	UnionID    string
	DingUserID string // 企业内部 userid（可能为空，若 getbyunionid 失败）
	CorpID     string
	Nick       string
	Mobile     string
	Email      string
}
