-- Migration 000903 (sqlite): DingTalk 通讯录全员快照 —— 组织浏览/批量导入的数据源。
-- SQLite 方言：无 TIMESTAMPTZ/JSONB，用 DATETIME/TEXT。

CREATE TABLE IF NOT EXISTS dingtalk_contacts (
    corp_id        VARCHAR(128) NOT NULL DEFAULT '',
    ding_user_id   VARCHAR(128) NOT NULL,
    union_id       VARCHAR(128) NOT NULL DEFAULT '',
    name           VARCHAR(255) NOT NULL DEFAULT '',
    mobile         VARCHAR(64)  NOT NULL DEFAULT '',
    email          VARCHAR(255) NOT NULL DEFAULT '',
    title          VARCHAR(255) NOT NULL DEFAULT '',
    job_number     VARCHAR(64)  NOT NULL DEFAULT '',
    dept_id_list   TEXT         NOT NULL DEFAULT '[]',
    last_synced_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (corp_id, ding_user_id)
);
