-- Migration 000123: DingTalk 部门表 —— 组织架构本地化存储。
-- 每日通讯录同步时全量替换（见 dingtalk.Repository.ReplaceDepartments），
-- 查询端点 GET /system/dingtalk/departments 只读本地，不再实时调钉钉。
-- 根部门（dept_id=1，钉钉默认公司节点）不落库，前端虚拟「全公司」根。

CREATE TABLE IF NOT EXISTS dingtalk_departments (
    dept_id        BIGINT       PRIMARY KEY,                 -- 钉钉部门 id（企业内唯一）
    corp_id        VARCHAR(128) NOT NULL DEFAULT '',         -- 企业 id，与 identity 表一致
    name           VARCHAR(255) NOT NULL DEFAULT '',
    parent_id      BIGINT       NOT NULL DEFAULT 1,          -- 父部门 id（顶层指向根 1）
    last_synced_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_dingtalk_departments_corp_parent
    ON dingtalk_departments (corp_id, parent_id);
