-- Migration 000034 (sqlite): DingTalk integration — 扫码登录身份绑定 + 通讯录同步记录。
-- SQLite 方言：无 BIGSERIAL/JSONB/TIMESTAMPTZ，主键用 INTEGER，jsonb 用 TEXT。

CREATE TABLE IF NOT EXISTS user_dingtalk_identities (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          VARCHAR(36)  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    union_id         VARCHAR(128) NOT NULL DEFAULT '',
    ding_user_id     VARCHAR(128) NOT NULL,
    corp_id          VARCHAR(128) NOT NULL DEFAULT '',
    nick             VARCHAR(255) NOT NULL DEFAULT '',
    mobile           VARCHAR(64)  NOT NULL DEFAULT '',
    email            VARCHAR(255) NOT NULL DEFAULT '',
    dept_id_list     TEXT         NOT NULL DEFAULT '[]',
    job_number       VARCHAR(64)  NOT NULL DEFAULT '',
    title            VARCHAR(255) NOT NULL DEFAULT '',
    user_access_token TEXT        NOT NULL DEFAULT '',
    token_expires_at DATETIME,
    last_login_at    DATETIME,
    last_synced_at   DATETIME,
    created_at       DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at       DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_dingtalk_identity_union_corp
    ON user_dingtalk_identities (union_id, corp_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_dingtalk_identity_dinguser_corp
    ON user_dingtalk_identities (ding_user_id, corp_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_dingtalk_identity_user
    ON user_dingtalk_identities (user_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS dingtalk_sync_runs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    trigger_type      VARCHAR(20) NOT NULL,
    status            VARCHAR(20) NOT NULL,
    started_at        DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at       DATETIME,
    total_remote      INTEGER     NOT NULL DEFAULT 0,
    created_count     INTEGER     NOT NULL DEFAULT 0,
    updated_count     INTEGER     NOT NULL DEFAULT 0,
    deactivated_count INTEGER     NOT NULL DEFAULT 0,
    error_message     TEXT        NOT NULL DEFAULT '',
    created_at        DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_dingtalk_sync_runs_started ON dingtalk_sync_runs (started_at DESC);
