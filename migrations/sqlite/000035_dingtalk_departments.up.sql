-- Migration 000035 (sqlite): DingTalk 部门表 —— 组织架构本地化存储。
-- SQLite 方言：无 TIMESTAMPTZ，用 DATETIME。

CREATE TABLE IF NOT EXISTS dingtalk_departments (
    dept_id        INTEGER      PRIMARY KEY,
    corp_id        VARCHAR(128) NOT NULL DEFAULT '',
    name           VARCHAR(255) NOT NULL DEFAULT '',
    parent_id      INTEGER      NOT NULL DEFAULT 1,
    last_synced_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_dingtalk_departments_corp_parent
    ON dingtalk_departments (corp_id, parent_id);
